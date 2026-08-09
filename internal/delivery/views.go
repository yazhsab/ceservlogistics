package delivery

import (
	"context"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Stop is one delivery on a run.
type Stop struct {
	ID            string     `json:"id"`
	Sequence      int        `json:"stopSequence"`
	Status        string     `json:"status"`
	DeliveryType  string     `json:"deliveryType"`
	ShipmentID    string     `json:"shipmentId"`
	AWB           string     `json:"awb"`
	ShipmentState string     `json:"shipmentStatus"`
	PieceCount    int        `json:"pieceCount"`
	PaymentMode   string     `json:"paymentMode"`
	CODAmount     int64      `json:"codAmountMinor"`
	CODCollected  int64      `json:"codCollectedMinor"`
	Currency      string     `json:"currency"`
	Recipient     string     `json:"recipientName,omitempty"`
	Phone         string     `json:"recipientPhone,omitempty"`
	Line1         string     `json:"line1,omitempty"`
	Line2         string     `json:"line2,omitempty"`
	Landmark      string     `json:"landmark,omitempty"`
	City          string     `json:"city,omitempty"`
	Pincode       string     `json:"pincode,omitempty"`
	Latitude      *float64   `json:"latitude,omitempty"`
	Longitude     *float64   `json:"longitude,omitempty"`
	PromisedAt    *time.Time `json:"promisedDeliveryAt,omitempty"`
	IsHeld        bool       `json:"isHeld"`
	AttemptCount  int        `json:"attemptCount"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

// RunDetail is the full delivery run.
type RunDetail struct {
	ID            string     `json:"id"`
	RunCode       string     `json:"runCode"`
	Status        string     `json:"status"`
	RunDate       string     `json:"runDate"`
	Branch        *ops.Ref   `json:"branch"`
	Agent         *ops.Ref   `json:"agent"`
	Vehicle       string     `json:"vehicleReference,omitempty"`
	Stops         []Stop     `json:"stops"`
	PlannedStops  int        `json:"plannedStops"`
	CompletedStop int        `json:"completedStops"`
	Delivered     int        `json:"deliveredCount"`
	Failed        int        `json:"failedCount"`
	CODExpected   int64      `json:"codExpectedMinor"`
	CODCollected  int64      `json:"codCollectedMinor"`
	Currency      string     `json:"currency"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`

	AllowedTransitions []string `json:"allowedTransitions"`
}

// RunSummary is one row of the delivery run list.
type RunSummary struct {
	ID           string     `json:"id"`
	RunCode      string     `json:"runCode"`
	Status       string     `json:"status"`
	RunDate      string     `json:"runDate"`
	Branch       *ops.Ref   `json:"branch"`
	Agent        *ops.Ref   `json:"agent"`
	PlannedStops int        `json:"plannedStops"`
	Completed    int        `json:"completedStops"`
	Delivered    int        `json:"deliveredCount"`
	Failed       int        `json:"failedCount"`
	CODExpected  int64      `json:"codExpectedMinor"`
	CODCollected int64      `json:"codCollectedMinor"`
	Currency     string     `json:"currency"`
	DispatchedAt *time.Time `json:"dispatchedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`

	cursorID int64
}

// Attempt is one recorded delivery visit.
type Attempt struct {
	ID            string     `json:"id"`
	AttemptNumber int        `json:"attemptNumber"`
	Outcome       string     `json:"outcome"`
	FailureReason string     `json:"failureReasonCode,omitempty"`
	Remarks       string     `json:"remarks,omitempty"`
	Recipient     string     `json:"recipientName,omitempty"`
	Relationship  string     `json:"recipientRelationship,omitempty"`
	OTPVerified   bool       `json:"otpVerified"`
	CODCollected  int64      `json:"codCollectedMinor"`
	CODMode       string     `json:"codPaymentMode,omitempty"`
	Currency      string     `json:"currency"`
	Agent         *ops.Ref   `json:"agent,omitempty"`
	Facility      string     `json:"facilityCode,omitempty"`
	OccurredAt    time.Time  `json:"occurredAt"`
	RecordedAt    time.Time  `json:"recordedAt"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
}

// QueueItem is one shipment awaiting delivery at a branch.
type QueueItem struct {
	ShipmentID   string     `json:"shipmentId"`
	AWB          string     `json:"awb"`
	Status       string     `json:"status"`
	ChangedAt    time.Time  `json:"statusChangedAt"`
	PieceCount   int        `json:"pieceCount"`
	WeightGrams  int        `json:"chargeableWeightGrams"`
	PaymentMode  string     `json:"paymentMode"`
	CODAmount    int64      `json:"codAmountMinor"`
	Currency     string     `json:"currency"`
	PromisedAt   *time.Time `json:"promisedDeliveryAt,omitempty"`
	AttemptCount int        `json:"deliveryAttemptCount"`
	IsHeld       bool       `json:"isHeld"`
	HoldReason   string     `json:"holdReason,omitempty"`
	Direction    string     `json:"movementDirection"`
	Recipient    string     `json:"recipientName,omitempty"`
	Phone        string     `json:"recipientPhone,omitempty"`
	Line1        string     `json:"line1,omitempty"`
	Line2        string     `json:"line2,omitempty"`
	Landmark     string     `json:"landmark,omitempty"`
	City         string     `json:"city,omitempty"`
	Pincode      string     `json:"pincode,omitempty"`
	Customer     *ops.Ref   `json:"customer,omitempty"`
	NDRCaseID    string     `json:"ndrCaseId,omitempty"`
	NDRReason    string     `json:"ndrReasonCode,omitempty"`
	NDRAction    string     `json:"ndrAction,omitempty"`
	NDRNextAt    *time.Time `json:"ndrNextAttemptAt,omitempty"`
	OnActiveRun  bool       `json:"onActiveRun"`
}

// GetRun returns one delivery run.
func (s *Service) GetRun(ctx context.Context, p *tenant.Principal, id string) (*RunDetail, error) {
	return s.loadRun(ctx, s.q, p, id)
}

func (s *Service) loadRun(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*RunDetail, error) {
	row, err := q.GetDeliveryRunByPublicID(ctx, dbgen.GetDeliveryRunByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Delivery run")
	}
	// An agent always sees their own run; anyone else needs the branch in scope.
	if row.AgentUserID != p.UserID {
		if scope := p.UnitScope("shipment.read_all"); scope != nil && !containsID(scope, row.BranchID) {
			return nil, apierr.NotFound("Delivery run")
		}
	}

	d := &RunDetail{
		ID: row.PublicID, RunCode: row.RunCode, Status: row.Status,
		RunDate:      row.RunDate.Format("2006-01-02"),
		Branch:       &ops.Ref{ID: row.BranchPublicID, Code: row.BranchCode, Name: row.BranchName},
		Agent:        &ops.Ref{ID: row.AgentPublicID, Name: row.AgentName},
		Vehicle:      ops.Deref(row.VehicleReference),
		Stops:        []Stop{},
		PlannedStops: int(row.PlannedStops), CompletedStop: int(row.CompletedStops),
		Delivered: int(row.DeliveredCount), Failed: int(row.FailedCount),
		CODExpected: row.CodExpectedMinor, CODCollected: row.CodCollectedMinor,
		Currency:     row.Currency,
		DispatchedAt: row.DispatchedAt, StartedAt: row.StartedAt,
		CompletedAt: row.CompletedAt, ClosedAt: row.ClosedAt,
		CreatedAt:          row.CreatedAt,
		AllowedTransitions: runTransitions[row.Status],
	}
	if d.AllowedTransitions == nil {
		d.AllowedTransitions = []string{}
	}

	active := true
	items, err := q.ListDeliveryRunItems(ctx, dbgen.ListDeliveryRunItemsParams{
		DeliveryRunID: row.ID, OnlyActive: &active,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, it := range items {
		d.Stops = append(d.Stops, Stop{
			ID: it.PublicID, Sequence: int(it.StopSequence), Status: it.Status,
			DeliveryType: it.DeliveryType, ShipmentID: it.ShipmentPublicID, AWB: it.Awb,
			ShipmentState: it.CurrentStatus, PieceCount: int(it.PieceCount),
			PaymentMode: it.PaymentMode, CODAmount: it.CodAmountMinor,
			CODCollected: it.CodCollectedMinor, Currency: it.Currency,
			Recipient: ops.Deref(it.RecipientName), Phone: ops.Deref(it.RecipientPhone),
			Line1: ops.Deref(it.Line1), Line2: ops.Deref(it.Line2),
			Landmark: ops.Deref(it.Landmark), City: ops.Deref(it.CityName),
			Pincode: ops.Deref(it.Pincode), Latitude: it.Latitude, Longitude: it.Longitude,
			PromisedAt: it.PromisedDeliveryAt, IsHeld: it.IsHeld,
			AttemptCount: int(it.AttemptCount),
			DispatchedAt: it.DispatchedAt, CompletedAt: it.CompletedAt,
		})
	}
	return d, nil
}

// RunFilter narrows the run list.
type RunFilter struct {
	Status  string
	RunDate *time.Time
	AgentID *int64
	Cursor  *pagination.Cursor
	Limit   int
}

// ListRuns returns delivery runs visible to the caller.
func (s *Service) ListRuns(
	ctx context.Context, p *tenant.Principal, f RunFilter,
) (pagination.CursorPage[RunSummary], error) {
	params := dbgen.ListDeliveryRunsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds: p.UnitScope("shipment.read_all"),
		RunDate: f.RunDate, AgentUserID: f.AgentID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListDeliveryRuns(ctx, params)
	if err != nil {
		return pagination.CursorPage[RunSummary]{}, apierr.Internal(err)
	}
	out := make([]RunSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, RunSummary{
			ID: r.PublicID, RunCode: r.RunCode, Status: r.Status,
			RunDate:      r.RunDate.Format("2006-01-02"),
			Branch:       &ops.Ref{ID: r.BranchPublicID, Code: r.BranchCode, Name: r.BranchName},
			Agent:        &ops.Ref{ID: r.AgentPublicID, Name: r.AgentName},
			PlannedStops: int(r.PlannedStops), Completed: int(r.CompletedStops),
			Delivered: int(r.DeliveredCount), Failed: int(r.FailedCount),
			CODExpected: r.CodExpectedMinor, CODCollected: r.CodCollectedMinor,
			Currency:     r.Currency,
			DispatchedAt: r.DispatchedAt, CompletedAt: r.CompletedAt,
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x RunSummary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// QueueFilter narrows the delivery-ready queue.
type QueueFilter struct {
	Status          string
	IncludeHeld     bool
	ExcludeAssigned bool
	Limit           int
}

// Queue returns the delivery-ready shipments at a branch (M15).
func (s *Service) Queue(
	ctx context.Context, p *tenant.Principal, branch *ops.Facility, f QueueFilter,
) ([]QueueItem, error) {
	params := dbgen.ListDeliveryQueueParams{
		OrganizationID: p.OrganizationID, BranchID: branch.ID,
		IncludeHeld: &f.IncludeHeld, ExcludeAssigned: &f.ExcludeAssigned,
		PageSize: int32(f.Limit),
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	rows, err := s.q.ListDeliveryQueue(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]QueueItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, QueueItem{
			ShipmentID: r.PublicID, AWB: r.Awb, Status: r.CurrentStatus,
			ChangedAt: r.StatusChangedAt, PieceCount: int(r.PieceCount),
			WeightGrams: int(r.ChargeableWeightGrams), PaymentMode: r.PaymentMode,
			CODAmount: r.CodAmountMinor, Currency: r.Currency,
			PromisedAt: r.PromisedDeliveryAt, AttemptCount: int(r.DeliveryAttemptCount),
			IsHeld: r.IsHeld, HoldReason: ops.Deref(r.HoldReason),
			Direction: r.MovementDirection,
			Recipient: ops.Deref(r.RecipientName), Phone: ops.Deref(r.RecipientPhone),
			Line1: ops.Deref(r.Line1), Line2: ops.Deref(r.Line2),
			Landmark: ops.Deref(r.Landmark), City: ops.Deref(r.CityName),
			Pincode:   ops.Deref(r.Pincode),
			Customer:  &ops.Ref{Code: r.CustomerCode, Name: r.CustomerName},
			NDRCaseID: ops.Deref(r.NdrCasePublicID), NDRReason: ops.Deref(r.NdrReason),
			NDRAction: ops.Deref(r.NdrAction), NDRNextAt: r.NdrNextAttemptAt,
			OnActiveRun: r.OnActiveRun,
		})
	}
	return out, nil
}

// Attempts returns the delivery history for a shipment.
func (s *Service) Attempts(ctx context.Context, p *tenant.Principal, shipmentID int64) ([]Attempt, error) {
	rows, err := s.q.ListDeliveryAttempts(ctx, shipmentID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]Attempt, 0, len(rows))
	for _, r := range rows {
		a := Attempt{
			ID: r.PublicID, AttemptNumber: int(r.AttemptNumber), Outcome: r.Outcome,
			FailureReason: ops.Deref(r.FailureReasonCode), Remarks: ops.Deref(r.Remarks),
			Recipient: ops.Deref(r.RecipientName), Relationship: ops.Deref(r.RecipientRelationship),
			OTPVerified: r.OtpVerified, CODCollected: r.CodCollectedMinor,
			CODMode: ops.Deref(r.CodPaymentMode), Currency: r.Currency,
			Facility:   ops.Deref(r.UnitCode),
			OccurredAt: r.OccurredAt, RecordedAt: r.RecordedAt,
			NextAttemptAt: r.NextAttemptAt,
		}
		if r.AgentPublicID != nil {
			a.Agent = &ops.Ref{ID: *r.AgentPublicID, Name: ops.Deref(r.AgentName)}
		}
		out = append(out, a)
	}
	return out, nil
}

func containsID(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
