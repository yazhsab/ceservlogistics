package hubops

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

// ReconciliationItem is one line of a count.
type ReconciliationItem struct {
	Barcode     string     `json:"barcode"`
	ItemType    string     `json:"itemType"`
	Outcome     string     `json:"outcome"`
	WasDeclared bool       `json:"wasDeclared"`
	ShipmentID  string     `json:"shipmentId,omitempty"`
	AWB         string     `json:"awb,omitempty"`
	BagCode     string     `json:"bagCode,omitempty"`
	Status      string     `json:"shipmentStatus,omitempty"`
	ScannedAt   *time.Time `json:"scannedAt,omitempty"`
	ExceptionID string     `json:"exceptionId,omitempty"`
	Remarks     string     `json:"remarks,omitempty"`
}

// ReconciliationDetail is the full count.
type ReconciliationDetail struct {
	ID           string               `json:"id"`
	Code         string               `json:"reconciliationCode"`
	SubjectType  string               `json:"subjectType"`
	Status       string               `json:"status"`
	Facility     *ops.Ref             `json:"facility"`
	BagID        string               `json:"bagId,omitempty"`
	BagCode      string               `json:"bagCode,omitempty"`
	ManifestID   string               `json:"manifestId,omitempty"`
	ManifestCode string               `json:"manifestCode,omitempty"`
	Expected     int                  `json:"expectedCount"`
	Scanned      int                  `json:"scannedCount"`
	Matched      int                  `json:"matchedCount"`
	Missing      int                  `json:"missingCount"`
	Excess       int                  `json:"excessCount"`
	Damaged      int                  `json:"damagedCount"`
	Items        []ReconciliationItem `json:"items"`
	StartedAt    time.Time            `json:"startedAt"`
	StartedBy    string               `json:"startedBy,omitempty"`
	CompletedAt  *time.Time           `json:"completedAt,omitempty"`
	CompletedBy  string               `json:"completedBy,omitempty"`
	Remarks      string               `json:"remarks,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
}

// ReconciliationSummary is one row of the reconciliation list.
type ReconciliationSummary struct {
	ID          string     `json:"id"`
	Code        string     `json:"reconciliationCode"`
	SubjectType string     `json:"subjectType"`
	Status      string     `json:"status"`
	Facility    string     `json:"facilityCode"`
	BagCode     string     `json:"bagCode,omitempty"`
	Manifest    string     `json:"manifestCode,omitempty"`
	Expected    int        `json:"expectedCount"`
	Scanned     int        `json:"scannedCount"`
	Matched     int        `json:"matchedCount"`
	Missing     int        `json:"missingCount"`
	Excess      int        `json:"excessCount"`
	Damaged     int        `json:"damagedCount"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	cursorID int64
}

// GetReconciliation returns one count.
func (s *Service) GetReconciliation(
	ctx context.Context, p *tenant.Principal, id string,
) (*ReconciliationDetail, error) {
	return s.loadReconciliation(ctx, s.q, p, id)
}

