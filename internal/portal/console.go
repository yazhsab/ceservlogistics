package portal

import (
	"context"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/tenant"
)

// M29 — the hub and branch console.
//
// This is the surface a handheld scanner talks to, and the only one where the
// *size* of the response is a design constraint rather than an afterthought.
// A branch runs it on a phone over a mobile connection, all day, thousands of
// times. Every field that travels and is not read is battery and bandwidth
// spent on nothing.
//
// So the projections here are narrow on purpose. `Parcel` carries what an
// operator looks at while deciding where to put a box: what it is, where it is
// going, whether money is owed on it, whether it is held. It does not carry the
// customer's email address, the pricing breakdown, the route snapshot or the
// forty other columns a shipment has — and the difference is roughly 200 bytes
// against 3 kB.

// Parcel is the console's view of one shipment.
type Parcel struct {
	ID       string `json:"id"`
	AWB      string `json:"awb"`
	Status   string `json:"status"`
	Pieces   int32  `json:"pieces"`
	Dest     string `json:"destinationPincode"`
	DestCity string `json:"destinationCity,omitempty"`
	DestUnit string `json:"destinationBranch,omitempty"`
	// AmountDueMinor is present only for COD. A prepaid parcel carries no money
	// field at all rather than a zero, so an operator cannot misread one.
	AmountDueMinor *int64 `json:"amountDueMinor,omitempty"`
	Currency       string `json:"currency,omitempty"`
	IsHeld         bool   `json:"isHeld"`
	IsReturning    bool   `json:"isReturning,omitempty"`
	CustodyUnit    string `json:"custodyUnit,omitempty"`
}

