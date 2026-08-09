// Package manifest implements M12: the electronic handover document between
// two facilities.
//
// A manifest says: "these bags and these loose shipments left facility A on
// this trip, bound for facility B". It is the document a driver signs and the
// document the receiving hub counts against. Two consequences:
//
//   - Closure freezes the contents, enforced by the manifest_contents_guard
//     trigger. What the driver signed for cannot be edited afterwards.
//   - Dispatch and receipt move every shipment on the manifest, in one
//     transaction. A manifest that is half-received is worse than one that was
//     never received, because reconciliation has nothing consistent to compare.
package manifest

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
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Manifest states.
const (
	StatusDraft      = "DRAFT"
	StatusClosed     = "CLOSED"
	StatusDispatched = "DISPATCHED"
	StatusReceived   = "RECEIVED"
	StatusReconciled = "RECONCILED"
	StatusCancelled  = "CANCELLED"
)

// AllStatuses is the published lifecycle.
var AllStatuses = []string{
	StatusDraft, StatusClosed, StatusDispatched, StatusReceived, StatusReconciled, StatusCancelled,
}

var transitions = map[string][]string{
	StatusDraft:      {StatusClosed, StatusCancelled},
	StatusClosed:     {StatusDispatched, StatusDraft, StatusCancelled},
	StatusDispatched: {StatusReceived},
	StatusReceived:   {StatusReconciled},
}

func canTransition(from, to string) bool {
	for _, a := range transitions[from] {
		if a == to {
			return true
		}
	}
	return false
}

func invalidState(from, to string) error {
	return apierr.Conflict("MANIFEST_INVALID_STATE",
		fmt.Sprintf("A manifest cannot move from %s to %s.", from, to)).
		WithDetail("currentStatus", from).
		WithDetail("attemptedStatus", to).
		WithDetail("allowedTransitions", transitions[from])
}

// Service implements the manifest workflow.
type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	units *ops.Resolver
	codes *ops.CodeAllocator
	trans *shipment.Transitioner
	jobs  *jobs.Enqueuer
	audit *audit.Recorder
	log   *slog.Logger
}

// NewService builds the manifest service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, enqueuer *jobs.Enqueuer, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, units: units, codes: codes, trans: trans, jobs: enqueuer, audit: rec, log: log}
}

// CreateInput opens a draft manifest.
type CreateInput struct {
	OriginUnitID      string
	DestinationUnitID string
	TripID            string
	Direction         string
	Remarks           string
	Metadata          map[string]any
}

// Create opens a draft manifest.
func (s *Service) Create(ctx context.Context, p *tenant.Principal, in CreateInput) (*Detail, error) {
	origin, err := s.units.RequireFacility(ctx, p, in.OriginUnitID)
	if err != nil {
		return nil, err
	}
	dest, err := s.lookupUnit(ctx, p, in.DestinationUnitID, "destinationUnitId")
	if err != nil {
		return nil, err
	}
	if dest.ID == origin.ID {
		return nil, apierr.Validation("A manifest cannot travel from a facility to itself.",
			map[string]any{"field": "destinationUnitId"})
	}

	var tripID *int64
	if in.TripID != "" {
		trip, tErr := s.q.GetTripByPublicID(ctx, dbgen.GetTripByPublicIDParams{
			PublicID: in.TripID, OrganizationID: p.OrganizationID,
		})
		if tErr != nil {
			return nil, ops.NotFoundOr(tErr, "Trip")
		}
		if trip.Status != "PLANNED" && trip.Status != "LOADING" {
			return nil, apierr.Conflict(apierr.CodeConflict,
				"This trip has already departed and cannot take new manifests.").
				WithDetail("tripStatus", trip.Status)
		}
		tripID = &trip.ID
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindManifest, origin.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *Detail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		created, cErr := q.CreateManifest(ctx, dbgen.CreateManifestParams{
			PublicID: publicid.New(publicid.PrefixManifest), OrganizationID: p.OrganizationID,
			ManifestCode: code, OriginUnitID: origin.ID, DestinationUnitID: dest.ID,
			TripID: tripID, Direction: orDefault(in.Direction, "FORWARD"),
			Remarks: ops.Optional(in.Remarks), Metadata: encodeJSON(in.Metadata),
		})
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create manifest: %w", cErr))
		}
		if _, eErr := s.appendEvent(ctx, q, p, created, origin, "CREATED", "", StatusDraft,
			fmt.Sprintf("Manifest opened at %s for %s", origin.Code, dest.Code), nil); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionManifestCreated, ResourceType: "manifest",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			OperatingUnitID: &origin.ID,
			After: map[string]any{
				"manifestCode": code, "origin": origin.Code, "destination": dest.Code,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, created.PublicID)
		return dErr
	})
	return detail, err
}

