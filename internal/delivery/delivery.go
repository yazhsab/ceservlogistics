// Package delivery implements M15 and M16: the destination branch queue,
// delivery runs, and the delivery attempt itself.
//
// Delivery is where the network's correctness is tested hardest, because it is
// the only point where money changes hands and the only point where two people
// can plausibly act on the same parcel at once. The guarantees, and how each is
// obtained:
//
//	one agent per parcel      partial unique index on delivery_run_items
//	one success per parcel    partial unique index on delivery_attempts
//	safe offline retry        unique (organization, device, deviceEventId)
//	no delivery from the
//	  wrong branch            CustodyDestinationBranch on the transition
//	no delivery by the
//	  wrong agent             CustodyAgent on the transition
//	no double status change   compare-and-swap in ApplyOperationalTransition
//
// Four of those six are database constraints. That is deliberate: a service
// check answers "is this request sensible", a constraint answers "did two
// requests race", and only the second question matters at 3pm on a Friday.
package delivery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
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

// Delivery run states.
const (
	StatusPlanned    = "PLANNED"
	StatusAssigned   = "ASSIGNED"
	StatusDispatched = "DISPATCHED"
	StatusInProgress = "IN_PROGRESS"
	StatusCompleted  = "COMPLETED"
	StatusClosed     = "CLOSED"
	StatusCancelled  = "CANCELLED"
)

// AllStatuses is the published run lifecycle.
var AllStatuses = []string{
	StatusPlanned, StatusAssigned, StatusDispatched, StatusInProgress,
	StatusCompleted, StatusClosed, StatusCancelled,
}

var runTransitions = map[string][]string{
	StatusPlanned:    {StatusAssigned, StatusDispatched, StatusCancelled},
	StatusAssigned:   {StatusDispatched, StatusPlanned, StatusCancelled},
	StatusDispatched: {StatusInProgress, StatusCompleted, StatusCancelled},
	StatusInProgress: {StatusCompleted},
	StatusCompleted:  {StatusClosed},
}

func canTransition(from, to string) bool {
	for _, a := range runTransitions[from] {
		if a == to {
			return true
		}
	}
	return false
}

func invalidState(from, to string) error {
	return apierr.Conflict("DELIVERY_RUN_INVALID_STATE",
		fmt.Sprintf("A delivery run cannot move from %s to %s.", from, to)).
		WithDetail("currentStatus", from).
		WithDetail("attemptedStatus", to).
		WithDetail("allowedTransitions", runTransitions[from])
}

// COD payment modes an agent may record.
var CODPaymentModes = []string{"CASH", "UPI", "CARD", "WALLET", "BANK_TRANSFER", "CHEQUE"}

// Service implements the delivery workflow.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	units   *ops.Resolver
	codes   *ops.CodeAllocator
	trans   *shipment.Transitioner
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics
	otpTTL  time.Duration
	maxStop int
	// ndr is installed by the api wiring so this package does not import the
	// ndr package, which already depends on this one.
	ndr NDROpener
}

// NewService builds the delivery service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics,
) *Service {
	return &Service{
		db: db, q: q, units: units, codes: codes, trans: trans, audit: rec, log: log, metrics: m,
		otpTTL: 4 * time.Hour, maxStop: 300,
	}
}

// CreateRunInput plans a delivery run.
type CreateRunInput struct {
	BranchID string
	AgentID  string
	RunDate  time.Time
	Vehicle  string
	Barcodes []string
	Metadata map[string]any
}

