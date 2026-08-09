// Package scanning implements M10: the scanner-grade operational API.
//
// This is the hottest write path in the system. A sorting hub puts thousands of
// parcels an hour through it, from handheld devices on flaky wifi, and every
// scan must be cheap, idempotent and impossible to apply to the wrong parcel.
// The design follows from that:
//
//   - One indexed lookup resolves a barcode to everything the custody check
//     needs, so a scan costs one read plus one write transaction.
//   - Every scan is recorded, including the rejected ones. A device scanning
//     parcels into the wrong facility is an operational problem, and it is
//     invisible if failures are only returned to the handset.
//   - Retries collapse on (device, deviceEventId). A dropped connection must
//     never turn one physical scan into two events.
//   - A bulk scan is a sequence of independent scans, not a transaction. One
//     bad barcode in a batch of two hundred must not roll back the other
//     hundred and ninety-nine.
package scanning

import (
	"context"
	"errors"
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

// ScanType is an operation a scanner can perform.
type ScanType string

const (
	// ScanReceive takes custody at a facility: the parcel is physically here.
	ScanReceive ScanType = "RECEIVE"
	// ScanArrival records a parcel arriving at a facility on a trip. It is
	// distinct from RECEIVE because arrival is the gate scan and receipt is the
	// inbound processing scan; hubs run them at different desks.
	ScanArrival ScanType = "ARRIVAL"
	// ScanDeparture records a parcel leaving.
	ScanDeparture ScanType = "DEPARTURE"
	// ScanSort records a sorting decision without changing custody.
	ScanSort ScanType = "SORT"
	// ScanHold stops the parcel moving until released.
	ScanHold ScanType = "HOLD"
	// ScanRelease lifts a hold.
	ScanRelease ScanType = "RELEASE"
	// ScanDamage declares physical damage.
	ScanDamage ScanType = "DAMAGE"
	// ScanException records anything else that needs a supervisor.
	ScanException ScanType = "EXCEPTION"
)

// AllScanTypes is the published enum.
var AllScanTypes = []ScanType{
	ScanReceive, ScanArrival, ScanDeparture, ScanSort,
	ScanHold, ScanRelease, ScanDamage, ScanException,
}

// scanPermissions maps each scan to the permission it demands. Kept beside the
// type so adding a scan without deciding who may perform it is impossible.
var scanPermissions = map[ScanType]string{
	ScanReceive:   "scan.inbound",
	ScanArrival:   "scan.inbound",
	ScanDeparture: "scan.outbound",
	ScanSort:      "scan.sort",
	ScanHold:      "scan.hold",
	ScanRelease:   "scan.hold",
	ScanDamage:    "scan.exception",
	ScanException: "scan.exception",
}

// Outcome of one scan.
const (
	OutcomeAccepted  = "ACCEPTED"
	OutcomeDuplicate = "DUPLICATE"
	OutcomeRejected  = "REJECTED"
)

// Service performs operational scans.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	units   *ops.Resolver
	trans   *shipment.Transitioner
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics
	// maxBulk bounds a batch. A handset that has been offline for a shift can
	// legitimately upload hundreds of scans; unbounded is how one client stalls
	// a worker for a minute.
	maxBulk int
}

// NewService builds the scanning service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, trans *shipment.Transitioner,
	rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics,
) *Service {
	return &Service{db: db, q: q, units: units, trans: trans, audit: rec, log: log, metrics: m, maxBulk: 200}
}

// MaxBulk is the batch limit, published so the contract can state it.
func (s *Service) MaxBulk() int { return s.maxBulk }

// Input is one scan.
type Input struct {
	Barcode    string
	ScanType   ScanType
	Facility   *ops.Facility
	Device     ops.Device
	OccurredAt *time.Time
	Latitude   *float64
	Longitude  *float64
	// Reason is required for HOLD, DAMAGE and EXCEPTION.
	Reason     string
	ReasonCode string
	Remarks    string
	// SortDestination names where a SORT scan routed the parcel.
	SortDestination string
	// Override forces the scan through a custody violation. Requires
	// shipment.override_custody and is always audited.
	Override bool
}