func (s *Service) loadReconciliation(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*ReconciliationDetail, error) {
	row, err := q.GetReconciliationByPublicID(ctx, dbgen.GetReconciliationByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Reconciliation")
	}
	if scope := p.UnitScope("shipment.read_all"); scope != nil && !containsID(scope, row.OperatingUnitID) {
		return nil, apierr.NotFound("Reconciliation")
	}

	d := &ReconciliationDetail{
		ID: row.PublicID, Code: row.ReconciliationCode, SubjectType: row.SubjectType,
		Status:   row.Status,
		Facility: &ops.Ref{Code: row.UnitCode, Name: row.UnitName},
		BagID:    ops.Deref(row.BagPublicID), BagCode: ops.Deref(row.BagCode),
		ManifestID: ops.Deref(row.ManifestPublicID), ManifestCode: ops.Deref(row.ManifestCode),
		Expected: int(row.ExpectedCount), Scanned: int(row.ScannedCount),
		Matched: int(row.MatchedCount), Missing: int(row.MissingCount),
		Excess: int(row.ExcessCount), Damaged: int(row.DamagedCount),
		Items:     []ReconciliationItem{},
		StartedAt: row.StartedAt, StartedBy: ops.Deref(row.StartedByName),
		CompletedAt: row.CompletedAt, CompletedBy: ops.Deref(row.CompletedByName),
		Remarks: ops.Deref(row.Remarks), CreatedAt: row.CreatedAt,
	}

	items, err := q.ListReconciliationItems(ctx, dbgen.ListReconciliationItemsParams{
		ReconciliationID: row.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, it := range items {
		d.Items = append(d.Items, ReconciliationItem{
			Barcode: it.RawBarcode, ItemType: it.ItemType, Outcome: it.Outcome,
			WasDeclared: it.WasDeclared,
			ShipmentID:  ops.Deref(it.ShipmentPublicID), AWB: ops.Deref(it.Awb),
			BagCode: ops.Deref(it.BagCode), Status: ops.Deref(it.CurrentStatus),
			ScannedAt:   it.ScannedAt,
			ExceptionID: ops.Deref(it.ExceptionPublicID),
			Remarks:     ops.Deref(it.Remarks),
		})
	}
	return d, nil
}

// ReconciliationFilter narrows the list.
type ReconciliationFilter struct {
	Status string
	Cursor *pagination.Cursor
	Limit  int
}

// ListReconciliations returns counts visible to the caller.
func (s *Service) ListReconciliations(
	ctx context.Context, p *tenant.Principal, f ReconciliationFilter,
) (pagination.CursorPage[ReconciliationSummary], error) {
	params := dbgen.ListReconciliationsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds: p.UnitScope("shipment.read_all"),
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListReconciliations(ctx, params)
	if err != nil {
		return pagination.CursorPage[ReconciliationSummary]{}, apierr.Internal(err)
	}
	out := make([]ReconciliationSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReconciliationSummary{
			ID: r.PublicID, Code: r.ReconciliationCode, SubjectType: r.SubjectType,
			Status: r.Status, Facility: r.UnitCode,
			BagCode: ops.Deref(r.BagCode), Manifest: ops.Deref(r.ManifestCode),
			Expected: int(r.ExpectedCount), Scanned: int(r.ScannedCount),
			Matched: int(r.MatchedCount), Missing: int(r.MissingCount),
			Excess: int(r.ExcessCount), Damaged: int(r.DamagedCount),
			StartedAt: r.StartedAt, CompletedAt: r.CompletedAt,
			cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x ReconciliationSummary) pagination.Cursor {
			at := x.StartedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// WorkloadSummary is the facility dashboard.
type WorkloadSummary struct {
	Facility         *ops.Ref  `json:"facility"`
	InCustody        int64     `json:"shipmentsInCustody"`
	OpenBags         int64     `json:"openBags"`
	ClosedBags       int64     `json:"closedBags"`
	InboundBags      int64     `json:"inboundBags"`
	DraftManifests   int64     `json:"draftManifests"`
	InboundManifests int64     `json:"inboundManifests"`
	InboundTrips     int64     `json:"inboundTrips"`
	OutboundTrips    int64     `json:"outboundTrips"`
	ReadyForDelivery int64     `json:"readyForDelivery"`
	OutForDelivery   int64     `json:"outForDelivery"`
	NDRPending       int64     `json:"ndrPending"`
	OpenExceptions   int64     `json:"openExceptions"`
	OnHold           int64     `json:"onHold"`
	PendingPickups   int64     `json:"pendingPickups"`
	GeneratedAt      time.Time `json:"generatedAt"`
}

// Workload returns the one-round-trip facility dashboard.
func (s *Service) Workload(
	ctx context.Context, p *tenant.Principal, facility *ops.Facility,
) (*WorkloadSummary, error) {
	row, err := s.q.GetHubWorkloadSummary(ctx, dbgen.GetHubWorkloadSummaryParams{
		OrganizationID: p.OrganizationID, UnitID: facility.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &WorkloadSummary{
		Facility:  ops.RefOf(facility),
		InCustody: row.InCustody, OpenBags: row.OpenBags, ClosedBags: row.ClosedBags,
		InboundBags: row.InboundBags, DraftManifests: row.DraftManifests,
		InboundManifests: row.InboundManifests,
		InboundTrips:     row.InboundTrips, OutboundTrips: row.OutboundTrips,
		ReadyForDelivery: row.ReadyForDelivery, OutForDelivery: row.OutForDelivery,
		NDRPending: row.NdrPending, OpenExceptions: row.OpenExceptions,
		OnHold: row.OnHold, PendingPickups: row.PendingPickups,
		GeneratedAt: time.Now(),
	}, nil
}

// ThroughputBucket is scan volume in one hour.
type ThroughputBucket struct {
	Hour     time.Time `json:"hour"`
	ScanType string    `json:"scanType"`
	Count    int64     `json:"count"`
}

// Throughput returns hourly scan volume at a facility.
func (s *Service) Throughput(
	ctx context.Context, p *tenant.Principal, facility *ops.Facility, since time.Time,
) ([]ThroughputBucket, error) {
	rows, err := s.q.GetUnitScanThroughput(ctx, dbgen.GetUnitScanThroughputParams{
		OrganizationID: p.OrganizationID, UnitID: facility.ID, Since: since,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]ThroughputBucket, 0, len(rows))
	for _, r := range rows {
		out = append(out, ThroughputBucket{Hour: r.Bucket, ScanType: r.ScanType, Count: r.Total})
	}
	return out, nil
}

// CustodyItem is one parcel physically at a facility.
type CustodyItem struct {
	ShipmentID  string     `json:"shipmentId"`
	AWB         string     `json:"awb"`
	Status      string     `json:"status"`
	ChangedAt   time.Time  `json:"statusChangedAt"`
	PieceCount  int        `json:"pieceCount"`
	WeightGrams int        `json:"chargeableWeightGrams"`
	Destination string     `json:"destinationPincode"`
	PromisedAt  *time.Time `json:"promisedDeliveryAt,omitempty"`
	IsHeld      bool       `json:"isHeld"`
	Direction   string     `json:"movementDirection"`
	BagCode     string     `json:"bagCode,omitempty"`
	DestBranch  string     `json:"destinationBranchCode,omitempty"`

	cursorAt time.Time
	cursorID int64
}

// CustodyStocktake lists what is physically at a facility right now.
func (s *Service) CustodyStocktake(
	ctx context.Context, p *tenant.Principal, facility *ops.Facility,
	status string, cursor *pagination.Cursor, limit int,
) (pagination.CursorPage[CustodyItem], error) {
	params := dbgen.ListUnitCustodyShipmentsParams{
		OrganizationID: p.OrganizationID, UnitID: facility.ID, PageSize: int32(limit + 1),
	}
	if status != "" {
		params.Status = &status
	}
	if cursor != nil {
		params.CursorChangedAt = cursor.Time
		params.CursorID = &cursor.ID
	}
	rows, err := s.q.ListUnitCustodyShipments(ctx, params)
	if err != nil {
		return pagination.CursorPage[CustodyItem]{}, apierr.Internal(err)
	}
	out := make([]CustodyItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, CustodyItem{
			ShipmentID: r.PublicID, AWB: r.Awb, Status: r.CurrentStatus,
			ChangedAt: r.StatusChangedAt, PieceCount: int(r.PieceCount),
			WeightGrams: int(r.ChargeableWeightGrams), Destination: r.DestinationPincode,
			PromisedAt: r.PromisedDeliveryAt, IsHeld: r.IsHeld,
			Direction: r.MovementDirection, BagCode: ops.Deref(r.BagCode),
			DestBranch: ops.Deref(r.DestinationBranchCode),
			cursorAt:   r.StatusChangedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, limit, "statusChangedAt", "desc",
		func(x CustodyItem) pagination.Cursor {
			at := x.cursorAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
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
