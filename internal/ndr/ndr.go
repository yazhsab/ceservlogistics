// Package ndr implements M17: non-delivery reports as a workflow, not a status.
//
// A failed delivery is the start of a conversation, not the end of one. The
// case holds that conversation: what went wrong, what was decided, who decided
// it, and when the next attempt is due. The parts:
//
//	ndr_reasons   configurable per tenant, with a default action and an
//	              attempt budget — the policy
//	ndr_cases     one open case per shipment — the state
//	ndr_attempts  append-only record of each failed visit — the history
//	ndr_actions   the instruction currently in force — the decision
//
// Separating the decision from the attempt matters: a customer can change their
// mind between attempts, operations can escalate, and the record has to show
// which instruction the agent was acting on when they knocked.
package ndr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Actions an NDR case can take, from Constitution §18.
var Actions = []string{
	"REATTEMPT", "RESCHEDULE", "CONTACT_REQUIRED", "ADDRESS_CORRECTION",
	"CUSTOMER_PICKUP", "RTO", "ESCALATE",
}

// Case states.
const (
	StatusOpen             = "OPEN"
	StatusPendingCustomer  = "PENDING_CUSTOMER"
	StatusScheduled        = "SCHEDULED"
	StatusEscalated        = "ESCALATED"
	StatusResolvedDeliverd = "RESOLVED_DELIVERED"
	StatusResolvedRTO      = "RESOLVED_RTO"
	StatusResolvedCancel   = "RESOLVED_CANCELLED"
	StatusClosed           = "CLOSED"
)

// AllStatuses is the published case lifecycle.
var AllStatuses = []string{
	StatusOpen, StatusPendingCustomer, StatusScheduled, StatusEscalated,
	StatusResolvedDeliverd, StatusResolvedRTO, StatusResolvedCancel, StatusClosed,
}

// openStatuses are the states in which a case is still live.
var openStatuses = map[string]bool{
	StatusOpen: true, StatusPendingCustomer: true,
	StatusScheduled: true, StatusEscalated: true,
}

// Service implements the NDR workflow.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	units   *ops.Resolver
	codes   *ops.CodeAllocator
	trans   *shipment.Transitioner
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics
	// rto is installed by the api wiring; an NDR case whose action is RTO hands
	// off to the RTO module.
	rto RTOInitiator
}

// RTOInitiator starts a return from an NDR decision.
//
// byPolicy separates an automatic return — the attempt budget ran out — from
// one an operator chose. Only the second needs the caller to hold rto.manage.
type RTOInitiator func(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, ndrCaseID *int64, reasonCode, notes string, byPolicy bool,
) (publicID string, err error)

// NewService builds the NDR service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics) *Service {
	return &Service{db: db, q: q, units: units, codes: codes, trans: trans, audit: rec, log: log, metrics: m}
}

// SetRTOInitiator installs the RTO hook.
func (s *Service) SetRTOInitiator(f RTOInitiator) { s.rto = f }

