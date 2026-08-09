package bagging

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

// Item is one shipment in a bag.
type Item struct {
	ShipmentID  string     `json:"shipmentId"`
	AWB         string     `json:"awb"`
	Status      string     `json:"status"`
	ItemStatus  string     `json:"itemStatus"`
	PieceCount  int        `json:"pieceCount"`
	WeightGrams int        `json:"weightGrams"`
	Destination string     `json:"destinationPincode"`
	AddedAt     time.Time  `json:"addedAt"`
	AddedBy     string     `json:"addedBy,omitempty"`
	RemovedAt   *time.Time `json:"removedAt,omitempty"`
	RemovalNote string     `json:"removalReason,omitempty"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
}

// Seal is a tamper-evident tag on a bag.
type Seal struct {
	SealNumber   string     `json:"sealNumber"`
	SealType     string     `json:"sealType"`
	AppliedAt    time.Time  `json:"appliedAt"`
	AppliedBy    string     `json:"appliedBy,omitempty"`
	VerifiedAt   *time.Time `json:"verifiedAt,omitempty"`
	VerifiedBy   string     `json:"verifiedBy,omitempty"`
	Verification string     `json:"verificationResult,omitempty"`
	Remarks      string     `json:"verificationRemarks,omitempty"`
	BrokenAt     *time.Time `json:"brokenAt,omitempty"`
}

// Event is one entry of a bag's history.
type Event struct {
	ID          string    `json:"id"`
	Sequence    int       `json:"sequence"`
	EventType   string    `json:"eventType"`
	FromStatus  string    `json:"fromStatus,omitempty"`
	ToStatus    string    `json:"toStatus,omitempty"`
	Description string    `json:"description"`
	ReasonCode  string    `json:"reasonCode,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	Facility    string    `json:"facility,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

// Detail is the full bag.
type Detail struct {
	ID            string           `json:"id"`
	BagCode       string           `json:"bagCode"`
	Barcode       string           `json:"barcode"`
	Status        string           `json:"status"`
	BagType       string           `json:"bagType"`
	Direction     string           `json:"direction"`
	Origin        *ops.Ref         `json:"origin"`
	Destination   *ops.Ref         `json:"destination"`
	Service       *ops.Ref         `json:"service,omitempty"`
	ShipmentCount int              `json:"shipmentCount"`
	PieceCount    int              `json:"pieceCount"`
	WeightGrams   int64            `json:"totalWeightGrams"`
	GrossGrams    *int             `json:"grossWeightGrams,omitempty"`
	MaxWeight     *int             `json:"maxWeightGrams,omitempty"`
	MaxShipments  *int             `json:"maxShipments,omitempty"`
	Items         []Item           `json:"items"`
	Seals         []Seal           `json:"seals"`
	Events        []Event          `json:"events,omitempty"`
	Declaration   *ContentSnapshot `json:"declaredContents,omitempty"`
	ClosedAt      *time.Time       `json:"closedAt,omitempty"`
	ClosedBy      string           `json:"closedBy,omitempty"`
	DispatchedAt  *time.Time       `json:"dispatchedAt,omitempty"`
	ReceivedAt    *time.Time       `json:"receivedAt,omitempty"`
	ReceivedBy    string           `json:"receivedBy,omitempty"`
	OpenedAt      *time.Time       `json:"openedAtDestination,omitempty"`
	ReconciledAt  *time.Time       `json:"reconciledAt,omitempty"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	// AllowedTransitions tells the console which buttons to render.
	AllowedTransitions []string `json:"allowedTransitions"`
}

// Summary is one row of the bag list.
type Summary struct {
	ID            string     `json:"id"`
	BagCode       string     `json:"bagCode"`
	Barcode       string     `json:"barcode"`
	Status        string     `json:"status"`
	BagType       string     `json:"bagType"`
	Direction     string     `json:"direction"`
	Origin        *ops.Ref   `json:"origin"`
	Destination   *ops.Ref   `json:"destination"`
	ServiceCode   string     `json:"serviceCode,omitempty"`
	ShipmentCount int        `json:"shipmentCount"`
	PieceCount    int        `json:"pieceCount"`
	WeightGrams   int64      `json:"totalWeightGrams"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	ReceivedAt    *time.Time `json:"receivedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`

	cursorID int64
}

// Get returns one bag.
func (s *Service) Get(ctx context.Context, p *tenant.Principal, id string) (*Detail, error) {
	return s.loadDetail(ctx, s.q, p, id)
}

func (s *Service) loadDetail(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*Detail, error) {
	row, err := q.GetBagByPublicID(ctx, dbgen.GetBagByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Bag")
	}
	if err := s.authorize(p, row.OriginUnitID, row.DestinationUnitID); err != nil {
		return nil, err
	}

	d := &Detail{
		ID: row.PublicID, BagCode: row.BagCode, Barcode: row.Barcode, Status: row.Status,
		BagType: row.BagType, Direction: row.Direction,
		Origin:        &ops.Ref{ID: row.OriginPublicID, Code: row.OriginCode, Name: row.OriginName},
		Destination:   &ops.Ref{ID: row.DestinationPublicID, Code: row.DestinationCode, Name: row.DestinationName},
		ShipmentCount: int(row.ShipmentCount), PieceCount: int(row.PieceCount),
		WeightGrams: row.TotalWeightGrams,
		Items:       []Item{}, Seals: []Seal{},
		ClosedAt: row.ClosedAt, ClosedBy: ops.Deref(row.ClosedByName),
		DispatchedAt: row.DispatchedAt, ReceivedAt: row.ReceivedAt,
		ReceivedBy: ops.Deref(row.ReceivedByName),
		OpenedAt:   row.OpenedAtDestinationAt, ReconciledAt: row.ReconciledAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		AllowedTransitions: bagTransitions[row.Status],
	}
	if d.AllowedTransitions == nil {
		d.AllowedTransitions = []string{}
	}
	if row.ServiceCode != nil {
		d.Service = &ops.Ref{Code: *row.ServiceCode, Name: ops.Deref(row.ServiceName)}
	}
	if row.GrossWeightGrams != nil {
		v := int(*row.GrossWeightGrams)
		d.GrossGrams = &v
	}
	if row.MaxWeightGrams != nil {
		v := int(*row.MaxWeightGrams)
		d.MaxWeight = &v
	}
	if row.MaxShipments != nil {
		v := int(*row.MaxShipments)
		d.MaxShipments = &v
	}
	if len(row.ClosedContents) > 0 {
		var snap ContentSnapshot
		if err := json.Unmarshal(row.ClosedContents, &snap); err == nil {
			d.Declaration = &snap
		}
	}

	items, err := q.ListBagItems(ctx, dbgen.ListBagItemsParams{BagID: row.ID})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, it := range items {
		d.Items = append(d.Items, Item{
			ShipmentID: it.ShipmentPublicID, AWB: it.Awb, Status: it.CurrentStatus,
			ItemStatus: it.Status, PieceCount: int(it.PieceCount), WeightGrams: int(it.WeightGrams),
			Destination: it.DestinationPincode, AddedAt: it.AddedAt,
			AddedBy: ops.Deref(it.AddedByName), RemovedAt: it.RemovedAt,
			RemovalNote: ops.Deref(it.RemovalReason), VerifiedAt: it.VerifiedAt,
		})
	}

	seals, err := q.ListBagSeals(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, sl := range seals {
		d.Seals = append(d.Seals, Seal{
			SealNumber: sl.SealNumber, SealType: sl.SealType, AppliedAt: sl.AppliedAt,
			AppliedBy: ops.Deref(sl.AppliedByName), VerifiedAt: sl.VerifiedAt,
			VerifiedBy:   ops.Deref(sl.VerifiedByName),
			Verification: ops.Deref(sl.VerificationResult),
			Remarks:      ops.Deref(sl.VerificationRemarks), BrokenAt: sl.BrokenAt,
		})
	}

	events, err := q.ListBagEvents(ctx, dbgen.ListBagEventsParams{BagID: row.ID, PageSize: 50})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, ev := range events {
		d.Events = append(d.Events, Event{
			ID: ev.PublicID, Sequence: int(ev.Sequence), EventType: ev.EventType,
			FromStatus: ops.Deref(ev.FromStatus), ToStatus: ops.Deref(ev.ToStatus),
			Description: ev.Description, ReasonCode: ops.Deref(ev.ReasonCode),
			Actor: ops.Deref(ev.ActorName), Facility: ops.Deref(ev.UnitCode),
			OccurredAt: ev.OccurredAt,
		})
	}
	return d, nil
}

// authorize restricts a bag to the facilities in the caller's scope.
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
	return apierr.NotFound("Bag")
}

// ListFilter narrows the bag list.
type ListFilter struct {
	Status    string
	OriginID  *int64
	DestID    *int64
	Direction string
	Cursor    *pagination.Cursor
	Limit     int
}

// List returns bags visible to the caller.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal, f ListFilter,
) (pagination.CursorPage[Summary], error) {
	params := dbgen.ListBagsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds:      p.UnitScope("shipment.read_all"),
		OriginUnitID: f.OriginID, DestinationUnitID: f.DestID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Direction != "" {
		params.Direction = &f.Direction
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListBags(ctx, params)
	if err != nil {
		return pagination.CursorPage[Summary]{}, apierr.Internal(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, Summary{
			ID: r.PublicID, BagCode: r.BagCode, Barcode: r.Barcode, Status: r.Status,
			BagType: r.BagType, Direction: r.Direction,
			Origin:        &ops.Ref{ID: r.OriginPublicID, Code: r.OriginCode, Name: r.OriginName},
			Destination:   &ops.Ref{ID: r.DestinationPublicID, Code: r.DestinationCode, Name: r.DestinationName},
			ServiceCode:   ops.Deref(r.ServiceCode),
			ShipmentCount: int(r.ShipmentCount), PieceCount: int(r.PieceCount),
			WeightGrams: r.TotalWeightGrams,
			ClosedAt:    r.ClosedAt, DispatchedAt: r.DispatchedAt, ReceivedAt: r.ReceivedAt,
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x Summary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// GetByBarcode resolves a bag from a scanned tag.
func (s *Service) GetByBarcode(ctx context.Context, p *tenant.Principal, barcode string) (*Detail, error) {
	row, err := s.q.GetBagByBarcode(ctx, dbgen.GetBagByBarcodeParams{
		OrganizationID: p.OrganizationID, Barcode: barcode,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Bag")
	}
	return s.loadDetail(ctx, s.q, p, row.PublicID)
}
