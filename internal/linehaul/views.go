package linehaul

import (
	"context"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Leg is one hop of a trip.
type Leg struct {
	ID          string     `json:"id"`
	Sequence    int        `json:"sequence"`
	Status      string     `json:"status"`
	Origin      *ops.Ref   `json:"origin"`
	Destination *ops.Ref   `json:"destination"`
	Scheduled   Window     `json:"scheduled"`
	Departed    *time.Time `json:"actualDeparture,omitempty"`
	Arrived     *time.Time `json:"actualArrival,omitempty"`
	DistanceKM  *int       `json:"distanceKm,omitempty"`
	Remarks     string     `json:"remarks,omitempty"`
}

// Window is a scheduled departure and arrival pair.
type Window struct {
	Departure time.Time `json:"departure"`
	Arrival   time.Time `json:"arrival"`
}

// CrewMember is one person assigned to a trip.
type CrewMember struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Name   string `json:"name"`
	Phone  string `json:"phone,omitempty"`
	Status string `json:"status"`
}

// ManifestLine is one manifest riding on a trip.
type ManifestLine struct {
	ID            string `json:"id"`
	ManifestCode  string `json:"manifestCode"`
	Status        string `json:"status"`
	Origin        string `json:"originCode"`
	Destination   string `json:"destinationCode"`
	BagCount      int    `json:"bagCount"`
	ShipmentCount int    `json:"shipmentCount"`
	PieceCount    int    `json:"pieceCount"`
	WeightGrams   int64  `json:"weightGrams"`
}

// TripEvent is one entry of a trip's history.
type TripEvent struct {
	ID          string    `json:"id"`
	Sequence    int       `json:"sequence"`
	EventType   string    `json:"eventType"`
	FromStatus  string    `json:"fromStatus,omitempty"`
	ToStatus    string    `json:"toStatus,omitempty"`
	Description string    `json:"description"`
	Actor       string    `json:"actor,omitempty"`
	Facility    string    `json:"facility,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

// TripDetail is the full trip.
type TripDetail struct {
	ID                string         `json:"id"`
	TripCode          string         `json:"tripCode"`
	Mode              string         `json:"mode"`
	Status            string         `json:"status"`
	Direction         string         `json:"direction"`
	Origin            *ops.Ref       `json:"origin"`
	Destination       *ops.Ref       `json:"destination"`
	Carrier           *ops.Ref       `json:"carrier,omitempty"`
	Vehicle           *VehicleRef    `json:"vehicle,omitempty"`
	Driver            *DriverRef     `json:"driver,omitempty"`
	ExternalReference string         `json:"externalReference,omitempty"`
	Scheduled         Window         `json:"scheduled"`
	ActualDeparture   *time.Time     `json:"actualDeparture,omitempty"`
	ActualArrival     *time.Time     `json:"actualArrival,omitempty"`
	CurrentLeg        *int           `json:"currentLegSequence,omitempty"`
	Legs              []Leg          `json:"legs"`
	Crew              []CrewMember   `json:"crew"`
	Manifests         []ManifestLine `json:"manifests"`
	Events            []TripEvent    `json:"events,omitempty"`
	ManifestCount     int            `json:"manifestCount"`
	BagCount          int            `json:"bagCount"`
	ShipmentCount     int            `json:"shipmentCount"`
	WeightGrams       int64          `json:"totalWeightGrams"`
	SealNumber        string         `json:"sealNumber,omitempty"`
	DepartureOdometer *int           `json:"departureOdometerKm,omitempty"`
	ArrivalOdometer   *int           `json:"arrivalOdometerKm,omitempty"`
	ClosedAt          *time.Time     `json:"closedAt,omitempty"`
	CancelledAt       *time.Time     `json:"cancelledAt,omitempty"`
	CancelReason      string         `json:"cancellationReason,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`

	AllowedTransitions []string `json:"allowedTransitions"`
}

// VehicleRef identifies a vehicle in a response.
type VehicleRef struct {
	ID           string `json:"id"`
	Registration string `json:"registrationNumber"`
	Type         string `json:"vehicleType,omitempty"`
}

// DriverRef identifies a driver in a response.
type DriverRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
}

// TripSummary is one row of the trip list.
type TripSummary struct {
	ID              string     `json:"id"`
	TripCode        string     `json:"tripCode"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	Direction       string     `json:"direction"`
	Origin          *ops.Ref   `json:"origin"`
	Destination     *ops.Ref   `json:"destination"`
	Scheduled       Window     `json:"scheduled"`
	ActualDeparture *time.Time `json:"actualDeparture,omitempty"`
	ActualArrival   *time.Time `json:"actualArrival,omitempty"`
	Vehicle         string     `json:"vehicleRegistration,omitempty"`
	Driver          string     `json:"driverName,omitempty"`
	CarrierName     string     `json:"carrierName,omitempty"`
	ManifestCount   int        `json:"manifestCount"`
	BagCount        int        `json:"bagCount"`
	ShipmentCount   int        `json:"shipmentCount"`
	WeightGrams     int64      `json:"totalWeightGrams"`
	LegCount        int        `json:"legCount"`
	CurrentLeg      *int       `json:"currentLegSequence,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`

	cursorID int64
}