// OpenFromDeliveryFailure is the hook the delivery module calls.
//
// It runs inside the delivery transaction so that a failed attempt and the case
// it opens either both land or neither does. If they could diverge, a parcel
// would sit at DELIVERY_FAILED with nobody owning the next decision.
func (s *Service) OpenFromDeliveryFailure(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, attempt dbgen.DeliveryAttempt, reasonCode, remarks string, branchID int64,
) (result string, result2 *time.Time, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("ndr", "failed")
			return
		}
		s.metrics.RecordBusiness("ndr", "opened")
	}()

	q := s.q.WithTx(tx)

	reason, err := s.resolveReason(ctx, q, p.OrganizationID, reasonCode)
	if err != nil {
		return "", nil, err
	}

	existing, err := q.GetActiveNDRCase(ctx, sh.ID)
	hasCase := err == nil
	if err != nil && !ops.IsNoRows(err) {
		return "", nil, apierr.Internal(err)
	}

	var caseRow dbgen.NdrCase
	if hasCase {
		caseRow = existing
	} else {
		code, cErr := s.codes.Allocate(ctx, p.OrganizationID, ops.KindNDRCase, "NDR", time.Now())
		if cErr != nil {
			return "", nil, cErr
		}
		params := dbgen.CreateNDRCaseParams{
			PublicID: publicid.New(publicid.PrefixNDRCase), OrganizationID: p.OrganizationID,
			CaseCode: code, ShipmentID: sh.ID, CurrentReasonCode: reason.Code,
			MaxAttempts: reason.MaxAttempts, BranchID: &branchID,
			Metadata: []byte("{}"),
		}
		params.NdrReasonID = &reason.ID
		created, crErr := q.CreateNDRCase(ctx, params)
		if crErr != nil {
			if ops.IsUnique(crErr, "ndr_cases_active_idx") {
				return "", nil, apierr.Conflict(apierr.CodeConcurrentModification,
					"An NDR case was opened for this shipment a moment ago. Reload and try again.")
			}
			return "", nil, apierr.Internal(fmt.Errorf("open ndr case: %w", crErr))
		}
		caseRow = created
	}

	// Record the attempt against the case.
	attemptNumber := caseRow.AttemptCount + 1
	if _, aErr := q.CreateNDRAttempt(ctx, dbgen.CreateNDRAttemptParams{
		PublicID: publicid.New(publicid.PrefixNDRAttempt), OrganizationID: p.OrganizationID,
		NdrCaseID: caseRow.ID, DeliveryAttemptID: &attempt.ID,
		AttemptNumber: attemptNumber, ReasonCode: reason.Code,
		Remarks: ops.Optional(remarks), OccurredAt: &attempt.OccurredAt,
		RecordedByUserID: &p.UserID, EvidenceObjectKeys: []string{},
		Metadata: []byte("{}"),
	}); aErr != nil {
		if ops.IsUnique(aErr, "ndr_attempts_number_unique") {
			return "", nil, apierr.Conflict(apierr.CodeConcurrentModification,
				"Another attempt was recorded against this case a moment ago.")
		}
		return "", nil, apierr.Internal(fmt.Errorf("record ndr attempt: %w", aErr))
	}

	// Decide the default next action from the reason's policy.
	action := reason.DefaultAction
	nextStatus := StatusOpen
	var nextAt *time.Time

	exhausted := attemptNumber >= caseRow.MaxAttempts
	switch {
	case exhausted && reason.AutoRtoAfterMax:
		action = "RTO"
	case exhausted:
		action = "ESCALATE"
		nextStatus = StatusEscalated
	}
	if action == "REATTEMPT" || action == "RESCHEDULE" {
		// Next working slot, kept deliberately simple: tomorrow. A calendar-aware
		// scheduler is a Release 4 concern; what matters now is that the case has
		// a due date so it appears on somebody's list.
		t := time.Now().Add(24 * time.Hour)
		nextAt = &t
		nextStatus = StatusScheduled
	}
	if action == "CONTACT_REQUIRED" || action == "ADDRESS_CORRECTION" || action == "CUSTOMER_PICKUP" {
		nextStatus = StatusPendingCustomer
	}

	updated, uErr := q.UpdateNDRCase(ctx, dbgen.UpdateNDRCaseParams{
		ID: caseRow.ID, OrganizationID: p.OrganizationID,
		ExpectedStatus: caseRow.Status, ToStatus: nextStatus,
		CurrentReasonCode: &reason.Code, NdrReasonID: &reason.ID,
		CurrentAction: &action, NextAttemptAt: nextAt,
		AttemptIncrement: 1,
	})
	if uErr != nil {
		return "", nil, ops.ConflictOr(uErr, "This NDR case changed while it was being updated.")
	}

	// Record the instruction as an action, superseding anything pending.
	if _, sErr := q.SupersedePendingNDRActions(ctx, caseRow.ID); sErr != nil {
		return "", nil, apierr.Internal(sErr)
	}
	seq, sqErr := q.NextNDRActionSequence(ctx, caseRow.ID)
	if sqErr != nil {
		return "", nil, apierr.Internal(sqErr)
	}
	if _, acErr := q.CreateNDRAction(ctx, dbgen.CreateNDRActionParams{
		PublicID: publicid.New(publicid.PrefixNDRAction), OrganizationID: p.OrganizationID,
		NdrCaseID: caseRow.ID, Sequence: seq, Action: action, RequestedBy: "SYSTEM",
		Instructions: ops.Optional(defaultInstruction(action, reason.Name)),
		ScheduledFor: nextAt, CreatedByUserID: &p.UserID, Metadata: []byte("{}"),
	}); acErr != nil {
		return "", nil, apierr.Internal(fmt.Errorf("record ndr action: %w", acErr))
	}

	// The shipment enters NDR so the branch queue shows it as needing a decision.
	if _, tErr := s.trans.Apply(ctx, tx, ops.Device{Source: "API"}.Actor(p, nil), shipment.Request{
		Shipment: sh, To: shipment.StatusNDR,
		Description: "Delivery exception: " + reason.Name,
		ReasonCode:  reason.Code, InternalRemarks: remarks,
		Metadata: map[string]any{
			"ndrCase": updated.CaseCode, "reasonCode": reason.Code, "action": action,
			"attemptNumber": attemptNumber, "maxAttempts": caseRow.MaxAttempts,
		},
	}); tErr != nil {
		return "", nil, tErr
	}

	// An exhausted case whose policy says so goes straight to RTO.
	if action == "RTO" && s.rto != nil {
		locked, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: sh.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return "", nil, apierr.Internal(lErr)
		}
		if _, rErr := s.rto(ctx, tx, p, locked, &caseRow.ID, reason.Code,
			fmt.Sprintf("Automatic RTO after %d failed attempts", attemptNumber), true); rErr != nil {
			return "", nil, rErr
		}
		if _, cErr := q.UpdateNDRCase(ctx, dbgen.UpdateNDRCaseParams{
			ID: caseRow.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: updated.Status, ToStatus: StatusResolvedRTO,
			ResolutionNotes: ops.Optional("Returned to sender after exhausting delivery attempts"),
		}); cErr != nil && !ops.IsNoRows(cErr) {
			return "", nil, apierr.Internal(cErr)
		}
	}

	if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionNDRAttempted, ResourceType: "ndr_case",
		ResourceID: &caseRow.ID, ResourcePublicID: updated.PublicID,
		OperatingUnitID: &branchID, Reason: reason.Code,
		After: map[string]any{
			"caseCode": updated.CaseCode, "awb": sh.Awb, "reasonCode": reason.Code,
			"attemptNumber": attemptNumber, "maxAttempts": caseRow.MaxAttempts,
			"action": action, "status": nextStatus,
		},
	})); aErr != nil {
		return "", nil, apierr.Internal(aErr)
	}
	return updated.PublicID, nextAt, nil
}

