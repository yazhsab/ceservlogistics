package cod

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Reconciliation statuses.
const (
	ReconOpen                  = "OPEN"
	ReconCounted               = "COUNTED"
	ReconCompleted             = "COMPLETED"
	ReconCompletedWithVariance = "COMPLETED_WITH_VARIANCE"
	ReconCancelled             = "CANCELLED"
)

// Item outcomes.
const (
	OutcomeMatched  = "MATCHED"
	OutcomeShort    = "SHORT"
	OutcomeExcess   = "EXCESS"
	OutcomeMissing  = "MISSING"
	OutcomeDisputed = "DISPUTED"
)

// OpenReconciliationRequest starts a count against one party.
type OpenReconciliationRequest struct {
	PartyType   string
	PartyID     int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	Notes       string
}

// OpenReconciliation starts a counted hand-in for a party.
//
// Only one may be open per party at a time — the partial unique index enforces
// it — because two simultaneous counts of the same cash would each see the
// other's uncounted rows and both would look short.
func (s *Service) OpenReconciliation(
	ctx context.Context, p *tenant.Principal, in OpenReconciliationRequest,
) (*dbgen.CodReconciliation, []dbgen.ListObligationsForPartyRow, error) {
	if in.PeriodEnd.Before(in.PeriodStart) {
		return nil, nil, apierr.Validation("The period ends before it starts.", nil)
	}
	if _, err := statusForCustodian(in.PartyType); err != nil {
		return nil, nil, err
	}

	var recon *dbgen.CodReconciliation
	var expected []dbgen.ListObligationsForPartyRow

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		// Everything the party is holding is what the count is against.
		held, err := q.ListObligationsForParty(ctx, dbgen.ListObligationsForPartyParams{
			OrganizationID: p.OrganizationID,
			CustodianType:  ops.Optional(in.PartyType),
			CustodianID:    &in.PartyID,
			Limit:          5000,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		var expectedMinor int64
		currency := p.OrganizationCurrency
		for _, ob := range held {
			expectedMinor += ob.CollectedMinor - ob.RemittedMinor
			currency = ob.Currency
		}

		code, err := s.allocateCode(ctx, q, p.OrganizationID, "CDR")
		if err != nil {
			return err
		}

		created, err := q.CreateCODReconciliation(ctx, dbgen.CreateCODReconciliationParams{
			PublicID:           publicid.New("cdr"),
			OrganizationID:     p.OrganizationID,
			ReconciliationCode: code,
			PartyType:          in.PartyType, PartyID: in.PartyID,
			PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd,
			ExpectedMinor: expectedMinor, Currency: currency,
			ShipmentCount: int32(len(held)),
			OpenedBy:      &p.UserID,
			Notes:         ops.Optional(in.Notes),
		})
		if err != nil {
			if ops.IsUnique(err, "cod_reconciliations_active_idx") {
				return apierr.Conflict(CodeInvalidState,
					"This party already has an open COD reconciliation. Complete it first.")
			}
			return apierr.Internal(err)
		}

		// Seed one item per held obligation, so the count starts from what the
		// books say is out there rather than from whatever gets scanned.
		for _, ob := range held {
			if _, err := q.AddCODReconciliationItem(ctx, dbgen.AddCODReconciliationItemParams{
				ReconciliationID: created.ID,
				ObligationID:     ob.ID,
				ExpectedMinor:    ob.CollectedMinor - ob.RemittedMinor,
				CountedMinor:     0,
				Outcome:          OutcomeMissing, // until counted
			}); err != nil {
				return apierr.Internal(err)
			}
		}

		recon, expected = &created, held
		return nil
	})
	return recon, expected, err
}

// CountRequest records what was actually counted for one obligation.
type CountRequest struct {
	ReconciliationID string
	ObligationID     string
	CountedMinor     int64
	Notes            string
}