// GetTrip returns one trip.
func (s *Service) GetTrip(ctx context.Context, p *tenant.Principal, id string) (*TripDetail, error) {
	return s.loadTripDetail(ctx, s.q, p, id)
}

func (s *Service) loadTripDetail(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*TripDetail, error) {
	row, err := q.GetTripByPublicID(ctx, dbgen.GetTripByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Trip")
	}
	if err := s.authorize(p, row.OriginUnitID, row.DestinationUnitID); err != nil {
		return nil, err
	}

	d := &TripDetail{
		ID: row.PublicID, TripCode: row.TripCode, Mode: row.Mode, Status: row.Status,
		Direction:         row.Direction,
		Origin:            &ops.Ref{ID: row.OriginPublicID, Code: row.OriginCode, Name: row.OriginName},
		Destination:       &ops.Ref{ID: row.DestinationPublicID, Code: row.DestinationCode, Name: row.DestinationName},
		ExternalReference: ops.Deref(row.ExternalReference),
		Scheduled:         Window{Departure: row.ScheduledDeparture, Arrival: row.ScheduledArrival},
		ActualDeparture:   row.ActualDeparture, ActualArrival: row.ActualArrival,
		Legs: []Leg{}, Crew: []CrewMember{}, Manifests: []ManifestLine{},
		ManifestCount: int(row.ManifestCount), BagCount: int(row.BagCount),
		ShipmentCount: int(row.ShipmentCount), WeightGrams: row.TotalWeightGrams,
		SealNumber: ops.Deref(row.SealNumber),
		ClosedAt:   row.ClosedAt, CancelledAt: row.CancelledAt,
		CancelReason: ops.Deref(row.CancellationReason),
		CreatedAt:    row.CreatedAt, UpdatedAt: row.UpdatedAt,
		AllowedTransitions: transitions[row.Status],
	}
	if d.AllowedTransitions == nil {
		d.AllowedTransitions = []string{}
	}
	if row.CurrentLegSequence != nil {
		v := int(*row.CurrentLegSequence)
		d.CurrentLeg = &v
	}
	if row.DepartureOdometerKm != nil {
		v := int(*row.DepartureOdometerKm)
		d.DepartureOdometer = &v
	}
	if row.ArrivalOdometerKm != nil {
		v := int(*row.ArrivalOdometerKm)
		d.ArrivalOdometer = &v
	}
	if row.CarrierCode != nil {
		d.Carrier = &ops.Ref{Code: *row.CarrierCode, Name: ops.Deref(row.CarrierName)}
	}
	if row.VehiclePublicID != nil {
		d.Vehicle = &VehicleRef{ID: *row.VehiclePublicID, Registration: ops.Deref(row.RegistrationNumber)}
	}
	if row.DriverPublicID != nil {
		d.Driver = &DriverRef{
			ID: *row.DriverPublicID, Name: ops.Deref(row.DriverName),
			Phone: ops.Deref(row.DriverPhone),
		}
	}

	legs, err := q.ListTripLegs(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, l := range legs {
		leg := Leg{
			ID: l.PublicID, Sequence: int(l.Sequence), Status: l.Status,
			Origin:      &ops.Ref{ID: l.OriginPublicID, Code: l.OriginCode, Name: l.OriginName},
			Destination: &ops.Ref{ID: l.DestinationPublicID, Code: l.DestinationCode, Name: l.DestinationName},
			Scheduled:   Window{Departure: l.ScheduledDeparture, Arrival: l.ScheduledArrival},
			Departed:    l.ActualDeparture, Arrived: l.ActualArrival,
			Remarks: ops.Deref(l.Remarks),
		}
		if l.DistanceKm != nil {
			v := int(*l.DistanceKm)
			leg.DistanceKM = &v
		}
		d.Legs = append(d.Legs, leg)
	}

	crew, err := q.ListTripAssignments(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, c := range crew {
		member := CrewMember{ID: c.PublicID, Role: c.Role, Status: c.Status}
		if c.DriverName != nil {
			member.Name = *c.DriverName
			member.Phone = ops.Deref(c.DriverPhone)
		} else if c.UserName != nil {
			member.Name = *c.UserName
		}
		d.Crew = append(d.Crew, member)
	}

	manifests, err := q.ListTripManifests(ctx, &row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, m := range manifests {
		d.Manifests = append(d.Manifests, ManifestLine{
			ID: m.PublicID, ManifestCode: m.ManifestCode, Status: m.Status,
			Origin: m.OriginCode, Destination: m.DestinationCode,
			BagCount: int(m.BagCount), ShipmentCount: int(m.TotalShipmentCount),
			PieceCount: int(m.TotalPieceCount), WeightGrams: m.TotalWeightGrams,
		})
	}

	events, err := q.ListTripEvents(ctx, dbgen.ListTripEventsParams{TripID: row.ID, PageSize: 50})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, ev := range events {
		d.Events = append(d.Events, TripEvent{
			ID: ev.PublicID, Sequence: int(ev.Sequence), EventType: ev.EventType,
			FromStatus: ops.Deref(ev.FromStatus), ToStatus: ops.Deref(ev.ToStatus),
			Description: ev.Description, Actor: ops.Deref(ev.ActorName),
			Facility: ops.Deref(ev.UnitCode), OccurredAt: ev.OccurredAt,
		})
	}
	return d, nil
}

func (s *Service) authorize(p *tenant.Principal, originID, destID int64) error {
	scope := p.UnitScope("shipment.read_all")
	if scope == nil {
		return nil
	}
	for _, id := range scope {
		if id == originID || id == destID {
			return nil
		}
	}
	return apierr.NotFound("Trip")
}

// TripFilter narrows the trip list.
type TripFilter struct {
	Status   string
	Mode     string
	OriginID *int64
	DestID   *int64
	Cursor   *pagination.Cursor
	Limit    int
}

// ListTrips returns trips visible to the caller.
func (s *Service) ListTrips(
	ctx context.Context, p *tenant.Principal, f TripFilter,
) (pagination.CursorPage[TripSummary], error) {
	params := dbgen.ListTripsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds:      p.UnitScope("shipment.read_all"),
		OriginUnitID: f.OriginID, DestinationUnitID: f.DestID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Mode != "" {
		params.Mode = &f.Mode
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListTrips(ctx, params)
	if err != nil {
		return pagination.CursorPage[TripSummary]{}, apierr.Internal(err)
	}
	out := make([]TripSummary, 0, len(rows))
	for _, r := range rows {
		item := TripSummary{
			ID: r.PublicID, TripCode: r.TripCode, Mode: r.Mode, Status: r.Status,
			Direction:       r.Direction,
			Origin:          &ops.Ref{ID: r.OriginPublicID, Code: r.OriginCode, Name: r.OriginName},
			Destination:     &ops.Ref{ID: r.DestinationPublicID, Code: r.DestinationCode, Name: r.DestinationName},
			Scheduled:       Window{Departure: r.ScheduledDeparture, Arrival: r.ScheduledArrival},
			ActualDeparture: r.ActualDeparture, ActualArrival: r.ActualArrival,
			Vehicle: ops.Deref(r.RegistrationNumber), Driver: ops.Deref(r.DriverName),
			CarrierName:   ops.Deref(r.CarrierName),
			ManifestCount: int(r.ManifestCount), BagCount: int(r.BagCount),
			ShipmentCount: int(r.ShipmentCount), WeightGrams: r.TotalWeightGrams,
			LegCount:  int(r.LegCount),
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		}
		if r.CurrentLegSequence != nil {
			v := int(*r.CurrentLegSequence)
			item.CurrentLeg = &v
		}
		out = append(out, item)
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x TripSummary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// InboundTrip is a vehicle heading to a facility.
type InboundTrip struct {
	ID            string     `json:"id"`
	TripCode      string     `json:"tripCode"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	Origin        *ops.Ref   `json:"origin"`
	ExpectedAt    time.Time  `json:"expectedArrival"`
	Departed      *time.Time `json:"actualDeparture,omitempty"`
	ManifestCount int        `json:"manifestCount"`
	BagCount      int        `json:"bagCount"`
	ShipmentCount int        `json:"shipmentCount"`
	Vehicle       string     `json:"vehicleRegistration,omitempty"`
	Driver        string     `json:"driverName,omitempty"`
	DriverPhone   string     `json:"driverPhone,omitempty"`
}

// InboundTrips lists vehicles heading to a facility (M14).
func (s *Service) InboundTrips(
	ctx context.Context, p *tenant.Principal, unitID int64, limit int,
) ([]InboundTrip, error) {
	rows, err := s.q.ListInboundTrips(ctx, dbgen.ListInboundTripsParams{
		OrganizationID: p.OrganizationID, DestinationUnitID: unitID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]InboundTrip, 0, len(rows))
	for _, r := range rows {
		out = append(out, InboundTrip{
			ID: r.PublicID, TripCode: r.TripCode, Mode: r.Mode, Status: r.Status,
			Origin:     &ops.Ref{Code: r.OriginCode, Name: r.OriginName},
			ExpectedAt: r.ScheduledArrival, Departed: r.ActualDeparture,
			ManifestCount: int(r.ManifestCount), BagCount: int(r.BagCount),
			ShipmentCount: int(r.ShipmentCount),
			Vehicle:       ops.Deref(r.RegistrationNumber),
			Driver:        ops.Deref(r.DriverName), DriverPhone: ops.Deref(r.DriverPhone),
		})
	}
	return out, nil
}