// resolveReason looks up a configured reason, falling back to a safe default.
func (s *Service) resolveReason(
	ctx context.Context, q *dbgen.Queries, orgID int64, code string,
) (dbgen.NdrReason, error) {
	if code != "" {
		reason, err := q.GetNDRReasonByCode(ctx, dbgen.GetNDRReasonByCodeParams{
			OrganizationID: orgID, Code: code,
		})
		if err == nil {
			if !reason.IsActive {
				return reason, apierr.Validation("This NDR reason is no longer in use.",
					map[string]any{"field": "failureReasonCode", "reasonCode": code})
			}
			return reason, nil
		}
		if !ops.IsNoRows(err) {
			return dbgen.NdrReason{}, apierr.Internal(err)
		}
		return dbgen.NdrReason{}, apierr.Validation("Unknown NDR reason.",
			map[string]any{"field": "failureReasonCode", "reasonCode": code})
	}
	// No reason supplied: fall back to the catalogue's generic entry so the case
	// still has a policy rather than none.
	reason, err := q.GetNDRReasonByCode(ctx, dbgen.GetNDRReasonByCodeParams{
		OrganizationID: orgID, Code: "CUSTOMER_NOT_AVAILABLE",
	})
	if err != nil {
		return dbgen.NdrReason{}, apierr.Validation("A delivery failure requires a reason code.",
			map[string]any{"field": "failureReasonCode"})
	}
	return reason, nil
}

