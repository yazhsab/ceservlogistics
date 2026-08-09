package hubops

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

// RaiseInput opens an exception by hand.
type RaiseInput struct {
	Type        string
	Severity    string
	Facility    *ops.Facility
	Barcode     string
	BagCode     string
	Description string
	Expected    *int
	Actual      *int
	AssignTo    string
	Metadata    map[string]any
}

// Raise opens an operational exception.
func (s *Service) Raise(ctx context.Context, p *tenant.Principal, in RaiseInput) (*ExceptionDetail, error) {
	if in.Facility == nil {
		return nil, apierr.Validation("Raising an exception requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	if err := p.RequireUnitInScope(in.Facility.ID); err != nil {
		return nil, err
	}
	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindException, in.Facility.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *ExceptionDetail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateOperationalExceptionParams{
			PublicID: publicid.New(publicid.PrefixException), OrganizationID: p.OrganizationID,
			ExceptionCode: code, ExceptionType: in.Type,
			Severity:        orDefault(in.Severity, severityFor(in.Type)),
			OperatingUnitID: in.Facility.ID,
			Description:     in.Description, RaisedByUserID: &p.UserID,
			Metadata: encodeJSON(in.Metadata),
		}
		if in.Barcode != "" {
			params.RawBarcode = &in.Barcode
			if resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
				Barcode: in.Barcode, OrganizationID: p.OrganizationID,
			}); rErr == nil {
				params.ShipmentID = &resolved.ID
			} else if !ops.IsNoRows(rErr) {
				return apierr.Internal(rErr)
			}
		}
		if in.BagCode != "" {
			bag, bErr := q.GetBagByBarcode(ctx, dbgen.GetBagByBarcodeParams{
				OrganizationID: p.OrganizationID, Barcode: in.BagCode,
			})
			if bErr != nil {
				return ops.NotFoundOr(bErr, "Bag")
			}
			params.BagID = &bag.ID
		}
		if in.Expected != nil {
			v := int32(*in.Expected)
			params.ExpectedCount = &v
		}
		if in.Actual != nil {
			v := int32(*in.Actual)
			params.ActualCount = &v
		}
		if in.AssignTo != "" {
			u, uErr := q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
				PublicID: in.AssignTo, OrganizationID: p.OrganizationID,
			})
			if uErr != nil {
				return ops.NotFoundOr(uErr, "User")
			}
			params.AssignedToUserID = &u.ID
		}

		created, cErr := q.CreateOperationalException(ctx, params)
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionExceptionRaised, ResourceType: "operational_exception",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			OperatingUnitID: &in.Facility.ID, Reason: in.Description,
			After: map[string]any{
				"code": code, "type": in.Type, "severity": params.Severity,
				"facility": in.Facility.Code, "barcode": in.Barcode,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadException(ctx, q, p, created.PublicID)
		return dErr
	})
	return detail, err
}

// ResolveInput closes an exception.
type ResolveInput struct {
	ExceptionID string
	Status      string
	Action      string
	Notes       string
	AssignTo    string
}

