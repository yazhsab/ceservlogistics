// Package bagging implements M11: bags as first-class operational entities.
//
// A bag is a sealed container of shipments moving between two facilities. The
// rules that matter, and where each is enforced:
//
//	only valid shipments may enter        service check, before the insert
//	destination/service compatibility     service check, with a clear message
//	duplicates rejected                   UNIQUE(bag_id, shipment_id)
//	conflicting active membership         partial unique index on shipment_id
//	removal only while OPEN               bag_items_guard trigger
//	closure freezes contents              bag_items_guard trigger
//	seal captured                         bag_seals, verified at receipt
//	post-close correction is exceptional  exception_id required by the trigger
//
// The service checks exist to produce a helpful error. The database checks
// exist because a helpful error is not a guarantee: two clerks pressing "add"
// at the same instant both pass the service check, and only the constraint
// stops the second.
package bagging

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
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Bag states.
const (
	StatusOpen       = "OPEN"
	StatusClosed     = "CLOSED"
	StatusDispatched = "DISPATCHED"
	StatusReceived   = "RECEIVED"
	StatusOpened     = "OPENED"
	StatusReconciled = "RECONCILED"
	StatusCancelled  = "CANCELLED"
)

// AllStatuses is the published lifecycle.
var AllStatuses = []string{
	StatusOpen, StatusClosed, StatusDispatched, StatusReceived,
	StatusOpened, StatusReconciled, StatusCancelled,
}

// bagTransitions is the authoritative bag lifecycle.
var bagTransitions = map[string][]string{
	StatusOpen:       {StatusClosed, StatusCancelled},
	StatusClosed:     {StatusDispatched, StatusOpen, StatusCancelled},
	StatusDispatched: {StatusReceived},
	StatusReceived:   {StatusOpened, StatusReconciled},
	StatusOpened:     {StatusReconciled},
}

func canTransition(from, to string) bool {
	for _, a := range bagTransitions[from] {
		if a == to {
			return true
		}
	}
	return false
}

func invalidState(from, to string) error {
	return apierr.Conflict("BAG_INVALID_STATE",
		fmt.Sprintf("A bag cannot move from %s to %s.", from, to)).
		WithDetail("currentStatus", from).
		WithDetail("attemptedStatus", to).
		WithDetail("allowedTransitions", bagTransitions[from])
}

// Service implements the bag workflow.
type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	units *ops.Resolver
	codes *ops.CodeAllocator
	trans *shipment.Transitioner
	audit *audit.Recorder
	log   *slog.Logger
}

// NewService builds the bagging service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, units: units, codes: codes, trans: trans, audit: rec, log: log}
}

// CreateInput opens a new bag.
type CreateInput struct {
	OriginUnitID      string
	DestinationUnitID string
	BagType           string
	ServiceCode       string
	Direction         string
	MaxWeightGrams    *int
	MaxShipments      *int
	Metadata          map[string]any
}

