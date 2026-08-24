package pickup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Legal pickup-request status transitions. Small enough to read at a glance,
// which is the point: the pickup lifecycle is where a dispatcher and a field
// agent act on the same record from two different apps.
var requestTransitions = map[string][]string{
	"REQUESTED":   {"SCHEDULED", "ASSIGNED", "CANCELLED", "EXPIRED"},
	"SCHEDULED":   {"ASSIGNED", "SCHEDULED", "CANCELLED", "EXPIRED"},
	"ASSIGNED":    {"ACCEPTED", "SCHEDULED", "IN_PROGRESS", "FAILED", "CANCELLED"},
	"ACCEPTED":    {"IN_PROGRESS", "SCHEDULED", "COMPLETED", "PARTIALLY_COMPLETED", "FAILED", "CANCELLED"},
	"IN_PROGRESS": {"COMPLETED", "PARTIALLY_COMPLETED", "FAILED", "SCHEDULED"},
}

func canTransition(from, to string) bool {
	for _, allowed := range requestTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func invalidRequestState(from, to string) error {
	return apierr.Conflict("PICKUP_INVALID_STATE",
		fmt.Sprintf("A pickup request cannot move from %s to %s.", from, to)).
		WithDetail("currentStatus", from).
		WithDetail("attemptedStatus", to).
		WithDetail("allowedTransitions", requestTransitions[from])
}

// AssignInput assigns a pickup to a field agent.
type AssignInput struct {
	RequestID    string
	AgentUserID  string
	RunID        string
	StopSequence *int
}

// Assign gives a pickup request to an agent, replacing any live assignment.
//
// Reassignment is a two-step inside one transaction: close the previous
// assignment, then create the new one. The partial unique index on
// pickup_assignments enforces that only one can be live, so two dispatchers
// racing produce one winner and one conflict rather than two agents dispatched
// to the same address.
func (s *Service) Assign(ctx context.Context, p *tenant.Principal, in AssignInput) (*Detail, error) {
	agent, err := s.q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
		PublicID: in.AgentUserID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, apierr.NotFound("User")
	}
	if agent.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict, "This agent is not active.")
	}

	var detail *Detail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		req, lErr := q.LockPickupRequest(ctx, dbgen.LockPickupRequestParams{
			PublicID: in.RequestID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Pickup request")
		}
		if err := p.RequireUnitInScope(req.BranchID); err != nil {
			return err
		}
		if !canTransition(req.Status, "ASSIGNED") {
			return invalidRequestState(req.Status, "ASSIGNED")
		}

		// Close any live assignment first. REASSIGNED rather than CANCELLED so
		// the history says what happened.
		if prev, pErr := q.GetActivePickupAssignment(ctx, req.ID); pErr == nil {
			if _, uErr := q.UpdatePickupAssignmentStatus(ctx, dbgen.UpdatePickupAssignmentStatusParams{
				ID: prev.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: prev.Status, ToStatus: "REASSIGNED",
			}); uErr != nil {
				return apierr.Internal(fmt.Errorf("close previous assignment: %w", uErr))
			}
		} else if !ops.IsNoRows(pErr) {
			return apierr.Internal(pErr)
		}

		var runID *int64
		if in.RunID != "" {
			run, rErr := q.GetPickupRunByPublicID(ctx, dbgen.GetPickupRunByPublicIDParams{
				PublicID: in.RunID, OrganizationID: p.OrganizationID,
			})
			if rErr != nil {
				return ops.NotFoundOr(rErr, "Pickup run")
			}
			if run.AgentUserID != agent.ID {
				return apierr.Validation("This run belongs to a different agent.",
					map[string]any{"field": "runId"})
			}
			if run.Status == "COMPLETED" || run.Status == "CANCELLED" {
				return apierr.Conflict(apierr.CodeConflict, "This run is closed.")
			}
			runID = &run.ID
		}

		params := dbgen.CreatePickupAssignmentParams{
			PublicID: publicid.New(publicid.PrefixPickupAssignment), OrganizationID: p.OrganizationID,
			PickupRequestID: req.ID, PickupRunID: runID, AgentUserID: agent.ID,
			AssignedByUserID: &p.UserID, Metadata: []byte("{}"),
		}
		if in.StopSequence != nil {
			seq := int32(*in.StopSequence)
			params.StopSequence = &seq
		}
		assignment, aErr := q.CreatePickupAssignment(ctx, params)
		if aErr != nil {
			if ops.IsUnique(aErr, "pickup_assignments_active_idx") {
				return apierr.Conflict(apierr.CodeConflict,
					"This pickup was assigned by someone else a moment ago. Reload and try again.")
			}
			if ops.IsUnique(aErr, "pickup_assignments_run_stop_idx") {
				return apierr.Conflict(apierr.CodeConflict,
					"Another stop already occupies that position on the run.").
					WithDetail("stopSequence", in.StopSequence)
			}
			return apierr.Internal(fmt.Errorf("create assignment: %w", aErr))
		}

		if _, uErr := q.UpdatePickupRequestStatus(ctx, dbgen.UpdatePickupRequestStatusParams{
			ID: req.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: req.Status, ToStatus: "ASSIGNED",
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This pickup request changed while it was being assigned.")
		}
		if runID != nil {
			if rErr := q.RecalculatePickupRunCounters(ctx, *runID); rErr != nil {
				return apierr.Internal(rErr)
			}
		}

		// Shipments already attached follow into PICKUP_ASSIGNED.
		if err := s.moveLinkedShipments(ctx, tx, p, req, shipment.StatusPickupAssigned,
			"Pickup assigned to an agent", nil); err != nil {
			return err
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionPickupAssigned, ResourceType: "pickup_request",
			ResourceID: &req.ID, ResourcePublicID: req.PublicID,
			OperatingUnitID: &req.BranchID,
			Before:          map[string]any{"status": req.Status},
			After: map[string]any{
				"status": "ASSIGNED", "assignmentId": assignment.PublicID,
				"agent": agent.PublicID, "agentName": agent.FullName,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, req.PublicID)
		return dErr
	})
	return detail, err
}

