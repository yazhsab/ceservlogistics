package commission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Calculation statuses.
const (
	StatusCalculated = "CALCULATED"
	StatusPosted     = "POSTED"
	StatusReversed   = "REVERSED"
	StatusCancelled  = "CANCELLED"
)

// Service resolves rules, calculates commission and posts it to the ledger.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	ledger  *ledger.Service
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics
}

func NewService(
	db *database.DB, q *dbgen.Queries, led *ledger.Service,
	rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics) *Service {
	return &Service{db: db, q: q, ledger: led, audit: rec, log: log, metrics: m}
}

// Facts are the shipment and network attributes a rule is matched against.
//
// They are gathered once and passed around, so the rule that a simulation
// selects is provably the rule a real calculation would select: same inputs,
// same query, same order.
type Facts struct {
	CommissionType string

	FranchiseID       *int64
	FranchiseCategory string
	OperatingUnitID   *int64
	ServiceID         *int64
	OriginZoneID      *int64
	DestinationZoneID *int64
	CustomerCategory  string
	PaymentMode       string

	SchemeID *int64
}

// Resolution records which rule won and what else was in the running.
//
// Candidates are returned so the simulation endpoint can explain a decision. A
// franchise that can see why a rule applied argues about the policy; one that
// cannot argues about the number.
type Resolution struct {
	Rule       dbgen.ResolveCommissionRuleRow
	Version    dbgen.CommissionRuleVersion
	Candidates []dbgen.ResolveCommissionRuleRow
}

// ResolveRule picks the rule and version that govern an event on a date.
func (s *Service) ResolveRule(
	ctx context.Context, q *dbgen.Queries, orgID int64, f Facts, on time.Time,
) (*Resolution, error) {
	candidates, err := q.ResolveCommissionRule(ctx, dbgen.ResolveCommissionRuleParams{
		OrganizationID:    orgID,
		CommissionType:    f.CommissionType,
		SchemeID:          f.SchemeID,
		FranchiseID:       f.FranchiseID,
		FranchiseCategory: ops.Optional(f.FranchiseCategory),
		OperatingUnitID:   f.OperatingUnitID,
		ServiceID:         f.ServiceID,
		OriginZoneID:      f.OriginZoneID,
		DestinationZoneID: f.DestinationZoneID,
		CustomerCategory:  ops.Optional(f.CustomerCategory),
		PaymentMode:       ops.Optional(f.PaymentMode),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if len(candidates) == 0 {
		return nil, apierr.Conflict(CodeNoRule,
			fmt.Sprintf("No active %s commission rule matches this shipment.", f.CommissionType)).
			WithDetail("commissionType", f.CommissionType)
	}

	// The query's ORDER BY is the precedence: specificity, then priority, then
	// id. Taking the head is the whole decision.
	winner := candidates[0]

	version, err := q.GetEffectiveRuleVersion(ctx, dbgen.GetEffectiveRuleVersionParams{
		RuleID: winner.ID, AsOf: on,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeNoVersion,
				fmt.Sprintf("Rule %s has no version in force on %s.",
					winner.Code, on.Format("2006-01-02"))).
				WithDetail("ruleCode", winner.Code).
				WithDetail("asOf", on.Format("2006-01-02"))
		}
		return nil, apierr.Internal(err)
	}

	return &Resolution{Rule: winner, Version: version, Candidates: candidates}, nil
}

// ruleFromVersion converts a stored version into the calculator's pure form.
func ruleFromVersion(v dbgen.CommissionRuleVersion) (Rule, error) {
	r := Rule{
		Method:           v.CalculationMethod,
		Currency:         v.Currency,
		FixedAmountMinor: v.FixedAmountMinor,
		RateBp:           v.RateBp,
		MinAmountMinor:   v.MinAmountMinor,
		MaxAmountMinor:   v.MaxAmountMinor,
	}
	if v.Basis != nil {
		r.BasisName = *v.Basis
	}
	if len(v.Slabs) > 0 {
		slabs, err := DecodeSlabs(v.Slabs)
		if err != nil {
			return Rule{}, err
		}
		r.Slabs = slabs
	}
	return r, nil
}

// SimulateRequest asks "what would this pay?" without writing anything.
type SimulateRequest struct {
	Facts Facts
	Basis Basis
	On    time.Time
}

// SimulateResult explains a hypothetical calculation.
type SimulateResult struct {
	Matched     bool            `json:"matched"`
	RuleCode    string          `json:"ruleCode,omitempty"`
	RuleName    string          `json:"ruleName,omitempty"`
	VersionNo   int32           `json:"versionNo,omitempty"`
	Outcome     *Outcome        `json:"calculation,omitempty"`
	Candidates  []CandidateView `json:"candidates"`
	Explanation string          `json:"explanation"`
}

// CandidateView shows a rule that was considered and why it did or did not win.
type CandidateView struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Specificity int32  `json:"specificity"`
	Priority    int32  `json:"priority"`
	Selected    bool   `json:"selected"`
}

