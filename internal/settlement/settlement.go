// Package settlement implements M24: the period statement between head office
// and one franchise.
//
// # Reproducibility
//
// §26 requires that recalculating a settlement over the same period produces
// the same answer. That is not achieved by caching the result — it is achieved
// by making the calculation a pure function of rows that cannot change:
//
//   - commission_calculations are frozen once posted (0021's guard trigger);
//   - cod_obligations cannot have their expected amount edited (0022's guard);
//   - every line records source_type + source_id, and a partial unique index
//     refuses the same source twice on one statement.
//
// Calculate therefore clears its working lines and rebuilds them from the
// sources. Running it twice yields identical lines, and the calculation_hash
// makes that checkable without diffing every row — which is what
// TestSettlementIsReproducible asserts.
//
// # Why an approved settlement never recalculates
//
// Once approved, the numbers have been agreed with a counterparty. Recalculating
// would silently change what both sides signed off. The settlements_guard
// trigger refuses it at the database level, and corrections go through
// settlement_adjustments, which are additive rows with their own maker/checker.
//
// # The accounting
//
//	approval   DR Commission Payable      CR Franchise Payable (party)
//	           — the accrued commission becomes a settled obligation to the franchise
//	payment    DR Franchise Payable       CR Cash and Bank
//	           — paying it down
//
// COD the franchise is holding reduces what head office owes it, which is why a
// settlement's net can be negative: a franchise that collected more COD than it
// earned in commission owes head office money rather than the reverse.
package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Settlement statuses.
const (
	StatusDraft         = "DRAFT"
	StatusCalculated    = "CALCULATED"
	StatusUnderReview   = "UNDER_REVIEW"
	StatusApproved      = "APPROVED"
	StatusPartiallyPaid = "PARTIALLY_PAID"
	StatusPaid          = "PAID"
	StatusClosed        = "CLOSED"
	StatusCancelled     = "CANCELLED"
)

// Period types.
const (
	PeriodWeekly      = "WEEKLY"
	PeriodFortnightly = "FORTNIGHTLY"
	PeriodMonthly     = "MONTHLY"
	PeriodCustom      = "CUSTOM"
)

// Line categories.
const (
	CatBookingCommission   = "BOOKING_COMMISSION"
	CatPickupCommission    = "PICKUP_COMMISSION"
	CatOriginHandling      = "ORIGIN_HANDLING"
	CatDestinationHandling = "DESTINATION_HANDLING"
	CatDeliveryCommission  = "DELIVERY_COMMISSION"
	CatCODCommission       = "COD_COMMISSION"
	CatVolumeIncentive     = "VOLUME_INCENTIVE"
	CatCustomCommission    = "CUSTOM_COMMISSION"
	CatCODLiability        = "COD_LIABILITY"
	CatCharge              = "CHARGE"
	CatPenalty             = "PENALTY"
	CatIncentive           = "INCENTIVE"
	CatAdjustment          = "ADJUSTMENT"
	CatTax                 = "TAX"
	CatWithholding         = "WITHHOLDING"
	CatOpeningBalance      = "OPENING_BALANCE"
)

// Source types for line provenance.
const (
	SrcCommission     = "COMMISSION_CALCULATION"
	SrcCODObligation  = "COD_OBLIGATION"
	SrcCODAdjustment  = "COD_ADJUSTMENT"
	SrcAdjustment     = "SETTLEMENT_ADJUSTMENT"
	SrcPrevSettlement = "PREVIOUS_SETTLEMENT"
	SrcTaxRule        = "TAX_RULE"
	SrcManual         = "MANUAL"
)

// Error codes.
const (
	CodeAlreadyExists   = "SETTLEMENT_ALREADY_EXISTS"
	CodeInvalidState    = "SETTLEMENT_INVALID_STATE"
	CodeSelfApproval    = "SETTLEMENT_SELF_APPROVAL"
	CodeNotApproved     = "SETTLEMENT_NOT_APPROVED"
	CodeAlreadyPaid     = "SETTLEMENT_ALREADY_PAID"
	CodeOverpayment     = "SETTLEMENT_OVERPAYMENT"
	CodeNothingToSettle = "SETTLEMENT_NOTHING_TO_SETTLE"
	CodeFrozen          = "SETTLEMENT_FROZEN"
)

