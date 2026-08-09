package linehaul

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// DepartInput records a trip leaving.
type DepartInput struct {
	TripID     string
	OdometerKM *int
	SealNumber string
	OccurredAt *time.Time
	Device     ops.Device
}

// Depart validates and records a trip's departure.
//
// The validation is the substance of this function (§17):
//
//	manifest closed      no draft manifests may travel
//	required assignment  a vehicle and a driver must be named
//	origin custody       every shipment must be dispatched from this facility
//
// The last one is delegated to the manifest dispatch that must already have
// happened: a manifest still in CLOSED has not been dispatched, so its
// shipments are still sitting in the origin's custody. Departing a trip
// dispatches every manifest on it, which is where the custody check actually
// bites.
func (s *Service) Depart(ctx context.Context, p *tenant.Principal, in DepartInput) (*TripDetail, error) {
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
		if !canTransition(trip.Status, StatusDeparted) {
			return invalidState(trip.Status, StatusDeparted)
		}

		// 1. Required assignment.
		if trip.VehicleID == nil && trip.Mode == "ROAD" {
			return apierr.Conflict("TRIP_NO_VEHICLE",
				"A road trip cannot depart without a vehicle.")
		}
		if trip.PrimaryDriverID == nil && trip.Mode == "ROAD" {
			return apierr.Conflict("TRIP_NO_DRIVER",
				"A road trip cannot depart without a driver.")
		}
		if trip.Mode != "ROAD" && trip.ExternalReference == nil {
			// A flight or train needs its number, or the parcels on it cannot be
			// traced when it goes missing.
			return apierr.Conflict("TRIP_NO_REFERENCE",
				fmt.Sprintf("A %s trip cannot depart without its carrier reference.", trip.Mode)).
				WithDetail("mode", trip.Mode)
		}

		// 2. Every manifest closed.
		openCount, oErr := q.CountOpenTripManifests(ctx, &trip.ID)
		if oErr != nil {
			return apierr.Internal(oErr)
		}
		if openCount > 0 {
			return apierr.Conflict("TRIP_HAS_DRAFT_MANIFEST",
				"Every manifest on the trip must be closed before it departs.").
				WithDetail("draftManifests", openCount)
		}

		manifests, mErr := q.ListTripManifests(ctx, &trip.ID)
		if mErr != nil {
			return apierr.Internal(mErr)
		}
		if len(manifests) == 0 {
			return apierr.Conflict("TRIP_EMPTY",
				"A trip with no manifests has nothing to carry.")
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, trip.OriginUnitID)
		if fErr != nil {
			return fErr
		}

		params := dbgen.UpdateTripStatusParams{
			ID: trip.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: trip.Status, ToStatus: StatusDeparted,
			OccurredAt: in.OccurredAt, SealNumber: ops.Optional(in.SealNumber),
			CurrentLegSequence: ops.Ptr(int32(1)),
		}
		if in.OdometerKM != nil {
			v := int32(*in.OdometerKM)
			params.DepartureOdometerKm = &v
		}
		updated, uErr := q.UpdateTripStatus(ctx, params)
		if uErr != nil {
			return ops.ConflictOr(uErr, "This trip changed state while it was departing.")
		}

		// 3. Dispatch every manifest that has not already gone, and put its
		// parcels in transit.
		if err := s.dispatchManifests(ctx, tx, p, updated, manifests, origin, in.Device); err != nil {
			return err
		}

		// The first leg departs with the trip.
		if leg, lErr := q.GetTripLegBySequence(ctx, dbgen.GetTripLegBySequenceParams{
			TripID: trip.ID, Sequence: 1,
		}); lErr == nil {
			if _, luErr := q.UpdateTripLegStatus(ctx, dbgen.UpdateTripLegStatusParams{
				ID: leg.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: leg.Status, ToStatus: "DEPARTED", OccurredAt: in.OccurredAt,
			}); luErr != nil && !ops.IsNoRows(luErr) {
				return apierr.Internal(luErr)
			}
		} else if !ops.IsNoRows(lErr) {
			return apierr.Internal(lErr)
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, nil, origin, "DEPARTED",
			trip.Status, StatusDeparted, "Trip departed from "+origin.Code,
			map[string]any{
				"manifestCount": len(manifests), "shipmentCount": updated.ShipmentCount,
				"sealNumber": in.SealNumber,
			}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripDeparted, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			OperatingUnitID: &origin.ID,
			Before:          map[string]any{"status": trip.Status},
			After: map[string]any{
				"status": StatusDeparted, "tripCode": trip.TripCode,
				"manifestCount": len(manifests), "shipmentCount": updated.ShipmentCount,
				"sealNumber": in.SealNumber, "odometerKm": in.OdometerKM,
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

// dispatchManifests moves every manifest and its shipments onto the road.
func (s *Service) dispatchManifests(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, trip dbgen.Trip,
	manifests []dbgen.ListTripManifestsRow, origin *ops.Facility, device ops.Device,
) error {
	q := s.q.WithTx(tx)
	actor := device.Actor(p, origin)

	for _, m := range manifests {
		if m.Status == "CLOSED" {
			if _, uErr := q.UpdateManifestStatus(ctx, dbgen.UpdateManifestStatusParams{
				ID: m.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: "CLOSED", ToStatus: "DISPATCHED",
				DispatchedByUserID: &p.UserID, TripID: &trip.ID,
			}); uErr != nil {
				return ops.ConflictOr(uErr, "A manifest on this trip changed state.")
			}
			// Bags travel with the manifest.
			active := true
			bags, bErr := q.ListManifestBags(ctx, dbgen.ListManifestBagsParams{
				ManifestID: m.ID, OnlyActive: &active,
			})
			if bErr != nil {
				return apierr.Internal(bErr)
			}
			for _, b := range bags {
				if b.BagStatus != "CLOSED" {
					continue
				}
				if _, sErr := q.UpdateBagStatus(ctx, dbgen.UpdateBagStatusParams{
					ID: b.BagID, OrganizationID: p.OrganizationID,
					ExpectedStatus: "CLOSED", ToStatus: "DISPATCHED",
				}); sErr != nil {
					return ops.ConflictOr(sErr, "A bag on this trip changed state.")
				}
			}
		}

		rows, rErr := q.ListManifestShipmentIDs(ctx, m.ID)
		if rErr != nil {
			return apierr.Internal(rErr)
		}
		for _, row := range rows {
			sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
				ID: row.ID, OrganizationID: p.OrganizationID,
			})
			if lErr != nil {
				return apierr.Internal(lErr)
			}
			current := shipment.Status(sh.CurrentStatus)
			// Parcels that were dispatched by the manifest are now in transit.
			// Anything else on the manifest is left alone: a parcel pulled for
			// an exception between closure and departure must not be dragged
			// onto the vehicle in the data when it is not on it in reality.
			target := shipment.StatusInTransit
			if _, fErr := shipment.Find(current, target); fErr != nil {
				if current == shipment.StatusOriginBagged || current == shipment.StatusOriginBranchReceived ||
					current == shipment.StatusTransitHubReceived {
					// The manifest was already DISPATCHED when the trip departed,
					// so dispatch these now.
					if _, dErr := s.dispatchShipment(ctx, tx, actor, sh, m.ManifestCode); dErr != nil {
						return dErr
					}
					sh, lErr = q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
						ID: row.ID, OrganizationID: p.OrganizationID,
					})
					if lErr != nil {
						return apierr.Internal(lErr)
					}
					if _, fErr2 := shipment.Find(shipment.Status(sh.CurrentStatus), target); fErr2 != nil {
						continue
					}
				} else {
					// Reverse-moving parcels stay RTO_IN_TRANSIT; record the leg.
					if _, eErr := s.trans.RecordEvent(ctx, tx, actor, sh, "TRIP",
						"Loaded onto trip "+trip.TripCode, shipment.Request{
							Metadata: map[string]any{"tripCode": trip.TripCode},
						}); eErr != nil {
						return eErr
					}
					continue
				}
			}
			tripID := trip.ID
			if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
				Shipment: sh, To: target,
				Description: "In transit on trip " + trip.TripCode,
				SetTrip:     true, TripID: &tripID,
				// The parcel is on a vehicle, so no facility holds it.
				SetCustodyUnit: true, CustodyUnitID: nil,
				Metadata: map[string]any{
					"tripCode": trip.TripCode, "manifestCode": m.ManifestCode, "mode": trip.Mode,
				},
			}); tErr != nil {
				return tErr
			}
		}
	}
	return nil
}