// Create opens a bag at the caller's facility.
func (s *Service) Create(ctx context.Context, p *tenant.Principal, in CreateInput) (*Detail, error) {
	origin, err := s.units.RequireFacility(ctx, p, in.OriginUnitID)
	if err != nil {
		return nil, err
	}
	// The destination is resolved but not scope-checked: a branch legitimately
	// bags towards a hub it has no staff at.
	dest, err := s.destinationFacility(ctx, p, in.DestinationUnitID)
	if err != nil {
		return nil, err
	}
	if dest.ID == origin.ID {
		return nil, apierr.Validation("A bag cannot travel from a facility to itself.",
			map[string]any{"field": "destinationUnitId"})
	}

	var serviceID *int64
	if in.ServiceCode != "" {
		svc, sErr := s.q.GetCourierServiceByCode(ctx, dbgen.GetCourierServiceByCodeParams{
			Code: in.ServiceCode, OrganizationID: p.OrganizationID,
		})
		if sErr != nil {
			return nil, apierr.Validation("Unknown courier service.",
				map[string]any{"field": "serviceCode"})
		}
		serviceID = &svc.ID
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindBag, origin.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *Detail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateBagParams{
			PublicID: publicid.New(publicid.PrefixBag), OrganizationID: p.OrganizationID,
			BagCode: code, Barcode: ops.Barcode(code),
			BagType:           orDefault(in.BagType, "STANDARD"),
			OriginUnitID:      origin.ID,
			DestinationUnitID: dest.ID,
			CourierServiceID:  serviceID,
			Direction:         orDefault(in.Direction, "FORWARD"),
			OpenedByUserID:    &p.UserID,
			Metadata:          encodeJSON(in.Metadata),
		}
		if in.MaxWeightGrams != nil {
			v := int32(*in.MaxWeightGrams)
			params.MaxWeightGrams = &v
		}
		if in.MaxShipments != nil {
			v := int32(*in.MaxShipments)
			params.MaxShipments = &v
		}
		bag, cErr := q.CreateBag(ctx, params)
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create bag: %w", cErr))
		}
		if _, eErr := s.appendEvent(ctx, q, p, bag, origin, "CREATED", "", StatusOpen,
			fmt.Sprintf("Bag opened at %s for %s", origin.Code, dest.Code), "", nil); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionBagCreated, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			OperatingUnitID: &origin.ID,
			After: map[string]any{
				"bagCode": code, "origin": origin.Code, "destination": dest.Code,
				"bagType": params.BagType, "direction": params.Direction,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return dErr
	})
	return detail, err
}

// AddItemsInput adds shipments to an open bag.
type AddItemsInput struct {
	BagID    string
	Barcodes []string
	Facility *ops.Facility
	Device   ops.Device
}

