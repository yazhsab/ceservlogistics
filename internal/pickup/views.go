package pickup

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

// Address is the snapshotted pickup location.
type Address struct {
	ContactName string   `json:"contactName"`
	Phone       string   `json:"phone"`
	AltPhone    string   `json:"altPhone,omitempty"`
	Line1       string   `json:"line1"`
	Line2       string   `json:"line2,omitempty"`
	Landmark    string   `json:"landmark,omitempty"`
	City        string   `json:"city"`
	State       string   `json:"state"`
	Pincode     string   `json:"pincode"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
}

// Window is the agreed collection window.
type Window struct {
	Date  string    `json:"date"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ShipmentLine is one shipment attached to a request.
type ShipmentLine struct {
	ShipmentID  string    `json:"shipmentId"`
	AWB         string    `json:"awb"`
	Status      string    `json:"status"`
	PieceCount  int       `json:"pieceCount"`
	WeightGrams int       `json:"weightGrams"`
	PaymentMode string    `json:"paymentMode"`
	AddedAt     time.Time `json:"addedAt"`
	Remarks     string    `json:"remarks,omitempty"`
}

// Assignment is the agent responsible for a request.
type Assignment struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Agent        *ops.Ref   `json:"agent"`
	StopSequence *int       `json:"stopSequence,omitempty"`
	AssignedAt   time.Time  `json:"assignedAt"`
	AcceptedAt   *time.Time `json:"acceptedAt,omitempty"`
	ArrivedAt    *time.Time `json:"arrivedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	Rejection    string     `json:"rejectionReason,omitempty"`
	RunID        string     `json:"runId,omitempty"`
}

// Attempt is one recorded visit.
type Attempt struct {
	ID              string     `json:"id"`
	AttemptNumber   int        `json:"attemptNumber"`
	Outcome         string     `json:"outcome"`
	FailureReason   string     `json:"failureReasonCode,omitempty"`
	Remarks         string     `json:"remarks,omitempty"`
	PiecesCollected int        `json:"piecesCollected"`
	WeightGrams     *int       `json:"weightGrams,omitempty"`
	AgentName       string     `json:"agentName,omitempty"`
	OccurredAt      time.Time  `json:"occurredAt"`
	RecordedAt      time.Time  `json:"recordedAt"`
	NextAttemptAt   *time.Time `json:"nextAttemptAt,omitempty"`
}

// Detail is the full pickup request.
type Detail struct {
	ID            string         `json:"id"`
	ReferenceCode string         `json:"referenceCode"`
	Status        string         `json:"status"`
	PickupType    string         `json:"pickupType"`
	Customer      *ops.Ref       `json:"customer"`
	Branch        *ops.Ref       `json:"branch"`
	Address       Address        `json:"address"`
	Window        Window         `json:"window"`
	ExpectedPiece int            `json:"expectedPieceCount"`
	ExpectedGrams *int           `json:"expectedWeightGrams,omitempty"`
	ActualPiece   int            `json:"actualPieceCount"`
	AttemptCount  int            `json:"attemptCount"`
	MaxAttempts   int            `json:"maxAttempts"`
	Instructions  string         `json:"specialInstructions,omitempty"`
	Shipments     []ShipmentLine `json:"shipments"`
	Assignment    *Assignment    `json:"assignment,omitempty"`
	Attempts      []Attempt      `json:"attempts"`
	CompletedAt   *time.Time     `json:"completedAt,omitempty"`
	CancelledAt   *time.Time     `json:"cancelledAt,omitempty"`
	CancelReason  string         `json:"cancellationReason,omitempty"`
	FailureReason string         `json:"failureReason,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	// ReplayedAttemptID is set when an offline retry returned the original
	// attempt instead of recording a second one.
	ReplayedAttemptID string `json:"replayedAttemptId,omitempty"`
}

// Summary is one row of the dispatcher queue.
type Summary struct {
	ID            string    `json:"id"`
	ReferenceCode string    `json:"referenceCode"`
	Status        string    `json:"status"`
	PickupType    string    `json:"pickupType"`
	Customer      *ops.Ref  `json:"customer"`
	Branch        *ops.Ref  `json:"branch"`
	ContactName   string    `json:"contactName"`
	Phone         string    `json:"phone"`
	Line1         string    `json:"line1"`
	City          string    `json:"city"`
	Pincode       string    `json:"pincode"`
	Window        Window    `json:"window"`
	ExpectedPiece int       `json:"expectedPieceCount"`
	ActualPiece   int       `json:"actualPieceCount"`
	AttemptCount  int       `json:"attemptCount"`
	MaxAttempts   int       `json:"maxAttempts"`
	AgentName     string    `json:"agentName,omitempty"`
	AgentID       string    `json:"agentId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`

	// cursorID carries the internal row id for keyset pagination and is never
	// serialised: §11 keeps sequential primary keys off the wire.
	cursorID int64
}