// Simulate runs the real resolution and the real calculator against supplied
// numbers, writing nothing.
//
// It shares ResolveRule and Calculate with the posting path rather than
// reimplementing them, which is what makes the preview trustworthy.
func (s *Service) Simulate(
	ctx context.Context, p *tenant.Principal, in SimulateRequest,
) (*SimulateResult, error) {
	if in.On.IsZero() {
		in.On = time.Now().UTC()
	}
	res, err := s.ResolveRule(ctx, s.q, p.OrganizationID, in.Facts, in.On)
	if err != nil {
		var apiErr *apierr.Error
		if ok := asAPIError(err, &apiErr); ok &&
			(apiErr.Code == CodeNoRule || apiErr.Code == CodeNoVersion) {
			return &SimulateResult{
				Matched:     false,
				Candidates:  []CandidateView{},
				Explanation: apiErr.Message,
			}, nil
		}
		return nil, err
	}

	rule, err := ruleFromVersion(res.Version)
	if err != nil {
		return nil, err
	}
	outcome, err := Calculate(rule, in.Basis)
	if err != nil {
		return nil, err
	}

	candidates := make([]CandidateView, 0, len(res.Candidates))
	for i, c := range res.Candidates {
		candidates = append(candidates, CandidateView{
			Code: c.Code, Name: c.Name,
			Specificity: c.Specificity, Priority: c.Priority,
			Selected: i == 0,
		})
	}

	return &SimulateResult{
		Matched: true, RuleCode: res.Rule.Code, RuleName: res.Rule.Name,
		VersionNo: res.Version.VersionNo, Outcome: outcome, Candidates: candidates,
		Explanation: fmt.Sprintf(
			"Rule %s version %d was selected from %d matching rule(s) on specificity %d.",
			res.Rule.Code, res.Version.VersionNo, len(res.Candidates), res.Rule.Specificity),
	}, nil
}

// CalculateRequest asks for a real, persisted calculation.
type CalculateRequest struct {
	Facts Facts
	Basis Basis

	ShipmentID        *int64
	QualifyingEvent   string
	QualifyingEventID *int64
	QualifiedAt       time.Time

	RecipientType string
	RecipientID   int64
	FranchiseID   *int64
	UnitID        *int64

	// PostImmediately writes the ledger entries in the same transaction.
	// Commission earned on a delivery is posted at once; a volume incentive
	// may be calculated and reviewed before posting.
	PostImmediately bool
}

