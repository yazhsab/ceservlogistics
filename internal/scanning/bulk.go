package scanning

import (
	"context"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/tenant"
)

// BulkItem is one barcode in a batch.
type BulkItem struct {
	Barcode         string     `json:"barcode"`
	OccurredAt      *time.Time `json:"occurredAt,omitempty"`
	DeviceEventID   string     `json:"deviceEventId,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ReasonCode      string     `json:"reasonCode,omitempty"`
	Remarks         string     `json:"remarks,omitempty"`
	SortDestination string     `json:"sortDestination,omitempty"`
}

// BulkResult is the batch outcome.
type BulkResult struct {
	Accepted  int      `json:"accepted"`
	Duplicate int      `json:"duplicate"`
	Rejected  int      `json:"rejected"`
	Results   []Result `json:"results"`
}

// BulkInput is a batch of scans sharing one facility, type and device.
type BulkInput struct {
	ScanType  ScanType
	Facility  *ops.Facility
	Device    ops.Device
	Latitude  *float64
	Longitude *float64
	Override  bool
	Items     []BulkItem
}

// BulkScan applies a batch of scans.
//
// Each barcode is its own transaction. That is the important decision here: a
// hub operator scanning two hundred parcels off a trolley must not lose the
// whole batch because parcel 147 was already received at another facility. The
// response reports every barcode's individual outcome so the handset can show a
// short list of exceptions rather than a single failure.
//
// The batch is processed serially. Parallelism would help throughput but would
// also let one batch monopolise the connection pool, and the pool is the
// scarcest resource on a 4-vCPU box (§28).
func (s *Service) BulkScan(ctx context.Context, p *tenant.Principal, in BulkInput) (*BulkResult, error) {
	if len(in.Items) == 0 {
		return nil, apierr.Validation("At least one barcode is required.",
			map[string]any{"field": "items"})
	}
	if len(in.Items) > s.maxBulk {
		return nil, apierr.Validation("Too many barcodes in one batch.",
			map[string]any{"field": "items", "max": s.maxBulk, "received": len(in.Items)})
	}
	if _, ok := scanPermissions[in.ScanType]; !ok {
		return nil, apierr.Validation("Unknown scan type.",
			map[string]any{"field": "scanType", "allowed": scanTypeStrings()})
	}
	if err := p.Require(scanPermissions[in.ScanType]); err != nil {
		return nil, err
	}

	out := &BulkResult{Results: make([]Result, 0, len(in.Items))}
	// A barcode repeated inside one batch is a double-trigger on the scanner,
	// not two parcels. It is reported as a duplicate without a second write.
	seen := make(map[string]bool, len(in.Items))

	for _, item := range in.Items {
		if seen[item.Barcode] {
			out.Results = append(out.Results, Result{
				Barcode: item.Barcode, Outcome: OutcomeDuplicate,
				RejectionCode:    "DUPLICATE_IN_BATCH",
				RejectionMessage: "This barcode appears more than once in the batch.",
			})
			out.Duplicate++
			continue
		}
		seen[item.Barcode] = true

		device := in.Device
		// Per-item event ids let one upload carry a whole offline shift while
		// each scan stays individually deduplicated.
		if item.DeviceEventID != "" {
			device.EventID = item.DeviceEventID
		} else if device.EventID != "" {
			device.EventID = device.EventID + ":" + item.Barcode
		}

		res, err := s.Scan(ctx, p, Input{
			Barcode: item.Barcode, ScanType: in.ScanType, Facility: in.Facility,
			Device: device, OccurredAt: item.OccurredAt,
			Latitude: in.Latitude, Longitude: in.Longitude,
			Reason: item.Reason, ReasonCode: item.ReasonCode, Remarks: item.Remarks,
			SortDestination: item.SortDestination, Override: in.Override,
		})
		if err != nil {
			// Only infrastructure failures reach here; business refusals come
			// back as a rejected Result. Aborting the batch on a database
			// outage is right — continuing would produce a misleading report.
			return nil, err
		}
		switch res.Outcome {
		case OutcomeAccepted:
			out.Accepted++
		case OutcomeDuplicate:
			out.Duplicate++
		default:
			out.Rejected++
		}
		out.Results = append(out.Results, *res)
	}
	return out, nil
}

// ScanSummary is one row of scan history.
type ScanSummary struct {
	ID         string    `json:"id"`
	Barcode    string    `json:"barcode"`
	ScanType   string    `json:"scanType"`
	Outcome    string    `json:"outcome"`
	Rejection  string    `json:"rejectionCode,omitempty"`
	Message    string    `json:"rejectionMessage,omitempty"`
	ShipmentID string    `json:"shipmentId,omitempty"`
	AWB        string    `json:"awb,omitempty"`
	FromStatus string    `json:"fromStatus,omitempty"`
	ToStatus   string    `json:"toStatus,omitempty"`
	Facility   *ops.Ref  `json:"facility"`
	ScannedBy  *ops.Ref  `json:"scannedBy,omitempty"`
	Source     string    `json:"source"`
	DeviceID   string    `json:"deviceId,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
	RecordedAt time.Time `json:"recordedAt"`

	cursorID int64
}