// RecordCount books one counted line.
func (s *Service) RecordCount(
	ctx context.Context, p *tenant.Principal, in CountRequest,
) (*dbgen.CodReconciliationItem, error) {
	var out *dbgen.CodReconciliationItem

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		recon, err := q.GetCODReconciliationByPublicID(ctx, dbgen.GetCODReconciliationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.ReconciliationID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD reconciliation")
		}
		if recon.Status != ReconOpen && recon.Status != ReconCounted {
			return apierr.Conflict(CodeInvalidState,
				fmt.Sprintf("Reconciliation %s is %s and can no longer be counted.",
					recon.ReconciliationCode, recon.Status))
		}

		ob, err := q.GetObligationByPublicID(ctx, dbgen.GetObligationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.ObligationID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD obligation")
		}

		expected := ob.CollectedMinor - ob.RemittedMinor
		outcome := OutcomeMatched
		switch {
		case in.CountedMinor == 0:
			outcome = OutcomeMissing
		case in.CountedMinor < expected:
			outcome = OutcomeShort
		case in.CountedMinor > expected:
			outcome = OutcomeExcess
		}

		item, err := q.AddCODReconciliationItem(ctx, dbgen.AddCODReconciliationItemParams{
			ReconciliationID: recon.ID,
			ObligationID:     ob.ID,
			ExpectedMinor:    expected,
			CountedMinor:     in.CountedMinor,
			Outcome:          outcome,
			Notes:            ops.Optional(in.Notes),
		})
		if err != nil {
			return apierr.Internal(err)
		}
		out = &item
		return nil
	})
	return out, err
}

// CompleteReconciliationRequest closes a count.
type CompleteReconciliationRequest struct {
	ReconciliationID string
	VarianceReason   string
}