// Calculate resolves, computes and persists a commission, optionally posting it.
//
// It is idempotent on (shipment, type, recipient): a retried delivery hook
// returns the existing calculation rather than paying twice. The partial unique
// index is the guarantee; the lookup here just makes the retry clean.
func (s *Service) Calculate(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, in CalculateRequest,
) (*dbgen.CommissionCalculation, bool, error) {
	q := s.q.WithTx(tx)

	if in.QualifiedAt.IsZero() {
		in.QualifiedAt = time.Now().UTC()
	}
	if in.RecipientType == "" || in.RecipientID == 0 {
		return nil, false, apierr.Conflict(CodeNoRecipient,
			"This event has no commission recipient, so nothing can be earned.")
	}

	// Idempotency probe.
	if in.ShipmentID != nil {
		existing, err := q.FindCalculationForShipment(ctx, dbgen.FindCalculationForShipmentParams{
			OrganizationID: p.OrganizationID,
			ShipmentID:     in.ShipmentID,
			CommissionType: in.Facts.CommissionType,
			RecipientType:  in.RecipientType,
			RecipientID:    in.RecipientID,
		})
		if err == nil {
			return &existing, true, nil
		}
		if !ops.IsNoRows(err) {
			return nil, false, apierr.Internal(err)
		}
	}

	res, err := s.ResolveRule(ctx, q, p.OrganizationID, in.Facts, in.QualifiedAt)
	if err != nil {
		return nil, false, err
	}
	rule, err := ruleFromVersion(res.Version)
	if err != nil {
		return nil, false, err
	}
	outcome, err := Calculate(rule, in.Basis)
	if err != nil {
		return nil, false, err
	}

	inputs, err := json.Marshal(map[string]any{
		"basis": in.Basis,
		"facts": map[string]any{
			"commissionType":    in.Facts.CommissionType,
			"franchiseCategory": in.Facts.FranchiseCategory,
			"customerCategory":  in.Facts.CustomerCategory,
			"paymentMode":       in.Facts.PaymentMode,
		},
		"ruleSpecificity": res.Rule.Specificity,
		"candidateCount":  len(res.Candidates),
	})
	if err != nil {
		return nil, false, apierr.Internal(err)
	}
	trace, err := json.Marshal(outcome.Steps)
	if err != nil {
		return nil, false, apierr.Internal(err)
	}

	calc, err := q.CreateCommissionCalculation(ctx, dbgen.CreateCommissionCalculationParams{
		PublicID:          publicid.New("ccl"),
		OrganizationID:    p.OrganizationID,
		CommissionType:    in.Facts.CommissionType,
		ShipmentID:        in.ShipmentID,
		QualifyingEvent:   in.QualifyingEvent,
		QualifyingEventID: in.QualifyingEventID,
		QualifiedAt:       in.QualifiedAt,
		RecipientType:     in.RecipientType,
		RecipientID:       in.RecipientID,
		FranchiseID:       in.FranchiseID,
		OperatingUnitID:   in.UnitID,
		RuleID:            res.Rule.ID,
		RuleVersionID:     res.Version.ID,
		RuleCode:          res.Rule.Code,
		RuleVersionNo:     res.Version.VersionNo,
		CalculationMethod: outcome.Method,
		Basis:             ops.Optional(outcome.BasisName),
		BaseAmountMinor:   outcome.BasisValue,
		RateBp:            outcome.RateBp,
		GrossAmountMinor:  outcome.GrossMinor,
		AmountMinor:       outcome.AmountMinor,
		Currency:          outcome.Currency,
		CalculationInputs: inputs,
		CalculationTrace:  trace,
		Status:            StatusCalculated,
		CreatedBy:         &p.UserID,
		RequestID:         ops.Optional(httpx.RequestID(ctx)),
	})
	if err != nil {
		// Lost a race with an identical concurrent calculation.
		if ops.IsUnique(err, "commission_calculations_shipment_idx") && in.ShipmentID != nil {
			if existing, fErr := q.FindCalculationForShipment(ctx, dbgen.FindCalculationForShipmentParams{
				OrganizationID: p.OrganizationID, ShipmentID: in.ShipmentID,
				CommissionType: in.Facts.CommissionType,
				RecipientType:  in.RecipientType, RecipientID: in.RecipientID,
			}); fErr == nil {
				return &existing, true, nil
			}
		}
		return nil, false, apierr.Internal(err)
	}

	if in.PostImmediately {
		posted, pErr := s.post(ctx, tx, p, calc)
		if pErr != nil {
			return nil, false, pErr
		}
		calc = *posted
	}
	return &calc, false, nil
}

