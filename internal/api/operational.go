package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

var startedAt = time.Now()

// mountOperational adds the probe and build endpoints.
//
// They live outside /api/v1 and outside authentication because a load balancer
// and an orchestrator must reach them without credentials. They expose no
// tenant data — only whether this process can serve traffic.
func (a *Application) mountOperational(r chi.Router, deps Dependencies) {
	// livez answers "is the process running", used by the container restart
	// policy. It must never touch a dependency: a database outage should not
	// cause a restart loop.
	r.Get("/livez", httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		return httpx.OK(w, map[string]any{
			"status": "alive", "uptimeSeconds": int(time.Since(startedAt).Seconds()),
		})
	}))

	// healthz is an alias kept for tooling that expects the conventional name.
	r.Get("/healthz", httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		return httpx.OK(w, map[string]any{"status": "ok"})
	}))

	// readyz answers "can this process serve traffic", used by the load
	// balancer. It checks dependencies and returns 503 when it cannot.
	r.Get("/readyz", httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		healthy, checks := a.Ready(req.Context())
		body := map[string]any{"status": "ready", "checks": checks}
		if !healthy {
			body["status"] = "not_ready"
			return httpx.JSON(w, http.StatusServiceUnavailable, body)
		}
		return httpx.OK(w, body)
	}))

	r.Get("/version", httpx.Wrap(func(w http.ResponseWriter, req *http.Request) error {
		return httpx.OK(w, map[string]any{
			"service":     a.Config.Service.Name,
			"version":     a.Config.Service.Version,
			"commit":      a.Config.Service.Commit,
			"builtAt":     a.Config.Service.BuiltAt,
			"environment": a.Config.Env,
			"apiVersion":  "v1",
		})
	}))
}

// ---- organization ----------------------------------------------------------

type updateOrganizationRequest struct {
	Name         *string        `json:"name,omitempty"`
	LegalName    *string        `json:"legalName,omitempty"`
	Timezone     *string        `json:"timezone,omitempty"`
	ContactEmail *string        `json:"contactEmail,omitempty"`
	ContactPhone *string        `json:"contactPhone,omitempty"`
	GSTNumber    *string        `json:"gstNumber,omitempty"`
	Settings     map[string]any `json:"settings,omitempty"`
}

func (a *Application) organizationRoutes(r chi.Router) {
	r.With(auth.RequirePermission("organization.read")).Get("/", httpx.Wrap(a.getOrganization))
	r.With(auth.RequirePermission("organization.update")).Patch("/", httpx.Wrap(a.updateOrganization))
}

func (a *Application) getOrganization(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	org, err := a.Queries.GetOrganizationByID(r.Context(), p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, organizationView(org))
}

func (a *Application) updateOrganization(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req updateOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	before, err := a.Queries.GetOrganizationByID(r.Context(), p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}

	v := validate.New()
	params := dbgen.UpdateOrganizationParams{ID: p.OrganizationID}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 160, true)
		params.Name = &n
	}
	if req.LegalName != nil {
		ln := v.Text("legalName", *req.LegalName, 2, 200, false)
		params.LegalName = &ln
	}
	if req.Timezone != nil {
		tz := v.Text("timezone", *req.Timezone, 3, 64, true)
		if _, tzErr := time.LoadLocation(tz); tzErr != nil {
			v.Add("timezone", "Must be a valid IANA time zone identifier, for example Asia/Kolkata.")
		}
		params.Timezone = &tz
	}
	if req.ContactEmail != nil {
		e := v.Email("contactEmail", *req.ContactEmail)
		params.ContactEmail = &e
	}
	if req.ContactPhone != nil {
		ph := v.Phone("contactPhone", *req.ContactPhone, false)
		params.ContactPhone = &ph
	}
	if req.GSTNumber != nil {
		g := v.GSTIN("gstNumber", *req.GSTNumber, false)
		params.GstNumber = &g
	}
	if req.Settings != nil {
		params.Settings = encodeJSON(req.Settings)
	}
	if err := v.Err(); err != nil {
		return err
	}

	// The AWB prefix, code, currency and status are deliberately not editable
	// here: changing a prefix would break the uniqueness assumption behind
	// existing AWBs, and the rest are platform-controlled.
	updated, err := a.Queries.UpdateOrganization(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	a.Audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionOrganizationUpdated, ResourceType: "organization",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
		Before: map[string]any{"name": before.Name, "timezone": before.Timezone},
		After:  map[string]any{"name": updated.Name, "timezone": updated.Timezone},
	}))
	return httpx.OK(w, organizationView(updated))
}

func organizationView(o dbgen.Organization) map[string]any {
	return map[string]any{
		"id": o.PublicID, "code": o.Code, "name": o.Name, "legalName": o.LegalName,
		"status": o.Status, "timezone": o.Timezone, "currency": o.Currency,
		"awbPrefix": o.AwbPrefix, "contactEmail": o.ContactEmail, "contactPhone": o.ContactPhone,
		"gstNumber": o.GstNumber, "settings": decodeJSON(o.Settings),
		"createdAt": o.CreatedAt, "updatedAt": o.UpdatedAt,
	}
}

// ---- audit -----------------------------------------------------------------

func (a *Application) auditRoutes(r chi.Router) {
	r.With(auth.RequirePermission("audit.read")).Get("/", httpx.Wrap(a.listAuditEvents))
}

func (a *Application) listAuditEvents(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	cursor, err := pagination.DecodeCursor(httpx.Query(r, "cursor"), "desc")
	if err != nil {
		return err
	}
	from, err := httpx.QueryTime(r, "occurredFrom")
	if err != nil {
		return err
	}
	to, err := httpx.QueryTime(r, "occurredTo")
	if err != nil {
		return err
	}

	params := dbgen.ListAuditEventsParams{
		OrganizationID: &p.OrganizationID,
		RowLimit:       int32(limit + 1),
		OccurredFrom:   from, OccurredTo: to,
	}
	if action := httpx.Query(r, "action"); action != "" {
		params.Action = &action
	}
	if rt := httpx.Query(r, "resourceType"); rt != "" {
		params.ResourceType = &rt
	}
	if rid := httpx.Query(r, "resourceId"); rid != "" {
		params.ResourcePublicID = &rid
	}
	if actorID, aErr := httpx.QueryPublicID(r, "actorId", publicid.PrefixUser); aErr != nil {
		return aErr
	} else if actorID != "" {
		user, uErr := a.Queries.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
			PublicID: actorID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			return apierr.NotFound("User")
		}
		params.ActorUserID = &user.ID
	}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := a.Queries.ListAuditEvents(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		items = append(items, map[string]any{
			"id": e.PublicID, "action": e.Action, "resourceType": e.ResourceType,
			"resourceId": e.ResourcePublicID, "actorType": e.ActorType,
			"actorEmail": e.ActorEmail, "actorName": e.ActorName,
			"reason": e.Reason, "requestId": e.RequestID, "clientIp": e.ClientIp,
			"before": decodeJSON(e.BeforeState), "after": decodeJSON(e.AfterState),
			"metadata": decodeJSON(e.Metadata), "occurredAt": e.OccurredAt,
			"_cursorId": e.ID,
		})
	}
	page := pagination.NewCursorPage(items, limit, "occurredAt", "desc", func(item map[string]any) pagination.Cursor {
		id, _ := item["_cursorId"].(int64)
		return pagination.Cursor{ID: id, Dir: "desc"}
	})
	for _, item := range page.Data {
		delete(item, "_cursorId")
	}
	return httpx.OK(w, page)
}