// RespondInput is a field agent accepting or rejecting an assignment.
type RespondInput struct {
	AssignmentID string
	Accept       bool
	Reason       string
}

// Respond records an agent's acceptance or rejection.
//
// Only the assigned agent may respond. A rejection returns the request to the
// scheduling pool rather than failing it: the parcel still needs collecting.
func (s *Service) Respond(ctx context.Context, p *tenant.Principal, in RespondInput) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		assignment, aErr := q.GetPickupAssignmentByPublicID(ctx, dbgen.GetPickupAssignmentByPublicIDParams{
			PublicID: in.AssignmentID, OrganizationID: p.OrganizationID,
		})
		if aErr != nil {
			return ops.NotFoundOr(aErr, "Pickup assignment")
		}
		// The agent themself, or a dispatcher acting for them.
		if assignment.AgentUserID != p.UserID && !p.Can("pickup.assign") {
			return apierr.NotFound("Pickup assignment")
		}
		if assignment.Status != "ASSIGNED" {
			return apierr.Conflict("PICKUP_INVALID_STATE",
				"This assignment has already been responded to.").
				WithDetail("currentStatus", assignment.Status)
		}

		to, requestTo := "ACCEPTED", "ACCEPTED"
		action := audit.ActionPickupAccepted
		if !in.Accept {
			to, requestTo = "REJECTED", "SCHEDULED"
			action = audit.ActionPickupRejected
			if in.Reason == "" {
				return apierr.Validation("A rejection requires a reason.",
					map[string]any{"field": "reason"})
			}
		}

		params := dbgen.UpdatePickupAssignmentStatusParams{
			ID: assignment.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: assignment.Status, ToStatus: to,
		}
		if !in.Accept {
			params.RejectionReason = &in.Reason
		}
		if _, uErr := q.UpdatePickupAssignmentStatus(ctx, params); uErr != nil {
			return ops.ConflictOr(uErr, "This assignment changed while it was being answered.")
		}

		req, lErr := q.LockPickupRequest(ctx, dbgen.LockPickupRequestParams{
			PublicID: assignment.RequestPublicID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Pickup request")
		}
		if canTransition(req.Status, requestTo) {
			if _, uErr := q.UpdatePickupRequestStatus(ctx, dbgen.UpdatePickupRequestStatusParams{
				ID: req.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: req.Status, ToStatus: requestTo,
			}); uErr != nil {
				return ops.ConflictOr(uErr, "This pickup request changed while it was being answered.")
			}
		}
		if !in.Accept {
			// A rejected pickup goes back to PICKUP_SCHEDULED so a different
			// agent can be assigned.
			if err := s.moveLinkedShipments(ctx, tx, p, req, shipment.StatusPickupScheduled,
				"Pickup returned for reassignment", nil); err != nil {
				return err
			}
		}

		if rErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: action, ResourceType: "pickup_assignment",
			ResourceID: &assignment.ID, ResourcePublicID: assignment.PublicID,
			OperatingUnitID: &req.BranchID, Reason: in.Reason,
			After: map[string]any{"status": to, "requestStatus": requestTo},
		})); rErr != nil {
			return apierr.Internal(rErr)
		}

		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, req.PublicID)
		return dErr
	})
	return detail, err
}

