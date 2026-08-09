package ndr

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Reason is a configured NDR reason.
type Reason struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Category        string `json:"category"`
	DefaultAction   string `json:"defaultAction"`
	MaxAttempts     int    `json:"maxAttempts"`
	CustomerFault   bool   `json:"isCustomerFault"`
	NeedsEvidence   bool   `json:"requiresEvidence"`
	AutoRTOAfterMax bool   `json:"autoRtoAfterMax"`
	IsActive        bool   `json:"isActive"`
	DisplayOrder    int    `json:"displayOrder"`
}

// CaseAttempt is one failed visit recorded against a case.
type CaseAttempt struct {
	ID            string    `json:"id"`
	AttemptNumber int       `json:"attemptNumber"`
	ReasonCode    string    `json:"reasonCode"`
	Remarks       string    `json:"remarks,omitempty"`
	Evidence      []string  `json:"evidenceObjectKeys,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
	RecordedAt    time.Time `json:"recordedAt"`
	RecordedBy    string    `json:"recordedBy,omitempty"`
}

// CaseAction is one instruction on a case.
type CaseAction struct {
	ID           string     `json:"id"`
	Sequence     int        `json:"sequence"`
	Action       string     `json:"action"`
	RequestedBy  string     `json:"requestedBy"`
	Status       string     `json:"status"`
	Instructions string     `json:"instructions,omitempty"`
	ScheduledFor *time.Time `json:"scheduledFor,omitempty"`
	ContactName  string     `json:"contactName,omitempty"`
	ContactPhone string     `json:"contactPhone,omitempty"`
	ContactNotes string     `json:"contactNotes,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	CreatedBy    string     `json:"createdBy,omitempty"`
	AppliedAt    *time.Time `json:"appliedAt,omitempty"`
}

// CorrectedAddress is the address supplied through the NDR workflow.
//
// It never replaces the shipment's booking snapshot; both are shown so the
// record of where the parcel was originally sent survives (§13).
type CorrectedAddress struct {
	Line1       string     `json:"line1,omitempty"`
	Line2       string     `json:"line2,omitempty"`
	Landmark    string     `json:"landmark,omitempty"`
	Pincode     string     `json:"pincode,omitempty"`
	Phone       string     `json:"phone,omitempty"`
	CorrectedAt *time.Time `json:"correctedAt,omitempty"`
	CorrectedBy string     `json:"correctedBy,omitempty"`
}

// CaseDetail is the full NDR case.
type CaseDetail struct {
	ID            string            `json:"id"`
	CaseCode      string            `json:"caseCode"`
	Status        string            `json:"status"`
	ShipmentID    string            `json:"shipmentId"`
	AWB           string            `json:"awb"`
	ShipmentState string            `json:"shipmentStatus"`
	Branch        *ops.Ref          `json:"branch,omitempty"`
	ReasonCode    string            `json:"currentReasonCode"`
	ReasonName    string            `json:"reasonName,omitempty"`
	Category      string            `json:"reasonCategory,omitempty"`
	CustomerFault bool              `json:"isCustomerFault"`
	Action        string            `json:"currentAction,omitempty"`
	AttemptCount  int               `json:"attemptCount"`
	MaxAttempts   int               `json:"maxAttempts"`
	NextAttemptAt *time.Time        `json:"nextAttemptAt,omitempty"`
	AssignedTo    string            `json:"assignedTo,omitempty"`
	Corrected     *CorrectedAddress `json:"correctedAddress,omitempty"`
	Attempts      []CaseAttempt     `json:"attempts"`
	Actions       []CaseAction      `json:"actions"`
	OpenedAt      time.Time         `json:"openedAt"`
	ResolvedAt    *time.Time        `json:"resolvedAt,omitempty"`
	Resolution    string            `json:"resolutionNotes,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	// AvailableActions is what an operator may choose next.
	AvailableActions []string `json:"availableActions"`
}

// CaseSummary is one row of the NDR list.
type CaseSummary struct {
	ID            string     `json:"id"`
	CaseCode      string     `json:"caseCode"`
	Status        string     `json:"status"`
	ShipmentID    string     `json:"shipmentId"`
	AWB           string     `json:"awb"`
	ShipmentState string     `json:"shipmentStatus"`
	ReasonCode    string     `json:"currentReasonCode"`
	ReasonName    string     `json:"reasonName,omitempty"`
	Category      string     `json:"reasonCategory,omitempty"`
	Action        string     `json:"currentAction,omitempty"`
	AttemptCount  int        `json:"attemptCount"`
	MaxAttempts   int        `json:"maxAttempts"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	Branch        *ops.Ref   `json:"branch,omitempty"`
	Customer      *ops.Ref   `json:"customer,omitempty"`
	Recipient     string     `json:"recipientName,omitempty"`
	Phone         string     `json:"recipientPhone,omitempty"`
	Pincode       string     `json:"pincode,omitempty"`
	PromisedAt    *time.Time `json:"promisedDeliveryAt,omitempty"`
	CODAmount     int64      `json:"codAmountMinor"`
	PaymentMode   string     `json:"paymentMode"`
	OpenedAt      time.Time  `json:"openedAt"`
	ResolvedAt    *time.Time `json:"resolvedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`

	cursorID int64
}