// Result is what one scan produced.
type Result struct {
	Barcode    string     `json:"barcode"`
	Outcome    string     `json:"outcome"`
	ScanID     string     `json:"scanId,omitempty"`
	ShipmentID string     `json:"shipmentId,omitempty"`
	AWB        string     `json:"awb,omitempty"`
	FromStatus string     `json:"fromStatus,omitempty"`
	ToStatus   string     `json:"toStatus,omitempty"`
	OccurredAt *time.Time `json:"occurredAt,omitempty"`
	// Set on rejection. The code is stable and machine-readable; the message is
	// written for the person holding the scanner.
	RejectionCode    string `json:"rejectionCode,omitempty"`
	RejectionMessage string `json:"rejectionMessage,omitempty"`
	// Next tells the operator what the parcel needs now, which is what turns a
	// scanner from a data-entry device into a work instruction.
	Next string `json:"nextAction,omitempty"`
}

// Scan performs one operational scan.
//
// The whole operation is one transaction so that the shipment transition, the
// scan record and any hold or exception land together. A rejection is written
// in its own transaction afterwards: it must survive even though the business
// transaction rolled back.
func (s *Service) Scan(ctx context.Context, p *tenant.Principal, in Input) (*Result, error) {
	perm, ok := scanPermissions[in.ScanType]
	if !ok {
		return nil, apierr.Validation("Unknown scan type.",
			map[string]any{"field": "scanType", "allowed": scanTypeStrings()})
	}
	if err := p.Require(perm); err != nil {
		return nil, err
	}

	// Offline retry: return the original outcome rather than scanning twice.
	if in.Device.HasEventKey() {
		if prior, err := s.q.GetScanEventByDeviceEvent(ctx, dbgen.GetScanEventByDeviceEventParams{
			OrganizationID: p.OrganizationID,
			DeviceID:       &in.Device.ID, DeviceEventID: &in.Device.EventID,
		}); err == nil {
			return replayResult(prior), nil
		} else if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	resolved, err := s.resolve(ctx, p, in.Barcode)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		// The barcode matched nothing. That is a real operational event — a
		// parcel from another network in our bag — so it is recorded, not
		// discarded.
		return s.recordRejection(ctx, p, in, nil, "UNKNOWN_BARCODE",
			"This barcode does not match any shipment in your organization."), nil
	}

	var result *Result
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		var iErr error
		result, iErr = s.applyScan(ctx, tx, p, in, resolved.ID, resolved.PackageID)
		return iErr
	})
	if err != nil {
		// A business refusal becomes a recorded rejection so a supervisor can
		// see the pattern. Anything internal is surfaced as-is.
		var ae *apierr.Error
		if errors.As(err, &ae) && ae.Status < 500 {
			code := string(ae.Code)
			if detail, okDetail := ae.Details["code"].(string); okDetail {
				code = detail
			}
			return s.recordRejection(ctx, p, in, &resolved.ID, code, ae.Message), nil
		}
		return nil, err
	}
	return result, nil
}

type resolvedBarcode struct {
	ID        int64
	PackageID *int64
}

func (s *Service) resolve(ctx context.Context, p *tenant.Principal, barcode string) (*resolvedBarcode, error) {
	row, err := s.q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
		Barcode: barcode, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, nil
		}
		return nil, apierr.Internal(fmt.Errorf("resolve barcode: %w", err))
	}
	return &resolvedBarcode{ID: row.ID, PackageID: row.PackageID}, nil
}