// ArriveInput records an agent reaching the pickup address.
type ArriveInput struct {
	AssignmentID string
	Latitude     *float64
	Longitude    *float64
}

// Arrive marks the agent on site.
func (s *Service) Arrive(ctx context.Context, p *tenant.Principal, in ArriveInput) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		assignment, aErr := q.GetPickupAssignmentByPublicID(ctx, dbgen.GetPickupAssignmentByPublicIDParams{
			PublicID: in.AssignmentID, OrganizationID: p.OrganizationID,
		})
		if aErr != nil {
			return ops.NotFoundOr(aErr, "Pickup assignment")
		}
		if assignment.AgentUserID != p.UserID && !p.Can("pickup.assign") {
			return apierr.NotFound("Pickup assignment")
		}
		if assignment.Status != "ACCEPTED" && assignment.Status != "ASSIGNED" {
			return apierr.Conflict("PICKUP_INVALID_STATE",
				"Arrival can only be recorded on an open assignment.").
				WithDetail("currentStatus", assignment.Status)
		}
		if _, uErr := q.UpdatePickupAssignmentStatus(ctx, dbgen.UpdatePickupAssignmentStatusParams{
			ID: assignment.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: assignment.Status, ToStatus: "ARRIVED",
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This assignment changed while arrival was being recorded.")
		}

		req, lErr := q.LockPickupRequest(ctx, dbgen.LockPickupRequestParams{
			PublicID: assignment.RequestPublicID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Pickup request")
		}
		if canTransition(req.Status, "IN_PROGRESS") {
			if _, uErr := q.UpdatePickupRequestStatus(ctx, dbgen.UpdatePickupRequestStatusParams{
				ID: req.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: req.Status, ToStatus: "IN_PROGRESS",
			}); uErr != nil {
				return ops.ConflictOr(uErr, "This pickup request changed while arrival was being recorded.")
			}
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, req.PublicID)
		return dErr
	})
	return detail, err
}

// CompleteInput records the outcome of a pickup visit.
type CompleteInput struct {
	RequestID string
	Outcome   string
	// CollectedShipmentIDs names what was actually handed over. Empty on a
	// failed visit.
	CollectedShipmentIDs []string
	// Pieces collected without a pre-booked shipment, for walk-up volume.
	LoosePieces   int
	WeightGrams   *int
	FailureReason string
	Remarks       string
	OccurredAt    *time.Time
	NextAttemptAt *time.Time
	Latitude      *float64
	Longitude     *float64
	Device        ops.Device
}