// CompleteReconciliation totals a count and posts the variance.
//
// A shortage does not disappear: it moves from the holder's receivable to COD
// Shortage Recoverable against that same holder, so somebody remains
// accountable until an adjustment resolves it. An excess becomes a payable
// rather than being quietly kept.
func (s *Service) CompleteReconciliation(
	ctx context.Context, p *tenant.Principal, in CompleteReconciliationRequest,
) (*dbgen.CodReconciliation, error) {
	var out *dbgen.CodReconciliation

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetCODReconciliationByPublicID(ctx, dbgen.GetCODReconciliationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.ReconciliationID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD reconciliation")
		}
		locked, err := q.LockCODReconciliation(ctx, dbgen.LockCODReconciliationParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != ReconOpen && locked.Status != ReconCounted {
			return apierr.Conflict(CodeInvalidState,
				fmt.Sprintf("Reconciliation %s is already %s.",
					locked.ReconciliationCode, locked.Status))
		}

		totals, err := q.SumCODReconciliationItems(ctx, locked.ID)
		if err != nil {
			return apierr.Internal(err)
		}

		hasVariance := totals.ShortageMinor != 0 || totals.ExcessMinor != 0
		if hasVariance && len([]rune(in.VarianceReason)) < 3 {
			return apierr.Validation(
				"The count does not match what was expected, so a reason is required.",
				map[string]any{
					"expectedMinor": totals.ExpectedMinor,
					"countedMinor":  totals.CountedMinor,
					"shortageMinor": totals.ShortageMinor,
					"excessMinor":   totals.ExcessMinor,
				})
		}

		status := ReconCompleted
		if hasVariance {
			status = ReconCompletedWithVariance
		}

		var journalID *int64
		if hasVariance {
			account, party, err := receivableAccountFor(locked.PartyType)
			if err != nil {
				return err
			}

			var legs []ledger.Leg
			if totals.ShortageMinor > 0 {
				legs = append(legs,
					ledger.Leg{
						AccountCode: ledger.AcctCODShortageRecoverable, Debit: totals.ShortageMinor,
						PartyType: party, PartyID: locked.PartyID,
						Memo: "Shortfall found on reconciliation",
					},
					ledger.Leg{
						AccountCode: account, Credit: totals.ShortageMinor,
						PartyType: party, PartyID: locked.PartyID,
						Memo: "Receivable reduced by the shortfall",
					})
			}
			if totals.ExcessMinor > 0 {
				legs = append(legs,
					ledger.Leg{
						AccountCode: account, Debit: totals.ExcessMinor,
						PartyType: party, PartyID: locked.PartyID,
						Memo: "Over-collection found on reconciliation",
					},
					ledger.Leg{
						AccountCode: ledger.AcctCODExcessPayable, Credit: totals.ExcessMinor,
						Memo: "Excess owed back",
					})
			}

			posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
				SourceType:     ledger.SourceCOD,
				SourceID:       &locked.ID,
				SourcePublicID: locked.PublicID,
				Purpose:        "RECONCILIATION",
				Description: fmt.Sprintf("COD reconciliation %s variance",
					locked.ReconciliationCode),
				Currency: locked.Currency, PostingDate: time.Now().UTC(),
				Legs: legs,
				Metadata: map[string]any{
					"reconciliationCode": locked.ReconciliationCode,
					"shortageMinor":      totals.ShortageMinor,
					"excessMinor":        totals.ExcessMinor,
				},
			})
			if err != nil {
				return err
			}
			journalID = &posting.Transaction.ID
		}

		completed, err := q.CompleteCODReconciliation(ctx, dbgen.CompleteCODReconciliationParams{
			OrganizationID: p.OrganizationID, ID: locked.ID,
			Status:               status,
			CountedMinor:         totals.CountedMinor,
			ShortageMinor:        totals.ShortageMinor,
			ExcessMinor:          totals.ExcessMinor,
			ExpectedMinor:        totals.ExpectedMinor,
			ShipmentCount:        int32(totals.ItemCount),
			VarianceReason:       ops.Optional(in.VarianceReason),
			JournalTransactionID: journalID,
			CompletedBy:          &p.UserID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					"This reconciliation was completed by another request.")
			}
			return apierr.Internal(err)
		}

		// Mark the counted obligations reconciled so they leave the hand-in
		// queue and become eligible for remittance.
		items, err := q.ListCODReconciliationItems(ctx, locked.ID)
		if err != nil {
			return apierr.Internal(err)
		}
		for _, item := range items {
			if item.Outcome == OutcomeMissing {
				continue // still outstanding; it stays in custody
			}
			if _, err := q.MarkObligationReconciled(ctx, dbgen.MarkObligationReconciledParams{
				OrganizationID: p.OrganizationID, ID: item.ObligationID,
				AdjustedMinor: item.CountedMinor - item.ExpectedMinor,
			}); err != nil && !ops.IsNoRows(err) {
				return apierr.Internal(err)
			}
		}

		out = &completed
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.reconciliation.completed", ResourceType: "cod_reconciliation",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Reason: in.VarianceReason,
			Metadata: map[string]any{
				"reconciliationCode": locked.ReconciliationCode,
				"expectedMinor":      totals.ExpectedMinor,
				"countedMinor":       totals.CountedMinor,
				"shortageMinor":      totals.ShortageMinor,
				"excessMinor":        totals.ExcessMinor,
				"status":             status,
			},
		}))
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Disputes
// ---------------------------------------------------------------------------

// DisputeRequest contests an amount.
type DisputeRequest struct {
	ObligationID  string
	RaisedByType  string
	RaisedByID    *int64
	DisputedMinor int64
	Category      string
	Description   string
}

// RaiseDispute parks a contested amount.
//
// A dispute deliberately does not block the rest of the chain: the other
// shipments in a hand-in still settle, and only the contested one waits. A
// design that froze the whole batch would push branches into settling disputes
// under time pressure.
func (s *Service) RaiseDispute(
	ctx context.Context, p *tenant.Principal, in DisputeRequest,
) (*dbgen.CodDispute, error) {
	if len([]rune(in.Description)) < 10 {
		return nil, apierr.Validation("A dispute needs a description of what is contested.",
			map[string]any{"description": "at least 10 characters"})
	}
	if in.DisputedMinor <= 0 {
		return nil, apierr.Validation("A dispute must name a positive amount.", nil)
	}

	var out *dbgen.CodDispute
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		ob, err := q.GetObligationByPublicID(ctx, dbgen.GetObligationByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.ObligationID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD obligation")
		}

		code, err := s.allocateCode(ctx, q, p.OrganizationID, "CDD")
		if err != nil {
			return err
		}

		created, err := q.CreateCODDispute(ctx, dbgen.CreateCODDisputeParams{
			PublicID:       publicid.New("cdd"),
			OrganizationID: p.OrganizationID,
			ObligationID:   ob.ID,
			DisputeCode:    code,
			RaisedByType:   in.RaisedByType,
			RaisedByID:     in.RaisedByID,
			DisputedMinor:  in.DisputedMinor,
			Currency:       ob.Currency,
			Category:       in.Category,
			Description:    in.Description,
			CreatedBy:      &p.UserID,
		})
		if err != nil {
			if ops.IsUnique(err, "cod_disputes_active_idx") {
				return apierr.Conflict(CodeActiveDispute,
					fmt.Sprintf("COD for %s already has an open dispute.", ob.Awb))
			}
			return apierr.Internal(err)
		}
		out = &created

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.dispute.raised", ResourceType: "cod_dispute",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Metadata: map[string]any{
				"disputeCode": code, "awb": ob.Awb,
				"disputedMinor": in.DisputedMinor, "category": in.Category,
			},
		}))
	})
	return out, err
}

