package settlement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Settlement adjustments — the correction path for a statement that can no
// longer be recalculated.
//
// # Why this exists
//
// Recalculating an approved settlement is refused: the net has been posted to
// the ledger and a franchise has been told what they are owed. Constitution §26
// requires corrections to go through an explicit adjustment or reversal
// instead, and §23 forbids "fixing" a balance by editing history.
//
// Until this file existed, `Generate` told an operator "Raise an adjustment
// instead" and pointed at a route that did not exist. The table, the queries
// and the permissions were all in place; nothing connected them.
//
// # Where the money lands depends on the settlement's state
//
// This is the one subtle part, and getting it wrong double-counts:
//
//   - Against a **DRAFT or CALCULATED** statement, an approved adjustment is
//     picked up by the next `calculate` as a line and folded into the net. It
//     must NOT post a journal of its own — the settlement's approval will post
//     the whole net including it.
//
//   - Against a **frozen** statement (APPROVED and beyond), the net is already
//     in the ledger. An approved adjustment therefore posts its own balanced
//     journal immediately and is marked APPLIED.
//
// `ListPendingAdjustmentsForSettlement` returns both APPROVED and APPLIED rows
// to the calculator, which is safe precisely because an APPLIED row can only
// belong to a frozen statement, and a frozen statement cannot be recalculated.
//
// # Maker/checker
//
// Enforced in three places, like every other approval in the finance modules:
// here in the service, in the UPDATE's WHERE clause, and by the
// settlement_adjustments_maker_is_not_checker CHECK constraint. Rejection is
// held to the same rule — somebody quietly withdrawing their own correction
// leaves the same hole in the record as approving it.

// Adjustment statuses.
const (
	AdjPending  = "PENDING_APPROVAL"
	AdjApproved = "APPROVED"
	AdjRejected = "REJECTED"
	AdjApplied  = "APPLIED"
)

// AdjustmentTypes are the reasons a statement can be corrected, matching the
// CHECK on settlement_adjustments.adjustment_type.
var AdjustmentTypes = []string{
	"CORRECTION", "PENALTY", "INCENTIVE", "WAIVER", "RECOVERY", "GOODWILL", "OTHER",
}

// Adjustment error codes.
const CodeAdjustmentNotPending = "SETTLEMENT_ADJUSTMENT_NOT_PENDING"

// AdjustmentRequest raises a correction against a statement.
type AdjustmentRequest struct {
	SettlementID string
	Type         string
	AmountMinor  int64
	Reason       string
}

// RaiseAdjustment records a correction awaiting approval.
//
// It moves no money. That is the whole point of the maker/checker split: the
// person who spots the error writes it down, and somebody else decides whether
// it is real.
func (s *Service) RaiseAdjustment(
	ctx context.Context, p *tenant.Principal, in AdjustmentRequest,
) (*dbgen.SettlementAdjustment, error) {
	if !validAdjustmentType(in.Type) {
		return nil, apierr.Validation("Unknown adjustment type.",
			map[string]any{"adjustmentType": in.Type, "allowed": AdjustmentTypes})
	}
	// A zero adjustment is not a correction; it is a note, and there is a
	// comment field for those.
	if in.AmountMinor == 0 {
		return nil, apierr.Validation("An adjustment must move a non-zero amount.",
			map[string]any{"amountMinor": "must not be zero"})
	}
	// The CHECK demands 10 characters. Saying so here beats a constraint
	// violation surfacing as a 500.
	if len([]rune(trimSpace(in.Reason))) < 10 {
		return nil, apierr.Validation(
			"An adjustment needs a reason of at least 10 characters.",
			map[string]any{"reason": "at least 10 characters"})
	}

	var out *dbgen.SettlementAdjustment
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.SettlementID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement")
		}
		// A cancelled statement is not a thing to correct — it never became
		// anybody's money.
		if header.Status == StatusCancelled {
			return apierr.Conflict(CodeInvalidState,
				"A cancelled settlement cannot be adjusted.")
		}

		created, err := q.CreateSettlementAdjustment(ctx, dbgen.CreateSettlementAdjustmentParams{
			PublicID: publicid.New("sad"), OrganizationID: p.OrganizationID,
			SettlementID: header.ID, AdjustmentType: in.Type,
			AmountMinor: in.AmountMinor, Currency: header.Currency,
			Reason: in.Reason, RequestedBy: p.UserID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		out = &created

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.adjustment.raised", ResourceType: "settlement_adjustment",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Reason: in.Reason,
			Metadata: map[string]any{
				"settlementNumber": header.SettlementNumber,
				"settlementStatus": header.Status,
				"adjustmentType":   in.Type,
				"amountMinor":      in.AmountMinor,
				"currency":         header.Currency,
			},
		}))
	})
	return out, err
}