// Service owns settlement generation, approval and payment.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	ledger  *ledger.Service
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics

	// WithholdingBp is the tax withheld from a franchise payout, in basis
	// points. Zero disables it. Configuration rather than law: the tax engine
	// is deliberately generic (§ "Keep India GST metadata possible but tax
	// engine configurable").
	WithholdingBp int32
}

func NewService(
	db *database.DB, q *dbgen.Queries, led *ledger.Service,
	rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics) *Service {
	return &Service{db: db, q: q, ledger: led, audit: rec, log: log, metrics: m}
}

// GenerateRequest asks for a settlement over a period.
type GenerateRequest struct {
	FranchiseID string
	PeriodType  string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Notes       string
}

// Result carries the statement and its lines.
type Result struct {
	Settlement dbgen.Settlement
	Lines      []dbgen.ListSettlementLinesRow
	// Replayed is true when an existing statement for the period was returned
	// rather than a new one created.
	Replayed bool
}

// Generate creates or returns the settlement for a franchise and period, then
// calculates it.
//
// Idempotent: the partial unique index on (franchise, period) means a retried
// generation returns the existing statement instead of producing a duplicate.
func (s *Service) Generate(
	ctx context.Context, p *tenant.Principal, in GenerateRequest,
) (result *Result, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("settlement_generation", "failed")
			return
		}
		s.metrics.RecordBusiness("settlement_generation", "generated")
	}()

	if in.PeriodEnd.Before(in.PeriodStart) {
		return nil, apierr.Validation("The period ends before it starts.", nil)
	}
	if in.PeriodType == "" {
		in.PeriodType = PeriodMonthly
	}

	var out *Result
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		franchise, err := q.GetFranchiseByPublicID(ctx, dbgen.GetFranchiseByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.FranchiseID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Franchise")
		}

		existing, err := q.FindSettlementForPeriod(ctx, dbgen.FindSettlementForPeriodParams{
			OrganizationID: p.OrganizationID, FranchiseID: franchise.ID,
			PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd,
		})
		switch {
		case err == nil:
			// Already exists. Recalculate only if it is still workable;
			// otherwise return it untouched, which is what §26 requires.
			if existing.Status != StatusDraft && existing.Status != StatusCalculated {
				lines, lErr := q.ListSettlementLines(ctx, existing.ID)
				if lErr != nil {
					return apierr.Internal(lErr)
				}
				out = &Result{Settlement: existing, Lines: lines, Replayed: true}
				return nil
			}
			res, cErr := s.calculate(ctx, tx, p, existing, franchise.ID)
			if cErr != nil {
				return cErr
			}
			res.Replayed = true
			out = res
			return nil

		case !ops.IsNoRows(err):
			return apierr.Internal(err)
		}

		number, err := s.allocateNumber(ctx, q, p.OrganizationID, in.PeriodEnd)
		if err != nil {
			return err
		}

		currency := p.OrganizationCurrency
		created, err := q.CreateSettlement(ctx, dbgen.CreateSettlementParams{
			PublicID:            publicid.New("stl"),
			OrganizationID:      p.OrganizationID,
			SettlementNumber:    number,
			FranchiseID:         franchise.ID,
			PeriodType:          in.PeriodType,
			PeriodStart:         in.PeriodStart,
			PeriodEnd:           in.PeriodEnd,
			Currency:            currency,
			OpeningBalanceMinor: 0,
			CreatedBy:           &p.UserID,
			RequestID:           ops.Optional(httpx.RequestID(ctx)),
			Notes:               ops.Optional(in.Notes),
		})
		if err != nil {
			if ops.IsUnique(err, "settlements_period_idx") {
				again, gErr := q.FindSettlementForPeriod(ctx, dbgen.FindSettlementForPeriodParams{
					OrganizationID: p.OrganizationID, FranchiseID: franchise.ID,
					PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd,
				})
				if gErr == nil {
					lines, _ := q.ListSettlementLines(ctx, again.ID)
					out = &Result{Settlement: again, Lines: lines, Replayed: true}
					return nil
				}
			}
			return apierr.Internal(err)
		}

		res, err := s.calculate(ctx, tx, p, created, franchise.ID)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