// Resolve moves an exception forward or closes it.
//
// Closure demands an action and a note. "Resolved" with no explanation is
// exactly the record that makes a later dispute unwinnable, and the CHECK
// constraint on the table refuses it regardless of what this code does.
func (s *Service) Resolve(ctx context.Context, p *tenant.Principal, in ResolveInput) (*ExceptionDetail, error) {
	var detail *ExceptionDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		exc, eErr := q.LockExceptionForUpdate(ctx, dbgen.LockExceptionForUpdateParams{
			PublicID: in.ExceptionID, OrganizationID: p.OrganizationID,
		})
		if eErr != nil {
			return ops.NotFoundOr(eErr, "Exception")
		}
		if err := p.RequireUnitInScope(exc.OperatingUnitID); err != nil {
			return err
		}
		if exc.Status == "RESOLVED" || exc.Status == "WRITTEN_OFF" || exc.Status == "CANCELLED" {
			return apierr.Conflict("EXCEPTION_CLOSED",
				"This exception is already closed.").WithDetail("currentStatus", exc.Status)
		}
		terminal := in.Status == "RESOLVED" || in.Status == "WRITTEN_OFF"
		if terminal && (in.Action == "" || in.Notes == "") {
			return apierr.Validation(
				"Closing an exception requires both a resolution action and a note.",
				map[string]any{"fields": []string{"resolutionAction", "resolutionNotes"}})
		}

		params := dbgen.UpdateExceptionStatusParams{
			ID: exc.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: exc.Status, ToStatus: in.Status,
			ResolutionNotes: ops.Optional(in.Notes),
		}
		if in.Action != "" {
			params.ResolutionAction = &in.Action
		}
		if terminal {
			params.ResolvedByUserID = &p.UserID
		}
		if in.AssignTo != "" {
			u, uErr := q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
				PublicID: in.AssignTo, OrganizationID: p.OrganizationID,
			})
			if uErr != nil {
				return ops.NotFoundOr(uErr, "User")
			}
			params.AssignedToUserID = &u.ID
		}
		updated, uErr := q.UpdateExceptionStatus(ctx, params)
		if uErr != nil {
			return ops.ConflictOr(uErr, "This exception changed while it was being updated.")
		}

		action := audit.ActionExceptionResolved
		if !terminal {
			action = audit.ActionExceptionAssigned
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: action, ResourceType: "operational_exception",
			ResourceID: &exc.ID, ResourcePublicID: exc.PublicID,
			OperatingUnitID: &exc.OperatingUnitID, Reason: in.Notes,
			Before: map[string]any{"status": exc.Status},
			After: map[string]any{
				"status": updated.Status, "resolutionAction": in.Action,
				"exceptionCode": exc.ExceptionCode,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadException(ctx, q, p, exc.PublicID)
		return dErr
	})
	return detail, err
}

// ExceptionDetail is the full exception record.
type ExceptionDetail struct {
	ID           string     `json:"id"`
	Code         string     `json:"exceptionCode"`
	Type         string     `json:"exceptionType"`
	Severity     string     `json:"severity"`
	Status       string     `json:"status"`
	Facility     *ops.Ref   `json:"facility"`
	ShipmentID   string     `json:"shipmentId,omitempty"`
	AWB          string     `json:"awb,omitempty"`
	BagID        string     `json:"bagId,omitempty"`
	BagCode      string     `json:"bagCode,omitempty"`
	ManifestID   string     `json:"manifestId,omitempty"`
	ManifestCode string     `json:"manifestCode,omitempty"`
	Barcode      string     `json:"rawBarcode,omitempty"`
	Description  string     `json:"description"`
	Expected     *int       `json:"expectedCount,omitempty"`
	Actual       *int       `json:"actualCount,omitempty"`
	RaisedAt     time.Time  `json:"raisedAt"`
	RaisedBy     string     `json:"raisedBy,omitempty"`
	AssignedTo   string     `json:"assignedTo,omitempty"`
	Action       string     `json:"resolutionAction,omitempty"`
	Notes        string     `json:"resolutionNotes,omitempty"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty"`
	ResolvedBy   string     `json:"resolvedBy,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// ExceptionSummary is one row of the exception list.
type ExceptionSummary struct {
	ID          string     `json:"id"`
	Code        string     `json:"exceptionCode"`
	Type        string     `json:"exceptionType"`
	Severity    string     `json:"severity"`
	Status      string     `json:"status"`
	Facility    *ops.Ref   `json:"facility"`
	AWB         string     `json:"awb,omitempty"`
	ShipmentID  string     `json:"shipmentId,omitempty"`
	BagCode     string     `json:"bagCode,omitempty"`
	Description string     `json:"description"`
	Barcode     string     `json:"rawBarcode,omitempty"`
	RaisedAt    time.Time  `json:"raisedAt"`
	RaisedBy    string     `json:"raisedBy,omitempty"`
	AssignedTo  string     `json:"assignedTo,omitempty"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Action      string     `json:"resolutionAction,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`

	cursorID int64
}

// GetException returns one exception.
func (s *Service) GetException(ctx context.Context, p *tenant.Principal, id string) (*ExceptionDetail, error) {
	return s.loadException(ctx, s.q, p, id)
}