// GetCase returns one NDR case.
func (s *Service) GetCase(ctx context.Context, p *tenant.Principal, id string) (*CaseDetail, error) {
	return s.loadCase(ctx, s.q, p, id)
}

func (s *Service) loadCase(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*CaseDetail, error) {
	row, err := q.GetNDRCaseByPublicID(ctx, dbgen.GetNDRCaseByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "NDR case")
	}
	if p.IsPortalUser {
		if !p.IsCustomerInScope(row.CustomerID) {
			return nil, apierr.NotFound("NDR case")
		}
	} else if scope := p.UnitScope("shipment.read_all"); scope != nil {
		if row.BranchID == nil || !containsID(scope, *row.BranchID) {
			return nil, apierr.NotFound("NDR case")
		}
	}

	d := &CaseDetail{
		ID: row.PublicID, CaseCode: row.CaseCode, Status: row.Status,
		ShipmentID: row.ShipmentPublicID, AWB: row.Awb, ShipmentState: row.ShipmentStatus,
		ReasonCode: row.CurrentReasonCode, ReasonName: ops.Deref(row.ReasonName),
		Category:      ops.Deref(row.ReasonCategory),
		CustomerFault: row.IsCustomerFault != nil && *row.IsCustomerFault,
		Action:        ops.Deref(row.CurrentAction),
		AttemptCount:  int(row.AttemptCount), MaxAttempts: int(row.MaxAttempts),
		NextAttemptAt: row.NextAttemptAt, AssignedTo: ops.Deref(row.AssignedToName),
		Attempts: []CaseAttempt{}, Actions: []CaseAction{},
		OpenedAt: row.OpenedAt, ResolvedAt: row.ResolvedAt,
		Resolution: ops.Deref(row.ResolutionNotes), CreatedAt: row.CreatedAt,
		AvailableActions: availableActions(row.Status),
	}
	if row.BranchCode != nil {
		d.Branch = &ops.Ref{Code: *row.BranchCode, Name: ops.Deref(row.BranchName)}
	}
	if row.CorrectedAt != nil {
		d.Corrected = &CorrectedAddress{
			Line1: ops.Deref(row.CorrectedAddressLine1), Line2: ops.Deref(row.CorrectedAddressLine2),
			Landmark: ops.Deref(row.CorrectedLandmark), Pincode: ops.Deref(row.CorrectedPincode),
			Phone: ops.Deref(row.CorrectedPhone), CorrectedAt: row.CorrectedAt,
		}
	}

	attempts, err := q.ListNDRAttempts(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range attempts {
		d.Attempts = append(d.Attempts, CaseAttempt{
			ID: a.PublicID, AttemptNumber: int(a.AttemptNumber), ReasonCode: a.ReasonCode,
			Remarks: ops.Deref(a.Remarks), Evidence: a.EvidenceObjectKeys,
			OccurredAt: a.OccurredAt, RecordedAt: a.RecordedAt,
			RecordedBy: ops.Deref(a.RecordedByName),
		})
	}

	actions, err := q.ListNDRActions(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range actions {
		d.Actions = append(d.Actions, CaseAction{
			ID: a.PublicID, Sequence: int(a.Sequence), Action: a.Action,
			RequestedBy: a.RequestedBy, Status: a.Status,
			Instructions: ops.Deref(a.Instructions), ScheduledFor: a.ScheduledFor,
			ContactName: ops.Deref(a.ContactName), ContactPhone: ops.Deref(a.ContactPhone),
			ContactNotes: ops.Deref(a.ContactNotes),
			CreatedAt:    a.CreatedAt, CreatedBy: ops.Deref(a.CreatedByName),
			AppliedAt: a.AppliedAt,
		})
	}
	return d, nil
}

func availableActions(status string) []string {
	if !openStatuses[status] {
		return []string{}
	}
	return Actions
}

// CaseFilter narrows the NDR list.
type CaseFilter struct {
	Status     string
	ReasonCode string
	Action     string
	BranchID   *int64
	Cursor     *pagination.Cursor
	Limit      int
}

// ListCases returns NDR cases visible to the caller.
func (s *Service) ListCases(
	ctx context.Context, p *tenant.Principal, f CaseFilter,
) (pagination.CursorPage[CaseSummary], error) {
	params := dbgen.ListNDRCasesParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds:     p.UnitScope("shipment.read_all"),
		CustomerIds: p.CustomerScope(),
		BranchID:    f.BranchID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.ReasonCode != "" {
		params.ReasonCode = &f.ReasonCode
	}
	if f.Action != "" {
		params.Action = &f.Action
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListNDRCases(ctx, params)
	if err != nil {
		return pagination.CursorPage[CaseSummary]{}, apierr.Internal(err)
	}
	out := make([]CaseSummary, 0, len(rows))
	for _, r := range rows {
		item := CaseSummary{
			ID: r.PublicID, CaseCode: r.CaseCode, Status: r.Status,
			ShipmentID: r.ShipmentPublicID, AWB: r.Awb, ShipmentState: r.ShipmentStatus,
			ReasonCode: r.CurrentReasonCode, ReasonName: ops.Deref(r.ReasonName),
			Category: ops.Deref(r.ReasonCategory), Action: ops.Deref(r.CurrentAction),
			AttemptCount: int(r.AttemptCount), MaxAttempts: int(r.MaxAttempts),
			NextAttemptAt: r.NextAttemptAt,
			Customer:      &ops.Ref{Code: r.CustomerCode, Name: r.CustomerName},
			Recipient:     ops.Deref(r.RecipientName), Phone: ops.Deref(r.RecipientPhone),
			Pincode: ops.Deref(r.Pincode), PromisedAt: r.PromisedDeliveryAt,
			CODAmount: r.CodAmountMinor, PaymentMode: r.PaymentMode,
			OpenedAt: r.OpenedAt, ResolvedAt: r.ResolvedAt,
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		}
		if r.BranchCode != nil {
			item.Branch = &ops.Ref{Code: *r.BranchCode, Name: ops.Deref(r.BranchName)}
		}
		out = append(out, item)
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x CaseSummary) pagination.Cursor {
			at := x.CreatedAt
			return pagination.Cursor{Time: &at, ID: x.cursorID, Dir: "desc"}
		}), nil
}

// ListReasons returns the tenant's NDR reason catalogue.
func (s *Service) ListReasons(ctx context.Context, p *tenant.Principal, activeOnly bool) ([]Reason, error) {
	params := dbgen.ListNDRReasonsParams{OrganizationID: p.OrganizationID}
	if activeOnly {
		t := true
		params.IsActive = &t
	}
	rows, err := s.q.ListNDRReasons(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]Reason, 0, len(rows))
	for _, r := range rows {
		out = append(out, reasonView(r))
	}
	return out, nil
}