// Complete records a pickup attempt and moves the collected shipments.
//
// This is the operation a field app retries over a bad connection, so it is
// idempotent on (device, deviceEventId): a duplicate upload returns the
// original attempt rather than collecting the same parcels twice.
func (s *Service) Complete(ctx context.Context, p *tenant.Principal, in CompleteInput) (*Detail, error) {
	// Offline retry check before any work.
	if in.Device.ID != "" && in.Device.EventID != "" {
		existing, err := s.q.GetPickupAttemptByDeviceEvent(ctx, dbgen.GetPickupAttemptByDeviceEventParams{
			OrganizationID: p.OrganizationID,
			DeviceID:       &in.Device.ID, DeviceEventID: &in.Device.EventID,
		})
		if err == nil {
			return s.Get(ctx, p, in.RequestID, withReplay(existing.PublicID))
		}
		if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		req, lErr := q.LockPickupRequest(ctx, dbgen.LockPickupRequestParams{
			PublicID: in.RequestID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Pickup request")
		}

		assignment, aErr := q.GetActivePickupAssignment(ctx, req.ID)
		hasAssignment := aErr == nil
		if aErr != nil && !ops.IsNoRows(aErr) {
			return apierr.Internal(aErr)
		}
		// A field agent may only complete their own pickup. A branch supervisor
		// with pickup.assign can complete on their behalf (agent's phone died,
		// parcels arrived at the counter).
		if hasAssignment && assignment.AgentUserID != p.UserID && !p.Can("pickup.assign") {
			return apierr.Forbidden("This pickup is assigned to another agent.")
		}
		if !hasAssignment {
			if err := p.RequireUnitInScope(req.BranchID); err != nil {
				return err
			}
		}

		branch, fErr := s.units.FacilityByID(ctx, p.OrganizationID, req.BranchID)
		if fErr != nil {
			return fErr
		}

		toStatus := map[string]string{
			"COMPLETED": "COMPLETED", "PARTIAL": "PARTIALLY_COMPLETED",
			"FAILED": "FAILED", "RESCHEDULED": "SCHEDULED", "CANCELLED_ON_SITE": "CANCELLED",
		}[in.Outcome]
		if toStatus == "" {
			return apierr.Validation("Unknown pickup outcome.", map[string]any{"field": "outcome"})
		}
		// A final failure only lands once the attempts are exhausted; before
		// that the request goes back into the pool.
		if in.Outcome == "FAILED" && req.AttemptCount+1 < req.MaxAttempts {
			toStatus = "SCHEDULED"
		}
		if !canTransition(req.Status, toStatus) {
			return invalidRequestState(req.Status, toStatus)
		}

		collected, cErr := s.applyCollection(ctx, tx, p, req, branch, in)
		if cErr != nil {
			return cErr
		}

		attempt, atErr := s.recordAttempt(ctx, q, p, req, assignment, hasAssignment, in, collected)
		if atErr != nil {
			return atErr
		}

		update := dbgen.UpdatePickupRequestStatusParams{
			ID: req.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: req.Status, ToStatus: toStatus,
			AttemptIncrement:   1,
			ActualPieceCount:   ops.Ptr(int32(len(collected) + in.LoosePieces)),
			CancellationReason: nil,
		}
		if in.Outcome == "FAILED" || in.Outcome == "RESCHEDULED" {
			update.FailureReason = ops.Optional(in.FailureReason)
		}
		if in.Outcome == "CANCELLED_ON_SITE" {
			update.CancellationReason = ops.Optional(orDefault(in.Remarks, "Cancelled by the customer at the door"))
		}
		if in.NextAttemptAt != nil {
			d := in.NextAttemptAt.Truncate(24 * time.Hour)
			update.ScheduledDate = &d
			update.WindowStart = in.NextAttemptAt
			end := in.NextAttemptAt.Add(4 * time.Hour)
			update.WindowEnd = &end
		}
		if _, uErr := q.UpdatePickupRequestStatus(ctx, update); uErr != nil {
			return ops.ConflictOr(uErr, "This pickup request changed while the visit was being recorded.")
		}

		if hasAssignment {
			assignTo := "COMPLETED"
			if in.Outcome == "FAILED" || in.Outcome == "RESCHEDULED" {
				assignTo = "FAILED"
			}
			if _, uErr := q.UpdatePickupAssignmentStatus(ctx, dbgen.UpdatePickupAssignmentStatusParams{
				ID: assignment.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: assignment.Status, ToStatus: assignTo,
			}); uErr != nil {
				return ops.ConflictOr(uErr, "This assignment changed while the visit was being recorded.")
			}
			if assignment.PickupRunID != nil {
				if rErr := q.RecalculatePickupRunCounters(ctx, *assignment.PickupRunID); rErr != nil {
					return apierr.Internal(rErr)
				}
			}
		}

		action := audit.ActionPickupCompleted
		if in.Outcome == "FAILED" || in.Outcome == "RESCHEDULED" {
			action = audit.ActionPickupFailed
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: action, ResourceType: "pickup_request",
			ResourceID: &req.ID, ResourcePublicID: req.PublicID,
			OperatingUnitID: &req.BranchID, Reason: in.FailureReason,
			Before: map[string]any{"status": req.Status},
			After: map[string]any{
				"status": toStatus, "outcome": in.Outcome, "attemptId": attempt.PublicID,
				"piecesCollected":   len(collected) + in.LoosePieces,
				"shipmentsPickedUp": len(collected),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		if toStatus == "COMPLETED" || toStatus == "PARTIALLY_COMPLETED" {
			if oErr := s.runCompletionObservers(ctx, tx, p, CompletionEvent{
				Request: req, Attempt: attempt, Status: toStatus, Shipments: collected,
			}); oErr != nil {
				return oErr
			}
		}

		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, req.PublicID)
		return dErr
	})
	return detail, err
}

// applyCollection moves the named shipments to PICKED_UP and resolves the rest.
//
// The shipments actually handed over are the ones the agent scanned. Anything
// else on the request is marked NOT_AVAILABLE and stays where it is — which is
// the difference between a pickup record that reflects reality and one that
// merely reflects the plan.
func (s *Service) applyCollection(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	req dbgen.PickupRequest, branch *ops.Facility, in CompleteInput,
) ([]dbgen.Shipment, error) {
	q := s.q.WithTx(tx)
	linked, err := q.ListPickupRequestShipments(ctx, req.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	wanted := make(map[string]bool, len(in.CollectedShipmentIDs))
	for _, id := range in.CollectedShipmentIDs {
		wanted[id] = true
	}

	actor := in.Device.Actor(p, branch)
	actor = ops.WithPosition(actor, in.Latitude, in.Longitude)

	var collected []dbgen.Shipment
	for _, item := range linked {
		if item.Status != "PENDING" {
			continue
		}
		if !wanted[item.ShipmentPublicID] {
			if in.Outcome == "COMPLETED" || in.Outcome == "PARTIAL" {
				if _, uErr := q.UpdatePickupRequestShipmentStatus(ctx,
					dbgen.UpdatePickupRequestShipmentStatusParams{
						PickupRequestID: req.ID, ShipmentID: item.ShipmentID,
						Status:  "NOT_AVAILABLE",
						Remarks: ops.Optional("Not handed over at the pickup visit"),
					}); uErr != nil {
					return nil, apierr.Internal(uErr)
				}
			}
			continue
		}
		delete(wanted, item.ShipmentPublicID)

		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: item.ShipmentID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return nil, apierr.Internal(lErr)
		}
		// Custody passes to the agent, and the parcel is now the branch's
		// responsibility even though it is physically in a van.
		res, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
			Shipment: sh, To: shipment.StatusPickedUp,
			Description:    "Picked up",
			OccurredAt:     in.OccurredAt,
			SetCustodyUser: true, CustodyUserID: &p.UserID,
			SetCustodyUnit: true, CustodyUnitID: &branch.ID,
			IncrementPickupAttempt: true,
			IdempotencyKey:         pickupEventKey(in.Device, item.ShipmentPublicID),
			Metadata: map[string]any{
				"pickupRequest": req.ReferenceCode, "branchCode": branch.Code,
			},
		})
		if tErr != nil {
			return nil, tErr
		}
		if _, uErr := q.UpdatePickupRequestShipmentStatus(ctx,
			dbgen.UpdatePickupRequestShipmentStatusParams{
				PickupRequestID: req.ID, ShipmentID: item.ShipmentID, Status: "PICKED_UP",
			}); uErr != nil {
			return nil, apierr.Internal(uErr)
		}
		collected = append(collected, res.Shipment)
	}

	// Anything named as collected that was not on the request is a client bug
	// or an attempt to reach a shipment through the wrong door.
	for id := range wanted {
		return nil, apierr.Validation("This shipment is not on the pickup request.",
			map[string]any{"field": "collectedShipmentIds", "shipmentId": id})
	}
	return collected, nil
}