func (s *Service) loadException(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*ExceptionDetail, error) {
	row, err := q.GetExceptionByPublicID(ctx, dbgen.GetExceptionByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Exception")
	}
	if scope := p.UnitScope("shipment.read_all"); scope != nil && !containsID(scope, row.OperatingUnitID) {
		return nil, apierr.NotFound("Exception")
	}
	d := &ExceptionDetail{
		ID: row.PublicID, Code: row.ExceptionCode, Type: row.ExceptionType,
		Severity: row.Severity, Status: row.Status,
		Facility:   &ops.Ref{ID: row.UnitPublicID, Code: row.UnitCode, Name: row.UnitName},
		ShipmentID: ops.Deref(row.ShipmentPublicID), AWB: ops.Deref(row.Awb),
		BagID: ops.Deref(row.BagPublicID), BagCode: ops.Deref(row.BagCode),
		ManifestID: ops.Deref(row.ManifestPublicID), ManifestCode: ops.Deref(row.ManifestCode),
		Barcode: ops.Deref(row.RawBarcode), Description: row.Description,
		RaisedAt: row.RaisedAt, RaisedBy: ops.Deref(row.RaisedByName),
		AssignedTo: ops.Deref(row.AssignedToName),
		Action:     ops.Deref(row.ResolutionAction), Notes: ops.Deref(row.ResolutionNotes),
		ResolvedAt: row.ResolvedAt, ResolvedBy: ops.Deref(row.ResolvedByName),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ExpectedCount != nil {
		v := int(*row.ExpectedCount)
		d.Expected = &v
	}
	if row.ActualCount != nil {
		v := int(*row.ActualCount)
		d.Actual = &v
	}
	return d, nil
}

// ExceptionFilter narrows the exception list.
type ExceptionFilter struct {
	Status     string
	Type       string
	Severity   string
	UnitID     *int64
	ShipmentID *int64
	Cursor     *pagination.Cursor
	Limit      int
}

// ListExceptions returns exceptions visible to the caller.
func (s *Service) ListExceptions(
	ctx context.Context, p *tenant.Principal, f ExceptionFilter,
) (pagination.CursorPage[ExceptionSummary], error) {
	params := dbgen.ListExceptionsParams{
		OrganizationID: p.OrganizationID, PageSize: int32(f.Limit + 1),
		UnitIds: p.UnitScope("shipment.read_all"),
		UnitID:  f.UnitID, ShipmentID: f.ShipmentID,
	}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Type != "" {
		params.ExceptionType = &f.Type
	}
	if f.Severity != "" {
		params.Severity = &f.Severity
	}
	if f.Cursor != nil {
		params.CursorCreatedAt = f.Cursor.Time
		params.CursorID = &f.Cursor.ID
	}
	rows, err := s.q.ListExceptions(ctx, params)
	if err != nil {
		return pagination.CursorPage[ExceptionSummary]{}, apierr.Internal(err)
	}
	out := make([]ExceptionSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ExceptionSummary{
			ID: r.PublicID, Code: r.ExceptionCode, Type: r.ExceptionType,
			Severity: r.Severity, Status: r.Status,
			Facility: &ops.Ref{Code: r.UnitCode, Name: r.UnitName},
			AWB:      ops.Deref(r.Awb), ShipmentID: ops.Deref(r.ShipmentPublicID),
			BagCode: ops.Deref(r.BagCode), Description: r.Description,
			Barcode:  ops.Deref(r.RawBarcode),
			RaisedAt: r.RaisedAt, RaisedBy: ops.Deref(r.RaisedByName),
			AssignedTo: ops.Deref(r.AssignedToName), ResolvedAt: r.ResolvedAt,
			Action:    ops.Deref(r.ResolutionAction),
			CreatedAt: r.CreatedAt, cursorID: r.ID,
		})
	}
	return pagination.NewCursorPage(out, f.Limit, "createdAt", "desc",
		func(x ExceptionSummary) pagination.Cursor {
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ExceptionID resolves an exception's public id to its internal id.
//
// The bag correction workflow needs to link to an exception without importing
// this package's row types, and the lookup is scope-checked so a correction
// cannot cite an exception from another facility as its justification.
func (s *Service) ExceptionID(ctx context.Context, p *tenant.Principal, publicID string) (int64, error) {
	row, err := s.q.GetExceptionByPublicID(ctx, dbgen.GetExceptionByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return 0, ops.NotFoundOr(err, "Exception")
	}
	if scope := p.UnitScope("shipment.read_all"); scope != nil && !containsID(scope, row.OperatingUnitID) {
		return 0, apierr.NotFound("Exception")
	}
	return row.ID, nil
}