// ResolveDisputeRequest closes a dispute.
type ResolveDisputeRequest struct {
	DisputeID     string
	Status        string // RESOLVED or REJECTED
	Resolution    string
	ResolvedMinor *int64
	AdjustmentID  string
}

// ResolveDispute records the outcome. Any money movement is a separate,
// approved adjustment rather than something the resolution posts directly —
// resolving a dispute and moving money are different authorisations.
func (s *Service) ResolveDispute(
	ctx context.Context, p *tenant.Principal, in ResolveDisputeRequest,
) (*dbgen.CodDispute, error) {
	if in.Status != "RESOLVED" && in.Status != "REJECTED" {
		return nil, apierr.Validation("A dispute is resolved or rejected.",
			map[string]any{"status": in.Status, "allowed": []string{"RESOLVED", "REJECTED"}})
	}
	if len([]rune(in.Resolution)) < 10 {
		return nil, apierr.Validation("Closing a dispute requires an explanation.",
			map[string]any{"resolution": "at least 10 characters"})
	}

	var out *dbgen.CodDispute
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		dispute, err := q.GetDisputeByPublicID(ctx, dbgen.GetDisputeByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.DisputeID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD dispute")
		}

		var adjustmentID *int64
		if in.AdjustmentID != "" {
			adj, aErr := q.GetAdjustmentByPublicID(ctx, dbgen.GetAdjustmentByPublicIDParams{
				OrganizationID: p.OrganizationID, PublicID: in.AdjustmentID,
			})
			if aErr != nil {
				return ops.NotFoundOr(aErr, "COD adjustment")
			}
			adjustmentID = &adj.ID
		}

		resolved, err := q.ResolveCODDispute(ctx, dbgen.ResolveCODDisputeParams{
			OrganizationID: p.OrganizationID, ID: dispute.ID,
			Status: in.Status, Resolution: &in.Resolution,
			ResolvedMinor: in.ResolvedMinor, ResolvedBy: &p.UserID,
			AdjustmentID: adjustmentID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					"This dispute is already closed.")
			}
			return apierr.Internal(err)
		}
		out = &resolved

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.dispute.resolved", ResourceType: "cod_dispute",
			ResourceID: &dispute.ID, ResourcePublicID: dispute.PublicID,
			Reason: in.Resolution,
			Metadata: map[string]any{
				"disputeCode": dispute.DisputeCode, "status": in.Status,
			},
		}))
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Read side
// ---------------------------------------------------------------------------

// CustodyPosition is what one party is holding, from two independent sources.
type CustodyPosition struct {
	PartyType string `json:"partyType"`
	PartyID   string `json:"partyId"`
	// From the obligation rows.
	HeldMinor       int64 `json:"heldMinor"`
	ObligationCount int64 `json:"obligationCount"`
	// From the ledger. These must agree; a difference means the operational
	// record and the books have diverged, which is a finance incident.
	LedgerBalanceMinor int64  `json:"ledgerBalanceMinor"`
	Reconciled         bool   `json:"reconciled"`
	Currency           string `json:"currency"`
}

