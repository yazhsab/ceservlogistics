// Package linehaul implements M13: carriers, vehicles, drivers and trips.
//
// A trip is the physical movement of manifests between facilities. The rule
// that shapes the whole module is in the Constitution §17: "Do not update
// shipment states blindly from trip operations; validate manifest membership
// and custody." A trip departing does not mean every parcel at the origin
// left — only the ones on its manifests did.
//
// Departure validation is therefore strict: every manifest must be closed, the
// vehicle and driver must be assigned, and the trip must be at its origin. A
// trip that departs with a draft manifest has left with contents nobody has
// declared, which is unreconcilable at the far end.
package linehaul

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

// Trip states.
const (
	StatusPlanned   = "PLANNED"
	StatusLoading   = "LOADING"
	StatusDeparted  = "DEPARTED"
	StatusInTransit = "IN_TRANSIT"
	StatusArrived   = "ARRIVED"
	StatusClosed    = "CLOSED"
	StatusCancelled = "CANCELLED"
)

// AllStatuses is the published trip lifecycle.
var AllStatuses = []string{
	StatusPlanned, StatusLoading, StatusDeparted, StatusInTransit,
	StatusArrived, StatusClosed, StatusCancelled,
}

// Modes of transport.
var AllModes = []string{"ROAD", "AIR", "RAIL", "PARTNER"}

var transitions = map[string][]string{
	StatusPlanned:   {StatusLoading, StatusDeparted, StatusCancelled},
	StatusLoading:   {StatusDeparted, StatusPlanned, StatusCancelled},
	StatusDeparted:  {StatusInTransit, StatusArrived},
	StatusInTransit: {StatusArrived},
	StatusArrived:   {StatusClosed, StatusInTransit},
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
	return apierr.Conflict("TRIP_INVALID_STATE",
		fmt.Sprintf("A trip cannot move from %s to %s.", from, to)).
		WithDetail("currentStatus", from).
		WithDetail("attemptedStatus", to).
		WithDetail("allowedTransitions", transitions[from])
}

// Service implements the line-haul workflow.
type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	units *ops.Resolver
	codes *ops.CodeAllocator
	trans *shipment.Transitioner
	audit *audit.Recorder
	log   *slog.Logger
}

// NewService builds the line-haul service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, units: units, codes: codes, trans: trans, audit: rec, log: log}
}

// LegInput is one hop of a multi-leg trip.
type LegInput struct {
	OriginUnitID       string
	DestinationUnitID  string
	ScheduledDeparture time.Time
	ScheduledArrival   time.Time
	DistanceKM         *int
}

// CreateTripInput plans a trip.
type CreateTripInput struct {
	Mode               string
	OriginUnitID       string
	DestinationUnitID  string
	Direction          string
	CarrierID          string
	VehicleID          string
	DriverID           string
	ExternalReference  string
	ScheduledDeparture time.Time
	ScheduledArrival   time.Time
	Legs               []LegInput
	Metadata           map[string]any
}