// UpsertReasonInput creates or updates a reason.
type UpsertReasonInput struct {
	ID              string
	Code            string
	Name            string
	Description     string
	Category        string
	DefaultAction   string
	MaxAttempts     int
	CustomerFault   bool
	NeedsEvidence   bool
	AutoRTOAfterMax bool
	DisplayOrder    int
	IsActive        *bool
}

// CreateReason adds a reason to the tenant's catalogue.
func (s *Service) CreateReason(ctx context.Context, p *tenant.Principal, in UpsertReasonInput) (*Reason, error) {
	var out *Reason
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.CreateNDRReason(ctx, dbgen.CreateNDRReasonParams{
			PublicID: publicid.New(publicid.PrefixNDRReason), OrganizationID: p.OrganizationID,
			Code: in.Code, Name: in.Name, Description: ops.Optional(in.Description),
			Category: in.Category, DefaultAction: in.DefaultAction,
			MaxAttempts: int32(in.MaxAttempts), IsCustomerFault: in.CustomerFault,
			RequiresEvidence: in.NeedsEvidence, AutoRtoAfterMax: in.AutoRTOAfterMax,
			DisplayOrder: int32(in.DisplayOrder),
		})
		if err != nil {
			if ops.IsUnique(err, "ndr_reasons_code_unique") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"An NDR reason with this code already exists.").WithDetail("code", in.Code)
			}
			return apierr.Internal(err)
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionNDRReasonChanged, ResourceType: "ndr_reason",
			ResourceID: &row.ID, ResourcePublicID: row.PublicID,
			After: map[string]any{
				"code": in.Code, "defaultAction": in.DefaultAction, "maxAttempts": in.MaxAttempts,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		v := reasonView(row)
		out = &v
		return nil
	})
	return out, err
}

