package reporting

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler serves the report engine.
type Handler struct {
	svc *Service
	q   *dbgen.Queries
}

func NewHandler(svc *Service, q *dbgen.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Routes mounts /reports.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", require(PermRead, h.list))
	r.Get("/types", require(PermRead, h.types))
	r.Post("/", require(PermRun, h.queue))
	r.Get("/{reportId}", require(PermRead, h.get))
	r.Get("/{reportId}/download", require(PermRead, h.download))
	r.Post("/{reportId}/cancel", require(PermRun, h.cancel))
}

func require(permission string, next httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenant.Require(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return next(w, r)
	})
}

type queueRequest struct {
	ReportType  string `json:"reportType"`
	Format      string `json:"format,omitempty"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	UnitID      string `json:"unitId,omitempty"`
	Status      string `json:"status,omitempty"`
}

// queue accepts a report request and returns a receipt.
//
// 202, never 200: the response is an acknowledgement that the work is queued,
// and a client that treated it as the result would be waiting for a file that
// is not there yet.
func (h *Handler) queue(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req queueRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	// Finance reports carry somebody's income and somebody else's liability, so
	// they need a second permission beyond "may run reports".
	if RequiresFinance(req.ReportType) {
		if err := p.Require(PermFinance); err != nil {
			return err
		}
	}

	start, err := parseDate(req.PeriodStart, "periodStart")
	if err != nil {
		return err
	}
	end, err := parseDate(req.PeriodEnd, "periodEnd")
	if err != nil {
		return err
	}

	in := RunRequest{
		ReportType: req.ReportType, Format: req.Format,
		PeriodStart: start, PeriodEnd: end, Status: req.Status,
	}

	// A named unit is checked against the caller's scope; an unscoped caller
	// with no unit named gets the whole tenant, which is what a head-office
	// report is for.
	if req.UnitID != "" {
		if !publicid.Valid(publicid.PrefixOperatingUnit, req.UnitID) {
			return apierr.Validation("unitId is not a valid operating unit identifier.", nil)
		}
		unit, uErr := h.q.GetOperatingUnitByPublicID(r.Context(),
			dbgen.GetOperatingUnitByPublicIDParams{
				PublicID: req.UnitID, OrganizationID: p.OrganizationID,
			})
		if uErr != nil {
			return apierr.NotFound("Operating unit")
		}
		if err := p.RequireUnitInScope(unit.ID); err != nil {
			return err
		}
		in.UnitID = &unit.ID
	} else if scope := p.UnitScope(); scope != nil {
		if len(scope) == 0 {
			return apierr.Forbidden("You are not assigned to any operating unit.")
		}
		// A scoped principal exports its own unit whether or not it asked, for
		// the same reason the command centre confines it.
		if len(scope) == 1 {
			in.UnitID = &scope[0]
		} else {
			return apierr.Validation(
				"Your access covers several operating units. Name one with unitId.", nil)
		}
	}

	run, err := h.svc.Queue(r.Context(), p, in)
	if err != nil {
		return err
	}
	return httpx.Accepted(w, map[string]any{
		"id": run.PublicID, "reportType": run.ReportType, "format": run.Format,
		"status": run.Status, "createdAt": run.CreatedAt,
		"pollUrl": "/api/v1/reports/" + run.PublicID,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "reportId", "rpt", "Report run")
	if err != nil {
		return err
	}
	run, err := h.svc.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	out := map[string]any{
		"id": run.PublicID, "reportType": run.ReportType, "format": run.Format,
		"status": run.Status, "rowCount": run.RowCount, "byteSize": run.ByteSize,
		"periodStart": dateString(run.PeriodStart), "periodEnd": dateString(run.PeriodEnd),
		"startedAt": run.StartedAt, "completedAt": run.CompletedAt,
		"durationMs": run.DurationMs, "expiresAt": run.ExpiresAt,
		"errorMessage": run.ErrorMessage, "createdAt": run.CreatedAt,
	}
	if run.Status == StatusCompleted {
		out["downloadUrl"] = "/api/v1/reports/" + run.PublicID + "/download"
	}
	return httpx.OK(w, out)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	reportType, err := httpx.QueryEnum(r, "reportType", AllTypes)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", []string{
		StatusQueued, StatusRunning, StatusCompleted,
		StatusFailed, StatusExpired, StatusCancelled,
	})
	if err != nil {
		return err
	}
	mine, err := httpx.QueryBool(r, "mine", false)
	if err != nil {
		return err
	}
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	cursor, err := httpx.QueryInt(r, "cursor", 0, 0, 1<<62)
	if err != nil {
		return err
	}
	var cursorID *int64
	if cursor > 0 {
		c := int64(cursor)
		cursorID = &c
	}

	rows, err := h.svc.List(r.Context(), p, ListFilter{
		ReportType: reportType, Status: status, Mine: mine,
		CursorID: cursorID, Limit: int32(limit),
	})
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, run := range rows {
		items = append(items, map[string]any{
			"id": run.PublicID, "reportType": run.ReportType, "format": run.Format,
			"status": run.Status, "rowCount": run.RowCount, "byteSize": run.ByteSize,
			"periodStart": dateString(run.PeriodStart), "periodEnd": dateString(run.PeriodEnd),
			"completedAt": run.CompletedAt, "expiresAt": run.ExpiresAt,
			"durationMs": run.DurationMs, "errorMessage": run.ErrorMessage,
			"requestedBy": run.RequestedByName, "createdAt": run.CreatedAt,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

// download hands over the file.
//
// A signed URL where the backend can presign, so the bytes never travel through
// the API at all; a stream where it cannot. Either way the link is short-lived,
// because a permanent URL to an export of customer data is a credential nobody
// thinks of as one.
func (h *Handler) download(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "reportId", "rpt", "Report run")
	if err != nil {
		return err
	}
	url, body, filename, err := h.svc.Download(r.Context(), p, id)
	if err != nil {
		return err
	}
	if url != "" {
		w.Header().Set("Location", url)
		// Cache nothing: the URL is a short-lived credential.
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusFound)
		return nil
	}

	defer func() { _ = body.Close() }()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// Streamed, not buffered: the file may be hundreds of megabytes and the API
	// must not hold it (§38).
	_, _ = io.Copy(w, body)
	return nil
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "reportId", "rpt", "Report run")
	if err != nil {
		return err
	}
	run, err := h.svc.Cancel(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"id": run.PublicID, "status": run.Status})
}

// types lists what can be run, and which entries need the finance permission.
func (h *Handler) types(w http.ResponseWriter, r *http.Request) error {
	items := make([]map[string]any, 0, len(AllTypes))
	for _, t := range AllTypes {
		items = append(items, map[string]any{
			"reportType": t, "requiresFinancePermission": RequiresFinance(t),
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func parseDate(raw, field string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, apierr.Validation("This date is required.",
			map[string]any{field: "expected YYYY-MM-DD"})
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, apierr.Validation("Invalid date.",
			map[string]any{field: "expected YYYY-MM-DD"})
	}
	return t, nil
}