// CreateTrip plans a trip and its legs.
//
// A single-hop trip gets one implicit leg, so downstream code never has to
// special-case "trip with no legs". A multi-leg trip has its legs validated as
// a chain: each leg must start where the previous one ended, and the whole
// chain must run from the trip's origin to its destination. A broken chain is
// a planning error that would otherwise surface as parcels stranded at an
// intermediate stop.
func (s *Service) CreateTrip(ctx context.Context, p *tenant.Principal, in CreateTripInput) (*TripDetail, error) {
	origin, err := s.units.RequireFacility(ctx, p, in.OriginUnitID)
	if err != nil {
		return nil, err
	}
	dest, err := s.lookupUnit(ctx, p, in.DestinationUnitID, "destinationUnitId")
	if err != nil {
		return nil, err
	}
	if dest.ID == origin.ID {
		return nil, apierr.Validation("A trip cannot start and end at the same facility.",
			map[string]any{"field": "destinationUnitId"})
	}

	carrier, vehicle, driver, err := s.resolveAssets(ctx, p, in)
	if err != nil {
		return nil, err
	}

	legs, err := s.validateLegs(ctx, p, in, origin, dest)
	if err != nil {
		return nil, err
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindTrip, origin.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *TripDetail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateTripParams{
			PublicID: publicid.New(publicid.PrefixTrip), OrganizationID: p.OrganizationID,
			TripCode: code, Mode: in.Mode,
			OriginUnitID: origin.ID, DestinationUnitID: dest.ID,
			Direction:          orDefault(in.Direction, "FORWARD"),
			ExternalReference:  ops.Optional(in.ExternalReference),
			ScheduledDeparture: in.ScheduledDeparture, ScheduledArrival: in.ScheduledArrival,
			LegCount: int32(len(legs)), CreatedByUserID: &p.UserID,
			Metadata: encodeJSON(in.Metadata),
		}
		if carrier != nil {
			params.CarrierID = &carrier.ID
		}
		if vehicle != nil {
			params.VehicleID = &vehicle.ID
		}
		if driver != nil {
			params.PrimaryDriverID = &driver.ID
		}
		trip, cErr := q.CreateTrip(ctx, params)
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create trip: %w", cErr))
		}

		for i, leg := range legs {
			legParams := dbgen.CreateTripLegParams{
				PublicID: publicid.New(publicid.PrefixTripLeg), OrganizationID: p.OrganizationID,
				TripID: trip.ID, Sequence: int32(i + 1),
				OriginUnitID: leg.originID, DestinationUnitID: leg.destID,
				ScheduledDeparture: leg.departure, ScheduledArrival: leg.arrival,
			}
			if leg.distanceKM != nil {
				v := int32(*leg.distanceKM)
				legParams.DistanceKm = &v
			}
			if _, lErr := q.CreateTripLeg(ctx, legParams); lErr != nil {
				return apierr.Internal(fmt.Errorf("create trip leg %d: %w", i+1, lErr))
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, trip, nil, origin, "CREATED", "", StatusPlanned,
			fmt.Sprintf("Trip planned from %s to %s", origin.Code, dest.Code),
			map[string]any{"legCount": len(legs), "mode": in.Mode}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripCreated, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			OperatingUnitID: &origin.ID,
			After: map[string]any{
				"tripCode": code, "mode": in.Mode, "origin": origin.Code,
				"destination": dest.Code, "legCount": len(legs),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadTripDetail(ctx, q, p, trip.PublicID)
		return dErr
	})
	return detail, err
}

type resolvedLeg struct {
	originID, destID int64
	departure        time.Time
	arrival          time.Time
	distanceKM       *int
}

// validateLegs checks that the legs form a connected chain from origin to
// destination, in ascending time order.
func (s *Service) validateLegs(
	ctx context.Context, p *tenant.Principal, in CreateTripInput, origin, dest *ops.Facility,
) ([]resolvedLeg, error) {
	if len(in.Legs) == 0 {
		return []resolvedLeg{{
			originID: origin.ID, destID: dest.ID,
			departure: in.ScheduledDeparture, arrival: in.ScheduledArrival,
		}}, nil
	}

	out := make([]resolvedLeg, 0, len(in.Legs))
	previousDest := origin.ID
	var previousArrival time.Time
	for i, leg := range in.Legs {
		field := fmt.Sprintf("legs[%d]", i)
		from, err := s.lookupUnit(ctx, p, leg.OriginUnitID, field+".originUnitId")
		if err != nil {
			return nil, err
		}
		to, err := s.lookupUnit(ctx, p, leg.DestinationUnitID, field+".destinationUnitId")
		if err != nil {
			return nil, err
		}
		if from.ID != previousDest {
			return nil, apierr.Validation(
				"Each leg must start where the previous one ended.",
				map[string]any{"field": field + ".originUnitId", "expectedOrigin": previousDest})
		}
		if from.ID == to.ID {
			return nil, apierr.Validation("A leg cannot start and end at the same facility.",
				map[string]any{"field": field})
		}
		if !leg.ScheduledArrival.After(leg.ScheduledDeparture) {
			return nil, apierr.Validation("A leg must arrive after it departs.",
				map[string]any{"field": field + ".scheduledArrival"})
		}
		if !previousArrival.IsZero() && leg.ScheduledDeparture.Before(previousArrival) {
			return nil, apierr.Validation("A leg cannot depart before the previous one arrives.",
				map[string]any{"field": field + ".scheduledDeparture"})
		}
		out = append(out, resolvedLeg{
			originID: from.ID, destID: to.ID,
			departure: leg.ScheduledDeparture, arrival: leg.ScheduledArrival,
			distanceKM: leg.DistanceKM,
		})
		previousDest = to.ID
		previousArrival = leg.ScheduledArrival
	}
	if previousDest != dest.ID {
		return nil, apierr.Validation("The last leg must end at the trip's destination.",
			map[string]any{"field": "legs"})
	}
	return out, nil
}

func (s *Service) resolveAssets(
	ctx context.Context, p *tenant.Principal, in CreateTripInput,
) (carrier *dbgen.Carrier, vehicle *dbgen.GetVehicleByPublicIDRow, driver *dbgen.GetDriverByPublicIDRow, err error) {
	if in.CarrierID != "" {
		c, cErr := s.q.GetCarrierByPublicID(ctx, dbgen.GetCarrierByPublicIDParams{
			PublicID: in.CarrierID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			return nil, nil, nil, ops.NotFoundOr(cErr, "Carrier")
		}
		if !c.IsActive {
			return nil, nil, nil, apierr.Conflict(apierr.CodeConflict, "This carrier is not active.")
		}
		if !contains(c.Modes, in.Mode) {
			return nil, nil, nil, apierr.Validation(
				"This carrier does not operate that mode of transport.",
				map[string]any{"field": "mode", "carrierModes": c.Modes})
		}
		carrier = &c
	}
	if in.VehicleID != "" {
		v, vErr := s.q.GetVehicleByPublicID(ctx, dbgen.GetVehicleByPublicIDParams{
			PublicID: in.VehicleID, OrganizationID: p.OrganizationID,
		})
		if vErr != nil {
			return nil, nil, nil, ops.NotFoundOr(vErr, "Vehicle")
		}
		if !v.IsActive {
			return nil, nil, nil, apierr.Conflict(apierr.CodeConflict, "This vehicle is not active.")
		}
		vehicle = &v
	}
	if in.DriverID != "" {
		d, dErr := s.q.GetDriverByPublicID(ctx, dbgen.GetDriverByPublicIDParams{
			PublicID: in.DriverID, OrganizationID: p.OrganizationID,
		})
		if dErr != nil {
			return nil, nil, nil, ops.NotFoundOr(dErr, "Driver")
		}
		if !d.IsActive {
			return nil, nil, nil, apierr.Conflict(apierr.CodeConflict, "This driver is not active.")
		}
		driver = &d
	}
	return carrier, vehicle, driver, nil
}

// AssignInput attaches or changes a trip's vehicle and crew.
type AssignInput struct {
	TripID    string
	VehicleID string
	DriverID  string
	CarrierID string
	Reference string
}

// Assign sets the vehicle, driver and carrier on a planned trip.
func (s *Service) Assign(ctx context.Context, p *tenant.Principal, in AssignInput) (*TripDetail, error) {
	var detail *TripDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		trip, tErr := q.LockTripForUpdate(ctx, dbgen.LockTripForUpdateParams{
			PublicID: in.TripID, OrganizationID: p.OrganizationID,
		})
		if tErr != nil {
			return ops.NotFoundOr(tErr, "Trip")
		}
		if err := p.RequireUnitInScope(trip.OriginUnitID); err != nil {
			return err
		}
		if trip.Status != StatusPlanned && trip.Status != StatusLoading {
			return apierr.Conflict("TRIP_INVALID_STATE",
				"A trip that has departed cannot have its vehicle or driver changed.").
				WithDetail("currentStatus", trip.Status)
		}

		params := dbgen.UpdateTripAssetsParams{ID: trip.ID, OrganizationID: p.OrganizationID}
		if in.VehicleID != "" {
			v, vErr := q.GetVehicleByPublicID(ctx, dbgen.GetVehicleByPublicIDParams{
				PublicID: in.VehicleID, OrganizationID: p.OrganizationID,
			})
			if vErr != nil {
				return ops.NotFoundOr(vErr, "Vehicle")
			}
			params.VehicleID = &v.ID
		}
		if in.DriverID != "" {
			d, dErr := q.GetDriverByPublicID(ctx, dbgen.GetDriverByPublicIDParams{
				PublicID: in.DriverID, OrganizationID: p.OrganizationID,
			})
			if dErr != nil {
				return ops.NotFoundOr(dErr, "Driver")
			}
			params.PrimaryDriverID = &d.ID
		}
		if in.CarrierID != "" {
			c, cErr := q.GetCarrierByPublicID(ctx, dbgen.GetCarrierByPublicIDParams{
				PublicID: in.CarrierID, OrganizationID: p.OrganizationID,
			})
			if cErr != nil {
				return ops.NotFoundOr(cErr, "Carrier")
			}
			params.CarrierID = &c.ID
		}
		if in.Reference != "" {
			params.ExternalReference = &in.Reference
		}
		if _, uErr := q.UpdateTripAssets(ctx, params); uErr != nil {
			// The partial unique indexes on vehicle and driver prevent a live
			// double-booking; report it as a conflict rather than a 500.
			switch {
			case ops.IsUnique(uErr, "trips_vehicle_active_idx"):
				return apierr.Conflict("VEHICLE_IN_USE",
					"This vehicle is already on another live trip.")
			case ops.IsUnique(uErr, "trips_driver_active_idx"):
				return apierr.Conflict("DRIVER_IN_USE",
					"This driver is already on another live trip.")
			}
			return ops.ConflictOr(uErr, "This trip changed while it was being updated.")
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripCrewChange, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			After: map[string]any{
				"vehicleId": in.VehicleID, "driverId": in.DriverID, "carrierId": in.CarrierID,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadTripDetail(ctx, q, p, trip.PublicID)
		return dErr
	})
	return detail, err
}

// AttachManifest puts a closed manifest on a trip.
func (s *Service) AttachManifest(
	ctx context.Context, p *tenant.Principal, tripID, manifestID string, legSequence *int,
) (*TripDetail, error) {
	var detail *TripDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		trip, tErr := q.LockTripForUpdate(ctx, dbgen.LockTripForUpdateParams{
			PublicID: tripID, OrganizationID: p.OrganizationID,
		})
		if tErr != nil {
			return ops.NotFoundOr(tErr, "Trip")
		}
		if err := p.RequireUnitInScope(trip.OriginUnitID); err != nil {
			return err
		}
		if trip.Status != StatusPlanned && trip.Status != StatusLoading {
			return apierr.Conflict("TRIP_INVALID_STATE",
				"This trip has already departed and cannot take new manifests.").
				WithDetail("currentStatus", trip.Status)
		}
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: manifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if m.Status != "DRAFT" && m.Status != "CLOSED" {
			return apierr.Conflict("MANIFEST_INVALID_STATE",
				"Only a draft or closed manifest can be attached to a trip.").
				WithDetail("currentStatus", m.Status)
		}

		var legID *int64
		if legSequence != nil {
			leg, lErr := q.GetTripLegBySequence(ctx, dbgen.GetTripLegBySequenceParams{
				TripID: trip.ID, Sequence: int32(*legSequence),
			})
			if lErr != nil {
				return ops.NotFoundOr(lErr, "Trip leg")
			}
			// The manifest must be going where this leg is going, or it will be
			// carried past its destination.
			if leg.DestinationUnitID != m.DestinationUnitID {
				return apierr.Conflict("MANIFEST_LEG_MISMATCH",
					"This leg does not end at the manifest's destination.").
					WithDetail("legSequence", *legSequence)
			}
			legID = &leg.ID
		} else if m.DestinationUnitID != trip.DestinationUnitID {
			// Without an explicit leg the manifest rides the whole trip, so its
			// destination must be the trip's destination.
			return apierr.Conflict("MANIFEST_TRIP_MISMATCH",
				"This manifest is not addressed to the trip's destination. Name the leg it travels on.").
				WithDetail("manifestCode", m.ManifestCode)
		}

		if _, aErr := q.AttachManifestToTrip(ctx, dbgen.AttachManifestToTripParams{
			ID: m.ID, OrganizationID: p.OrganizationID, TripID: &trip.ID, TripLegID: legID,
		}); aErr != nil {
			return ops.ConflictOr(aErr, "This manifest changed while it was being attached.")
		}
		if _, rErr := q.RecalculateTripCounters(ctx, trip.ID); rErr != nil {
			return apierr.Internal(rErr)
		}
		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, trip.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		if _, eErr := s.appendEvent(ctx, q, p, trip, legID, origin, "MANIFEST_ATTACHED", "", "",
			"Manifest "+m.ManifestCode+" attached",
			map[string]any{"manifestCode": m.ManifestCode}); eErr != nil {
			return eErr
		}
		var dErr error
		detail, dErr = s.loadTripDetail(ctx, q, p, trip.PublicID)
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
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, trip dbgen.Trip,
	legID *int64, at *ops.Facility, eventType, from, to, description string, metadata map[string]any,
) (dbgen.TripEvent, error) {
	seq, err := q.BumpTripEventSequence(ctx, dbgen.BumpTripEventSequenceParams{
		ID: trip.ID, OrganizationID: trip.OrganizationID,
	})
	if err != nil {
		return dbgen.TripEvent{}, apierr.Internal(fmt.Errorf("reserve trip event sequence: %w", err))
	}
	params := dbgen.AppendTripEventParams{
		PublicID: publicid.New(publicid.PrefixTripEvent), OrganizationID: trip.OrganizationID,
		TripID: trip.ID, TripLegID: legID, Sequence: seq.EventSequence,
		EventType: eventType, Description: description, Metadata: encodeJSON(metadata),
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
	ev, err := q.AppendTripEvent(ctx, params)
	if err != nil {
		return ev, apierr.Internal(fmt.Errorf("append trip event: %w", err))
	}
	return ev, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
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