// AddContentInput loads bags or loose shipments onto a draft manifest.
type AddContentInput struct {
	ManifestID string
	BagCodes   []string
	Barcodes   []string
	Device     ops.Device
}

// ContentResult is the outcome for one item.
type ContentResult struct {
	Reference string `json:"reference"`
	Kind      string `json:"kind"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
}

// AddContentResult is the batch outcome.
type AddContentResult struct {
	Added    int             `json:"added"`
	Rejected int             `json:"rejected"`
	Results  []ContentResult `json:"results"`
	Manifest *Detail         `json:"manifest"`
}

// AddContent loads bags and loose shipments onto a draft manifest.
func (s *Service) AddContent(ctx context.Context, p *tenant.Principal, in AddContentInput) (*AddContentResult, error) {
	out := &AddContentResult{Results: make([]ContentResult, 0, len(in.BagCodes)+len(in.Barcodes))}

	for _, code := range in.BagCodes {
		res := s.addOne(ctx, p, in.ManifestID, code, "BAG")
		out.Results = append(out.Results, res)
		if res.Outcome == "ADDED" {
			out.Added++
		} else {
			out.Rejected++
		}
	}
	for _, barcode := range in.Barcodes {
		res := s.addOne(ctx, p, in.ManifestID, barcode, "SHIPMENT")
		out.Results = append(out.Results, res)
		if res.Outcome == "ADDED" {
			out.Added++
		} else {
			out.Rejected++
		}
	}

	detail, err := s.Get(ctx, p, in.ManifestID)
	if err != nil {
		return nil, err
	}
	out.Manifest = detail
	return out, nil
}

func (s *Service) addOne(ctx context.Context, p *tenant.Principal, manifestID, ref, kind string) ContentResult {
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: manifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if m.Status != StatusDraft {
			return apierr.Conflict("MANIFEST_NOT_DRAFT",
				"Contents can only be added while the manifest is a draft.").
				WithDetail("currentStatus", m.Status)
		}
		if err := p.RequireUnitInScope(m.OriginUnitID); err != nil {
			return err
		}

		if kind == "BAG" {
			return s.addBag(ctx, q, p, m, ref)
		}
		return s.addLooseShipment(ctx, q, p, m, ref)
	})
	if err != nil {
		var ae *apierr.Error
		if asAPIError(err, &ae) && ae.Status < 500 {
			return ContentResult{Reference: ref, Kind: kind, Outcome: "REJECTED",
				Reason: string(ae.Code), Message: ae.Message}
		}
		return ContentResult{Reference: ref, Kind: kind, Outcome: "REJECTED",
			Reason: "INTERNAL_ERROR", Message: "The item could not be loaded."}
	}
	return ContentResult{Reference: ref, Kind: kind, Outcome: "ADDED"}
}

// addBag loads a closed bag onto the manifest.
//
// The bag must be CLOSED: an open bag has no frozen declaration, so loading one
// would put a manifest into the world describing contents that can still change.
func (s *Service) addBag(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, m dbgen.Manifest, code string,
) error {
	bag, err := q.GetBagByBarcode(ctx, dbgen.GetBagByBarcodeParams{
		OrganizationID: p.OrganizationID, Barcode: code,
	})
	if err != nil {
		return ops.NotFoundOr(err, "Bag")
	}
	if bag.Status != "CLOSED" {
		return apierr.Conflict("BAG_NOT_CLOSED",
			"Only a closed bag can be loaded onto a manifest.").
			WithDetail("bagCode", bag.BagCode).
			WithDetail("bagStatus", bag.Status)
	}
	if bag.OriginUnitID != m.OriginUnitID {
		return apierr.Conflict("MANIFEST_ORIGIN_MISMATCH",
			"This bag was not closed at the manifest's origin facility.").
			WithDetail("bagCode", bag.BagCode)
	}
	// The bag's destination must be reachable on this manifest's leg. Loading a
	// Chennai bag onto a Delhi manifest is the classic misroute, and catching it
	// at load time is far cheaper than at the far end.
	if bag.DestinationUnitID != m.DestinationUnitID {
		return apierr.Conflict("MANIFEST_DESTINATION_MISMATCH",
			"This bag is addressed to a different facility than the manifest.").
			WithDetail("bagCode", bag.BagCode)
	}
	if bag.Direction != m.Direction {
		return apierr.Conflict("MANIFEST_DIRECTION_MISMATCH",
			"This bag is travelling in the other direction.").
			WithDetail("bagCode", bag.BagCode)
	}

	if _, err := q.AddManifestBag(ctx, dbgen.AddManifestBagParams{
		OrganizationID: p.OrganizationID, ManifestID: m.ID, BagID: bag.ID,
		DeclaredShipmentCount: bag.ShipmentCount, DeclaredPieceCount: bag.PieceCount,
		DeclaredWeightGrams: bag.TotalWeightGrams, AddedByUserID: &p.UserID,
	}); err != nil {
		switch {
		case ops.IsUnique(err, "manifest_bags_active_idx"):
			return apierr.Conflict("BAG_ALREADY_MANIFESTED",
				"This bag is already travelling on another manifest.").
				WithDetail("bagCode", bag.BagCode)
		case ops.IsUnique(err, "manifest_bags_unique"):
			return apierr.Conflict(apierr.CodeDuplicate, "This bag is already on this manifest.")
		}
		if msg, ok := ops.TriggerMessage(err); ok {
			return apierr.Conflict("MANIFEST_NOT_DRAFT", msg)
		}
		return apierr.Internal(fmt.Errorf("add manifest bag: %w", err))
	}
	updated, err := q.RecalculateManifestCounters(ctx, m.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	_, err = s.appendEvent(ctx, q, p, updated, nil, "BAG_ADDED", "", "",
		"Bag "+bag.BagCode+" loaded", map[string]any{"bagCode": bag.BagCode})
	return err
}

// addLooseShipment puts a single parcel on the manifest without a bag.
func (s *Service) addLooseShipment(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, m dbgen.Manifest, barcode string,
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
	// A parcel inside a bag travels with the bag. Manifesting it separately
	// would double-count it and make reconciliation impossible.
	if sh.CurrentBagID != nil {
		return apierr.Conflict("SHIPMENT_IN_BAG",
			"This shipment is inside a bag. Load the bag instead.").
			WithDetail("awb", sh.Awb)
	}
	if sh.CurrentCustodyUnitID == nil || *sh.CurrentCustodyUnitID != m.OriginUnitID {
		return apierr.Conflict("CUSTODY_VIOLATION",
			"This shipment is not held at the manifest's origin facility.").
			WithDetail("awb", sh.Awb)
	}
	if sh.IsHeld {
		return apierr.Conflict("SHIPMENT_ON_HOLD",
			"This shipment is on hold and cannot be dispatched.").WithDetail("awb", sh.Awb)
	}
	if sh.MovementDirection != m.Direction {
		return apierr.Conflict("MANIFEST_DIRECTION_MISMATCH",
			"This shipment is travelling in the other direction.").WithDetail("awb", sh.Awb)
	}

	if _, err := q.AddManifestLooseShipment(ctx, dbgen.AddManifestLooseShipmentParams{
		OrganizationID: p.OrganizationID, ManifestID: m.ID, ShipmentID: sh.ID,
		PieceCount: sh.PieceCount, WeightGrams: sh.ChargeableWeightGrams,
		AddedByUserID: &p.UserID,
	}); err != nil {
		switch {
		case ops.IsUnique(err, "manifest_loose_active_idx"):
			return apierr.Conflict("SHIPMENT_ALREADY_MANIFESTED",
				"This shipment is already travelling on another manifest.").
				WithDetail("awb", sh.Awb)
		case ops.IsUnique(err, "manifest_loose_unique"):
			return apierr.Conflict(apierr.CodeDuplicate, "This shipment is already on this manifest.")
		}
		if msg, ok := ops.TriggerMessage(err); ok {
			return apierr.Conflict("MANIFEST_NOT_DRAFT", msg)
		}
		return apierr.Internal(fmt.Errorf("add loose shipment: %w", err))
	}
	updated, err := q.RecalculateManifestCounters(ctx, m.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	_, err = s.appendEvent(ctx, q, p, updated, nil, "SHIPMENT_ADDED", "", "",
		"Shipment "+sh.Awb+" loaded", map[string]any{"awb": sh.Awb})
	return err
}

// RemoveContent takes an item off a draft manifest.
func (s *Service) RemoveContent(
	ctx context.Context, p *tenant.Principal, manifestID, ref, kind, reason string,
) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: manifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if err := p.RequireUnitInScope(m.OriginUnitID); err != nil {
			return err
		}
		if m.Status != StatusDraft {
			return apierr.Conflict("MANIFEST_CONTENTS_FROZEN",
				"This manifest is closed. Its contents cannot be changed.").
				WithDetail("currentStatus", m.Status)
		}

		if kind == "BAG" {
			bag, bErr := q.GetBagByBarcode(ctx, dbgen.GetBagByBarcodeParams{
				OrganizationID: p.OrganizationID, Barcode: ref,
			})
			if bErr != nil {
				return ops.NotFoundOr(bErr, "Bag")
			}
			if _, rErr := q.RemoveManifestBag(ctx, dbgen.RemoveManifestBagParams{
				ManifestID: m.ID, BagID: bag.ID, RemovalReason: &reason,
			}); rErr != nil {
				if ops.IsNoRows(rErr) {
					return apierr.NotFound("Manifest bag")
				}
				if msg, ok := ops.TriggerMessage(rErr); ok {
					return apierr.Conflict("MANIFEST_CONTENTS_FROZEN", msg)
				}
				return apierr.Internal(rErr)
			}
		} else {
			resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
				Barcode: ref, OrganizationID: p.OrganizationID,
			})
			if rErr != nil {
				return ops.NotFoundOr(rErr, "Shipment")
			}
			if _, remErr := q.RemoveManifestLooseShipment(ctx, dbgen.RemoveManifestLooseShipmentParams{
				ManifestID: m.ID, ShipmentID: resolved.ID, RemovalReason: &reason,
			}); remErr != nil {
				if ops.IsNoRows(remErr) {
					return apierr.NotFound("Manifest shipment")
				}
				if msg, ok := ops.TriggerMessage(remErr); ok {
					return apierr.Conflict("MANIFEST_CONTENTS_FROZEN", msg)
				}
				return apierr.Internal(remErr)
			}
		}
		updated, cErr := q.RecalculateManifestCounters(ctx, m.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		eventType := "BAG_REMOVED"
		if kind != "BAG" {
			eventType = "SHIPMENT_REMOVED"
		}
		if _, eErr := s.appendEvent(ctx, q, p, updated, nil, eventType, "", "",
			ref+" removed from the manifest", map[string]any{"reference": ref, "reason": reason}); eErr != nil {
			return eErr
		}
		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, m.PublicID)
		return dErr
	})
	return detail, err
}

func (s *Service) lookupUnit(
	ctx context.Context, p *tenant.Principal, publicID, field string,
) (*ops.Facility, error) {
	if publicID == "" {
		return nil, apierr.Validation("This facility is required.", map[string]any{"field": field})
	}
	row, err := s.q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Operating unit")
	}
	return &ops.Facility{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Name: row.Name,
		UnitType: row.UnitType, Status: row.Status, Pincode: row.Pincode,
	}, nil
}

func (s *Service) appendEvent(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, m dbgen.Manifest,
	at *ops.Facility, eventType, from, to, description string, metadata map[string]any,
) (dbgen.ManifestEvent, error) {
	seq, err := q.BumpManifestEventSequence(ctx, dbgen.BumpManifestEventSequenceParams{
		ID: m.ID, OrganizationID: m.OrganizationID,
	})
	if err != nil {
		return dbgen.ManifestEvent{}, apierr.Internal(fmt.Errorf("reserve manifest event sequence: %w", err))
	}
	params := dbgen.AppendManifestEventParams{
		PublicID: publicid.New(publicid.PrefixManifestEvent), OrganizationID: m.OrganizationID,
		ManifestID: m.ID, Sequence: seq.EventSequence, EventType: eventType,
		Description: description, Metadata: encodeJSON(metadata),
	}
	if from != "" {
		params.FromStatus = &from
	}
	if to != "" {
		params.ToStatus = &to
	}
	if p != nil {
		params.ActorUserID = &p.UserID
	}
	if at != nil {
		params.OperatingUnitID = &at.ID
	}
	ev, err := q.AppendManifestEvent(ctx, params)
	if err != nil {
		return ev, apierr.Internal(fmt.Errorf("append manifest event: %w", err))
	}
	return ev, nil
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