// Recalculate rebuilds a workable settlement from its sources.
func (s *Service) Recalculate(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*Result, error) {
	var out *Result
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}
		locked, err := q.LockSettlement(ctx, dbgen.LockSettlementParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != StatusDraft && locked.Status != StatusCalculated {
			return apierr.Conflict(CodeFrozen,
				fmt.Sprintf("Settlement %s is %s and cannot be recalculated. Raise an adjustment instead.",
					locked.SettlementNumber, locked.Status))
		}

		res, err := s.calculate(ctx, tx, p, locked, locked.FranchiseID)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

// lineDraft is a line before it is written, so the calculation can be built,
// ordered and hashed deterministically before anything touches the database.
type lineDraft struct {
	Category    string
	Description string
	AmountMinor int64
	Quantity    int32
	SourceType  string
	SourceID    *int64
	SourcePubID string
	ShipmentID  *int64
}

// calculate rebuilds every line from the immutable sources.
//
// The order of operations matters for reproducibility: sources are swept in a
// deterministic order, lines are sorted before numbering, and the hash is taken
// over the sorted lines. Two runs over unchanged sources therefore produce
// byte-identical output.
func (s *Service) calculate(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	stl dbgen.Settlement, franchiseID int64,
) (*Result, error) {
	q := s.q.WithTx(tx)

	// Clear the previous working lines. Legal only while the statement is
	// workable; the guard trigger enforces that independently.
	if err := q.DeleteSettlementLines(ctx, stl.ID); err != nil {
		return nil, apierr.Internal(err)
	}

	periodStart := stl.PeriodStart
	periodEnd := stl.PeriodEnd.AddDate(0, 0, 1) // exclusive upper bound

	var drafts []lineDraft

	// --- Opening balance ----------------------------------------------------
	var openingBalance int64
	if prev, err := q.GetPreviousSettlementBalance(ctx, dbgen.GetPreviousSettlementBalanceParams{
		OrganizationID: p.OrganizationID, FranchiseID: franchiseID, Before: stl.PeriodStart,
	}); err == nil {
		openingBalance = prev.CarryForwardMinor
		if openingBalance != 0 {
			drafts = append(drafts, lineDraft{
				Category:    CatOpeningBalance,
				Description: fmt.Sprintf("Balance carried forward from %s", prev.SettlementNumber),
				AmountMinor: openingBalance, Quantity: 1,
				SourceType: SrcPrevSettlement, SourcePubID: prev.SettlementNumber,
			})
		}
	} else if !ops.IsNoRows(err) {
		return nil, apierr.Internal(err)
	}

	// --- Commission ---------------------------------------------------------
	commissions, err := q.SweepCommissionForSettlement(ctx, dbgen.SweepCommissionForSettlementParams{
		OrganizationID: p.OrganizationID, FranchiseID: &franchiseID,
		SettlementID: &stl.ID,
		PeriodStart:  periodStart, PeriodEnd: periodEnd,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	var commissionTotal, incentiveTotal int64
	commissionIDs := make([]int64, 0, len(commissions))
	for _, c := range commissions {
		category := categoryForCommission(c.CommissionType)
		amount := c.AmountMinor
		if category == CatVolumeIncentive {
			incentiveTotal += amount
		} else {
			commissionTotal += amount
		}
		id := c.ID
		drafts = append(drafts, lineDraft{
			Category: category,
			Description: fmt.Sprintf("%s commission, rule %s v%d",
				c.CommissionType, c.RuleCode, c.RuleVersionNo),
			AmountMinor: amount, Quantity: 1,
			SourceType: SrcCommission, SourceID: &id, SourcePubID: c.PublicID,
			ShipmentID: c.ShipmentID,
		})
		commissionIDs = append(commissionIDs, id)
	}

	// --- COD the franchise is holding ---------------------------------------
	//
	// COD reduces what head office owes: the franchise already has the cash.
	// This is why a settlement's net can legitimately be negative.
	obligations, err := q.SweepCODForSettlement(ctx, dbgen.SweepCODForSettlementParams{
		OrganizationID: p.OrganizationID, DestinationFranchiseID: &franchiseID,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	var codTotal int64
	for _, ob := range obligations {
		held := ob.CollectedMinor - ob.RemittedMinor
		if held == 0 {
			continue
		}
		codTotal -= held
		id := ob.ID
		drafts = append(drafts, lineDraft{
			Category:    CatCODLiability,
			Description: fmt.Sprintf("COD held for %s", ob.Awb),
			AmountMinor: -held, Quantity: 1,
			SourceType: SrcCODObligation, SourceID: &id, SourcePubID: ob.PublicID,
			ShipmentID: &ob.ShipmentID,
		})
	}

	// --- Approved adjustments ------------------------------------------------
	adjustments, err := q.ListPendingAdjustmentsForSettlement(ctx, stl.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	var adjustmentTotal, penaltyTotal, incentiveAdj int64
	for _, a := range adjustments {
		id := a.ID
		category := CatAdjustment
		switch a.AdjustmentType {
		case "PENALTY":
			category, penaltyTotal = CatPenalty, penaltyTotal+a.AmountMinor
		case "INCENTIVE":
			category, incentiveAdj = CatIncentive, incentiveAdj+a.AmountMinor
		default:
			adjustmentTotal += a.AmountMinor
		}
		drafts = append(drafts, lineDraft{
			Category:    category,
			Description: fmt.Sprintf("%s: %s", a.AdjustmentType, a.Reason),
			AmountMinor: a.AmountMinor, Quantity: 1,
			SourceType: SrcAdjustment, SourceID: &id, SourcePubID: a.PublicID,
		})
	}
	incentiveTotal += incentiveAdj

	// --- Withholding tax -----------------------------------------------------
	//
	// Applied to the earnings, not to the COD the franchise is holding: tax is
	// withheld from what is being paid out, and COD is the franchise's own
	// float rather than income.
	var withholding int64
	earnings := commissionTotal + incentiveTotal
	if s.WithholdingBp > 0 && earnings > 0 {
		withholding = money.ApplyBP(earnings, money.BasisPoints(s.WithholdingBp))
		drafts = append(drafts, lineDraft{
			Category: CatWithholding,
			Description: fmt.Sprintf("Withholding tax at %s%% on earnings",
				formatBP(s.WithholdingBp)),
			AmountMinor: -withholding, Quantity: 1,
			SourceType: SrcTaxRule,
		})
	}

	// Deterministic order, so line numbers and the hash do not depend on the
	// order the sweeps happened to return.
	sort.SliceStable(drafts, func(i, j int) bool {
		if drafts[i].Category != drafts[j].Category {
			return drafts[i].Category < drafts[j].Category
		}
		if drafts[i].SourcePubID != drafts[j].SourcePubID {
			return drafts[i].SourcePubID < drafts[j].SourcePubID
		}
		return drafts[i].AmountMinor < drafts[j].AmountMinor
	})

	var net int64
	for i, d := range drafts {
		net += d.AmountMinor
		if _, err := q.CreateSettlementLine(ctx, dbgen.CreateSettlementLineParams{
			PublicID:       publicid.New("sln"),
			OrganizationID: p.OrganizationID,
			SettlementID:   stl.ID,
			LineNo:         int32(i + 1),
			Category:       d.Category,
			Description:    d.Description,
			AmountMinor:    d.AmountMinor,
			Currency:       stl.Currency,
			Quantity:       maxInt32(d.Quantity, 1),
			SourceType:     d.SourceType,
			SourceID:       d.SourceID,
			SourcePublicID: ops.Optional(d.SourcePubID),
			ShipmentID:     d.ShipmentID,
			Metadata:       []byte("{}"),
		}); err != nil {
			if ops.IsUnique(err, "settlement_lines_source_idx") {
				return nil, apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("Source %s appears twice on this settlement.", d.SourcePubID))
			}
			return nil, apierr.Internal(err)
		}
	}

	updated, err := q.ApplySettlementTotals(ctx, dbgen.ApplySettlementTotalsParams{
		OrganizationID:      p.OrganizationID,
		ID:                  stl.ID,
		CommissionMinor:     commissionTotal,
		IncentiveMinor:      incentiveTotal,
		CodLiabilityMinor:   codTotal,
		ChargesMinor:        0,
		PenaltiesMinor:      penaltyTotal,
		AdjustmentsMinor:    adjustmentTotal,
		TaxMinor:            0,
		WithholdingMinor:    -withholding,
		OpeningBalanceMinor: openingBalance,
		NetAmountMinor:      net,
		CalculationHash:     ops.Optional(hashDrafts(drafts)),
		CalculatedBy:        &p.UserID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeFrozen,
				"This settlement is no longer workable.")
		}
		return nil, apierr.Internal(err)
	}

	// Claim the commission rows so a later period cannot sweep them again.
	if len(commissionIDs) > 0 {
		if err := q.AttachCalculationsToSettlement(ctx, dbgen.AttachCalculationsToSettlementParams{
			OrganizationID: p.OrganizationID, SettlementID: &updated.ID, Ids: commissionIDs,
		}); err != nil {
			return nil, apierr.Internal(err)
		}
	}

	lines, err := q.ListSettlementLines(ctx, updated.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &Result{Settlement: updated, Lines: lines}, nil
}

// Submit sends a calculated statement for review.
func (s *Service) Submit(
	ctx context.Context, p *tenant.Principal, publicID, comment string,
) (*dbgen.Settlement, error) {
	var out *dbgen.Settlement
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}
		submitted, err := q.SubmitSettlement(ctx, dbgen.SubmitSettlementParams{
			OrganizationID: p.OrganizationID, ID: header.ID, SubmittedBy: &p.UserID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("Settlement %s is %s and cannot be submitted.",
						header.SettlementNumber, header.Status))
			}
			return apierr.Internal(err)
		}
		if _, err := q.RecordSettlementApproval(ctx, dbgen.RecordSettlementApprovalParams{
			PublicID: publicid.New("sap"), OrganizationID: p.OrganizationID,
			SettlementID: header.ID, Action: "SUBMITTED",
			NetAmountMinor: submitted.NetAmountMinor, Currency: submitted.Currency,
			Comment: ops.Optional(comment), ActorID: p.UserID,
			RequestID: ops.Optional(httpx.RequestID(ctx)),
		}); err != nil {
			return apierr.Internal(err)
		}
		out = &submitted
		return nil
	})
	return out, err
}

// Approve accepts a statement and posts it.
//
// Maker/checker: the approver must differ from whoever calculated it, enforced
// in the service, in the UPDATE's WHERE, and by a CHECK constraint.
func (s *Service) Approve(
	ctx context.Context, p *tenant.Principal, publicID, comment string,
) (*dbgen.Settlement, error) {
	var out *dbgen.Settlement

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}
		locked, err := q.LockSettlement(ctx, dbgen.LockSettlementParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.CalculatedBy != nil && *locked.CalculatedBy == p.UserID {
			return apierr.Forbidden(
				"A settlement must be approved by someone other than the person who calculated it.")
		}
		if locked.Status != StatusCalculated && locked.Status != StatusUnderReview {
			return apierr.Conflict(CodeInvalidState,
				fmt.Sprintf("Settlement %s is %s and cannot be approved.",
					locked.SettlementNumber, locked.Status))
		}

		// Approval turns the accrued commission into a settled obligation to
		// the franchise. A zero net has nothing to post but is still a valid
		// statement — a quiet month is not an error.
		var journalID *int64
		if locked.NetAmountMinor != 0 {
			amount := locked.NetAmountMinor
			legs := []ledger.Leg{}
			if amount > 0 {
				// Head office owes the franchise.
				legs = []ledger.Leg{
					{AccountCode: ledger.AcctCommissionPayable, Debit: amount,
						FranchiseID: &locked.FranchiseID,
						Memo:        "Accrued commission settled"},
					{AccountCode: ledger.AcctFranchisePayable, Credit: amount,
						PartyType: ledger.PartyFranchise, PartyID: locked.FranchiseID,
						FranchiseID: &locked.FranchiseID,
						Memo:        "Net payable for " + locked.SettlementNumber},
				}
			} else {
				// The franchise owes head office, typically because it is
				// holding more COD than it earned.
				legs = []ledger.Leg{
					{AccountCode: ledger.AcctFranchiseReceivable, Debit: -amount,
						PartyType: ledger.PartyFranchise, PartyID: locked.FranchiseID,
						FranchiseID: &locked.FranchiseID,
						Memo:        "Net receivable for " + locked.SettlementNumber},
					{AccountCode: ledger.AcctCommissionPayable, Credit: -amount,
						FranchiseID: &locked.FranchiseID,
						Memo:        "Settled against accrued commission"},
				}
			}

			posting, pErr := s.ledger.Post(ctx, tx, p, ledger.Posting{
				SourceType:     ledger.SourceSettlement,
				SourceID:       &locked.ID,
				SourcePublicID: locked.PublicID,
				Purpose:        "APPROVAL",
				Description:    "Settlement " + locked.SettlementNumber,
				Currency:       locked.Currency,
				PostingDate:    locked.PeriodEnd,
				Legs:           legs,
				Metadata: map[string]any{
					"settlementNumber": locked.SettlementNumber,
					"periodStart":      locked.PeriodStart.Format("2006-01-02"),
					"periodEnd":        locked.PeriodEnd.Format("2006-01-02"),
				},
			})
			if pErr != nil {
				return pErr
			}
			journalID = &posting.Transaction.ID
		}

		approver := p.UserID
		approved, err := q.ApproveSettlement(ctx, dbgen.ApproveSettlementParams{
			OrganizationID: p.OrganizationID, ID: locked.ID,
			ApprovedBy: &approver, JournalTransactionID: journalID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					"This settlement was approved by another request, or you calculated it yourself.")
			}
			return apierr.Internal(err)
		}

		if _, err := q.RecordSettlementApproval(ctx, dbgen.RecordSettlementApprovalParams{
			PublicID: publicid.New("sap"), OrganizationID: p.OrganizationID,
			SettlementID: locked.ID, Action: "APPROVED",
			NetAmountMinor: approved.NetAmountMinor, Currency: approved.Currency,
			Comment: ops.Optional(comment), ActorID: p.UserID,
			RequestID: ops.Optional(httpx.RequestID(ctx)),
		}); err != nil {
			return apierr.Internal(err)
		}

		out = &approved
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.approved", ResourceType: "settlement",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Reason: comment,
			Metadata: map[string]any{
				"settlementNumber": locked.SettlementNumber,
				"netAmountMinor":   approved.NetAmountMinor,
				"calculatedBy":     locked.CalculatedBy,
				"approvedBy":       p.UserID,
			},
		}))
	})
	return out, err
}