// SetActionInput records an operator's or customer's decision.
type SetActionInput struct {
	CaseID       string
	Action       string
	RequestedBy  string
	Instructions string
	ScheduledFor *time.Time
	ContactName  string
	ContactPhone string
	ContactNotes string
	// Address correction fields, used when Action is ADDRESS_CORRECTION.
	Line1    string
	Line2    string
	Landmark string
	Pincode  string
	Phone    string
	AssignTo string
}

// SetAction sets the instruction currently in force on a case.
//
// The correction never rewrites the shipment's address snapshot (§13: booking
// snapshots are append-only). It is stored on the case, and the delivery agent
// is shown the corrected address alongside the original, so the record of where
// the parcel was originally addressed survives.
func (s *Service) SetAction(ctx context.Context, p *tenant.Principal, in SetActionInput) (*CaseDetail, error) {
	var detail *CaseDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, cErr := q.LockNDRCaseForUpdate(ctx, dbgen.LockNDRCaseForUpdateParams{
			PublicID: in.CaseID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			return ops.NotFoundOr(cErr, "NDR case")
		}
		if !openStatuses[c.Status] {
			return apierr.Conflict("NDR_CASE_CLOSED",
				"This NDR case is already resolved.").WithDetail("currentStatus", c.Status)
		}
		if c.BranchID != nil && !p.IsPortalUser {
			if err := p.RequireUnitInScope(*c.BranchID); err != nil {
				return err
			}
		}

		sh, sErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: c.ShipmentID, OrganizationID: p.OrganizationID,
		})
		if sErr != nil {
			return ops.NotFoundOr(sErr, "Shipment")
		}
		if p.IsPortalUser && !p.IsCustomerInScope(sh.CustomerID) {
			return apierr.NotFound("NDR case")
		}

		if in.Action == "ADDRESS_CORRECTION" {
			if in.Line1 == "" && in.Pincode == "" && in.Phone == "" {
				return apierr.Validation(
					"An address correction needs at least one corrected field.",
					map[string]any{"field": "correctedAddress"})
			}
			if _, aErr := q.ApplyNDRAddressCorrection(ctx, dbgen.ApplyNDRAddressCorrectionParams{
				ID: c.ID, OrganizationID: p.OrganizationID,
				Line1: ops.Optional(in.Line1), Line2: ops.Optional(in.Line2),
				Landmark: ops.Optional(in.Landmark), Pincode: ops.Optional(in.Pincode),
				Phone: ops.Optional(in.Phone), CorrectedByUserID: &p.UserID,
			}); aErr != nil {
				return apierr.Internal(aErr)
			}
		}

		nextStatus := statusForAction(in.Action)
		var nextAt *time.Time
		if in.ScheduledFor != nil {
			nextAt = in.ScheduledFor
		} else if in.Action == "REATTEMPT" {
			t := time.Now().Add(24 * time.Hour)
			nextAt = &t
		}

		if _, supErr := q.SupersedePendingNDRActions(ctx, c.ID); supErr != nil {
			return apierr.Internal(supErr)
		}
		seq, sqErr := q.NextNDRActionSequence(ctx, c.ID)
		if sqErr != nil {
			return apierr.Internal(sqErr)
		}
		if _, acErr := q.CreateNDRAction(ctx, dbgen.CreateNDRActionParams{
			PublicID: publicid.New(publicid.PrefixNDRAction), OrganizationID: p.OrganizationID,
			NdrCaseID: c.ID, Sequence: seq, Action: in.Action,
			RequestedBy:  orDefault(in.RequestedBy, requesterFor(p)),
			Instructions: ops.Optional(in.Instructions), ScheduledFor: nextAt,
			ContactName: ops.Optional(in.ContactName), ContactPhone: ops.Optional(in.ContactPhone),
			ContactNotes:    ops.Optional(in.ContactNotes),
			CreatedByUserID: &p.UserID, Metadata: []byte("{}"),
		}); acErr != nil {
			return apierr.Internal(fmt.Errorf("record ndr action: %w", acErr))
		}

		params := dbgen.UpdateNDRCaseParams{
			ID: c.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: c.Status, ToStatus: nextStatus,
			CurrentAction: &in.Action, NextAttemptAt: nextAt,
			ClearNextAttempt: nextAt == nil && in.Action != "REATTEMPT",
		}
		if in.AssignTo != "" {
			u, uErr := q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
				PublicID: in.AssignTo, OrganizationID: p.OrganizationID,
			})
			if uErr != nil {
				return ops.NotFoundOr(uErr, "User")
			}
			params.AssignedToUserID = &u.ID
		}
		updated, uErr := q.UpdateNDRCase(ctx, params)
		if uErr != nil {
			return ops.ConflictOr(uErr, "This NDR case changed while it was being updated.")
		}

		// A REATTEMPT puts the parcel back on the branch shelf so it can be
		// loaded onto a run; RTO hands off to the returns workflow.
		switch in.Action {
		case "REATTEMPT", "RESCHEDULE":
			if _, fErr := shipment.Find(shipment.Status(sh.CurrentStatus),
				shipment.StatusDestinationBranchReceived); fErr == nil {
				if _, tErr := s.trans.Apply(ctx, tx, ops.Device{Source: "API"}.Actor(p, nil),
					shipment.Request{
						Shipment: sh, To: shipment.StatusDestinationBranchReceived,
						Description: "Scheduled for another delivery attempt",
						Metadata:    map[string]any{"ndrCase": c.CaseCode, "action": in.Action},
					}); tErr != nil {
					return tErr
				}
			}
		case "RTO":
			if s.rto == nil {
				return apierr.Internal(fmt.Errorf("rto initiator not configured"))
			}
			if _, rErr := s.rto(ctx, tx, p, sh, &c.ID, c.CurrentReasonCode,
				orDefault(in.Instructions, "Return requested from the NDR workflow"), false); rErr != nil {
				return rErr
			}
			if _, clErr := q.UpdateNDRCase(ctx, dbgen.UpdateNDRCaseParams{
				ID: c.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: updated.Status, ToStatus: StatusResolvedRTO,
				ResolutionNotes: ops.Optional("Return to sender started"),
			}); clErr != nil && !ops.IsNoRows(clErr) {
				return apierr.Internal(clErr)
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: auditActionFor(in.Action), ResourceType: "ndr_case",
			ResourceID: &c.ID, ResourcePublicID: c.PublicID,
			OperatingUnitID: c.BranchID, Reason: in.Instructions,
			Before: map[string]any{"status": c.Status, "action": ops.Deref(c.CurrentAction)},
			After: map[string]any{
				"status": nextStatus, "action": in.Action, "caseCode": c.CaseCode,
				"scheduledFor": nextAt, "requestedBy": in.RequestedBy,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var dErr error
		detail, dErr = s.loadCase(ctx, q, p, c.PublicID)
		return dErr
	})
	return detail, err
}

