package delivery

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

// AttemptInput records the outcome of a delivery attempt.
type AttemptInput struct {
	Barcode   string
	Outcome   string
	RunID     string
	Facility  *ops.Facility
	Device    ops.Device
	Latitude  *float64
	Longitude *float64
	Accuracy  *int

	RecipientName         string
	RecipientRelationship string
	RecipientPhone        string

	OTP string

	CODCollectedMinor int64
	CODPaymentMode    string
	CODReference      string

	FailureReasonCode string
	Remarks           string
	OccurredAt        *time.Time
	NextAttemptAt     *time.Time
}

// AttemptResult reports what one attempt produced.
type AttemptResult struct {
	AttemptID     string     `json:"attemptId"`
	AttemptNumber int        `json:"attemptNumber"`
	ShipmentID    string     `json:"shipmentId"`
	AWB           string     `json:"awb"`
	Outcome       string     `json:"outcome"`
	FromStatus    string     `json:"fromStatus"`
	ToStatus      string     `json:"toStatus"`
	OccurredAt    time.Time  `json:"occurredAt"`
	CODCollected  int64      `json:"codCollectedMinor"`
	Currency      string     `json:"currency"`
	NDRCaseID     string     `json:"ndrCaseId,omitempty"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	// Replayed is true when an offline retry returned the original attempt.
	Replayed bool   `json:"replayed"`
	Next     string `json:"nextAction,omitempty"`
}

// NDROpener creates an NDR case from a failed delivery. It is supplied by the
// api wiring so the delivery module does not import the ndr package and create
// a cycle: ndr already needs delivery's types to close a case.
type NDROpener func(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, attempt dbgen.DeliveryAttempt, reasonCode, remarks string,
	branchID int64,
) (publicID string, nextAttemptAt *time.Time, err error)

// SetNDROpener installs the NDR hook.
func (s *Service) SetNDROpener(f NDROpener) { s.ndr = f }

// RecordAttempt records a delivery success or failure.
//
// This is the single most concurrency-sensitive call in the system, so the
// order is deliberate:
//
//  1. Check the device event key before doing anything — a retry must be free.
//  2. Lock the shipment row, which serialises two agents on the same parcel.
//  3. Verify the OTP inside the same transaction, so a code cannot be spent
//     twice by two simultaneous requests.
//  4. Insert the attempt. The partial unique index on successful attempts is
//     the last line of defence against a double delivery.
//  5. Transition the shipment, with CustodyAgent proving it was this agent's
//     parcel to deliver.
func (s *Service) RecordAttempt(ctx context.Context, p *tenant.Principal, in AttemptInput) (*AttemptResult, error) {
	// 1. Offline retry.
	if in.Device.HasEventKey() {
		if prior, err := s.q.GetDeliveryAttemptByDeviceEvent(ctx, dbgen.GetDeliveryAttemptByDeviceEventParams{
			OrganizationID: p.OrganizationID,
			DeviceID:       &in.Device.ID, DeviceEventID: &in.Device.EventID,
		}); err == nil {
			return &AttemptResult{
				AttemptID: prior.PublicID, AttemptNumber: int(prior.AttemptNumber),
				ShipmentID: prior.ShipmentPublicID, AWB: prior.Awb,
				Outcome: prior.Outcome, ToStatus: prior.CurrentStatus,
				OccurredAt: prior.OccurredAt, CODCollected: prior.CodCollectedMinor,
				Currency: prior.Currency, Replayed: true,
			}, nil
		} else if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	var result *AttemptResult
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: in.Barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Shipment")
		}
		// 2. Row lock: two agents completing the same parcel serialise here.
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}

		item, hasItem, iErr := s.findRunItem(ctx, q, p, sh, in.RunID)
		if iErr != nil {
			return iErr
		}

		// The agent completing must be the one holding it, unless a supervisor
		// with delivery.manage is closing out for them.
		if sh.CurrentCustodyUserID != nil && *sh.CurrentCustodyUserID != p.UserID &&
			!p.Can("delivery.manage") {
			return apierr.Forbidden("This shipment is out with another agent.").
				WithDetail("code", "CUSTODY_VIOLATION")
		}

		// 3. OTP, inside the transaction so a code cannot be spent twice.
		otpVerified := false
		if in.Outcome == "DELIVERED" && in.OTP != "" {
			if vErr := s.verifyOTP(ctx, q, sh.ID, in.OTP); vErr != nil {
				return vErr
			}
			otpVerified = true
		}

		if in.Outcome == "DELIVERED" {
			if err := s.validateCOD(sh, in); err != nil {
				return err
			}
		}

		last, cErr := q.CountDeliveryAttempts(ctx, sh.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}

		// 4. The attempt itself.
		attempt, aErr := s.writeAttempt(ctx, q, p, sh, item, hasItem, in, last+1, otpVerified)
		if aErr != nil {
			return aErr
		}

		// 5. The state change.
		res, moved, tErr := s.applyOutcome(ctx, tx, p, sh, attempt, in)
		if tErr != nil {
			return tErr
		}

		if hasItem {
			itemStatus := map[string]string{
				"DELIVERED": "DELIVERED", "FAILED": "FAILED",
				"RESCHEDULED": "FAILED", "CANCELLED": "RETURNED_TO_BRANCH",
			}[in.Outcome]
			if _, uErr := q.UpdateDeliveryRunItemStatus(ctx, dbgen.UpdateDeliveryRunItemStatusParams{
				ID: item.ID, ToStatus: itemStatus,
				CodCollectedMinor: &in.CODCollectedMinor,
				AttemptIncrement:  1,
			}); uErr != nil {
				return apierr.Internal(uErr)
			}
			if _, rcErr := q.RecalculateDeliveryRunCounters(ctx, item.DeliveryRunID); rcErr != nil {
				return apierr.Internal(rcErr)
			}
			if _, sErr := q.UpdateDeliveryRunStatus(ctx, dbgen.UpdateDeliveryRunStatusParams{
				ID: item.DeliveryRunID, OrganizationID: p.OrganizationID,
				ExpectedStatus: StatusDispatched, ToStatus: StatusInProgress,
			}); sErr != nil && !ops.IsNoRows(sErr) {
				return apierr.Internal(sErr)
			}
		}

		result = res
		result.AttemptID = attempt.PublicID
		result.AttemptNumber = int(attempt.AttemptNumber)
		result.OccurredAt = attempt.OccurredAt
		result.CODCollected = attempt.CodCollectedMinor
		result.Currency = attempt.Currency

		// A failure opens or advances an NDR case.
		if in.Outcome == "FAILED" || in.Outcome == "RESCHEDULED" {
			if s.ndr != nil {
				branchID := sh.DestinationBranchID
				if branchID == nil && in.Facility != nil {
					branchID = &in.Facility.ID
				}
				if branchID != nil {
					// The case is opened against the post-transition shipment:
					// it must move DELIVERY_FAILED -> NDR, and passing the stale
					// row would make the compare-and-swap expect the state the
					// parcel was in before this attempt.
					caseID, nextAt, nErr := s.ndr(ctx, tx, p, moved, attempt,
						in.FailureReasonCode, in.Remarks, *branchID)
					if nErr != nil {
						return nErr
					}
					result.NDRCaseID = caseID
					result.NextAttemptAt = nextAt
				}
			}
		}

		action := audit.ActionDeliveryCompleted
		if in.Outcome != "DELIVERED" {
			action = audit.ActionDeliveryFailed
		}
		auditAfter := map[string]any{
			"awb": sh.Awb, "outcome": in.Outcome, "attemptNumber": attempt.AttemptNumber,
			"toStatus": result.ToStatus, "codCollectedMinor": attempt.CodCollectedMinor,
			"otpVerified": otpVerified,
		}
		if in.Outcome == "DELIVERED" {
			auditAfter["recipientName"] = in.RecipientName
			auditAfter["recipientRelationship"] = in.RecipientRelationship
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: action, ResourceType: "shipment",
			ResourceID: &sh.ID, ResourcePublicID: sh.PublicID,
			OperatingUnitID: facilityID(in.Facility), Reason: in.FailureReasonCode,
			Before: map[string]any{"status": sh.CurrentStatus},
			After:  auditAfter,
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.metrics.RecordBusiness("delivery", in.Outcome)
	return result, nil
}

// findRunItem locates the run stop this attempt belongs to.
func (s *Service) findRunItem(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, sh dbgen.Shipment, runID string,
) (dbgen.DeliveryRunItem, bool, error) {
	if runID != "" {
		run, rErr := q.LockDeliveryRunForUpdate(ctx, dbgen.LockDeliveryRunForUpdateParams{
			PublicID: runID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return dbgen.DeliveryRunItem{}, false, ops.NotFoundOr(rErr, "Delivery run")
		}
		item, iErr := q.GetDeliveryRunItemForShipment(ctx, dbgen.GetDeliveryRunItemForShipmentParams{
			DeliveryRunID: run.ID, ShipmentID: sh.ID,
		})
		if iErr != nil {
			if ops.IsNoRows(iErr) {
				return dbgen.DeliveryRunItem{}, false, apierr.Conflict(apierr.CodeConflict,
					"This shipment is not on that delivery run.").WithDetail("awb", sh.Awb)
			}
			return dbgen.DeliveryRunItem{}, false, apierr.Internal(iErr)
		}
		return item, true, nil
	}
	// No run named: find the live one, if any. A branch counter handover has no
	// run at all, which is legitimate.
	active, err := q.GetActiveDeliveryRunItem(ctx, sh.ID)
	if err != nil {
		if ops.IsNoRows(err) {
			return dbgen.DeliveryRunItem{}, false, nil
		}
		return dbgen.DeliveryRunItem{}, false, apierr.Internal(err)
	}
	item, iErr := q.GetDeliveryRunItemForShipment(ctx, dbgen.GetDeliveryRunItemForShipmentParams{
		DeliveryRunID: active.DeliveryRunID, ShipmentID: sh.ID,
	})
	if iErr != nil {
		return dbgen.DeliveryRunItem{}, false, apierr.Internal(iErr)
	}
	return item, true, nil
}

// validateCOD refuses a delivery whose money does not add up.
//
// Constitution §12: money is integer minor units and the backend is
// authoritative. A COD parcel handed over without the cash is a loss the
// franchise will be asked to cover, so under-collection needs a supervisor
// rather than an agent's tap.
func (s *Service) validateCOD(sh dbgen.Shipment, in AttemptInput) error {
	if sh.CodAmountMinor == 0 {
		if in.CODCollectedMinor != 0 {
			return apierr.Validation("This shipment has no COD amount to collect.",
				map[string]any{"field": "codCollectedMinor"})
		}
		return nil
	}
	if in.CODCollectedMinor <= 0 {
		return apierr.Conflict("COD_NOT_COLLECTED",
			"This is a COD shipment. Record the amount collected before completing the delivery.").
			WithDetail("codAmountMinor", sh.CodAmountMinor).
			WithDetail("currency", sh.Currency)
	}
	if in.CODCollectedMinor != sh.CodAmountMinor {
		return apierr.Conflict("COD_AMOUNT_MISMATCH",
			"The amount collected does not match the COD amount due.").
			WithDetail("codAmountMinor", sh.CodAmountMinor).
			WithDetail("collectedMinor", in.CODCollectedMinor).
			WithDetail("currency", sh.Currency)
	}
	if in.CODPaymentMode == "" {
		return apierr.Validation("Record how the COD amount was paid.",
			map[string]any{"field": "codPaymentMode", "allowed": CODPaymentModes})
	}
	return nil
}

func (s *Service) writeAttempt(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, sh dbgen.Shipment,
	item dbgen.DeliveryRunItem, hasItem bool, in AttemptInput, number int32, otpVerified bool,
) (dbgen.DeliveryAttempt, error) {
	params := dbgen.CreateDeliveryAttemptParams{
		PublicID: publicid.New(publicid.PrefixDeliveryAttempt), OrganizationID: p.OrganizationID,
		ShipmentID: sh.ID, AttemptNumber: number, Outcome: in.Outcome,
		Remarks:        ops.Optional(in.Remarks),
		OtpRequired:    in.OTP != "",
		OtpVerified:    otpVerified,
		CodAmountMinor: sh.CodAmountMinor, CodCollectedMinor: in.CODCollectedMinor,
		Currency: sh.Currency, AgentUserID: &p.UserID,
		OccurredAt: in.OccurredAt,
		Latitude:   in.Latitude, Longitude: in.Longitude,
		DeviceID: ops.Optional(in.Device.ID), DeviceEventID: ops.Optional(in.Device.EventID),
		NextAttemptAt: in.NextAttemptAt, Metadata: []byte("{}"),
	}
	if otpVerified {
		now := time.Now()
		params.OtpVerifiedAt = &now
	}
	if hasItem {
		params.DeliveryRunID = &item.DeliveryRunID
		params.DeliveryRunItemID = &item.ID
	}
	if in.Facility != nil {
		params.OperatingUnitID = &in.Facility.ID
	}
	if in.RecipientName != "" {
		params.RecipientName = &in.RecipientName
	}
	if in.RecipientRelationship != "" {
		params.RecipientRelationship = &in.RecipientRelationship
	}
	if in.RecipientPhone != "" {
		params.RecipientPhone = &in.RecipientPhone
	}
	if in.CODPaymentMode != "" {
		params.CodPaymentMode = &in.CODPaymentMode
	}
	if in.CODReference != "" {
		params.CodReference = &in.CODReference
	}
	if in.FailureReasonCode != "" {
		params.FailureReasonCode = &in.FailureReasonCode
	}
	if in.Accuracy != nil {
		v := int32(*in.Accuracy)
		params.LocationAccuracyM = &v
	}

	attempt, err := q.CreateDeliveryAttempt(ctx, params)
	if err != nil {
		switch {
		case ops.IsUnique(err, "delivery_attempts_success_idx"):
			// The structural guarantee against double delivery.
			return attempt, apierr.Conflict("ALREADY_DELIVERED",
				"This shipment has already been delivered.").
				WithDetail("awb", sh.Awb)
		case ops.IsUnique(err, "delivery_attempts_device_event_idx"):
			return attempt, apierr.Conflict(apierr.CodeDuplicate,
				"This delivery attempt was already recorded.")
		case ops.IsUnique(err, "delivery_attempts_number_unique"):
			return attempt, apierr.Conflict(apierr.CodeConcurrentModification,
				"Another attempt was recorded a moment ago. Reload and try again.")
		}
		return attempt, apierr.Internal(fmt.Errorf("record delivery attempt: %w", err))
	}
	return attempt, nil
}

// applyOutcome moves the shipment and returns both the API result and the
// updated row, so a caller that needs to make a further transition works from
// the current state rather than the one it read.
func (s *Service) applyOutcome(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, sh dbgen.Shipment,
	attempt dbgen.DeliveryAttempt, in AttemptInput,
) (*AttemptResult, dbgen.Shipment, error) {
	actor := ops.WithPosition(in.Device.Actor(p, in.Facility), in.Latitude, in.Longitude)
	from := shipment.Status(sh.CurrentStatus)

	var to shipment.Status
	switch in.Outcome {
	case "DELIVERED":
		to = shipment.StatusDelivered
		if sh.MovementDirection == string(shipment.DirectionReverse) {
			to = shipment.StatusRTODelivered
		}
	case "FAILED", "RESCHEDULED":
		to = shipment.StatusDeliveryFailed
	case "CANCELLED":
		to = shipment.StatusDestinationBranchReceived
	default:
		return nil, sh, apierr.Validation("Unknown delivery outcome.",
			map[string]any{"field": "outcome",
				"allowed": []string{"DELIVERED", "FAILED", "RESCHEDULED", "CANCELLED"}})
	}

	req := shipment.Request{
		Shipment: sh, To: to,
		Description: describeOutcome(in.Outcome, in.RecipientName),
		Reason:      in.FailureReasonCode, ReasonCode: in.FailureReasonCode,
		InternalRemarks: in.Remarks, OccurredAt: in.OccurredAt,
		IncrementDeliveryAttempt: true,
		IdempotencyKey:           deliveryKey(in.Device, sh.Awb),
		Metadata: map[string]any{
			"attemptNumber": attempt.AttemptNumber, "outcome": in.Outcome,
		},
	}
	if in.Outcome == "DELIVERED" {
		// Delivery ends custody entirely: the parcel is with the customer.
		req.SetCustodyUser, req.CustodyUserID = true, nil
		req.SetCustodyUnit, req.CustodyUnitID = true, nil
	}
	if in.Outcome == "CANCELLED" && in.Facility != nil {
		unitID := in.Facility.ID
		req.SetCustodyUnit, req.CustodyUnitID = true, &unitID
		req.SetCustodyUser, req.CustodyUserID = true, nil
		req.Reason = orDefault(in.Remarks, "Returned to branch")
	}

	res, err := s.trans.Apply(ctx, tx, actor, req)
	if err != nil {
		return nil, sh, err
	}
	return &AttemptResult{
		ShipmentID: res.Shipment.PublicID, AWB: res.Shipment.Awb,
		Outcome: in.Outcome, FromStatus: string(from), ToStatus: string(res.To),
		NextAttemptAt: in.NextAttemptAt,
		Next:          nextActionFor(in.Outcome),
	}, res.Shipment, nil
}

func describeOutcome(outcome, recipient string) string {
	switch outcome {
	case "DELIVERED":
		if recipient != "" {
			return "Delivered to " + recipient
		}
		return "Delivered"
	case "RESCHEDULED":
		return "Delivery rescheduled at the customer's request"
	case "CANCELLED":
		return "Returned to the delivery facility"
	default:
		return "Delivery attempted but not completed"
	}
}

func nextActionFor(outcome string) string {
	switch outcome {
	case "DELIVERED":
		return "Submit proof of delivery."
	case "FAILED", "RESCHEDULED":
		return "Set the next NDR action."
	default:
		return ""
	}
}

func deliveryKey(d ops.Device, awb string) string {
	if !d.HasEventKey() {
		return ""
	}
	return "delivery:" + d.EventID + ":" + awb
}

func facilityID(f *ops.Facility) *int64 {
	if f == nil {
		return nil
	}
	id := f.ID
	return &id
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