// PayRequest records money moving against an approved settlement.
type PayRequest struct {
	SettlementID string
	AmountMinor  int64
	PaymentMode  string
	Reference    string
	PaidOn       time.Time
}

// Pay records a settlement payment and posts it.
func (s *Service) Pay(
	ctx context.Context, p *tenant.Principal, in PayRequest,
) (result *dbgen.SettlementPayment, result2 *dbgen.Settlement, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("settlement_payment", "failed")
			return
		}
		s.metrics.RecordBusiness("settlement_payment", "paid")
	}()

	if in.AmountMinor <= 0 {
		return nil, nil, apierr.Validation("A payment must be for a positive amount.", nil)
	}
	if in.PaidOn.IsZero() {
		in.PaidOn = time.Now().UTC()
	}

	var payment *dbgen.SettlementPayment
	var updated *dbgen.Settlement

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.SettlementID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}
		locked, err := q.LockSettlement(ctx, dbgen.LockSettlementParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != StatusApproved && locked.Status != StatusPartiallyPaid {
			return apierr.Conflict(CodeNotApproved,
				fmt.Sprintf("Settlement %s is %s; only an approved settlement can be paid.",
					locked.SettlementNumber, locked.Status))
		}

		outstanding := abs64(locked.NetAmountMinor) - locked.PaidMinor
		if in.AmountMinor > outstanding {
			return apierr.Conflict(CodeOverpayment,
				fmt.Sprintf("This payment of %d exceeds the %d still outstanding.",
					in.AmountMinor, outstanding)).
				WithDetail("outstandingMinor", outstanding).
				WithDetail("amountMinor", in.AmountMinor)
		}

		direction := "OUTBOUND"
		var legs []ledger.Leg
		if locked.NetAmountMinor > 0 {
			// Paying the franchise what it is owed.
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctFranchisePayable, Debit: in.AmountMinor,
					PartyType: ledger.PartyFranchise, PartyID: locked.FranchiseID,
					FranchiseID: &locked.FranchiseID,
					Memo:        "Settlement payment " + locked.SettlementNumber},
				{AccountCode: ledger.AcctCash, Credit: in.AmountMinor,
					Memo: "Paid to franchise"},
			}
		} else {
			// Collecting what the franchise owes.
			direction = "INBOUND"
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctCash, Debit: in.AmountMinor,
					Memo: "Received from franchise"},
				{AccountCode: ledger.AcctFranchiseReceivable, Credit: in.AmountMinor,
					PartyType: ledger.PartyFranchise, PartyID: locked.FranchiseID,
					FranchiseID: &locked.FranchiseID,
					Memo:        "Settlement recovery " + locked.SettlementNumber},
			}
		}

		number, err := s.allocatePaymentNumber(ctx, q, p.OrganizationID, in.PaidOn)
		if err != nil {
			return err
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourcePayment,
			SourceID:       &locked.ID,
			SourcePublicID: locked.PublicID,
			Purpose:        "SETTLEMENT_PAYMENT:" + number,
			Description:    "Payment " + number + " against " + locked.SettlementNumber,
			Currency:       locked.Currency, PostingDate: in.PaidOn,
			Legs: legs,
			Metadata: map[string]any{
				"settlementNumber": locked.SettlementNumber, "paymentNumber": number,
			},
		})
		if err != nil {
			return err
		}

		created, err := q.CreateSettlementPayment(ctx, dbgen.CreateSettlementPaymentParams{
			PublicID:             publicid.New("spm"),
			OrganizationID:       p.OrganizationID,
			SettlementID:         locked.ID,
			PaymentNumber:        number,
			Direction:            direction,
			AmountMinor:          in.AmountMinor,
			Currency:             locked.Currency,
			PaymentMode:          in.PaymentMode,
			Reference:            ops.Optional(in.Reference),
			PaidOn:               in.PaidOn,
			JournalTransactionID: &posting.Transaction.ID,
			RecordedBy:           p.UserID,
			RequestID:            ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}

		advanced, err := q.RecordSettlementPaid(ctx, dbgen.RecordSettlementPaidParams{
			OrganizationID: p.OrganizationID, ID: locked.ID, AmountMinor: in.AmountMinor,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					"This settlement changed state during payment.")
			}
			return apierr.Internal(err)
		}

		payment, updated = &created, &advanced
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.payment.recorded", ResourceType: "settlement",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Metadata: map[string]any{
				"settlementNumber": locked.SettlementNumber,
				"paymentNumber":    number, "amountMinor": in.AmountMinor,
				"direction": direction, "status": advanced.Status,
			},
		}))
	})
	return payment, updated, err
}