// ApproveAdjustment accepts a correction, and posts it when the statement it
// belongs to is already frozen.
func (s *Service) ApproveAdjustment(
	ctx context.Context, p *tenant.Principal, publicID, comment string,
) (*dbgen.SettlementAdjustment, error) {
	var out *dbgen.SettlementAdjustment

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		existing, err := q.GetSettlementAdjustmentByPublicID(ctx,
			dbgen.GetSettlementAdjustmentByPublicIDParams{
				OrganizationID: p.OrganizationID, PublicID: publicID,
			})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement adjustment")
		}
		if existing.Status != AdjPending {
			return apierr.Conflict(CodeAdjustmentNotPending,
				fmt.Sprintf("This adjustment is %s and cannot be approved.", existing.Status)).
				WithDetail("status", existing.Status)
		}
		// Checked here as well as in the WHERE clause so the caller gets an
		// explanation rather than a bare "not found".
		if existing.RequestedBy == p.UserID {
			return apierr.Forbidden(
				"An adjustment must be approved by someone other than the person who raised it.")
		}

		settlement, err := q.LockSettlement(ctx, dbgen.LockSettlementParams{
			OrganizationID: p.OrganizationID, ID: existing.SettlementID,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		approved, err := q.ApproveSettlementAdjustment(ctx,
			dbgen.ApproveSettlementAdjustmentParams{
				OrganizationID: p.OrganizationID, ID: existing.ID, ApprovedBy: &p.UserID,
			})
		if err != nil {
			if ops.IsNoRows(err) {
				// Lost a race, or the maker/checker predicate refused it.
				return apierr.Conflict(CodeAdjustmentNotPending,
					"This adjustment is no longer pending approval.")
			}
			return apierr.Internal(err)
		}
		out = &approved

		// A statement still open will pick this up on its next calculation, and
		// its own approval will post the whole net. Posting here too would
		// count the correction twice.
		posted := false
		if isFrozen(settlement.Status) {
			journalID, pErr := s.postAdjustment(ctx, tx, p, settlement, approved)
			if pErr != nil {
				return pErr
			}
			applied, aErr := q.MarkSettlementAdjustmentApplied(ctx,
				dbgen.MarkSettlementAdjustmentAppliedParams{
					OrganizationID:       p.OrganizationID,
					ID:                   approved.ID,
					JournalTransactionID: journalID,
				})
			if aErr != nil {
				return apierr.Internal(aErr)
			}
			out = &applied
			posted = true
		}

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.adjustment.approved", ResourceType: "settlement_adjustment",
			ResourceID: &existing.ID, ResourcePublicID: existing.PublicID,
			Reason: comment,
			Before: map[string]any{"status": AdjPending},
			After:  map[string]any{"status": out.Status},
			Metadata: map[string]any{
				"settlementNumber": settlement.SettlementNumber,
				"settlementStatus": settlement.Status,
				"amountMinor":      existing.AmountMinor,
				"requestedBy":      existing.RequestedBy,
				"approvedBy":       p.UserID,
				// Says plainly whether money moved now or will move at the next
				// calculation, which is the question an auditor asks first.
				"postedImmediately": posted,
			},
		}))
	})
	return out, err
}

// RejectAdjustment refuses a correction, with a reason.
func (s *Service) RejectAdjustment(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.SettlementAdjustment, error) {
	if len([]rune(trimSpace(reason))) < 5 {
		return nil, apierr.Validation("Rejecting an adjustment requires a reason.",
			map[string]any{"reason": "at least 5 characters"})
	}

	var out *dbgen.SettlementAdjustment
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		existing, err := q.GetSettlementAdjustmentByPublicID(ctx,
			dbgen.GetSettlementAdjustmentByPublicIDParams{
				OrganizationID: p.OrganizationID, PublicID: publicID,
			})
		if err != nil {
			return ops.NotFoundOr(err, "Settlement adjustment")
		}
		if existing.Status != AdjPending {
			return apierr.Conflict(CodeAdjustmentNotPending,
				fmt.Sprintf("This adjustment is %s and cannot be rejected.", existing.Status)).
				WithDetail("status", existing.Status)
		}
		if existing.RequestedBy == p.UserID {
			return apierr.Forbidden(
				"An adjustment must be decided by someone other than the person who raised it.")
		}

		rejected, err := q.RejectSettlementAdjustment(ctx,
			dbgen.RejectSettlementAdjustmentParams{
				OrganizationID: p.OrganizationID, ID: existing.ID,
				RejectedBy: &p.UserID, RejectionReason: &reason,
			})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeAdjustmentNotPending,
					"This adjustment is no longer pending approval.")
			}
			return apierr.Internal(err)
		}
		out = &rejected

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "settlement.adjustment.rejected", ResourceType: "settlement_adjustment",
			ResourceID: &existing.ID, ResourcePublicID: existing.PublicID,
			Reason: reason,
			Before: map[string]any{"status": AdjPending},
			After:  map[string]any{"status": AdjRejected},
			Metadata: map[string]any{
				"amountMinor": existing.AmountMinor,
				"requestedBy": existing.RequestedBy,
				"rejectedBy":  p.UserID,
			},
		}))
	})
	return out, err
}

