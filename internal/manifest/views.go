package manifest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/tenant"
)

// BagLine is one bag travelling on a manifest.
type BagLine struct {
	BagID         string     `json:"bagId"`
	BagCode       string     `json:"bagCode"`
	Barcode       string     `json:"barcode"`
	BagStatus     string     `json:"bagStatus"`
	LineStatus    string     `json:"lineStatus"`
	Destination   string     `json:"destinationCode"`
	DeclaredCount int        `json:"declaredShipmentCount"`
	DeclaredPiece int        `json:"declaredPieceCount"`
	DeclaredGrams int64      `json:"declaredWeightGrams"`
	ActualCount   int        `json:"actualShipmentCount"`
	AddedAt       time.Time  `json:"addedAt"`
	ReceivedAt    *time.Time `json:"receivedAt,omitempty"`
}

// LooseLine is one shipment travelling without a bag.
type LooseLine struct {
	ShipmentID  string     `json:"shipmentId"`
	AWB         string     `json:"awb"`
	Status      string     `json:"status"`
	LineStatus  string     `json:"lineStatus"`
	PieceCount  int        `json:"pieceCount"`
	WeightGrams int        `json:"weightGrams"`
	Destination string     `json:"destinationPincode"`
	AddedAt     time.Time  `json:"addedAt"`
	ReceivedAt  *time.Time `json:"receivedAt,omitempty"`
}

// Event is one entry of a manifest's history.
type Event struct {
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

// Detail is the full manifest.
type Detail struct {
	ID             string           `json:"id"`
	ManifestCode   string           `json:"manifestCode"`
	Status         string           `json:"status"`
	Direction      string           `json:"direction"`
	Origin         *ops.Ref         `json:"origin"`
	Destination    *ops.Ref         `json:"destination"`
	Trip           *TripRef         `json:"trip,omitempty"`
	BagCount       int              `json:"bagCount"`
	LooseCount     int              `json:"looseShipmentCount"`
	ShipmentCount  int              `json:"totalShipmentCount"`
	PieceCount     int              `json:"totalPieceCount"`
	WeightGrams    int64            `json:"totalWeightGrams"`
	DeclaredGrams  *int64           `json:"declaredWeightGrams,omitempty"`
	Bags           []BagLine        `json:"bags"`
	LooseShipments []LooseLine      `json:"looseShipments"`
	Events         []Event          `json:"events,omitempty"`
	Declaration    *ContentSnapshot `json:"declaredContents,omitempty"`
	DocumentReady  bool             `json:"documentReady"`
	ClosedAt       *time.Time       `json:"closedAt,omitempty"`
	ClosedBy       string           `json:"closedBy,omitempty"`
	DispatchedAt   *time.Time       `json:"dispatchedAt,omitempty"`
	ReceivedAt     *time.Time       `json:"receivedAt,omitempty"`
	ReceivedBy     string           `json:"receivedBy,omitempty"`
	ReconciledAt   *time.Time       `json:"reconciledAt,omitempty"`
	Remarks        string           `json:"remarks,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`

	AllowedTransitions []string `json:"allowedTransitions"`
}

// TripRef is the trip a manifest rides on.
type TripRef struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Status string `json:"status"`
	Mode   string `json:"mode"`
}

// Summary is one row of the manifest list.
type Summary struct {
	ID            string     `json:"id"`
	ManifestCode  string     `json:"manifestCode"`
	Status        string     `json:"status"`
	Direction     string     `json:"direction"`
	Origin        *ops.Ref   `json:"origin"`
	Destination   *ops.Ref   `json:"destination"`
	TripCode      string     `json:"tripCode,omitempty"`
	BagCount      int        `json:"bagCount"`
	LooseCount    int        `json:"looseShipmentCount"`
	ShipmentCount int        `json:"totalShipmentCount"`
	PieceCount    int        `json:"totalPieceCount"`
	WeightGrams   int64      `json:"totalWeightGrams"`
	DocumentReady bool       `json:"documentReady"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	ReceivedAt    *time.Time `json:"receivedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`

	cursorID int64
}