// applyScan does the work inside the caller's transaction.
func (s *Service) applyScan(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, in Input, shipmentID int64, packageID *int64,
) (*Result, error) {
	q := s.q.WithTx(tx)
	sh, err := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
		ID: shipmentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Shipment")
	}

	actor := ops.WithPosition(in.Device.Actor(p, in.Facility), in.Latitude, in.Longitude)
	from := shipment.Status(sh.CurrentStatus)

	var (
		result  *Result
		eventID *int64
		to      = from
	)

	switch in.ScanType {
	case ScanReceive, ScanArrival:
		res, rErr := s.applyReceive(ctx, tx, p, actor, in, sh)
		if rErr != nil {
			return nil, rErr
		}
		to = res.To
		eventID = &res.Event.ID
		result = &Result{
			ShipmentID: res.Shipment.PublicID, AWB: res.Shipment.Awb,
			FromStatus: string(res.From), ToStatus: string(res.To),
			Next: nextAction(res.Shipment, res.To),
		}

	case ScanDeparture:
		// Departure is driven by manifest dispatch, which knows what is on the
		// vehicle. A bare departure scan records the fact and, where the state
		// machine allows it, advances the parcel.
		res, rErr := s.applyDeparture(ctx, tx, p, actor, in, sh)
		if rErr != nil {
			return nil, rErr
		}
		to = res.To
		eventID = &res.Event.ID
		result = &Result{
			ShipmentID: res.Shipment.PublicID, AWB: res.Shipment.Awb,
			FromStatus: string(res.From), ToStatus: string(res.To),
			Next: nextAction(res.Shipment, res.To),
		}

	case ScanSort:
		// A sort does not change lifecycle state; it records where the parcel
		// was routed on the belt. Custody must still be here.
		if err := s.requireCustodyHere(in.Facility, sh); err != nil {
			return nil, err
		}
		ev, eErr := s.trans.RecordEvent(ctx, tx, actor, sh, "SCAN",
			sortDescription(in.SortDestination), shipment.Request{
				OccurredAt: in.OccurredAt, ReasonCode: in.ReasonCode,
				InternalRemarks: in.Remarks,
				IdempotencyKey:  deviceKey(in.Device),
				Metadata: map[string]any{
					"scanType": string(ScanSort), "sortDestination": in.SortDestination,
				},
			})
		if eErr != nil {
			return nil, eErr
		}
		eventID = &ev.ID
		result = &Result{
			ShipmentID: sh.PublicID, AWB: sh.Awb,
			FromStatus: sh.CurrentStatus, ToStatus: sh.CurrentStatus,
			Next: nextAction(sh, from),
		}

	case ScanHold:
		res, rErr := s.applyHold(ctx, tx, p, actor, in, sh)
		if rErr != nil {
			return nil, rErr
		}
		eventID = res.eventID
		result = res.result

	case ScanRelease:
		res, rErr := s.applyRelease(ctx, tx, p, actor, in, sh)
		if rErr != nil {
			return nil, rErr
		}
		eventID = res.eventID
		result = res.result

	case ScanDamage:
		if in.Reason == "" {
			return nil, apierr.Validation("A damage scan requires a description of the damage.",
				map[string]any{"field": "reason"})
		}
		res, rErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
			Shipment: sh, To: shipment.StatusDamaged,
			Reason: in.Reason, ReasonCode: orDefault(in.ReasonCode, "DAMAGE_OBSERVED"),
			InternalRemarks: in.Remarks, OccurredAt: in.OccurredAt,
			IdempotencyKey: deviceKey(in.Device), AuditAction: audit.ActionShipmentDamaged,
			SkipCustodyCheck: in.Override,
			Metadata:         map[string]any{"scanType": string(ScanDamage)},
		})
		if rErr != nil {
			return nil, rErr
		}
		to = res.To
		eventID = &res.Event.ID
		result = &Result{
			ShipmentID: res.Shipment.PublicID, AWB: res.Shipment.Awb,
			FromStatus: string(res.From), ToStatus: string(res.To),
			Next: "Raise an exception and decide whether to forward, return or write off.",
		}

	case ScanException:
		if in.Reason == "" {
			return nil, apierr.Validation("An exception scan requires a reason.",
				map[string]any{"field": "reason"})
		}
		ev, eErr := s.trans.RecordEvent(ctx, tx, actor, sh, "EXCEPTION",
			"Exception recorded at "+facilityName(in.Facility), shipment.Request{
				OccurredAt: in.OccurredAt, ReasonCode: orDefault(in.ReasonCode, "OPERATIONAL_EXCEPTION"),
				InternalRemarks: orDefault(in.Remarks, in.Reason),
				IdempotencyKey:  deviceKey(in.Device),
				Metadata:        map[string]any{"scanType": string(ScanException), "reason": in.Reason},
			})
		if eErr != nil {
			return nil, eErr
		}
		eventID = &ev.ID
		result = &Result{
			ShipmentID: sh.PublicID, AWB: sh.Awb,
			FromStatus: sh.CurrentStatus, ToStatus: sh.CurrentStatus,
			Next: "A supervisor must resolve the exception.",
		}
	}

	scan, err := s.writeScanEvent(ctx, q, p, in, &shipmentID, packageID,
		OutcomeAccepted, "", "", eventID, string(from), string(to))
	if err != nil {
		return nil, err
	}
	result.Barcode = in.Barcode
	result.Outcome = OutcomeAccepted
	result.ScanID = scan.PublicID
	result.OccurredAt = &scan.OccurredAt

	s.metrics.RecordBusiness("scan", string(in.ScanType))
	return result, nil
}