// Cancel voids a settlement before it is approved and releases its commission.
func (s *Service) Cancel(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.Settlement, error) {
	if len([]rune(reason)) < 10 {
		return nil, apierr.Validation("Cancelling a settlement requires a substantive reason.",
			map[string]any{"reason": "at least 10 characters"})
	}

	var out *dbgen.Settlement
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}

		// Release the commission back to the pool, or it would be stranded:
		// attached to a cancelled statement and invisible to the next run.
		if err := q.DetachCalculationsFromSettlement(ctx, dbgen.DetachCalculationsFromSettlementParams{
			OrganizationID: p.OrganizationID, SettlementID: &header.ID,
		}); err != nil {
			return apierr.Internal(err)
		}

		cancelled, err := q.CancelSettlement(ctx, dbgen.CancelSettlementParams{
			OrganizationID: p.OrganizationID, ID: header.ID, CancelReason: &reason,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("Settlement %s is %s and can no longer be cancelled. Raise an adjustment.",
						header.SettlementNumber, header.Status))
			}
			return apierr.Internal(err)
		}
		out = &cancelled

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.cancelled", ResourceType: "settlement",
			ResourceID: &header.ID, ResourcePublicID: header.PublicID, Reason: reason,
			Metadata: map[string]any{"settlementNumber": header.SettlementNumber},
		}))
	})
	return out, err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func categoryForCommission(commissionType string) string {
	switch commissionType {
	case "BOOKING":
		return CatBookingCommission
	case "PICKUP":
		return CatPickupCommission
	case "ORIGIN_HANDLING":
		return CatOriginHandling
	case "DESTINATION_HANDLING":
		return CatDestinationHandling
	case "DELIVERY":
		return CatDeliveryCommission
	case "COD":
		return CatCODCommission
	case "VOLUME_INCENTIVE":
		return CatVolumeIncentive
	}
	return CatCustomCommission
}