// GetAdjustment returns one correction.
func (s *Service) GetAdjustment(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetSettlementAdjustmentByPublicIDRow, error) {
	row, err := s.q.GetSettlementAdjustmentByPublicID(ctx,
		dbgen.GetSettlementAdjustmentByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Settlement adjustment")
	}
	return &row, nil
}

// ListAdjustments returns corrections, newest first.
func (s *Service) ListAdjustments(
	ctx context.Context, p *tenant.Principal, settlementPublicID, status string, limit int32,
) ([]dbgen.ListSettlementAdjustmentsRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	params := dbgen.ListSettlementAdjustmentsParams{
		OrganizationID: p.OrganizationID, Status: ops.Optional(status), Limit: limit,
	}
	if settlementPublicID != "" {
		header, err := s.q.GetSettlementByPublicID(ctx, dbgen.GetSettlementByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: settlementPublicID,
		})
		if err != nil {
			return nil, ops.NotFoundOr(err, "Settlement")
		}
		params.SettlementID = &header.ID
	}
	rows, err := s.q.ListSettlementAdjustments(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// Posting
// ---------------------------------------------------------------------------

// postAdjustment writes the journal for a correction to a frozen statement.
//
// The legs mirror settlement approval, because the effect is the same: a
// positive adjustment increases what head office owes the franchise, a negative
// one reduces it. Keeping the two postings structurally identical is what makes
// a franchise account statement readable — a correction appears as the same
// shape of entry as the thing it corrects.
func (s *Service) postAdjustment(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	stl dbgen.Settlement, adj dbgen.SettlementAdjustment,
) (*int64, error) {
	amount := adj.AmountMinor
	memo := fmt.Sprintf("%s adjustment on %s", adj.AdjustmentType, stl.SettlementNumber)

	var legs []ledger.Leg
	if amount > 0 {
		legs = []ledger.Leg{
			{AccountCode: ledger.AcctCommissionPayable, Debit: amount,
				FranchiseID: &stl.FranchiseID, Memo: memo},
			{AccountCode: ledger.AcctFranchisePayable, Credit: amount,
				PartyType: ledger.PartyFranchise, PartyID: stl.FranchiseID,
				FranchiseID: &stl.FranchiseID, Memo: memo},
		}
	} else {
		legs = []ledger.Leg{
			{AccountCode: ledger.AcctFranchiseReceivable, Debit: -amount,
				PartyType: ledger.PartyFranchise, PartyID: stl.FranchiseID,
				FranchiseID: &stl.FranchiseID, Memo: memo},
			{AccountCode: ledger.AcctCommissionPayable, Credit: -amount,
				FranchiseID: &stl.FranchiseID, Memo: memo},
		}
	}

	posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
		SourceType: ledger.SourceAdjustment,
		// The adjustment, not the settlement: the natural idempotency key is
		// (source_type, source_id, purpose), and keying on the settlement would
		// let only one adjustment per statement ever post.
		SourceID:       &adj.ID,
		SourcePublicID: adj.PublicID,
		Purpose:        "SETTLEMENT_ADJUSTMENT",
		Description:    memo,
		// SourceAdjustment postings require a reason (§34): a correction to a
		// settled figure must say why it was made, and the operator already
		// supplied one when raising it.
		Reason:      adj.Reason,
		Currency:    adj.Currency,
		PostingDate: stl.PeriodEnd,
		Legs:        legs,
		Metadata: map[string]any{
			"settlementNumber": stl.SettlementNumber,
			"settlementStatus": stl.Status,
			"adjustmentType":   adj.AdjustmentType,
			"reason":           adj.Reason,
		},
	})
	if err != nil {
		return nil, err
	}
	return &posting.Transaction.ID, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// isFrozen reports whether a statement's net has already reached the ledger.
//
// UNDER_REVIEW is deliberately *not* frozen: nothing has posted yet, so a
// correction there still belongs in the recalculation.
func isFrozen(status string) bool {
	switch status {
	case StatusApproved, StatusPartiallyPaid, StatusPaid, StatusClosed:
		return true
	}
	return false
}

func validAdjustmentType(t string) bool {
	for _, known := range AdjustmentTypes {
		if known == t {
			return true
		}
	}
	return false
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