// applyReceive takes custody at the scanning facility.
//
// Which state a receipt produces depends on where the parcel is in its journey
// relative to this facility: the same physical action means "arrived at origin",
// "arrived at a transit hub" or "arrived at the delivery branch" depending on
// the route. Working that out here rather than making the scanner choose is the
// difference between a usable handheld and one that needs a trained operator.
func (s *Service) applyReceive(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, actor shipment.Actor,
	in Input, sh dbgen.Shipment,
) (*shipment.Result, error) {
	if in.Facility == nil {
		return nil, apierr.Validation("A receive scan requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	target, err := s.receiveTarget(sh, in.Facility)
	if err != nil {
		return nil, err
	}
	unitID := in.Facility.ID
	return s.trans.Apply(ctx, tx, actor, shipment.Request{
		Shipment: sh, To: target,
		OccurredAt: in.OccurredAt, InternalRemarks: in.Remarks,
		SetCustodyUnit: true, CustodyUnitID: &unitID,
		// Receipt at a facility ends any agent's personal custody and takes the
		// parcel out of whatever bag or vehicle carried it here.
		SetCustodyUser: true, CustodyUserID: nil,
		SetBag: true, BagID: nil, SetTrip: true, TripID: nil,
		IdempotencyKey:   deviceKey(in.Device),
		SkipCustodyCheck: in.Override,
		Reason:           in.Reason,
		Metadata: map[string]any{
			"scanType": string(in.ScanType), "facilityCode": in.Facility.Code,
		},
	})
}

// receiveTarget decides which received-state a scan at this facility produces.
func (s *Service) receiveTarget(sh dbgen.Shipment, at *ops.Facility) (shipment.Status, error) {
	current := shipment.Status(sh.CurrentStatus)

	// Reverse movement: an RTO parcel being received anywhere stays
	// RTO_IN_TRANSIT until it reaches the branch that hands it back. The
	// facility is recorded on the event, which is how the reverse journey is
	// tracked without a parallel status ladder.
	if sh.MovementDirection == string(shipment.DirectionReverse) {
		return "", errReverseReceiveHandledElsewhere
	}

	isDestinationBranch := sh.DestinationBranchID != nil && *sh.DestinationBranchID == at.ID
	isDestinationHub := sh.DestinationHubID != nil && *sh.DestinationHubID == at.ID
	isOriginBranch := sh.OriginBranchID != nil && *sh.OriginBranchID == at.ID

	// Try the most specific interpretation first, and fall back through the
	// alternatives the state machine actually allows from here. A misrouted
	// parcel therefore still gets received — as a transit-hub arrival — rather
	// than being refused at the door, which would leave it in limbo.
	var candidates []shipment.Status
	switch {
	case isDestinationBranch:
		candidates = []shipment.Status{
			shipment.StatusDestinationBranchReceived,
			shipment.StatusOriginBranchReceived,
		}
	case isDestinationHub:
		candidates = []shipment.Status{
			shipment.StatusDestinationHubReceived,
			shipment.StatusTransitHubReceived,
		}
	case isOriginBranch:
		candidates = []shipment.Status{
			shipment.StatusOriginBranchReceived,
			shipment.StatusTransitHubReceived,
		}
	default:
		candidates = []shipment.Status{
			shipment.StatusTransitHubReceived,
			shipment.StatusDestinationHubReceived,
			shipment.StatusOriginBranchReceived,
			shipment.StatusDestinationBranchReceived,
		}
	}
	for _, c := range candidates {
		if c == current {
			continue
		}
		if _, err := shipment.Validate(current, c); err == nil {
			return c, nil
		}
	}
	return "", apierr.Conflict("SHIPMENT_INVALID_STATE",
		fmt.Sprintf("A %s shipment cannot be received at %s.", current, at.Code)).
		WithDetail("currentStatus", string(current)).
		WithDetail("facilityCode", at.Code).
		WithDetail("allowedTransitions", statusStrings(shipment.AllowedFrom(current)))
}

var errReverseReceiveHandledElsewhere = apierr.Conflict("RTO_RECEIVE_REQUIRED",
	"This shipment is on its way back to the sender. Use the RTO receive endpoint so the return leg is recorded.")

// applyDeparture records a parcel leaving a facility.
func (s *Service) applyDeparture(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, actor shipment.Actor,
	in Input, sh dbgen.Shipment,
) (*shipment.Result, error) {
	if in.Facility == nil {
		return nil, apierr.Validation("A departure scan requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	current := shipment.Status(sh.CurrentStatus)
	target := shipment.StatusOriginDispatched
	if current == shipment.StatusTransitHubReceived || current == shipment.StatusDestinationHubReceived {
		target = shipment.StatusTransitHubDispatched
	}
	return s.trans.Apply(ctx, tx, actor, shipment.Request{
		Shipment: sh, To: target,
		OccurredAt: in.OccurredAt, InternalRemarks: in.Remarks, Reason: in.Reason,
		IdempotencyKey:   deviceKey(in.Device),
		SkipCustodyCheck: in.Override,
		Metadata: map[string]any{
			"scanType": string(ScanDeparture), "facilityCode": in.Facility.Code,
		},
	})
}

type scanSideEffect struct {
	eventID *int64
	result  *Result
}

// applyHold stops a parcel at a facility.
func (s *Service) applyHold(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, actor shipment.Actor,
	in Input, sh dbgen.Shipment,
) (*scanSideEffect, error) {
	if in.Reason == "" {
		return nil, apierr.Validation("A hold requires a reason.", map[string]any{"field": "reason"})
	}
	if in.Facility == nil {
		return nil, apierr.Validation("A hold requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	if sh.IsHeld {
		return nil, apierr.Conflict(apierr.CodeConflict, "This shipment is already on hold.")
	}
	if err := s.requireCustodyHere(in.Facility, sh); err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)

	code := orDefault(in.ReasonCode, "OTHER")
	if _, err := q.CreateShipmentHold(ctx, dbgen.CreateShipmentHoldParams{
		PublicID: publicid.New(publicid.PrefixHold), OrganizationID: p.OrganizationID,
		ShipmentID: sh.ID, OperatingUnitID: in.Facility.ID,
		HoldReasonCode: code, Reason: in.Reason, HeldByUserID: &p.UserID,
		Metadata: []byte("{}"),
	}); err != nil {
		if ops.IsUnique(err, "shipment_holds_active_idx") {
			return nil, apierr.Conflict(apierr.CodeConflict, "This shipment is already on hold.")
		}
		return nil, apierr.Internal(fmt.Errorf("create hold: %w", err))
	}
	if _, err := q.SetShipmentHoldFlag(ctx, dbgen.SetShipmentHoldFlagParams{
		ID: sh.ID, OrganizationID: p.OrganizationID, IsHeld: true, HoldReason: &in.Reason,
	}); err != nil {
		return nil, apierr.Internal(err)
	}

	ev, err := s.trans.RecordEvent(ctx, tx, actor, sh, "HOLD",
		"Held at "+facilityName(in.Facility), shipment.Request{
			OccurredAt: in.OccurredAt, ReasonCode: code,
			InternalRemarks: orDefault(in.Remarks, in.Reason),
			IdempotencyKey:  deviceKey(in.Device),
			Metadata:        map[string]any{"scanType": string(ScanHold), "holdReasonCode": code},
		})
	if err != nil {
		return nil, err
	}
	if err := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionShipmentHeld, ResourceType: "shipment",
		ResourceID: &sh.ID, ResourcePublicID: sh.PublicID,
		OperatingUnitID: &in.Facility.ID, Reason: in.Reason,
		After: map[string]any{"holdReasonCode": code, "facility": in.Facility.Code},
	})); err != nil {
		return nil, apierr.Internal(err)
	}
	return &scanSideEffect{
		eventID: &ev.ID,
		result: &Result{
			ShipmentID: sh.PublicID, AWB: sh.Awb,
			FromStatus: sh.CurrentStatus, ToStatus: sh.CurrentStatus,
			Next: "The shipment will not move until the hold is released.",
		},
	}, nil
}

// applyRelease lifts a hold.
func (s *Service) applyRelease(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, actor shipment.Actor,
	in Input, sh dbgen.Shipment,
) (*scanSideEffect, error) {
	if !sh.IsHeld {
		return nil, apierr.Conflict(apierr.CodeConflict, "This shipment is not on hold.")
	}
	q := s.q.WithTx(tx)
	if _, err := q.ReleaseShipmentHold(ctx, dbgen.ReleaseShipmentHoldParams{
		ShipmentID: sh.ID, ReleasedByUserID: &p.UserID,
		ReleaseNotes: ops.Optional(orDefault(in.Remarks, in.Reason)),
	}); err != nil {
		return nil, ops.ConflictOr(err, "This hold was released a moment ago.")
	}
	if _, err := q.SetShipmentHoldFlag(ctx, dbgen.SetShipmentHoldFlagParams{
		ID: sh.ID, OrganizationID: p.OrganizationID, IsHeld: false, HoldReason: nil,
	}); err != nil {
		return nil, apierr.Internal(err)
	}
	ev, err := s.trans.RecordEvent(ctx, tx, actor, sh, "RELEASE",
		"Released from hold", shipment.Request{
			OccurredAt: in.OccurredAt, InternalRemarks: in.Remarks,
			IdempotencyKey: deviceKey(in.Device),
			Metadata:       map[string]any{"scanType": string(ScanRelease)},
		})
	if err != nil {
		return nil, err
	}
	if err := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionShipmentReleased, ResourceType: "shipment",
		ResourceID: &sh.ID, ResourcePublicID: sh.PublicID,
		Reason: in.Reason, After: map[string]any{"released": true},
	})); err != nil {
		return nil, apierr.Internal(err)
	}
	return &scanSideEffect{
		eventID: &ev.ID,
		result: &Result{
			ShipmentID: sh.PublicID, AWB: sh.Awb,
			FromStatus: sh.CurrentStatus, ToStatus: sh.CurrentStatus,
			Next: nextAction(sh, shipment.Status(sh.CurrentStatus)),
		},
	}, nil
}