func (s *Service) recordAttempt(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal,
	req dbgen.PickupRequest, assignment dbgen.PickupAssignment, hasAssignment bool,
	in CompleteInput, collected []dbgen.Shipment,
) (dbgen.PickupAttempt, error) {
	params := dbgen.CreatePickupAttemptParams{
		PublicID: publicid.New(publicid.PrefixPickupAttempt), OrganizationID: p.OrganizationID,
		PickupRequestID: req.ID, AttemptNumber: req.AttemptCount + 1,
		Outcome:         in.Outcome,
		Remarks:         ops.Optional(in.Remarks),
		PiecesCollected: int32(len(collected) + in.LoosePieces),
		AgentUserID:     &p.UserID,
		OccurredAt:      in.OccurredAt,
		Latitude:        in.Latitude, Longitude: in.Longitude,
		DeviceID: ops.Optional(in.Device.ID), DeviceEventID: ops.Optional(in.Device.EventID),
		NextAttemptAt: in.NextAttemptAt,
		Metadata:      []byte("{}"),
	}
	if hasAssignment {
		params.PickupAssignmentID = &assignment.ID
	}
	if in.FailureReason != "" {
		params.FailureReasonCode = &in.FailureReason
	}
	if in.WeightGrams != nil {
		g := int32(*in.WeightGrams)
		params.WeightGrams = &g
	}
	attempt, err := q.CreatePickupAttempt(ctx, params)
	if err != nil {
		if ops.IsUnique(err, "pickup_attempts_device_event_idx") {
			return attempt, apierr.Conflict(apierr.CodeDuplicate,
				"This pickup visit was already recorded.")
		}
		if ops.IsUnique(err, "pickup_attempts_number_unique") {
			return attempt, apierr.Conflict(apierr.CodeConcurrentModification,
				"Another visit was recorded for this pickup a moment ago. Reload and try again.")
		}
		return attempt, apierr.Internal(fmt.Errorf("record pickup attempt: %w", err))
	}
	return attempt, nil
}