// CustodyPosition reports what a party holds, derived twice and cross-checked.
//
// The cross-check is the point. The obligation rows and the ledger are written
// in the same transaction but by different code paths, so comparing them
// catches a whole class of bug that neither alone would reveal.
func (s *Service) CustodyPosition(
	ctx context.Context, p *tenant.Principal, partyType string, partyID int64,
) (*CustodyPosition, error) {
	held, err := s.q.SumCODInCustody(ctx, dbgen.SumCODInCustodyParams{
		OrganizationID: p.OrganizationID,
		CustodianType:  ops.Optional(partyType),
		CustodianID:    &partyID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	_, ledgerParty, err := receivableAccountFor(partyType)
	if err != nil {
		return nil, err
	}
	balance, err := s.ledger.PartyBalance(ctx, p, ledgerParty, partyID, nil)
	if err != nil {
		return nil, err
	}

	return &CustodyPosition{
		PartyType: partyType, PartyID: fmt.Sprint(partyID),
		HeldMinor: held.HeldMinor, ObligationCount: held.ObligationCount,
		LedgerBalanceMinor: balance,
		Reconciled:         held.HeldMinor == balance,
		Currency:           p.OrganizationCurrency,
	}, nil
}

// Summary is the COD dashboard.
type Summary struct {
	ExpectedCount      int64  `json:"expectedCount"`
	WithAgentCount     int64  `json:"withAgentCount"`
	WithBranchCount    int64  `json:"withBranchCount"`
	WithFranchiseCount int64  `json:"withFranchiseCount"`
	RemittedCount      int64  `json:"remittedCount"`
	ExpectedMinor      int64  `json:"expectedMinor"`
	InCustodyMinor     int64  `json:"inCustodyMinor"`
	RemittedMinor      int64  `json:"remittedMinor"`
	Currency           string `json:"currency"`
}

// Summary returns the COD position for the tenant or one franchise.
func (s *Service) Summary(
	ctx context.Context, p *tenant.Principal, franchiseID *int64,
) (*Summary, error) {
	row, err := s.q.GetCODSummary(ctx, dbgen.GetCODSummaryParams{
		OrganizationID: p.OrganizationID, FranchiseID: franchiseID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &Summary{
		ExpectedCount:      row.ExpectedCount,
		WithAgentCount:     row.WithAgentCount,
		WithBranchCount:    row.WithBranchCount,
		WithFranchiseCount: row.WithFranchiseCount,
		RemittedCount:      row.RemittedCount,
		ExpectedMinor:      row.ExpectedMinor,
		InCustodyMinor:     row.InCustodyMinor,
		RemittedMinor:      row.RemittedMinor,
		Currency:           p.OrganizationCurrency,
	}, nil
}

// ListObligations returns the COD queue.
func (s *Service) ListObligations(
	ctx context.Context, p *tenant.Principal,
	status, custodianType, awb string, custodianID, franchiseID, cursor *int64, limit int32,
) ([]dbgen.ListCODObligationsRow, error) {
	rows, err := s.q.ListCODObligations(ctx, dbgen.ListCODObligationsParams{
		OrganizationID: p.OrganizationID,
		Status:         ops.Optional(status),
		CustodianType:  ops.Optional(custodianType),
		CustodianID:    custodianID,
		FranchiseID:    franchiseID,
		Awb:            ops.Optional(awb),
		CursorID:       cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// GetObligation returns one obligation with its collections.
func (s *Service) GetObligation(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetObligationByPublicIDRow, []dbgen.CodCollection, error) {
	ob, err := s.q.GetObligationByPublicID(ctx, dbgen.GetObligationByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, nil, ops.NotFoundOr(err, "COD obligation")
	}
	collections, err := s.q.ListCollectionsForObligation(ctx, ob.ID)
	if err != nil {
		return nil, nil, apierr.Internal(err)
	}
	return &ob, collections, nil
}

// ResolveShipmentID turns a shipment public id into an internal id under this
// tenant. The transport needs it to accept a public identifier on the
// collection endpoint without reaching into the database itself.
func (s *Service) ResolveShipmentID(
	ctx context.Context, p *tenant.Principal, publicID string,
) (int64, error) {
	sh, err := s.q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return 0, ops.NotFoundOr(err, "Shipment")
	}
	return sh.ID, nil
}