// CreateRun plans a run and loads its stops.
func (s *Service) CreateRun(ctx context.Context, p *tenant.Principal, in CreateRunInput) (*RunDetail, error) {
	branch, err := s.units.RequireFacility(ctx, p, in.BranchID)
	if err != nil {
		return nil, err
	}
	agent, err := s.q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
		PublicID: in.AgentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "User")
	}
	if agent.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict, "This agent is not active.")
	}
	if len(in.Barcodes) > s.maxStop {
		return nil, apierr.Validation("Too many stops for one run.",
			map[string]any{"field": "barcodes", "max": s.maxStop})
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindDeliveryRun, branch.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *RunDetail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		run, cErr := q.CreateDeliveryRun(ctx, dbgen.CreateDeliveryRunParams{
			PublicID: publicid.New(publicid.PrefixDeliveryRun), OrganizationID: p.OrganizationID,
			RunCode: code, BranchID: branch.ID, AgentUserID: agent.ID,
			RunDate: in.RunDate, VehicleReference: ops.Optional(in.Vehicle),
			Currency: p.OrganizationCurrency, CreatedByUserID: &p.UserID,
			Metadata: encodeJSON(in.Metadata),
		})
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create delivery run: %w", cErr))
		}
		for i, barcode := range in.Barcodes {
			if err := s.addStop(ctx, q, p, run, branch, barcode, i+1); err != nil {
				return err
			}
		}
		if _, rErr := q.RecalculateDeliveryRunCounters(ctx, run.ID); rErr != nil {
			return apierr.Internal(rErr)
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionDeliveryRunCreated, ResourceType: "delivery_run",
			ResourceID: &run.ID, ResourcePublicID: run.PublicID,
			OperatingUnitID: &branch.ID,
			After: map[string]any{
				"runCode": code, "branch": branch.Code, "agent": agent.PublicID,
				"agentName": agent.FullName, "stops": len(in.Barcodes),
				"runDate": in.RunDate.Format("2006-01-02"),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadRun(ctx, q, p, run.PublicID)
		return dErr
	})
	return detail, err
}

// addStop puts one shipment on a run.
//
// The partial unique index on delivery_run_items is what actually prevents two
// runs holding the same parcel; the lookup here is only so the error names the
// other run rather than a constraint.
func (s *Service) addStop(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal,
	run dbgen.DeliveryRun, branch *ops.Facility, barcode string, sequence int,
) error {
	resolved, err := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
		Barcode: barcode, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return ops.NotFoundOr(err, "Shipment")
	}
	sh, err := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
		ID: resolved.ID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return ops.NotFoundOr(err, "Shipment")
	}

	if sh.IsHeld {
		return apierr.Conflict("SHIPMENT_ON_HOLD",
			"This shipment is on hold and cannot go out for delivery.").
			WithDetail("awb", sh.Awb).
			WithDetail("holdReason", ops.Deref(sh.HoldReason))
	}
	// The parcel must be at this branch, in a deliverable state.
	switch shipment.Status(sh.CurrentStatus) {
	case shipment.StatusDestinationBranchReceived, shipment.StatusNDR,
		shipment.StatusDeliveryFailed, shipment.StatusRTOInTransit:
	default:
		return apierr.Conflict("SHIPMENT_INVALID_STATE",
			"This shipment is not ready for delivery.").
			WithDetail("awb", sh.Awb).WithDetail("currentStatus", sh.CurrentStatus)
	}
	if sh.CurrentCustodyUnitID == nil || *sh.CurrentCustodyUnitID != branch.ID {
		return apierr.Conflict("CUSTODY_VIOLATION",
			"This shipment is not held at the branch running the delivery.").
			WithDetail("awb", sh.Awb)
	}

	if existing, eErr := q.GetActiveDeliveryRunItem(ctx, sh.ID); eErr == nil {
		return apierr.Conflict("SHIPMENT_ALREADY_ASSIGNED",
			"This shipment is already out with another agent.").
			WithDetail("awb", sh.Awb).
			WithDetail("runCode", existing.RunCode).
			WithDetail("agentName", existing.AgentName)
	} else if !ops.IsNoRows(eErr) {
		return apierr.Internal(eErr)
	}

	deliveryType := "FORWARD"
	if sh.MovementDirection == string(shipment.DirectionReverse) {
		deliveryType = "RTO"
	}
	if _, iErr := q.AddDeliveryRunItem(ctx, dbgen.AddDeliveryRunItemParams{
		PublicID: publicid.New(publicid.PrefixDeliveryRunItem), OrganizationID: p.OrganizationID,
		DeliveryRunID: run.ID, ShipmentID: sh.ID, StopSequence: int32(sequence),
		DeliveryType: deliveryType, CodAmountMinor: sh.CodAmountMinor,
		AddedByUserID: &p.UserID,
	}); iErr != nil {
		switch {
		case ops.IsUnique(iErr, "delivery_run_items_active_idx"):
			return apierr.Conflict("SHIPMENT_ALREADY_ASSIGNED",
				"This shipment was put on another run a moment ago.").WithDetail("awb", sh.Awb)
		case ops.IsUnique(iErr, "delivery_run_items_unique"):
			return apierr.Conflict(apierr.CodeDuplicate,
				"This shipment is already on this run.").WithDetail("awb", sh.Awb)
		case ops.IsUnique(iErr, "delivery_run_items_stop_idx"):
			return apierr.Conflict(apierr.CodeConflict,
				"Another stop already occupies that position.").WithDetail("stopSequence", sequence)
		}
		return apierr.Internal(fmt.Errorf("add delivery stop: %w", iErr))
	}
	return nil
}