// AgentStop is one entry in a field agent's work list.
type AgentStop struct {
	AssignmentID  string     `json:"assignmentId"`
	RequestID     string     `json:"requestId"`
	ReferenceCode string     `json:"referenceCode"`
	Status        string     `json:"status"`
	RequestStatus string     `json:"requestStatus"`
	PickupType    string     `json:"pickupType"`
	StopSequence  *int       `json:"stopSequence,omitempty"`
	CustomerName  string     `json:"customerName"`
	Address       Address    `json:"address"`
	Window        Window     `json:"window"`
	ExpectedPiece int        `json:"expectedPieceCount"`
	Instructions  string     `json:"specialInstructions,omitempty"`
	AcceptedAt    *time.Time `json:"acceptedAt,omitempty"`
	ArrivedAt     *time.Time `json:"arrivedAt,omitempty"`
}

type detailOption func(*Detail)

func withReplay(attemptID string) detailOption {
	return func(d *Detail) { d.ReplayedAttemptID = attemptID }
}

// Get returns one pickup request.
func (s *Service) Get(ctx context.Context, p *tenant.Principal, id string, opts ...detailOption) (*Detail, error) {
	d, err := s.loadDetail(ctx, s.q, p, id)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(d)
	}
	return d, nil
}

func (s *Service) loadDetail(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*Detail, error) {
	row, err := q.GetPickupRequestByPublicID(ctx, dbgen.GetPickupRequestByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Pickup request")
	}
	if err := s.authorize(p, row.CustomerID, row.BranchID); err != nil {
		return nil, err
	}

	d := &Detail{
		ID: row.PublicID, ReferenceCode: row.ReferenceCode, Status: row.Status,
		PickupType: row.PickupType,
		Customer:   &ops.Ref{ID: row.CustomerPublicID, Code: row.CustomerCode, Name: row.CustomerName},
		Branch:     &ops.Ref{ID: row.BranchPublicID, Code: row.BranchCode, Name: row.BranchName},
		Address: Address{
			ContactName: row.ContactName, Phone: row.ContactPhone,
			AltPhone: ops.Deref(row.AltPhone), Line1: row.Line1, Line2: ops.Deref(row.Line2),
			Landmark: ops.Deref(row.Landmark), City: row.CityName, State: row.StateName,
			Pincode: row.Pincode, Latitude: row.Latitude, Longitude: row.Longitude,
		},
		Window: Window{
			Date:  row.ScheduledDate.Format("2006-01-02"),
			Start: row.WindowStart, End: row.WindowEnd,
		},
		ExpectedPiece: int(row.ExpectedPieceCount), ActualPiece: int(row.ActualPieceCount),
		AttemptCount: int(row.AttemptCount), MaxAttempts: int(row.MaxAttempts),
		Instructions: ops.Deref(row.SpecialInstructions),
		Shipments:    []ShipmentLine{}, Attempts: []Attempt{},
		CompletedAt: row.CompletedAt, CancelledAt: row.CancelledAt,
		CancelReason:  ops.Deref(row.CancellationReason),
		FailureReason: ops.Deref(row.FailureReason),
		CreatedAt:     row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ExpectedWeightGrams != nil {
		g := int(*row.ExpectedWeightGrams)
		d.ExpectedGrams = &g
	}

	items, err := q.ListPickupRequestShipments(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, it := range items {
		d.Shipments = append(d.Shipments, ShipmentLine{
			ShipmentID: it.ShipmentPublicID, AWB: it.Awb, Status: it.Status,
			PieceCount: int(it.PieceCount), WeightGrams: int(it.ActualWeightGrams),
			PaymentMode: it.PaymentMode, AddedAt: it.AddedAt, Remarks: ops.Deref(it.Remarks),
		})
	}

	if a, aErr := q.GetActivePickupAssignment(ctx, row.ID); aErr == nil {
		d.Assignment = assignmentView(a, q, ctx, p)
	} else if !ops.IsNoRows(aErr) {
		return nil, apierr.Internal(aErr)
	}

	attempts, err := q.ListPickupAttempts(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range attempts {
		at := Attempt{
			ID: a.PublicID, AttemptNumber: int(a.AttemptNumber), Outcome: a.Outcome,
			FailureReason: ops.Deref(a.FailureReasonCode), Remarks: ops.Deref(a.Remarks),
			PiecesCollected: int(a.PiecesCollected), AgentName: ops.Deref(a.AgentName),
			OccurredAt: a.OccurredAt, RecordedAt: a.RecordedAt, NextAttemptAt: a.NextAttemptAt,
		}
		if a.WeightGrams != nil {
			g := int(*a.WeightGrams)
			at.WeightGrams = &g
		}
		d.Attempts = append(d.Attempts, at)
	}
	return d, nil
}

func assignmentView(
	a dbgen.PickupAssignment, q *dbgen.Queries, ctx context.Context, p *tenant.Principal,
) *Assignment {
	v := &Assignment{
		ID: a.PublicID, Status: a.Status, AssignedAt: a.AssignedAt,
		AcceptedAt: a.AcceptedAt, ArrivedAt: a.ArrivedAt, CompletedAt: a.CompletedAt,
		Rejection: ops.Deref(a.RejectionReason),
	}
	if a.StopSequence != nil {
		s := int(*a.StopSequence)
		v.StopSequence = &s
	}
	if u, err := q.GetUserByID(ctx, a.AgentUserID); err == nil && u.OrganizationID == p.OrganizationID {
		v.Agent = &ops.Ref{ID: u.PublicID, Name: u.FullName}
	}
	return v
}

// authorize applies the two access rules a pickup request answers to: a portal
// user sees only their own customer's requests, and a staff user sees only the
// branches their role covers.
func (s *Service) authorize(p *tenant.Principal, customerID, branchID int64) error {
	if p.IsPortalUser {
		if !p.IsCustomerInScope(customerID) {
			return apierr.NotFound("Pickup request")
		}
		return nil
	}
	if scope := p.UnitScope("shipment.read_all"); scope != nil {
		for _, id := range scope {
			if id == branchID {
				return nil
			}
		}
		// Out of scope reads as absent, so a branch manager cannot enumerate
		// another branch's work by trying ids.
		return apierr.NotFound("Pickup request")
	}
	return nil
}

// List returns the dispatcher queue.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal, f ListFilter,
) (pagination.CursorPage[Summary], error) {
	params := dbgen.ListPickupRequestsParams{
		OrganizationID: p.OrganizationID,
		PageSize:       int32(f.Limit + 1),
		UnitIds:        p.UnitScope("shipment.read_all"),
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.PickupType != "" {
		params.PickupType = &f.PickupType
	}
	if f.CustomerID != nil {
		params.CustomerID = f.CustomerID
	}
	if f.ScheduledDate != nil {
		params.ScheduledDate = f.ScheduledDate
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}

	rows, err := s.q.ListPickupRequests(ctx, params)
	if err != nil {
		return pagination.CursorPage[Summary]{}, apierr.Internal(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, Summary{
			ID: r.PublicID, ReferenceCode: r.ReferenceCode, Status: r.Status,
			PickupType:  r.PickupType,
			Customer:    &ops.Ref{ID: r.CustomerPublicID, Code: r.CustomerCode, Name: r.CustomerName},
			Branch:      &ops.Ref{ID: r.BranchPublicID, Code: r.BranchCode, Name: r.BranchName},
			ContactName: r.ContactName, Phone: r.ContactPhone, Line1: r.Line1,
			City: r.CityName, Pincode: r.Pincode,
			Window: Window{
				Date:  r.ScheduledDate.Format("2006-01-02"),
				Start: r.WindowStart, End: r.WindowEnd,
			},
			ExpectedPiece: int(r.ExpectedPieceCount), ActualPiece: int(r.ActualPieceCount),
			AttemptCount: int(r.AttemptCount), MaxAttempts: int(r.MaxAttempts),
			AgentName: ops.Deref(r.AgentName), AgentID: ops.Deref(r.AgentPublicID),
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc", func(s Summary) pagination.Cursor {
		created := s.CreatedAt
		return pagination.Cursor{Time: &created, ID: s.cursorID, Dir: "desc"}
	}), nil
}

// ListFilter is the dispatcher queue filter.
type ListFilter struct {
	Status        string
	PickupType    string
	CustomerID    *int64
	ScheduledDate *time.Time
	Cursor        *pagination.Cursor
	Limit         int
}

// AgentWorkList returns a field agent's stops for a day.
func (s *Service) AgentWorkList(
	ctx context.Context, p *tenant.Principal, agentID int64, date *time.Time, onlyOpen bool, limit int,
) ([]AgentStop, error) {
	rows, err := s.q.ListAgentPickupAssignments(ctx, dbgen.ListAgentPickupAssignmentsParams{
		OrganizationID: p.OrganizationID, AgentUserID: agentID,
		RunDate: date, OnlyOpen: &onlyOpen, PageSize: int32(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]AgentStop, 0, len(rows))
	for _, r := range rows {
		stop := AgentStop{
			AssignmentID: r.PublicID, RequestID: r.RequestPublicID,
			ReferenceCode: r.ReferenceCode, Status: r.Status, RequestStatus: r.RequestStatus,
			PickupType: r.PickupType, CustomerName: r.CustomerName,
			Address: Address{
				ContactName: r.ContactName, Phone: r.ContactPhone,
				Line1: r.Line1, Line2: ops.Deref(r.Line2), Landmark: ops.Deref(r.Landmark),
				City: r.CityName, Pincode: r.Pincode,
				Latitude: r.Latitude, Longitude: r.Longitude,
			},
			Window:        Window{Start: r.WindowStart, End: r.WindowEnd},
			ExpectedPiece: int(r.ExpectedPieceCount),
			Instructions:  ops.Deref(r.SpecialInstructions),
			AcceptedAt:    r.AcceptedAt, ArrivedAt: r.ArrivedAt,
		}
		if r.StopSequence != nil {
			v := int(*r.StopSequence)
			stop.StopSequence = &v
		}
		out = append(out, stop)
	}
	return out, nil
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