// post writes the ledger entries for a calculated commission.
//
//	DR Commission Expense    the cost to head office
//	CR Commission Payable    earned but not yet settled
//
// The credit goes to the *accrual* account (2100), not to the franchise's own
// payable (2200). That separation is what the chart of accounts describes —
// 2100 is "earned and not yet settled", 2200 is "the net owed to this
// franchise" — and it is what makes settlement meaningful: approving a
// statement moves the accrual to the franchise's account, and only then does
// the franchise's own balance reflect an agreed, payable amount.
//
// Crediting 2200 here instead would credit the franchise twice: once when the
// commission was earned and again when the settlement that contains it was
// approved.
func (s *Service) post(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, calc dbgen.CommissionCalculation,
) (*dbgen.CommissionCalculation, error) {
	q := s.q.WithTx(tx)

	if calc.Status != StatusCalculated {
		return nil, apierr.Conflict(CodeAlreadyPosted,
			fmt.Sprintf("Commission %s is %s and cannot be posted again.", calc.PublicID, calc.Status))
	}
	// A zero commission is a legitimate outcome but has nothing to post, and a
	// zero-value journal would fail the ledger's own validation.
	if calc.AmountMinor == 0 {
		return &calc, nil
	}

	expenseAccount := ledger.AcctCommissionExpense
	if calc.CommissionType == TypeVolumeIncentive {
		expenseAccount = ledger.AcctIncentiveExpense
	}

	result, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
		SourceType:     ledger.SourceCommission,
		SourceID:       &calc.ID,
		SourcePublicID: calc.PublicID,
		Purpose:        calc.CommissionType,
		Description: fmt.Sprintf("%s commission, rule %s v%d",
			calc.CommissionType, calc.RuleCode, calc.RuleVersionNo),
		Currency:    calc.Currency,
		PostingDate: calc.QualifiedAt,
		Legs: []ledger.Leg{
			{
				AccountCode: expenseAccount, Debit: calc.AmountMinor,
				FranchiseID: calc.FranchiseID, ShipmentID: calc.ShipmentID,
				Memo: "Commission earned by the network",
			},
			{
				// Accrual, not a party balance. The franchise appears as a
				// reporting dimension so the accrual can be broken down by
				// counterparty, but the amount is not yet owed to them as a
				// settled figure.
				AccountCode: ledger.AcctCommissionPayable, Credit: calc.AmountMinor,
				FranchiseID: calc.FranchiseID, ShipmentID: calc.ShipmentID,
				Memo: fmt.Sprintf("Accrued %s commission", calc.CommissionType),
			},
		},
		Metadata: map[string]any{
			"ruleCode": calc.RuleCode, "ruleVersion": calc.RuleVersionNo,
			"calculationId": calc.PublicID,
		},
	})
	if err != nil {
		return nil, err
	}

	updated, err := q.MarkCalculationPosted(ctx, dbgen.MarkCalculationPostedParams{
		OrganizationID: p.OrganizationID, ID: calc.ID,
		JournalTransactionID: &result.Transaction.ID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeAlreadyPosted,
				"This commission was posted by another request.")
		}
		return nil, apierr.Internal(err)
	}

	if _, err := q.CreateCommissionEntry(ctx, dbgen.CreateCommissionEntryParams{
		PublicID:        publicid.New("cen"),
		OrganizationID:  p.OrganizationID,
		CalculationID:   calc.ID,
		FranchiseID:     calc.FranchiseID,
		OperatingUnitID: calc.OperatingUnitID,
		EntryType:       "EARNED",
		CommissionType:  calc.CommissionType,
		AmountMinor:     calc.AmountMinor,
		Currency:        calc.Currency,
		ShipmentID:      calc.ShipmentID,
		EarnedOn:        calc.QualifiedAt,
		Memo:            ops.Optional(fmt.Sprintf("Rule %s v%d", calc.RuleCode, calc.RuleVersionNo)),
	}); err != nil {
		return nil, apierr.Internal(err)
	}

	return &updated, nil
}