func statusForAction(action string) string {
	switch action {
	case "REATTEMPT", "RESCHEDULE":
		return StatusScheduled
	case "CONTACT_REQUIRED", "ADDRESS_CORRECTION", "CUSTOMER_PICKUP":
		return StatusPendingCustomer
	case "ESCALATE":
		return StatusEscalated
	default:
		return StatusOpen
	}
}

func auditActionFor(action string) string {
	if action == "ADDRESS_CORRECTION" {
		return audit.ActionNDRAddressFixed
	}
	return audit.ActionNDRActionSet
}

func requesterFor(p *tenant.Principal) string {
	if p.IsPortalUser {
		return "CUSTOMER"
	}
	return "OPERATIONS"
}

func defaultInstruction(action, reasonName string) string {
	switch action {
	case "REATTEMPT":
		return "Attempt delivery again: " + reasonName
	case "RESCHEDULE":
		return "Reschedule with the consignee: " + reasonName
	case "CONTACT_REQUIRED":
		return "Contact the consignee before the next attempt: " + reasonName
	case "ADDRESS_CORRECTION":
		return "Obtain a corrected address: " + reasonName
	case "CUSTOMER_PICKUP":
		return "Hold at the branch for collection: " + reasonName
	case "RTO":
		return "Return to sender: " + reasonName
	case "ESCALATE":
		return "Escalate to a supervisor: " + reasonName
	default:
		return reasonName
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func encodeJSON(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