// AddStopsInput adds shipments to an existing run.
type AddStopsInput struct {
	RunID    string
	Barcodes []string
}

// StopResult is the outcome for one barcode.
type StopResult struct {
	Barcode string `json:"barcode"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// AddStops loads more shipments onto a run that has not left.
func (s *Service) AddStops(ctx context.Context, p *tenant.Principal, in AddStopsInput) (*RunDetail, []StopResult, error) {
	results := make([]StopResult, 0, len(in.Barcodes))
	var detail *RunDetail

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		run, rErr := q.LockDeliveryRunForUpdate(ctx, dbgen.LockDeliveryRunForUpdateParams{
			PublicID: in.RunID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Delivery run")
		}
		if err := p.RequireUnitInScope(run.BranchID); err != nil {
			return err
		}
		if run.Status != StatusPlanned && run.Status != StatusAssigned {
			return apierr.Conflict("DELIVERY_RUN_INVALID_STATE",
				"Stops can only be added before the run is dispatched.").
				WithDetail("currentStatus", run.Status)
		}
		branch, fErr := s.units.FacilityByID(ctx, p.OrganizationID, run.BranchID)
		if fErr != nil {
			return fErr
		}
		next := int(run.PlannedStops) + 1
		for _, barcode := range in.Barcodes {
			if err := s.addStop(ctx, q, p, run, branch, barcode, next); err != nil {
				var ae *apierr.Error
				if asAPIError(err, &ae) && ae.Status < 500 {
					results = append(results, StopResult{
						Barcode: barcode, Outcome: "REJECTED",
						Reason: string(ae.Code), Message: ae.Message,
					})
					continue
				}
				return err
			}
			results = append(results, StopResult{Barcode: barcode, Outcome: "ADDED"})
			next++
		}
		if _, cErr := q.RecalculateDeliveryRunCounters(ctx, run.ID); cErr != nil {
			return apierr.Internal(cErr)
		}
		var dErr error
		detail, dErr = s.loadRun(ctx, q, p, run.PublicID)
		return dErr
	})
	return detail, results, err
}

// Assign changes the agent on a run that has not left.
func (s *Service) Assign(ctx context.Context, p *tenant.Principal, runID, agentID string) (*RunDetail, error) {
	var detail *RunDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		run, rErr := q.LockDeliveryRunForUpdate(ctx, dbgen.LockDeliveryRunForUpdateParams{
			PublicID: runID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Delivery run")
		}
		if err := p.RequireUnitInScope(run.BranchID); err != nil {
			return err
		}
		if !canTransition(run.Status, StatusAssigned) && run.Status != StatusAssigned {
			return invalidState(run.Status, StatusAssigned)
		}
		agent, aErr := q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
			PublicID: agentID, OrganizationID: p.OrganizationID,
		})
		if aErr != nil {
			return ops.NotFoundOr(aErr, "User")
		}
		if agent.Status != "ACTIVE" {
			return apierr.Conflict(apierr.CodeConflict, "This agent is not active.")
		}
		if _, uErr := q.UpdateDeliveryRunStatus(ctx, dbgen.UpdateDeliveryRunStatusParams{
			ID: run.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: run.Status, ToStatus: StatusAssigned,
			AgentUserID: &agent.ID,
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This run changed while it was being assigned.")
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionDeliveryAssigned, ResourceType: "delivery_run",
			ResourceID: &run.ID, ResourcePublicID: run.PublicID,
			OperatingUnitID: &run.BranchID,
			Before:          map[string]any{"status": run.Status},
			After: map[string]any{
				"status": StatusAssigned, "agent": agent.PublicID, "agentName": agent.FullName,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadRun(ctx, q, p, run.PublicID)
		return dErr
	})
	return detail, err
}

// Dispatch sends a run out and marks every stop out for delivery.
//
// This is the only place OUT_FOR_DELIVERY is set, and the transition carries
// CustodyDestinationBranch, so a run at the wrong branch cannot dispatch a
// parcel it does not hold (§M15: "Do not permit OFD directly from arbitrary
// facility").
func (s *Service) Dispatch(
	ctx context.Context, p *tenant.Principal, runID string, device ops.Device,
) (*RunDetail, error) {
	var detail *RunDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		run, rErr := q.LockDeliveryRunForUpdate(ctx, dbgen.LockDeliveryRunForUpdateParams{
			PublicID: runID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Delivery run")
		}
		if err := p.RequireUnitInScope(run.BranchID); err != nil {
			return err
		}
		if !canTransition(run.Status, StatusDispatched) {
			return invalidState(run.Status, StatusDispatched)
		}
		if run.PlannedStops == 0 {
			return apierr.Conflict("DELIVERY_RUN_EMPTY",
				"A run with no stops has nothing to deliver.")
		}
		branch, fErr := s.units.FacilityByID(ctx, p.OrganizationID, run.BranchID)
		if fErr != nil {
			return fErr
		}

		items, iErr := q.ListDeliveryRunShipmentIDs(ctx, run.ID)
		if iErr != nil {
			return apierr.Internal(iErr)
		}
		actor := device.Actor(p, branch)
		agentID := run.AgentUserID

		for _, item := range items {
			sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
				ID: item.ShipmentID, OrganizationID: p.OrganizationID,
			})
			if lErr != nil {
				return apierr.Internal(lErr)
			}
			if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
				Shipment: sh, To: shipment.StatusOutForDelivery,
				Description: "Out for delivery",
				// Custody passes to the agent; the branch remains accountable.
				SetCustodyUser: true, CustodyUserID: &agentID,
				Metadata: map[string]any{
					"runCode": run.RunCode, "stopSequence": item.StopSequence,
				},
			}); tErr != nil {
				return tErr
			}
			if _, uErr := q.UpdateDeliveryRunItemStatus(ctx, dbgen.UpdateDeliveryRunItemStatusParams{
				ID: item.ItemID, ToStatus: "OUT_FOR_DELIVERY",
			}); uErr != nil {
				return apierr.Internal(uErr)
			}
		}

		if _, uErr := q.UpdateDeliveryRunStatus(ctx, dbgen.UpdateDeliveryRunStatusParams{
			ID: run.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: run.Status, ToStatus: StatusDispatched,
			DispatchedByUserID: &p.UserID,
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This run changed while it was being dispatched.")
		}
		if _, cErr := q.RecalculateDeliveryRunCounters(ctx, run.ID); cErr != nil {
			return apierr.Internal(cErr)
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionDeliveryDispatched, ResourceType: "delivery_run",
			ResourceID: &run.ID, ResourcePublicID: run.PublicID,
			OperatingUnitID: &run.BranchID,
			Before:          map[string]any{"status": run.Status},
			After: map[string]any{
				"status": StatusDispatched, "runCode": run.RunCode, "stops": len(items),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadRun(ctx, q, p, run.PublicID)
		return dErr
	})
	return detail, err
}

// IssueOTP generates a delivery code for a shipment.
//
// The code is returned exactly once, to the caller who issued it, so it can be
// sent to the consignee. Only its digest is stored (§35), which means a later
// read of the database — or a debug endpoint, or a log — cannot reveal it.
func (s *Service) IssueOTP(
	ctx context.Context, p *tenant.Principal, barcode string,
) (code string, maskedTo string, expiresAt time.Time, err error) {
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Shipment")
		}
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}
		if shipment.IsTerminal(shipment.Status(sh.CurrentStatus)) {
			return apierr.Conflict("SHIPMENT_INVALID_STATE",
				"This shipment has already reached a final state.").
				WithDetail("currentStatus", sh.CurrentStatus)
		}

		// Replace any live code: issuing a second one must invalidate the first,
		// or two codes would both open the same parcel.
		if prior, pErr := q.GetActiveDeliveryOTP(ctx, sh.ID); pErr == nil {
			if _, cErr := q.ConsumeDeliveryOTP(ctx, prior.ID); cErr != nil {
				return apierr.Internal(cErr)
			}
		} else if !ops.IsNoRows(pErr) {
			return apierr.Internal(pErr)
		}

		generated, gErr := generateOTP(6)
		if gErr != nil {
			return apierr.Internal(gErr)
		}
		digest := sha256.Sum256([]byte(generated))
		expires := time.Now().Add(s.otpTTL)

		recipient, mErr := q.GetShipmentAddressSnapshot(ctx, dbgen.GetShipmentAddressSnapshotParams{
			ShipmentID: sh.ID, Role: "RECIPIENT",
		})
		masked := ""
		if mErr == nil {
			masked = maskPhone(recipient.Phone)
		}

		if _, cErr := q.CreateDeliveryOTP(ctx, dbgen.CreateDeliveryOTPParams{
			OrganizationID: p.OrganizationID, ShipmentID: sh.ID,
			CodeHash: digest[:], SentToMasked: ops.Optional(masked),
			ExpiresAt: expires, MaxAttempts: 5, IssuedByUserID: &p.UserID,
		}); cErr != nil {
			return apierr.Internal(fmt.Errorf("issue delivery otp: %w", cErr))
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionOTPIssued, ResourceType: "shipment",
			ResourceID: &sh.ID, ResourcePublicID: sh.PublicID,
			// The code itself is never audited.
			After: map[string]any{"awb": sh.Awb, "sentTo": masked, "expiresAt": expires},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		code, maskedTo, expiresAt = generated, masked, expires
		return nil
	})
	return code, maskedTo, expiresAt, err
}

// verifyOTP checks a supplied code and consumes it.
func (s *Service) verifyOTP(
	ctx context.Context, q *dbgen.Queries, shipmentID int64, supplied string,
) error {
	otp, err := q.GetActiveDeliveryOTP(ctx, shipmentID)
	if err != nil {
		if ops.IsNoRows(err) {
			return apierr.Conflict("OTP_NOT_ISSUED",
				"No delivery code has been issued for this shipment.")
		}
		return apierr.Internal(err)
	}
	if time.Now().After(otp.ExpiresAt) {
		if _, cErr := q.ConsumeDeliveryOTP(ctx, otp.ID); cErr != nil {
			return apierr.Internal(cErr)
		}
		return apierr.Conflict("OTP_EXPIRED",
			"The delivery code has expired. Issue a new one.")
	}
	if otp.AttemptCount >= otp.MaxAttempts {
		return apierr.Conflict("OTP_ATTEMPTS_EXCEEDED",
			"Too many incorrect codes. Issue a new one.")
	}
	digest := sha256.Sum256([]byte(supplied))
	// Constant-time compare: a timing side channel on a six-digit code is a
	// real attack when the endpoint can be called repeatedly.
	if subtle.ConstantTimeCompare(digest[:], otp.CodeHash) != 1 {
		if _, iErr := q.IncrementDeliveryOTPAttempts(ctx, otp.ID); iErr != nil {
			return apierr.Internal(iErr)
		}
		return apierr.Conflict("OTP_INVALID", "The delivery code is not correct.")
	}
	if _, cErr := q.ConsumeDeliveryOTP(ctx, otp.ID); cErr != nil {
		return apierr.Internal(cErr)
	}
	return nil
}

// generateOTP produces a numeric code from a cryptographic source.
func generateOTP(digits int) (string, error) {
	max := big.NewInt(1)
	for i := 0; i < digits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", digits, n), nil
}

// maskPhone keeps the last four digits, which is enough for support to confirm
// where a code went without exposing the number.
func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "******" + phone[len(phone)-4:]
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

func asAPIError(err error, target **apierr.Error) bool {
	e, ok := err.(*apierr.Error)
	if ok {
		*target = e
	}
	return ok
}