// UpdateReason edits a reason.
func (s *Service) UpdateReason(ctx context.Context, p *tenant.Principal, in UpsertReasonInput) (*Reason, error) {
	var out *Reason
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.UpdateNDRReasonParams{
			PublicID: in.ID, OrganizationID: p.OrganizationID,
			IsActive: in.IsActive,
		}
		if in.Name != "" {
			params.Name = &in.Name
		}
		if in.Description != "" {
			params.Description = &in.Description
		}
		if in.Category != "" {
			params.Category = &in.Category
		}
		if in.DefaultAction != "" {
			params.DefaultAction = &in.DefaultAction
		}
		if in.MaxAttempts > 0 {
			v := int32(in.MaxAttempts)
			params.MaxAttempts = &v
		}
		row, err := q.UpdateNDRReason(ctx, params)
		if err != nil {
			return ops.NotFoundOr(err, "NDR reason")
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionNDRReasonChanged, ResourceType: "ndr_reason",
			ResourceID: &row.ID, ResourcePublicID: row.PublicID,
			After: map[string]any{"code": row.Code, "isActive": row.IsActive},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		v := reasonView(row)
		out = &v
		return nil
	})
	return out, err
}

func reasonView(r dbgen.NdrReason) Reason {
	return Reason{
		ID: r.PublicID, Code: r.Code, Name: r.Name,
		Description: ops.Deref(r.Description), Category: r.Category,
		DefaultAction: r.DefaultAction, MaxAttempts: int(r.MaxAttempts),
		CustomerFault: r.IsCustomerFault, NeedsEvidence: r.RequiresEvidence,
		AutoRTOAfterMax: r.AutoRtoAfterMax, IsActive: r.IsActive,
		DisplayOrder: int(r.DisplayOrder),
	}
}

func containsID(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