// hashDrafts fingerprints a calculation so two runs can be compared without
// diffing every line. Built from the sorted drafts, so it is stable.
func hashDrafts(drafts []lineDraft) string {
	h := sha256.New()
	for _, d := range drafts {
		fmt.Fprintf(h, "%s|%s|%d|%s|%s\n",
			d.Category, d.SourceType, d.AmountMinor, d.SourcePubID, d.Description)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (s *Service) allocateNumber(
	ctx context.Context, q *dbgen.Queries, orgID int64, periodEnd time.Time,
) (string, error) {
	scope := periodEnd.Format("200601")
	next, err := q.AllocateSettlementNumber(ctx, dbgen.AllocateSettlementNumberParams{
		OrganizationID: orgID, Kind: "STL", ScopeKey: scope,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return "", apierr.Conflict(apierr.CodeConflict,
				"The settlement number sequence is exhausted for this month.")
		}
		return "", apierr.Internal(err)
	}
	return fmt.Sprintf("STL-%s-%05d", scope, next), nil
}

func (s *Service) allocatePaymentNumber(
	ctx context.Context, q *dbgen.Queries, orgID int64, on time.Time,
) (string, error) {
	scope := on.Format("200601")
	next, err := q.AllocateSettlementNumber(ctx, dbgen.AllocateSettlementNumberParams{
		OrganizationID: orgID, Kind: "SPM", ScopeKey: scope,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return "", apierr.Conflict(apierr.CodeConflict,
				"The settlement payment sequence is exhausted for this month.")
		}
		return "", apierr.Internal(err)
	}
	return fmt.Sprintf("SPM-%s-%05d", scope, next), nil
}

func formatBP(bp int32) string {
	whole, frac := bp/100, bp%100
	if frac == 0 {
		return fmt.Sprint(whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%02d", whole, frac), "0")
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// Get returns a settlement with its lines, adjustments, approvals and payments.
func (s *Service) Get(
	ctx context.Context, p *tenant.Principal, publicID string,
) (map[string]any, error) {
	header, err := s.q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Settlement")
	}
	lines, err := s.q.ListSettlementLines(ctx, header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	approvals, err := s.q.ListSettlementApprovals(ctx, header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	payments, err := s.q.ListSettlementPayments(ctx, header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	byCategory, err := s.q.SumSettlementLinesByCategory(ctx, header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return map[string]any{
		"settlement": header, "lines": lines, "byCategory": byCategory,
		"approvals": approvals, "payments": payments,
	}, nil
}

// List returns the settlement register.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal,
	status string, franchiseID, cursor *int64, limit int32,
) ([]dbgen.ListSettlementsRow, error) {
	rows, err := s.q.ListSettlements(ctx, dbgen.ListSettlementsParams{
		OrganizationID: p.OrganizationID,
		Status:         ops.Optional(status),
		FranchiseID:    franchiseID,
		CursorID:       cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}