// moveLinkedShipments advances every pending shipment on a request.
func (s *Service) moveLinkedShipments(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, req dbgen.PickupRequest,
	to shipment.Status, description string, facility *ops.Facility,
) error {
	q := s.q.WithTx(tx)
	linked, err := q.ListPickupRequestShipments(ctx, req.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	if facility == nil {
		facility, err = s.units.FacilityByID(ctx, p.OrganizationID, req.BranchID)
		if err != nil {
			return err
		}
	}
	actor := ops.Device{Source: "API"}.Actor(p, facility)
	for _, item := range linked {
		if item.Status != "PENDING" {
			continue
		}
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: item.ShipmentID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return apierr.Internal(lErr)
		}
		if shipment.Status(sh.CurrentStatus) == to {
			continue
		}
		if _, fErr := shipment.Find(shipment.Status(sh.CurrentStatus), to); fErr != nil {
			// Not every linked shipment can follow — one may already have been
			// received at the branch by another route. Skipping is correct; the
			// pickup does not own that shipment's lifecycle.
			continue
		}
		if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
			Shipment: sh, To: to, Description: description,
			Metadata: map[string]any{"pickupRequest": req.ReferenceCode},
		}); tErr != nil {
			return tErr
		}
	}
	return nil
}

// Cancel withdraws a pickup request.
func (s *Service) Cancel(ctx context.Context, p *tenant.Principal, requestID, reason string) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		req, lErr := q.LockPickupRequest(ctx, dbgen.LockPickupRequestParams{
			PublicID: requestID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Pickup request")
		}
		if !p.IsPortalUser {
			if err := p.RequireUnitInScope(req.BranchID); err != nil {
				return err
			}
		} else if !s.customerOwnsRequest(ctx, q, p, req) {
			return apierr.NotFound("Pickup request")
		}
		if !canTransition(req.Status, "CANCELLED") {
			return invalidRequestState(req.Status, "CANCELLED")
		}

		if prev, pErr := q.GetActivePickupAssignment(ctx, req.ID); pErr == nil {
			if _, uErr := q.UpdatePickupAssignmentStatus(ctx, dbgen.UpdatePickupAssignmentStatusParams{
				ID: prev.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: prev.Status, ToStatus: "CANCELLED",
			}); uErr != nil {
				return apierr.Internal(uErr)
			}
		} else if !ops.IsNoRows(pErr) {
			return apierr.Internal(pErr)
		}

		if _, uErr := q.UpdatePickupRequestStatus(ctx, dbgen.UpdatePickupRequestStatusParams{
			ID: req.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: req.Status, ToStatus: "CANCELLED",
			CancellationReason: &reason,
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This pickup request changed while it was being cancelled.")
		}

		// Linked shipments go back to BOOKED: cancelling the collection does not
		// cancel the shipment.
		if err := s.moveLinkedShipments(ctx, tx, p, req, shipment.StatusBooked,
			"Pickup cancelled", nil); err != nil {
			return err
		}
		for _, item := range mustList(ctx, q, req.ID) {
			if item.Status == "PENDING" {
				if _, uErr := q.UpdatePickupRequestShipmentStatus(ctx,
					dbgen.UpdatePickupRequestShipmentStatusParams{
						PickupRequestID: req.ID, ShipmentID: item.ShipmentID,
						Status: "CANCELLED", Remarks: ops.Optional(reason),
					}); uErr != nil {
					return apierr.Internal(uErr)
				}
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionPickupCancelled, ResourceType: "pickup_request",
			ResourceID: &req.ID, ResourcePublicID: req.PublicID,
			OperatingUnitID: &req.BranchID, Reason: reason,
			Before: map[string]any{"status": req.Status},
			After:  map[string]any{"status": "CANCELLED"},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, req.PublicID)
		return dErr
	})
	return detail, err
}