// InboundSummary is one expected arrival at a facility.
type InboundSummary struct {
	ID            string     `json:"id"`
	ManifestCode  string     `json:"manifestCode"`
	Origin        *ops.Ref   `json:"origin"`
	BagCount      int        `json:"bagCount"`
	ShipmentCount int        `json:"totalShipmentCount"`
	PieceCount    int        `json:"totalPieceCount"`
	WeightGrams   int64      `json:"totalWeightGrams"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	Trip          *TripRef   `json:"trip,omitempty"`
	ExpectedAt    *time.Time `json:"expectedArrival,omitempty"`
	Vehicle       string     `json:"vehicleRegistration,omitempty"`
}

// Get returns one manifest.
func (s *Service) Get(ctx context.Context, p *tenant.Principal, id string) (*Detail, error) {
	return s.loadDetail(ctx, s.q, p, id)
}

func (s *Service) loadDetail(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*Detail, error) {
	row, err := q.GetManifestByPublicID(ctx, dbgen.GetManifestByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Manifest")
	}
	if err := s.authorize(p, row.OriginUnitID, row.DestinationUnitID); err != nil {
		return nil, err
	}

	d := &Detail{
		ID: row.PublicID, ManifestCode: row.ManifestCode, Status: row.Status,
		Direction:   row.Direction,
		Origin:      &ops.Ref{ID: row.OriginPublicID, Code: row.OriginCode, Name: row.OriginName},
		Destination: &ops.Ref{ID: row.DestinationPublicID, Code: row.DestinationCode, Name: row.DestinationName},
		BagCount:    int(row.BagCount), LooseCount: int(row.LooseShipmentCount),
		ShipmentCount: int(row.TotalShipmentCount), PieceCount: int(row.TotalPieceCount),
		WeightGrams: row.TotalWeightGrams, DeclaredGrams: row.DeclaredWeightGrams,
		Bags: []BagLine{}, LooseShipments: []LooseLine{},
		DocumentReady: row.DocumentObjectKey != nil,
		ClosedAt:      row.ClosedAt, ClosedBy: ops.Deref(row.ClosedByName),
		DispatchedAt: row.DispatchedAt, ReceivedAt: row.ReceivedAt,
		ReceivedBy: ops.Deref(row.ReceivedByName), ReconciledAt: row.ReconciledAt,
		Remarks:   ops.Deref(row.Remarks),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		AllowedTransitions: transitions[row.Status],
	}
	if d.AllowedTransitions == nil {
		d.AllowedTransitions = []string{}
	}
	if row.TripPublicID != nil {
		d.Trip = &TripRef{
			ID: *row.TripPublicID, Code: ops.Deref(row.TripCode),
			Status: ops.Deref(row.TripStatus), Mode: ops.Deref(row.TripMode),
		}
	}
	if len(row.ClosedContents) > 0 {
		var snap ContentSnapshot
		if err := json.Unmarshal(row.ClosedContents, &snap); err == nil {
			d.Declaration = &snap
		}
	}

	bags, err := q.ListManifestBags(ctx, dbgen.ListManifestBagsParams{ManifestID: row.ID})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, b := range bags {
		d.Bags = append(d.Bags, BagLine{
			BagID: b.BagPublicID, BagCode: b.BagCode, Barcode: b.Barcode,
			BagStatus: b.BagStatus, LineStatus: b.Status, Destination: b.BagDestinationCode,
			DeclaredCount: int(b.DeclaredShipmentCount), DeclaredPiece: int(b.DeclaredPieceCount),
			DeclaredGrams: b.DeclaredWeightGrams, ActualCount: int(b.ShipmentCount),
			AddedAt: b.AddedAt, ReceivedAt: b.ReceivedAt,
		})
	}
	loose, err := q.ListManifestLooseShipments(ctx, dbgen.ListManifestLooseShipmentsParams{
		ManifestID: row.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, l := range loose {
		d.LooseShipments = append(d.LooseShipments, LooseLine{
			ShipmentID: l.ShipmentPublicID, AWB: l.Awb, Status: l.CurrentStatus,
			LineStatus: l.Status, PieceCount: int(l.PieceCount), WeightGrams: int(l.WeightGrams),
			Destination: l.DestinationPincode, AddedAt: l.AddedAt, ReceivedAt: l.ReceivedAt,
		})
	}

	events, err := q.ListManifestEvents(ctx, dbgen.ListManifestEventsParams{
		ManifestID: row.ID, PageSize: 50,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, ev := range events {
		d.Events = append(d.Events, Event{
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
	return apierr.NotFound("Manifest")
}

// ListFilter narrows the manifest list.
type ListFilter struct {
	Status   string
	OriginID *int64
	DestID   *int64
	TripID   *int64
	Cursor   *pagination.Cursor
	Limit    int
}

// List returns manifests visible to the caller.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal, f ListFilter,
) (pagination.CursorPage[Summary], error) {
	params := dbgen.ListManifestsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds:      p.UnitScope("shipment.read_all"),
		OriginUnitID: f.OriginID, DestinationUnitID: f.DestID, TripID: f.TripID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListManifests(ctx, params)
	if err != nil {
		return pagination.CursorPage[Summary]{}, apierr.Internal(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, Summary{
			ID: r.PublicID, ManifestCode: r.ManifestCode, Status: r.Status,
			Direction:   r.Direction,
			Origin:      &ops.Ref{ID: r.OriginPublicID, Code: r.OriginCode, Name: r.OriginName},
			Destination: &ops.Ref{ID: r.DestinationPublicID, Code: r.DestinationCode, Name: r.DestinationName},
			TripCode:    ops.Deref(r.TripCode),
			BagCount:    int(r.BagCount), LooseCount: int(r.LooseShipmentCount),
			ShipmentCount: int(r.TotalShipmentCount), PieceCount: int(r.TotalPieceCount),
			WeightGrams: r.TotalWeightGrams, DocumentReady: r.DocumentObjectKey != nil,
			ClosedAt: r.ClosedAt, DispatchedAt: r.DispatchedAt, ReceivedAt: r.ReceivedAt,
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x Summary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// ExpectedInbound lists manifests heading to a facility (M14).
func (s *Service) ExpectedInbound(
	ctx context.Context, p *tenant.Principal, unitID int64, limit int,
) ([]InboundSummary, error) {
	rows, err := s.q.ListExpectedInboundManifests(ctx, dbgen.ListExpectedInboundManifestsParams{
		OrganizationID: p.OrganizationID, DestinationUnitID: unitID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]InboundSummary, 0, len(rows))
	for _, r := range rows {
		item := InboundSummary{
			ID: r.PublicID, ManifestCode: r.ManifestCode,
			Origin:   &ops.Ref{Code: r.OriginCode, Name: r.OriginName},
			BagCount: int(r.BagCount), ShipmentCount: int(r.TotalShipmentCount),
			PieceCount: int(r.TotalPieceCount), WeightGrams: r.TotalWeightGrams,
			DispatchedAt: r.DispatchedAt, ExpectedAt: r.ScheduledArrival,
			Vehicle: ops.Deref(r.VehicleRegistration),
		}
		if r.TripPublicID != nil {
			item.Trip = &TripRef{
				ID: *r.TripPublicID, Code: ops.Deref(r.TripCode),
				Status: ops.Deref(r.TripStatus), Mode: ops.Deref(r.TripMode),
			}
		}
		out = append(out, item)
	}
	return out, nil
}