// Post posts a previously calculated commission.
func (s *Service) Post(
	ctx context.Context, p *tenant.Principal, publicID string,
) (result *dbgen.CommissionCalculation, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("commission_posting", "failed")
			return
		}
		s.metrics.RecordBusiness("commission_posting", "posted")
	}()

	var out *dbgen.CommissionCalculation
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		calc, err := q.GetCalculationByPublicID(ctx, dbgen.GetCalculationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Commission calculation")
		}
		posted, pErr := s.post(ctx, tx, p, dbgen.CommissionCalculation{
			ID: calc.ID, PublicID: calc.PublicID, OrganizationID: calc.OrganizationID,
			CommissionType: calc.CommissionType, ShipmentID: calc.ShipmentID,
			QualifiedAt: calc.QualifiedAt, RecipientType: calc.RecipientType,
			RecipientID: calc.RecipientID, FranchiseID: calc.FranchiseID,
			OperatingUnitID: calc.OperatingUnitID, RuleCode: calc.RuleCode,
			RuleVersionNo: calc.RuleVersionNo, AmountMinor: calc.AmountMinor,
			Currency: calc.Currency, Status: calc.Status,
		})
		if pErr != nil {
			return pErr
		}
		out = posted
		return nil
	})
	return out, err
}

// Reverse undoes a posted commission: the ledger entries are mirrored, the
// calculation is marked REVERSED, and a negative entry records the clawback.
//
// This is the only way to correct a commission (§24). The original stays
// exactly as calculated, so the trace that explains it remains true.
func (s *Service) Reverse(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.CommissionCalculation, error) {
	if len([]rune(reason)) < 10 {
		return nil, apierr.Validation("Reversing a commission requires a substantive reason.",
			map[string]any{"reason": "at least 10 characters"})
	}

	var out *dbgen.CommissionCalculation
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		calc, err := q.GetCalculationByPublicID(ctx, dbgen.GetCalculationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Commission calculation")
		}
		switch calc.Status {
		case StatusReversed:
			return apierr.Conflict(CodeAlreadyReversed,
				"This commission has already been reversed.")
		case StatusCalculated:
			return apierr.Conflict(CodeNotCalculated,
				"This commission was never posted, so there is nothing to reverse. Cancel it instead.")
		}
		if calc.JournalTransactionID == nil {
			return apierr.Conflict(CodeNotCalculated,
				"This commission has no ledger transaction to reverse.")
		}

		if _, err := s.ledger.Reverse(ctx, tx, p, *calc.JournalTransactionID, reason, time.Now().UTC()); err != nil {
			return err
		}
		updated, err := q.MarkCalculationReversed(ctx, dbgen.MarkCalculationReversedParams{
			OrganizationID: p.OrganizationID, ID: calc.ID, ReversedByID: nil,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeAlreadyReversed,
					"This commission was reversed by another request.")
			}
			return apierr.Internal(err)
		}

		// The clawback, so the franchise's commission history shows both the
		// earning and its removal rather than the earning disappearing.
		if _, err := q.CreateCommissionEntry(ctx, dbgen.CreateCommissionEntryParams{
			PublicID:        publicid.New("cen"),
			OrganizationID:  p.OrganizationID,
			CalculationID:   calc.ID,
			FranchiseID:     calc.FranchiseID,
			OperatingUnitID: calc.OperatingUnitID,
			EntryType:       "REVERSAL",
			CommissionType:  calc.CommissionType,
			AmountMinor:     -calc.AmountMinor,
			Currency:        calc.Currency,
			ShipmentID:      calc.ShipmentID,
			EarnedOn:        time.Now().UTC(),
			Memo:            ops.Optional("Reversal: " + reason),
		}); err != nil {
			return apierr.Internal(err)
		}

		if err := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "commission.reversed", ResourceType: "commission_calculation",
			ResourceID: &calc.ID, ResourcePublicID: calc.PublicID, Reason: reason,
			Metadata: map[string]any{
				"amountMinor": calc.AmountMinor, "currency": calc.Currency,
				"commissionType": calc.CommissionType, "ruleCode": calc.RuleCode,
			},
		})); err != nil {
			return err
		}

		out = &updated
		return nil
	})
	return out, err
}

// asAPIError unwraps an apierr.Error if that is what err is.
func asAPIError(err error, target **apierr.Error) bool {
	if e, ok := err.(*apierr.Error); ok {
		*target = e
		return true
	}
	return false
}