func (s *Service) customerOwnsRequest(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, req dbgen.PickupRequest,
) bool {
	return p.IsCustomerInScope(req.CustomerID)
}

func mustList(ctx context.Context, q *dbgen.Queries, requestID int64) []dbgen.ListPickupRequestShipmentsRow {
	rows, err := q.ListPickupRequestShipments(ctx, requestID)
	if err != nil {
		return nil
	}
	return rows
}

// pickupEventKey makes the shipment-level PICKED_UP event idempotent per device
// event, so a retried completion cannot append a second event.
func pickupEventKey(d ops.Device, shipmentPublicID string) string {
	if d.ID == "" || d.EventID == "" {
		return ""
	}
	return "pickup:" + d.EventID + ":" + shipmentPublicID
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// CreateRunInput plans an agent's pickup route for a day.
type CreateRunInput struct {
	Branch      *ops.Facility
	AgentUserID int64
	RunDate     time.Time
	Vehicle     string
}

// CreateRun opens a pickup run.
func (s *Service) CreateRun(ctx context.Context, p *tenant.Principal, in CreateRunInput) (*RunDetail, error) {
	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindPickupRun, in.Branch.Code, time.Now())
	if err != nil {
		return nil, err
	}
	var out *RunDetail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		run, cErr := q.CreatePickupRun(ctx, dbgen.CreatePickupRunParams{
			PublicID: publicid.New(publicid.PrefixPickupRun), OrganizationID: p.OrganizationID,
			RunCode: code, BranchID: in.Branch.ID, AgentUserID: in.AgentUserID,
			RunDate: in.RunDate, VehicleReference: ops.Optional(in.Vehicle),
			CreatedByUserID: &p.UserID, Metadata: []byte("{}"),
		})
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create pickup run: %w", cErr))
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionPickupRunCreated, ResourceType: "pickup_run",
			ResourceID: &run.ID, ResourcePublicID: run.PublicID,
			OperatingUnitID: &in.Branch.ID,
			After: map[string]any{
				"runCode": code, "branch": in.Branch.Code,
				"runDate": in.RunDate.Format("2006-01-02"),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		out = &RunDetail{
			ID: run.PublicID, RunCode: run.RunCode, Status: run.Status,
			RunDate:   run.RunDate.Format("2006-01-02"),
			Branch:    ops.RefOf(in.Branch),
			Vehicle:   ops.Deref(run.VehicleReference),
			CreatedAt: run.CreatedAt,
		}
		return nil
	})
	return out, err
}

// RunDetail is a pickup run.
type RunDetail struct {
	ID             string    `json:"id"`
	RunCode        string    `json:"runCode"`
	Status         string    `json:"status"`
	RunDate        string    `json:"runDate"`
	Branch         *ops.Ref  `json:"branch"`
	Agent          *ops.Ref  `json:"agent,omitempty"`
	Vehicle        string    `json:"vehicleReference,omitempty"`
	PlannedStops   int       `json:"plannedStops"`
	CompletedStops int       `json:"completedStops"`
	Pieces         int       `json:"collectedPieces"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ListRuns returns pickup runs visible to the caller.
func (s *Service) ListRuns(
	ctx context.Context, p *tenant.Principal, status string, date *time.Time, limit, offset int,
) ([]RunDetail, error) {
	params := dbgen.ListPickupRunsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(limit), RowOffset: int32(offset),
		UnitIds: p.UnitScope("shipment.read_all"), RunDate: date,
	}
	if status != "" {
		params.Status = &status
	}
	rows, err := s.q.ListPickupRuns(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]RunDetail, 0, len(rows))
	for _, r := range rows {
		out = append(out, RunDetail{
			ID: r.PublicID, RunCode: r.RunCode, Status: r.Status,
			RunDate:      r.RunDate.Format("2006-01-02"),
			Branch:       &ops.Ref{Code: r.BranchCode, Name: r.BranchName},
			Agent:        &ops.Ref{ID: r.AgentPublicID, Name: r.AgentName},
			PlannedStops: int(r.PlannedStops), CompletedStops: int(r.CompletedStops),
			Pieces: int(r.CollectedPieces), CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
