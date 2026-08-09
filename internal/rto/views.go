package rto

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

// ReturnAddress is where the parcel is going back to.
type ReturnAddress struct {
	ContactName string `json:"contactName,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Line1       string `json:"line1,omitempty"`
	Line2       string `json:"line2,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	Pincode     string `json:"pincode,omitempty"`
}

// LegView is one hop of the return journey.
type LegView struct {
	Sequence    int        `json:"sequence"`
	Type        string     `json:"legType"`
	Status      string     `json:"status"`
	From        *ops.Ref   `json:"from,omitempty"`
	To          *ops.Ref   `json:"to,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// CaseDetail is the full RTO case.
type CaseDetail struct {
	ID            string         `json:"id"`
	CaseCode      string         `json:"caseCode"`
	Status        string         `json:"status"`
	ShipmentID    string         `json:"shipmentId"`
	AWB           string         `json:"awb"`
	ShipmentState string         `json:"shipmentStatus"`
	Direction     string         `json:"movementDirection"`
	ReasonCode    string         `json:"reasonCode"`
	ReasonNotes   string         `json:"reasonNotes,omitempty"`
	ReturnBranch  *ops.Ref       `json:"returnBranch,omitempty"`
	RouteSource   string         `json:"routeResolutionSource,omitempty"`
	Legs          []LegView      `json:"legs"`
	Return        *ReturnAddress `json:"returnAddress,omitempty"`
	ChargeMinor   int64          `json:"rtoChargeMinor"`
	Currency      string         `json:"currency"`
	ChargeBearer  string         `json:"chargeBearer"`
	ChargeNotes   string         `json:"chargeRuleNotes,omitempty"`
	InitiatedAt   time.Time      `json:"initiatedAt"`
	InitiatedBy   string         `json:"initiatedBy,omitempty"`
	ReturnedAt    *time.Time     `json:"returnedAt,omitempty"`
	ReturnedTo    string         `json:"returnedToName,omitempty"`
	ClosedAt      *time.Time     `json:"closedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	// Explanation records how the reverse route was chosen.
	Explanation map[string]any `json:"routeExplanation,omitempty"`
}

// CaseSummary is one row of the RTO list.
type CaseSummary struct {
	ID            string     `json:"id"`
	CaseCode      string     `json:"caseCode"`
	Status        string     `json:"status"`
	ShipmentID    string     `json:"shipmentId"`
	AWB           string     `json:"awb"`
	ShipmentState string     `json:"shipmentStatus"`
	ReasonCode    string     `json:"reasonCode"`
	ReturnBranch  *ops.Ref   `json:"returnBranch,omitempty"`
	Customer      *ops.Ref   `json:"customer,omitempty"`
	ChargeMinor   int64      `json:"rtoChargeMinor"`
	Currency      string     `json:"currency"`
	ChargeBearer  string     `json:"chargeBearer"`
	InitiatedAt   time.Time  `json:"initiatedAt"`
	ReturnedAt    *time.Time `json:"returnedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`

	cursorID int64
}

// GetCase returns one RTO case.
func (s *Service) GetCase(ctx context.Context, p *tenant.Principal, id string) (*CaseDetail, error) {
	return s.loadCase(ctx, s.q, p, id)
}