func (s *Service) dispatchShipment(
	ctx context.Context, tx pgx.Tx, actor shipment.Actor, sh dbgen.Shipment, manifestCode string,
) (*shipment.Result, error) {
	current := shipment.Status(sh.CurrentStatus)
	target := shipment.StatusOriginDispatched
	if current == shipment.StatusTransitHubReceived {
		target = shipment.StatusTransitHubDispatched
	}
	return s.trans.Apply(ctx, tx, actor, shipment.Request{
		Shipment: sh, To: target,
		Description: "Dispatched on manifest " + manifestCode,
		Metadata:    map[string]any{"manifestCode": manifestCode},
	})
}

// ArriveInput records a trip reaching a facility.
type ArriveInput struct {
	TripID     string
	Facility   *ops.Facility
	OdometerKM *int
	OccurredAt *time.Time
	Device     ops.Device
	// LegSequence names the leg that arrived, for a multi-leg trip.
	LegSequence *int
}

// Arrive records a trip's arrival.
//
// Arrival does not receive the parcels. That is deliberate: a vehicle reaching
// the gate is not the same event as the hub counting what came off it, and
// conflating them is how shipments end up marked received at a facility that
// never saw them. Arrival marks the trip and the leg; receipt happens through
// manifest receipt or inbound scans (§17).
func (s *Service) Arrive(ctx context.Context, p *tenant.Principal, in ArriveInput) (*TripDetail, error) {
	var detail *TripDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		trip, tErr := q.LockTripForUpdate(ctx, dbgen.LockTripForUpdateParams{
			PublicID: in.TripID, OrganizationID: p.OrganizationID,
		})
		if tErr != nil {
			return ops.NotFoundOr(tErr, "Trip")
		}
		if in.Facility == nil {
			return apierr.Validation("Recording an arrival requires the operating unit you are working at.",
				map[string]any{"field": "operatingUnitId"})
		}
		if err := p.RequireUnitInScope(in.Facility.ID); err != nil {
			return err
		}

		// Intermediate stop: complete the leg and keep the trip in transit.
		if in.LegSequence != nil || in.Facility.ID != trip.DestinationUnitID {
			return s.arriveAtLeg(ctx, tx, p, trip, in, &detail)
		}
		if !canTransition(trip.Status, StatusArrived) {
			return invalidState(trip.Status, StatusArrived)
		}

		params := dbgen.UpdateTripStatusParams{
			ID: trip.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: trip.Status, ToStatus: StatusArrived,
			OccurredAt: in.OccurredAt,
		}
		if in.OdometerKM != nil {
			v := int32(*in.OdometerKM)
			params.ArrivalOdometerKm = &v
		}
		updated, uErr := q.UpdateTripStatus(ctx, params)
		if uErr != nil {
			return ops.ConflictOr(uErr, "This trip changed state while its arrival was being recorded.")
		}

		// Close the final leg.
		if leg, lErr := q.GetTripLegBySequence(ctx, dbgen.GetTripLegBySequenceParams{
			TripID: trip.ID, Sequence: trip.LegCount,
		}); lErr == nil && leg.Status != "ARRIVED" {
			if _, luErr := q.UpdateTripLegStatus(ctx, dbgen.UpdateTripLegStatusParams{
				ID: leg.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: leg.Status, ToStatus: "ARRIVED", OccurredAt: in.OccurredAt,
			}); luErr != nil && !ops.IsNoRows(luErr) {
				return apierr.Internal(luErr)
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, nil, in.Facility, "ARRIVED",
			trip.Status, StatusArrived, "Trip arrived at "+in.Facility.Code,
			map[string]any{"facility": in.Facility.Code}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripArrived, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			OperatingUnitID: &in.Facility.ID,
			Before:          map[string]any{"status": trip.Status},
			After: map[string]any{
				"status": StatusArrived, "tripCode": trip.TripCode,
				"facility": in.Facility.Code, "odometerKm": in.OdometerKM,
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

func (s *Service) arriveAtLeg(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, trip dbgen.Trip,
	in ArriveInput, out **TripDetail,
) error {
	q := s.q.WithTx(tx)
	sequence := int32(0)
	if in.LegSequence != nil {
		sequence = int32(*in.LegSequence)
	} else {
		legs, err := q.ListTripLegs(ctx, trip.ID)
		if err != nil {
			return apierr.Internal(err)
		}
		for _, l := range legs {
			if l.DestinationUnitID == in.Facility.ID && l.Status != "ARRIVED" {
				sequence = l.Sequence
				break
			}
		}
		if sequence == 0 {
			return apierr.Conflict("TRIP_WRONG_FACILITY",
				"This trip does not stop at your facility.").
				WithDetail("tripCode", trip.TripCode).
				WithDetail("scannedAt", in.Facility.Code)
		}
	}
	leg, err := q.GetTripLegBySequence(ctx, dbgen.GetTripLegBySequenceParams{
		TripID: trip.ID, Sequence: sequence,
	})
	if err != nil {
		return ops.NotFoundOr(err, "Trip leg")
	}
	if leg.DestinationUnitID != in.Facility.ID {
		return apierr.Conflict("TRIP_WRONG_FACILITY",
			"This leg does not end at your facility.").
			WithDetail("legSequence", sequence)
	}
	if _, uErr := q.UpdateTripLegStatus(ctx, dbgen.UpdateTripLegStatusParams{
		ID: leg.ID, OrganizationID: p.OrganizationID,
		ExpectedStatus: leg.Status, ToStatus: "ARRIVED", OccurredAt: in.OccurredAt,
	}); uErr != nil {
		return ops.ConflictOr(uErr, "This leg changed state while its arrival was being recorded.")
	}
	// The trip itself is now in transit between stops.
	if canTransition(trip.Status, StatusInTransit) {
		if _, tErr := q.UpdateTripStatus(ctx, dbgen.UpdateTripStatusParams{
			ID: trip.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: trip.Status, ToStatus: StatusInTransit,
			CurrentLegSequence: ops.Ptr(sequence + 1),
		}); tErr != nil && !ops.IsNoRows(tErr) {
			return apierr.Internal(tErr)
		}
	}
	if _, eErr := s.appendEvent(ctx, q, p, trip, &leg.ID, in.Facility, "LEG_ARRIVED", "", "",
		fmt.Sprintf("Leg %d arrived at %s", sequence, in.Facility.Code),
		map[string]any{"legSequence": sequence, "facility": in.Facility.Code}); eErr != nil {
		return eErr
	}
	detail, dErr := s.loadTripDetail(ctx, q, p, trip.PublicID)
	*out = detail
	return dErr
}

// Close finishes a trip after everything has been unloaded.
func (s *Service) Close(ctx context.Context, p *tenant.Principal, tripID string) (*TripDetail, error) {
	var detail *TripDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		trip, tErr := q.LockTripForUpdate(ctx, dbgen.LockTripForUpdateParams{
			PublicID: tripID, OrganizationID: p.OrganizationID,
		})
		if tErr != nil {
			return ops.NotFoundOr(tErr, "Trip")
		}
		if !canTransition(trip.Status, StatusClosed) {
			return invalidState(trip.Status, StatusClosed)
		}
		if err := p.RequireUnitInScope(trip.DestinationUnitID); err != nil {
			return err
		}
		// A trip cannot be closed while a manifest on it is still unreceived:
		// closing it would leave those parcels with no facility accountable.
		manifests, mErr := q.ListTripManifests(ctx, &trip.ID)
		if mErr != nil {
			return apierr.Internal(mErr)
		}
		for _, m := range manifests {
			if m.Status == "DISPATCHED" {
				return apierr.Conflict("TRIP_MANIFEST_UNRECEIVED",
					"A manifest on this trip has not been received yet.").
					WithDetail("manifestCode", m.ManifestCode)
			}
		}
		updated, uErr := q.UpdateTripStatus(ctx, dbgen.UpdateTripStatusParams{
			ID: trip.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: trip.Status, ToStatus: StatusClosed,
		})
		if uErr != nil {
			return ops.ConflictOr(uErr, "This trip changed state while it was being closed.")
		}
		dest, fErr := s.units.FacilityByID(ctx, p.OrganizationID, trip.DestinationUnitID)
		if fErr != nil {
			return fErr
		}
		if _, eErr := s.appendEvent(ctx, q, p, updated, nil, dest, "CLOSED",
			trip.Status, StatusClosed, "Trip closed", nil); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripClosed, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			Before: map[string]any{"status": trip.Status},
			After:  map[string]any{"status": StatusClosed, "tripCode": trip.TripCode},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadTripDetail(ctx, q, p, trip.PublicID)
		return dErr
	})
	return detail, err
}

// Cancel abandons a planned trip.
func (s *Service) Cancel(ctx context.Context, p *tenant.Principal, tripID, reason string) (*TripDetail, error) {
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
		if !canTransition(trip.Status, StatusCancelled) {
			return invalidState(trip.Status, StatusCancelled)
		}
		updated, uErr := q.UpdateTripStatus(ctx, dbgen.UpdateTripStatusParams{
			ID: trip.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: trip.Status, ToStatus: StatusCancelled,
			CancellationReason: &reason,
		})
		if uErr != nil {
			return ops.ConflictOr(uErr, "This trip changed state while it was being cancelled.")
		}
		// Manifests go back to being unattached so they can ride another trip.
		manifests, mErr := q.ListTripManifests(ctx, &trip.ID)
		if mErr != nil {
			return apierr.Internal(mErr)
		}
		for _, m := range manifests {
			if m.Status == "DISPATCHED" {
				return apierr.Conflict("TRIP_ALREADY_DISPATCHED",
					"A manifest on this trip has already been dispatched.").
					WithDetail("manifestCode", m.ManifestCode)
			}
		}
		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, trip.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		if _, eErr := s.appendEvent(ctx, q, p, updated, nil, origin, "CANCELLED",
			trip.Status, StatusCancelled, "Trip cancelled: "+reason, nil); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionTripCancelled, ResourceType: "trip",
			ResourceID: &trip.ID, ResourcePublicID: trip.PublicID,
			Reason: reason,
			Before: map[string]any{"status": trip.Status},
			After:  map[string]any{"status": StatusCancelled, "tripCode": trip.TripCode},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadTripDetail(ctx, q, p, trip.PublicID)
		return dErr
	})
	return detail, err
}