// HistoryFilter narrows the scan log.
type HistoryFilter struct {
	UnitID     *int64
	ShipmentID *int64
	ScanType   string
	Outcome    string
	Cursor     *pagination.Cursor
	Limit      int
}

// History returns the operational scan log.
func (s *Service) History(
	ctx context.Context, p *tenant.Principal, f HistoryFilter,
) (pagination.CursorPage[ScanSummary], error) {
	params := dbgen.ListScanEventsParams{
		OrganizationID: p.OrganizationID,
		PageSize:       int32(f.Limit + 1),
		UnitIds:        p.UnitScope("shipment.read_all"),
		UnitID:         f.UnitID,
		ShipmentID:     f.ShipmentID,
	}
	if f.ScanType != "" {
		params.ScanType = &f.ScanType
	}
	if f.Outcome != "" {
		params.Outcome = &f.Outcome
	}
	if f.Cursor != nil {
		params.CursorOccurredAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListScanEvents(ctx, params)
	if err != nil {
		return pagination.CursorPage[ScanSummary]{}, apierr.Internal(err)
	}
	out := make([]ScanSummary, 0, len(rows))
	for _, r := range rows {
		item := ScanSummary{
			ID: r.PublicID, Barcode: r.RawBarcode, ScanType: r.ScanType, Outcome: r.Outcome,
			Rejection: ops.Deref(r.RejectionCode), Message: ops.Deref(r.RejectionMessage),
			ShipmentID: ops.Deref(r.ShipmentPublicID), AWB: ops.Deref(r.Awb),
			FromStatus: ops.Deref(r.FromStatus), ToStatus: ops.Deref(r.ToStatus),
			Facility: &ops.Ref{Code: r.UnitCode, Name: r.UnitName},
			Source:   r.Source, DeviceID: ops.Deref(r.DeviceID),
			OccurredAt: r.OccurredAt, RecordedAt: r.RecordedAt,
			cursorID: r.ID,
		}
		if r.ScannedByPublicID != nil {
			item.ScannedBy = &ops.Ref{ID: *r.ScannedByPublicID, Name: ops.Deref(r.ScannedByName)}
		}
		out = append(out, item)
	}
	return pagination.NewCursorPage(out, f.Limit, "occurredAt", "desc",
		func(x ScanSummary) pagination.Cursor {
			at := x.OccurredAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// ShipmentScans returns the scan history for one shipment.
func (s *Service) ShipmentScans(
	ctx context.Context, p *tenant.Principal, shipmentID int64, limit int,
) ([]ScanSummary, error) {
	rows, err := s.q.ListShipmentScans(ctx, dbgen.ListShipmentScansParams{
		ShipmentID: &shipmentID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]ScanSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ScanSummary{
			ID: r.PublicID, Barcode: r.RawBarcode, ScanType: r.ScanType, Outcome: r.Outcome,
			FromStatus: ops.Deref(r.FromStatus), ToStatus: ops.Deref(r.ToStatus),
			Facility:   &ops.Ref{Code: r.UnitCode, Name: r.UnitName},
			ScannedBy:  refName(r.ScannedByName),
			OccurredAt: r.OccurredAt,
		})
	}
	return out, nil
}

func refName(name *string) *ops.Ref {
	if name == nil {
		return nil
	}
	return &ops.Ref{Name: *name}
}