func (s *Service) loadCase(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*CaseDetail, error) {
	row, err := q.GetRTOCaseByPublicID(ctx, dbgen.GetRTOCaseByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "RTO case")
	}
	if p.IsPortalUser {
		if !p.IsCustomerInScope(row.CustomerID) {
			return nil, apierr.NotFound("RTO case")
		}
	} else if scope := p.UnitScope("shipment.read_all"); scope != nil {
		if row.ReturnBranchID == nil || !containsID(scope, *row.ReturnBranchID) {
			return nil, apierr.NotFound("RTO case")
		}
	}

	d := &CaseDetail{
		ID: row.PublicID, CaseCode: row.CaseCode, Status: row.Status,
		ShipmentID: row.ShipmentPublicID, AWB: row.Awb, ShipmentState: row.ShipmentStatus,
		Direction: row.MovementDirection, ReasonCode: row.ReasonCode,
		ReasonNotes: ops.Deref(row.ReasonNotes),
		RouteSource: ops.Deref(row.RouteResolutionSource),
		Legs:        []LegView{},
		ChargeMinor: row.RtoChargeMinor, Currency: row.Currency,
		ChargeBearer: row.ChargeBearer, ChargeNotes: ops.Deref(row.ChargeRuleNotes),
		InitiatedAt: row.InitiatedAt, InitiatedBy: ops.Deref(row.InitiatedByName),
		ReturnedAt: row.ReturnedAt, ReturnedTo: ops.Deref(row.ReturnedToName),
		ClosedAt: row.ClosedAt, CreatedAt: row.CreatedAt,
	}
	if row.ReturnBranchCode != nil {
		d.ReturnBranch = &ops.Ref{
			Code: *row.ReturnBranchCode, Name: ops.Deref(row.ReturnBranchName),
		}
	}
	if row.ReturnLine1 != nil || row.ReturnPincode != nil {
		d.Return = &ReturnAddress{
			ContactName: ops.Deref(row.ReturnContactName), Phone: ops.Deref(row.ReturnPhone),
			Line1: ops.Deref(row.ReturnLine1), Line2: ops.Deref(row.ReturnLine2),
			City: ops.Deref(row.ReturnCity), State: ops.Deref(row.ReturnState),
			Pincode: ops.Deref(row.ReturnPincode),
		}
	}
	if len(row.RouteExplanation) > 0 {
		var exp map[string]any
		if err := json.Unmarshal(row.RouteExplanation, &exp); err == nil && len(exp) > 0 {
			d.Explanation = exp
		}
	}

	legs, err := q.ListRTOLegs(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, l := range legs {
		view := LegView{
			Sequence: int(l.Sequence), Type: l.LegType, Status: l.Status,
			StartedAt: l.StartedAt, CompletedAt: l.CompletedAt,
		}
		if l.FromCode != nil {
			view.From = &ops.Ref{Code: *l.FromCode, Name: ops.Deref(l.FromName)}
		}
		if l.ToCode != nil {
			view.To = &ops.Ref{Code: *l.ToCode, Name: ops.Deref(l.ToName)}
		}
		d.Legs = append(d.Legs, view)
	}
	return d, nil
}

// CaseFilter narrows the RTO list.
type CaseFilter struct {
	Status   string
	BranchID *int64
	Cursor   *pagination.Cursor
	Limit    int
}

// ListCases returns RTO cases visible to the caller.
func (s *Service) ListCases(
	ctx context.Context, p *tenant.Principal, f CaseFilter,
) (pagination.CursorPage[CaseSummary], error) {
	params := dbgen.ListRTOCasesParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds:        p.UnitScope("shipment.read_all"),
		CustomerIds:    p.CustomerScope(),
		ReturnBranchID: f.BranchID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListRTOCases(ctx, params)
	if err != nil {
		return pagination.CursorPage[CaseSummary]{}, apierr.Internal(err)
	}
	out := make([]CaseSummary, 0, len(rows))
	for _, r := range rows {
		item := CaseSummary{
			ID: r.PublicID, CaseCode: r.CaseCode, Status: r.Status,
			ShipmentID: r.ShipmentPublicID, AWB: r.Awb, ShipmentState: r.ShipmentStatus,
			ReasonCode:  r.ReasonCode,
			Customer:    &ops.Ref{Code: r.CustomerCode, Name: r.CustomerName},
			ChargeMinor: r.RtoChargeMinor, Currency: r.Currency,
			ChargeBearer: r.ChargeBearer,
			InitiatedAt:  r.InitiatedAt, ReturnedAt: r.ReturnedAt,
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		}
		if r.ReturnBranchCode != nil {
			item.ReturnBranch = &ops.Ref{
				Code: *r.ReturnBranchCode, Name: ops.Deref(r.ReturnBranchName),
			}
		}
		out = append(out, item)
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x CaseSummary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

func containsID(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