// Lookup resolves one parcel by AWB or piece barcode.
//
// The single most-used call on the console: an operator scans, and this is what
// comes back. Tenant-scoped, and a miss is a plain 404 — a scanner that could
// probe another tenant's AWB space would be a cross-tenant read with a physical
// interface.
func (s *Service) Lookup(
	ctx context.Context, p *tenant.Principal, barcode string,
) (*Parcel, error) {
	if barcode == "" {
		return nil, apierr.Validation("A barcode is required.", nil)
	}
	row, err := s.q.ConsoleLookupBarcode(ctx, dbgen.ConsoleLookupBarcodeParams{
		OrganizationID: p.OrganizationID, Barcode: barcode,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Shipment")
	}

	out := &Parcel{
		ID: row.PublicID, AWB: row.Awb, Status: row.CurrentStatus,
		Pieces: row.PieceCount, Dest: row.DestinationPincode,
		IsHeld:      row.IsHeld,
		IsReturning: row.MovementDirection == "REVERSE",
	}
	if row.DestinationCity != nil {
		out.DestCity = *row.DestinationCity
	}
	if row.DestinationBranchCode != nil {
		out.DestUnit = *row.DestinationBranchCode
	}
	if row.CustodyUnitCode != nil {
		out.CustodyUnit = *row.CustodyUnitCode
	}
	if row.PaymentMode == "COD" && row.CodAmountMinor > 0 {
		amount := row.CodAmountMinor
		out.AmountDueMinor = &amount
		out.Currency = row.Currency
	}
	return out, nil
}

// QueueItem is one row of a facility's work list.
//
// Even narrower than Parcel: a queue is a list, so every field is multiplied by
// the page size.
type QueueItem struct {
	ID             string  `json:"id"`
	AWB            string  `json:"awb"`
	Status         string  `json:"status"`
	Pieces         int32   `json:"pieces"`
	Dest           string  `json:"destinationPincode"`
	DestUnit       string  `json:"destinationBranch,omitempty"`
	AmountDueMinor *int64  `json:"amountDueMinor,omitempty"`
	PromisedBy     *string `json:"promisedBy,omitempty"`
	IsHeld         bool    `json:"isHeld,omitempty"`
}

// Queue lists what is sitting at a facility.
func (s *Service) Queue(
	ctx context.Context, p *tenant.Principal, unitID int64,
	status string, cursor *int64, limit int32,
) ([]QueueItem, error) {
	if err := p.RequireUnitInScope(unitID); err != nil {
		return nil, err
	}
	rows, err := s.q.ConsoleWorkQueue(ctx, dbgen.ConsoleWorkQueueParams{
		OrganizationID: p.OrganizationID, UnitID: &unitID,
		Status: ops.Optional(status), CursorID: cursor, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]QueueItem, 0, len(rows))
	for _, r := range rows {
		item := QueueItem{
			ID: r.PublicID, AWB: r.Awb, Status: r.CurrentStatus,
			Pieces: r.PieceCount, Dest: r.DestinationPincode, IsHeld: r.IsHeld,
		}
		if r.DestinationBranchCode != nil {
			item.DestUnit = *r.DestinationBranchCode
		}
		if r.PaymentMode == "COD" && r.CodAmountMinor > 0 {
			amount := r.CodAmountMinor
			item.AmountDueMinor = &amount
		}
		item.PromisedBy = timeOrNil(r.PromisedDeliveryAt)
		out = append(out, item)
	}
	return out, nil
}

// FacilitySummary is the strip above a console's queue.
type FacilitySummary struct {
	Unit            Ref   `json:"unit"`
	InCustody       int64 `json:"inCustody"`
	OutForDelivery  int64 `json:"outForDelivery"`
	Exceptions      int64 `json:"exceptions"`
	Held            int64 `json:"held"`
	Overdue         int64 `json:"overdue"`
	OpenBags        int64 `json:"openBags"`
	InboundExpected int64 `json:"inboundExpected"`
}

// FacilityOverview counts what a facility is holding and what is coming.
func (s *Service) FacilityOverview(
	ctx context.Context, p *tenant.Principal, unit *ops.Facility,
) (*FacilitySummary, error) {
	if err := p.RequireUnitInScope(unit.ID); err != nil {
		return nil, err
	}
	counts, err := s.q.ConsoleFacilitySummary(ctx, dbgen.ConsoleFacilitySummaryParams{
		OrganizationID: p.OrganizationID, UnitID: &unit.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	bags, err := s.q.ConsoleOpenBags(ctx, dbgen.ConsoleOpenBagsParams{
		OrganizationID: p.OrganizationID, UnitID: unit.ID, RowLimit: 200,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	inbound, err := s.q.ConsoleInboundManifests(ctx, dbgen.ConsoleInboundManifestsParams{
		OrganizationID: p.OrganizationID, UnitID: unit.ID, RowLimit: 200,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	return &FacilitySummary{
		Unit:            Ref{ID: unit.PublicID, Code: unit.Code, Name: unit.Name},
		InCustody:       counts.InCustody,
		OutForDelivery:  counts.OutForDelivery,
		Exceptions:      counts.Exceptions,
		Held:            counts.Held,
		Overdue:         counts.Overdue,
		OpenBags:        int64(len(bags)),
		InboundExpected: int64(len(inbound)),
	}, nil
}

// BagSummary is one bag on a console screen.
type BagSummary struct {
	ID          string `json:"id"`
	Code        string `json:"bagCode"`
	Status      string `json:"status"`
	Shipments   int32  `json:"shipments"`
	WeightGrams int64  `json:"weightGrams"`
	Destination string `json:"destination,omitempty"`
}

// Bags lists a facility's open and closed bags.
func (s *Service) Bags(
	ctx context.Context, p *tenant.Principal, unitID int64, limit int32,
) ([]BagSummary, error) {
	if err := p.RequireUnitInScope(unitID); err != nil {
		return nil, err
	}
	rows, err := s.q.ConsoleOpenBags(ctx, dbgen.ConsoleOpenBagsParams{
		OrganizationID: p.OrganizationID, UnitID: unitID, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]BagSummary, 0, len(rows))
	for _, r := range rows {
		b := BagSummary{
			ID: r.PublicID, Code: r.BagCode, Status: r.Status,
			Shipments: r.ShipmentCount, WeightGrams: r.TotalWeightGrams,
		}
		if r.DestinationCode != nil {
			b.Destination = *r.DestinationCode
		}
		out = append(out, b)
	}
	return out, nil
}

// InboundManifest is one consignment heading here.
type InboundManifest struct {
	ID           string  `json:"id"`
	Code         string  `json:"manifestCode"`
	Status       string  `json:"status"`
	Bags         int32   `json:"bags"`
	Shipments    int32   `json:"shipments"`
	WeightGrams  int64   `json:"weightGrams"`
	Origin       string  `json:"origin,omitempty"`
	DispatchedAt *string `json:"dispatchedAt,omitempty"`
}

// Inbound lists what is on its way to a facility, so it can staff for it.
func (s *Service) Inbound(
	ctx context.Context, p *tenant.Principal, unitID int64, limit int32,
) ([]InboundManifest, error) {
	if err := p.RequireUnitInScope(unitID); err != nil {
		return nil, err
	}
	rows, err := s.q.ConsoleInboundManifests(ctx, dbgen.ConsoleInboundManifestsParams{
		OrganizationID: p.OrganizationID, UnitID: unitID, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]InboundManifest, 0, len(rows))
	for _, r := range rows {
		m := InboundManifest{
			ID: r.PublicID, Code: r.ManifestCode, Status: r.Status,
			Bags: r.BagCount, Shipments: r.TotalShipmentCount,
			WeightGrams:  r.TotalWeightGrams,
			DispatchedAt: timeOrNil(r.DispatchedAt),
		}
		if r.OriginCode != nil {
			m.Origin = *r.OriginCode
		}
		out = append(out, m)
	}
	return out, nil
}