// ItemResult is the outcome for one barcode.
type ItemResult struct {
	Barcode    string `json:"barcode"`
	Outcome    string `json:"outcome"`
	ShipmentID string `json:"shipmentId,omitempty"`
	AWB        string `json:"awb,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
}

// AddItemsResult is the batch outcome.
type AddItemsResult struct {
	Added    int          `json:"added"`
	Rejected int          `json:"rejected"`
	Results  []ItemResult `json:"results"`
	Bag      *Detail      `json:"bag"`
}

// AddItems puts shipments into an open bag.
//
// Like a bulk scan, each barcode succeeds or fails on its own. A hub operator
// filling a bag from a trolley should not lose nineteen good scans because the
// twentieth parcel is routed elsewhere.
func (s *Service) AddItems(ctx context.Context, p *tenant.Principal, in AddItemsInput) (*AddItemsResult, error) {
	out := &AddItemsResult{Results: make([]ItemResult, 0, len(in.Barcodes))}
	seen := make(map[string]bool, len(in.Barcodes))

	for _, barcode := range in.Barcodes {
		if seen[barcode] {
			out.Results = append(out.Results, ItemResult{
				Barcode: barcode, Outcome: "REJECTED", Reason: "DUPLICATE_IN_BATCH",
				Message: "This barcode appears more than once in the batch.",
			})
			out.Rejected++
			continue
		}
		seen[barcode] = true

		res, err := s.addOne(ctx, p, in, barcode)
		if err != nil {
			var ae *apierr.Error
			if asAPIError(err, &ae) && ae.Status < 500 {
				out.Results = append(out.Results, ItemResult{
					Barcode: barcode, Outcome: "REJECTED",
					Reason: string(ae.Code), Message: ae.Message,
				})
				out.Rejected++
				continue
			}
			return nil, err
		}
		out.Results = append(out.Results, *res)
		out.Added++
	}

	detail, err := s.Get(ctx, p, in.BagID)
	if err != nil {
		return nil, err
	}
	out.Bag = detail
	return out, nil
}

func (s *Service) addOne(
	ctx context.Context, p *tenant.Principal, in AddItemsInput, barcode string,
) (*ItemResult, error) {
	var res *ItemResult
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: in.BagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		if bag.Status != StatusOpen {
			return apierr.Conflict("BAG_NOT_OPEN",
				"Shipments can only be added while the bag is open.").
				WithDetail("currentStatus", bag.Status)
		}
		if err := p.RequireUnitInScope(bag.OriginUnitID); err != nil {
			return err
		}

		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			if ops.IsNoRows(rErr) {
				return apierr.NotFound("Shipment")
			}
			return apierr.Internal(rErr)
		}
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}

		if err := s.checkCompatibility(ctx, q, bag, sh); err != nil {
			return err
		}

		// The active-membership index is the real guard; this read turns the
		// violation into a message naming the other bag.
		if existing, eErr := q.GetActiveBagItemForShipment(ctx, sh.ID); eErr == nil {
			return apierr.Conflict("BAG_MEMBERSHIP_CONFLICT",
				"This shipment is already inside another bag.").
				WithDetail("awb", sh.Awb).
				WithDetail("bagCode", existing.BagCode).
				WithDetail("bagStatus", existing.BagStatus)
		} else if !ops.IsNoRows(eErr) {
			return apierr.Internal(eErr)
		}

		if _, iErr := q.AddBagItem(ctx, dbgen.AddBagItemParams{
			OrganizationID: p.OrganizationID, BagID: bag.ID, ShipmentID: sh.ID,
			PieceCount: sh.PieceCount, WeightGrams: sh.ChargeableWeightGrams,
			AddedByUserID: &p.UserID,
		}); iErr != nil {
			switch {
			case ops.IsUnique(iErr, "bag_items_active_membership_idx"):
				return apierr.Conflict("BAG_MEMBERSHIP_CONFLICT",
					"This shipment was put into another bag a moment ago.")
			case ops.IsUnique(iErr, "bag_items_unique"):
				return apierr.Conflict(apierr.CodeDuplicate,
					"This shipment is already in this bag.").WithDetail("awb", sh.Awb)
			}
			if msg, ok := ops.TriggerMessage(iErr); ok {
				return apierr.Conflict("BAG_NOT_OPEN", msg)
			}
			return apierr.Internal(fmt.Errorf("add bag item: %w", iErr))
		}

		updated, cErr := q.RecalculateBagCounters(ctx, bag.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		if err := s.checkCapacity(updated); err != nil {
			return err
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, bag.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		actor := in.Device.Actor(p, origin)

		// Forward-moving parcels enter ORIGIN_BAGGED. Reverse (RTO) parcels stay
		// where they are: the bag is a container, not a lifecycle stage, on the
		// way back.
		if sh.MovementDirection == string(shipment.DirectionForward) {
			bagID := bag.ID
			if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
				Shipment: sh, To: shipment.StatusOriginBagged,
				Description: "Added to bag " + bag.BagCode,
				SetBag:      true, BagID: &bagID,
				IdempotencyKey: bagKey(in.Device, bag.BagCode, sh.Awb),
				Metadata:       map[string]any{"bagCode": bag.BagCode},
			}); tErr != nil {
				return tErr
			}
		} else {
			bagID := bag.ID
			if _, sErr := q.SetShipmentCustody(ctx, dbgen.SetShipmentCustodyParams{
				ID: sh.ID, OrganizationID: p.OrganizationID,
				SetBag: true, BagID: &bagID,
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
			if _, eErr := s.trans.RecordEvent(ctx, tx, actor, sh, "BAG",
				"Added to return bag "+bag.BagCode, shipment.Request{
					IdempotencyKey: bagKey(in.Device, bag.BagCode, sh.Awb),
					Metadata:       map[string]any{"bagCode": bag.BagCode},
				}); eErr != nil {
				return eErr
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, origin, "ITEM_ADDED", "", "",
			fmt.Sprintf("%s added to the bag", sh.Awb), "",
			map[string]any{"awb": sh.Awb, "shipmentCount": updated.ShipmentCount}); eErr != nil {
			return eErr
		}

		res = &ItemResult{
			Barcode: barcode, Outcome: "ADDED",
			ShipmentID: sh.PublicID, AWB: sh.Awb,
		}
		return nil
	})
	return res, err
}

// checkCompatibility applies the bag's routing and service rules.
//
// The destination rule is the operationally important one: a bag going to the
// Delhi hub must not contain a parcel for Chennai, because the whole point of a
// bag is that the receiving facility can process its contents without
// re-sorting them against the network.
func (s *Service) checkCompatibility(
	ctx context.Context, q *dbgen.Queries, bag dbgen.Bag, sh dbgen.Shipment,
) error {
	// State: only a parcel the origin facility actually holds can be bagged.
	switch shipment.Status(sh.CurrentStatus) {
	case shipment.StatusOriginBranchReceived, shipment.StatusTransitHubReceived,
		shipment.StatusDestinationHubReceived, shipment.StatusRTOInTransit,
		shipment.StatusRTOInitiated:
	default:
		return apierr.Conflict("SHIPMENT_INVALID_STATE",
			"This shipment is not in a state that can be bagged.").
			WithDetail("awb", sh.Awb).
			WithDetail("currentStatus", sh.CurrentStatus)
	}
	if sh.CurrentCustodyUnitID == nil || *sh.CurrentCustodyUnitID != bag.OriginUnitID {
		return apierr.Conflict("CUSTODY_VIOLATION",
			"This shipment is not held at the facility that opened the bag.").
			WithDetail("awb", sh.Awb)
	}
	if sh.IsHeld {
		return apierr.Conflict("SHIPMENT_ON_HOLD",
			"This shipment is on hold and cannot be bagged.").WithDetail("awb", sh.Awb)
	}

	// Direction.
	if sh.MovementDirection != bag.Direction {
		return apierr.Conflict("BAG_DIRECTION_MISMATCH",
			fmt.Sprintf("This is a %s bag and the shipment is moving %s.",
				bag.Direction, sh.MovementDirection)).
			WithDetail("awb", sh.Awb)
	}

	// Service restriction.
	if bag.CourierServiceID != nil && *bag.CourierServiceID != sh.CourierServiceID {
		return apierr.Conflict("BAG_SERVICE_MISMATCH",
			"This bag is restricted to a different courier service.").
			WithDetail("awb", sh.Awb)
	}

	// Destination: the bag's destination must be on the shipment's forward path.
	if !s.destinationOnPath(bag.DestinationUnitID, sh) {
		return apierr.Conflict("BAG_DESTINATION_MISMATCH",
			"This shipment is not routed through the bag's destination facility.").
			WithDetail("awb", sh.Awb).
			WithDetail("shipmentDestinationPincode", sh.DestinationPincode)
	}
	return nil
}

// destinationOnPath reports whether a facility is a point on the shipment's
// resolved route.
func (s *Service) destinationOnPath(unitID int64, sh dbgen.Shipment) bool {
	if sh.MovementDirection == string(shipment.DirectionReverse) {
		// The reverse path is the forward path read backwards, plus the origin
		// branch as the final return point.
		for _, candidate := range []*int64{sh.OriginBranchID, sh.OriginHubID, sh.DestinationHubID} {
			if candidate != nil && *candidate == unitID {
				return true
			}
		}
		return false
	}
	for _, candidate := range []*int64{sh.OriginHubID, sh.DestinationHubID, sh.DestinationBranchID} {
		if candidate != nil && *candidate == unitID {
			return true
		}
	}
	return false
}

func (s *Service) checkCapacity(bag dbgen.Bag) error {
	if bag.MaxShipments != nil && bag.ShipmentCount > *bag.MaxShipments {
		return apierr.Conflict("BAG_CAPACITY_EXCEEDED",
			"This bag is full.").
			WithDetail("maxShipments", *bag.MaxShipments)
	}
	if bag.MaxWeightGrams != nil && bag.TotalWeightGrams > int64(*bag.MaxWeightGrams) {
		return apierr.Conflict("BAG_CAPACITY_EXCEEDED",
			"This bag has exceeded its weight limit.").
			WithDetail("maxWeightGrams", *bag.MaxWeightGrams)
	}
	return nil
}

// RemoveItem takes a shipment out of an open bag.
func (s *Service) RemoveItem(
	ctx context.Context, p *tenant.Principal, bagID, barcode, reason string, device ops.Device,
) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: bagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		if err := p.RequireUnitInScope(bag.OriginUnitID); err != nil {
			return err
		}
		if bag.Status != StatusOpen {
			// The trigger would refuse anyway; this says why in operational terms.
			return apierr.Conflict("BAG_CONTENTS_FROZEN",
				"This bag is closed. Removing a shipment now requires the exception workflow.").
				WithDetail("currentStatus", bag.Status).
				WithDetail("hint", "Use POST /api/v1/bags/{bagId}/exception-correction")
		}

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
		if _, remErr := q.RemoveBagItem(ctx, dbgen.RemoveBagItemParams{
			BagID: bag.ID, ShipmentID: sh.ID,
			RemovedByUserID: &p.UserID, RemovalReason: &reason,
		}); remErr != nil {
			if ops.IsNoRows(remErr) {
				return apierr.NotFound("Bag item")
			}
			if msg, ok := ops.TriggerMessage(remErr); ok {
				return apierr.Conflict("BAG_CONTENTS_FROZEN", msg)
			}
			return apierr.Internal(remErr)
		}
		updated, cErr := q.RecalculateBagCounters(ctx, bag.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, bag.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		actor := device.Actor(p, origin)
		if shipment.Status(sh.CurrentStatus) == shipment.StatusOriginBagged {
			if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
				Shipment: sh, To: shipment.StatusOriginBranchReceived,
				Description: "Removed from bag " + bag.BagCode, Reason: reason,
				SetBag: true, BagID: nil,
				Metadata: map[string]any{"bagCode": bag.BagCode},
			}); tErr != nil {
				return tErr
			}
		} else {
			if _, sErr := q.SetShipmentCustody(ctx, dbgen.SetShipmentCustodyParams{
				ID: sh.ID, OrganizationID: p.OrganizationID, SetBag: true, BagID: nil,
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, origin, "ITEM_REMOVED", "", "",
			fmt.Sprintf("%s removed from the bag", sh.Awb), reason,
			map[string]any{"awb": sh.Awb}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionBagItemRemoved, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			OperatingUnitID: &bag.OriginUnitID, Reason: reason,
			After: map[string]any{"awb": sh.Awb, "bagCode": bag.BagCode},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return dErr
	})
	return detail, err
}

func (s *Service) destinationFacility(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*ops.Facility, error) {
	if publicID == "" {
		return nil, apierr.Validation("The destination facility is required.",
			map[string]any{"field": "destinationUnitId"})
	}
	row, err := s.q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Operating unit")
	}
	if row.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict,
			"The destination facility is not active.").WithDetail("operatingUnitCode", row.Code)
	}
	return &ops.Facility{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Name: row.Name,
		UnitType: row.UnitType, Status: row.Status, Pincode: row.Pincode,
	}, nil
}

func (s *Service) appendEvent(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, bag dbgen.Bag,
	at *ops.Facility, eventType, from, to, description, reason string, metadata map[string]any,
) (dbgen.BagEvent, error) {
	seq, err := q.BumpBagEventSequence(ctx, dbgen.BumpBagEventSequenceParams{
		ID: bag.ID, OrganizationID: bag.OrganizationID,
	})
	if err != nil {
		return dbgen.BagEvent{}, apierr.Internal(fmt.Errorf("reserve bag event sequence: %w", err))
	}
	params := dbgen.AppendBagEventParams{
		PublicID: publicid.New(publicid.PrefixBagEvent), OrganizationID: bag.OrganizationID,
		BagID: bag.ID, Sequence: seq.EventSequence, EventType: eventType,
		Description: description, Metadata: encodeJSON(metadata),
	}
	if from != "" {
		params.FromStatus = &from
	}
	if to != "" {
		params.ToStatus = &to
	}
	if reason != "" {
		params.ReasonCode = &reason
	}
	if p != nil {
		params.ActorUserID = &p.UserID
	}
	if at != nil {
		params.OperatingUnitID = &at.ID
	}
	ev, err := q.AppendBagEvent(ctx, params)
	if err != nil {
		return ev, apierr.Internal(fmt.Errorf("append bag event: %w", err))
	}
	return ev, nil
}

func bagKey(d ops.Device, bagCode, awb string) string {
	if !d.HasEventKey() {
		return ""
	}
	return "bag:" + d.EventID + ":" + bagCode + ":" + awb
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

func asAPIError(err error, target **apierr.Error) bool {
	e, ok := err.(*apierr.Error)
	if ok {
		*target = e
	}
	return ok
}