// requireCustodyHere refuses an action at a facility that does not hold the
// parcel.
func (s *Service) requireCustodyHere(at *ops.Facility, sh dbgen.Shipment) error {
	if at == nil {
		return apierr.Validation("This scan requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	if sh.CurrentCustodyUnitID == nil || *sh.CurrentCustodyUnitID != at.ID {
		return apierr.Conflict("CUSTODY_VIOLATION",
			"This shipment is not currently held at your facility.").
			WithDetail("code", "CUSTODY_VIOLATION").
			WithDetail("facilityCode", at.Code)
	}
	return nil
}

// recordRejection writes a refused scan on its own connection.
//
// It runs outside the failed business transaction on purpose: the rejection is
// the only durable trace that a device tried something invalid, and it would be
// rolled back with everything else if it shared that transaction.
func (s *Service) recordRejection(
	ctx context.Context, p *tenant.Principal, in Input, shipmentID *int64, code, message string,
) *Result {
	res := &Result{
		Barcode: in.Barcode, Outcome: OutcomeRejected,
		RejectionCode: code, RejectionMessage: message,
	}
	scan, err := s.writeScanEvent(context.WithoutCancel(ctx), s.q, p, in, shipmentID, nil,
		OutcomeRejected, code, message, nil, "", "")
	if err != nil {
		// A failure to record the rejection must not hide the rejection itself.
		s.log.Warn("could not record scan rejection",
			slog.String("barcode", in.Barcode), slog.String("code", code),
			slog.String("error", err.Error()))
		return res
	}
	res.ScanID = scan.PublicID
	res.OccurredAt = &scan.OccurredAt
	s.metrics.RecordBusiness("scan_rejected", code)
	return res
}

func (s *Service) writeScanEvent(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, in Input,
	shipmentID, packageID *int64, outcome, rejectionCode, rejectionMessage string,
	eventID *int64, from, to string,
) (dbgen.ScanEvent, error) {
	var unitID int64
	if in.Facility != nil {
		unitID = in.Facility.ID
	}
	params := dbgen.RecordScanEventParams{
		PublicID: publicid.New(publicid.PrefixScanEvent), OrganizationID: p.OrganizationID,
		RawBarcode: in.Barcode, ShipmentID: shipmentID, PackageID: packageID,
		ScanType: string(in.ScanType), OperatingUnitID: unitID, ScannedByUserID: &p.UserID,
		DeviceEventID: ops.Optional(in.Device.EventID), DeviceID: ops.Optional(in.Device.ID),
		Source: sourceOrScanner(in.Device.Source), OccurredAt: in.OccurredAt,
		Outcome:  outcome,
		Latitude: in.Latitude, Longitude: in.Longitude,
		Metadata: []byte("{}"),
	}
	if rejectionCode != "" {
		params.RejectionCode = &rejectionCode
		params.RejectionMessage = &rejectionMessage
	}
	params.ShipmentEventID = eventID
	if from != "" {
		params.FromStatus = &from
	}
	if to != "" {
		params.ToStatus = &to
	}
	scan, err := q.RecordScanEvent(ctx, params)
	if err != nil {
		if ops.IsUnique(err, "scan_events_device_event_idx") {
			// A concurrent retry of the same device event beat us here. Report
			// it as a duplicate rather than a failure.
			return scan, apierr.Conflict(apierr.CodeDuplicate, "This scan was already recorded.")
		}
		return scan, apierr.Internal(fmt.Errorf("record scan event: %w", err))
	}
	return scan, nil
}

func replayResult(prior dbgen.GetScanEventByDeviceEventRow) *Result {
	r := &Result{
		Barcode: prior.RawBarcode, Outcome: OutcomeDuplicate,
		ScanID: prior.PublicID, OccurredAt: &prior.OccurredAt,
		FromStatus: ops.Deref(prior.FromStatus), ToStatus: ops.Deref(prior.ToStatus),
		AWB: ops.Deref(prior.Awb),
	}
	if prior.ShipmentPublicID != nil {
		r.ShipmentID = *prior.ShipmentPublicID
	}
	if prior.Outcome == OutcomeRejected {
		r.RejectionCode = ops.Deref(prior.RejectionCode)
		r.RejectionMessage = ops.Deref(prior.RejectionMessage)
	}
	return r
}

// nextAction turns a state into a work instruction for the operator.
func nextAction(sh dbgen.Shipment, status shipment.Status) string {
	if sh.IsHeld {
		return "On hold. Release before moving."
	}
	switch status {
	case shipment.StatusOriginBranchReceived:
		return "Bag for dispatch."
	case shipment.StatusOriginBagged:
		return "Add the bag to a manifest."
	case shipment.StatusOriginDispatched, shipment.StatusTransitHubDispatched:
		return "Load onto the trip and depart."
	case shipment.StatusInTransit:
		return "In transit. Receive at the next facility."
	case shipment.StatusTransitHubReceived:
		return "Sort and dispatch onward."
	case shipment.StatusDestinationHubReceived:
		return "Forward to the delivery branch."
	case shipment.StatusDestinationBranchReceived:
		return "Add to a delivery run."
	case shipment.StatusOutForDelivery:
		return "Complete the delivery."
	case shipment.StatusDeliveryFailed, shipment.StatusNDR:
		return "Resolve the delivery exception."
	case shipment.StatusDamaged:
		return "Raise an exception."
	default:
		return ""
	}
}

func sortDescription(destination string) string {
	if destination == "" {
		return "Sorted"
	}
	return "Sorted for " + destination
}

func facilityName(f *ops.Facility) string {
	if f == nil {
		return "the facility"
	}
	return f.Name
}

func deviceKey(d ops.Device) string {
	if !d.HasEventKey() {
		return ""
	}
	return "scan:" + d.EventID
}

func sourceOrScanner(s string) string {
	if s == "" || s == "API" {
		return "SCANNER"
	}
	return s
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func scanTypeStrings() []string {
	out := make([]string, 0, len(AllScanTypes))
	for _, t := range AllScanTypes {
		out = append(out, string(t))
	}
	return out
}

func statusStrings(in []shipment.Status) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, string(s))
	}
	return out
}
