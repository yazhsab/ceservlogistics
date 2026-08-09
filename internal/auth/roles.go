package auth

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// RoleHandler exposes role and permission administration.
type RoleHandler struct{ svc *Service }

// NewRoleHandler builds the role administration handler.
func NewRoleHandler(svc *Service) *RoleHandler { return &RoleHandler{svc: svc} }

// Routes mounts role administration under /roles.
func (h *RoleHandler) Routes(r chi.Router) {
	r.With(RequirePermission("role.read")).Get("/", httpx.Wrap(h.list))
	r.With(RequirePermission("role.create")).Post("/", httpx.Wrap(h.create))
	r.With(RequirePermission("role.read")).Get("/{roleId}", httpx.Wrap(h.get))
	r.With(RequirePermission("role.update")).Patch("/{roleId}", httpx.Wrap(h.update))
	r.With(RequirePermission("role.update")).Put("/{roleId}/permissions", httpx.Wrap(h.setPermissions))
	r.With(RequirePermission("role.delete")).Delete("/{roleId}", httpx.Wrap(h.delete))
}

// PermissionRoutes mounts the read-only permission catalogue.
func (h *RoleHandler) PermissionRoutes(r chi.Router) {
	r.With(RequirePermission("permission.read")).Get("/", httpx.Wrap(h.listPermissions))
}

// RoleView is the API representation of a role.
type RoleView struct {
	ID              string   `json:"id"`
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	IsSystem        bool     `json:"isSystem"`
	ScopeRequired   bool     `json:"scopeRequired"`
	PermissionCount int64    `json:"permissionCount"`
	Permissions     []string `json:"permissions,omitempty"`
}

// PermissionView is the API representation of a permission.
type PermissionView struct {
	Code        string `json:"code"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Module      string `json:"module"`
	Description string `json:"description"`
}

type createRoleRequest struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	ScopeRequired bool     `json:"scopeRequired,omitempty"`
	Permissions   []string `json:"permissions,omitempty"`
}

type updateRoleRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	ScopeRequired *bool   `json:"scopeRequired,omitempty"`
}

type setPermissionsRequest struct {
	Permissions []string `json:"permissions"`
}

func (h *RoleHandler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListRolesForOrganization(r.Context(), &p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]RoleView, 0, len(rows))
	for _, role := range rows {
		out = append(out, RoleView{
			ID: role.PublicID, Code: role.Code, Name: role.Name, Description: role.Description,
			IsSystem: role.IsSystem, ScopeRequired: role.ScopeRequired, PermissionCount: role.PermissionCount,
		})
	}
	return httpx.OK(w, map[string]any{"data": out})
}

func (h *RoleHandler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rolePublicID, err := httpx.PathPublicID(r, "roleId", publicid.PrefixRole, "Role")
	if err != nil {
		return err
	}
	role, err := h.svc.q.GetRoleByPublicID(r.Context(), dbgen.GetRoleByPublicIDParams{
		PublicID: rolePublicID, OrganizationID: &p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Role")
		}
		return apierr.Internal(err)
	}
	perms, err := h.svc.q.ListRolePermissionCodes(r.Context(), role.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, RoleView{
		ID: role.PublicID, Code: role.Code, Name: role.Name, Description: role.Description,
		IsSystem: role.IsSystem, ScopeRequired: role.ScopeRequired,
		PermissionCount: int64(len(perms)), Permissions: perms,
	})
}

func (h *RoleHandler) create(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRoleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	description := v.Text("description", req.Description, 0, 500, false)
	if err := v.Err(); err != nil {
		return err
	}
	if err := h.checkGrantablePermissions(r, p, req.Permissions); err != nil {
		return err
	}

	var role dbgen.Role
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		var cErr error
		role, cErr = qtx.CreateRole(r.Context(), dbgen.CreateRoleParams{
			PublicID:       publicid.New(publicid.PrefixRole),
			OrganizationID: &p.OrganizationID,
			Code:           code,
			Name:           name,
			Description:    description,
			ScopeRequired:  req.ScopeRequired,
		})
		if cErr != nil {
			if database.IsUniqueViolation(cErr) {
				return apierr.Duplicate("A role with this code already exists.")
			}
			return apierr.Internal(fmt.Errorf("create role: %w", cErr))
		}
		if len(req.Permissions) > 0 {
			if _, sErr := qtx.SetRolePermissions(r.Context(), dbgen.SetRolePermissionsParams{
				RoleID: role.ID, PermissionCodes: req.Permissions,
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoleCreated, ResourceType: "role",
		ResourceID: &role.ID, ResourcePublicID: role.PublicID,
		After: map[string]any{"code": role.Code, "permissions": req.Permissions},
	}))
	return httpx.Created(w, "/api/v1/roles/"+role.PublicID, RoleView{
		ID: role.PublicID, Code: role.Code, Name: role.Name, Description: role.Description,
		IsSystem: false, ScopeRequired: role.ScopeRequired,
		PermissionCount: int64(len(req.Permissions)), Permissions: req.Permissions,
	})
}

func (h *RoleHandler) update(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	role, err := h.loadCustomRole(r, p)
	if err != nil {
		return err
	}
	var req updateRoleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	params := dbgen.UpdateRoleParams{ID: role.ID, OrganizationID: &p.OrganizationID}
	if req.Name != nil {
		name := v.Text("name", *req.Name, 2, 120, true)
		params.Name = &name
	}
	if req.Description != nil {
		desc := v.Text("description", *req.Description, 0, 500, false)
		params.Description = &desc
	}
	params.ScopeRequired = req.ScopeRequired
	if err := v.Err(); err != nil {
		return err
	}

	updated, err := h.svc.q.UpdateRole(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Role")
		}
		return apierr.Internal(err)
	}
	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoleUpdated, ResourceType: "role",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
		Before: map[string]any{"name": role.Name, "description": role.Description},
		After:  map[string]any{"name": updated.Name, "description": updated.Description},
	}))
	return httpx.OK(w, RoleView{
		ID: updated.PublicID, Code: updated.Code, Name: updated.Name,
		Description: updated.Description, ScopeRequired: updated.ScopeRequired,
	})
}

func (h *RoleHandler) setPermissions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	role, err := h.loadCustomRole(r, p)
	if err != nil {
		return err
	}
	var req setPermissionsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Permissions) > 200 {
		return apierr.Validation("Too many permissions supplied.", nil)
	}
	if err := h.checkGrantablePermissions(r, p, req.Permissions); err != nil {
		return err
	}

	before, err := h.svc.q.ListRolePermissionCodes(r.Context(), role.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		if cErr := qtx.ClearRolePermissions(r.Context(), role.ID); cErr != nil {
			return apierr.Internal(cErr)
		}
		if len(req.Permissions) > 0 {
			if _, sErr := qtx.SetRolePermissions(r.Context(), dbgen.SetRolePermissionsParams{
				RoleID: role.ID, PermissionCodes: req.Permissions,
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Everyone holding this role must have their cached authorization rebuilt,
	// otherwise a revoked permission would linger for up to one cache TTL.
	h.invalidateRoleHolders(r, role.ID)

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRolePermissionsChanged, ResourceType: "role",
		ResourceID: &role.ID, ResourcePublicID: role.PublicID,
		Before: map[string]any{"permissions": before},
		After:  map[string]any{"permissions": req.Permissions},
	}))
	after, err := h.svc.q.ListRolePermissionCodes(r.Context(), role.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, RoleView{
		ID: role.PublicID, Code: role.Code, Name: role.Name, Description: role.Description,
		ScopeRequired: role.ScopeRequired, PermissionCount: int64(len(after)), Permissions: after,
	})
}

func (h *RoleHandler) delete(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	role, err := h.loadCustomRole(r, p)
	if err != nil {
		return err
	}
	assigned, err := h.svc.q.CountRoleAssignments(r.Context(), role.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	if assigned > 0 {
		return apierr.Conflict(apierr.CodeResourceInUse,
			"This role is still assigned to users. Revoke the assignments before deleting it.").
			WithDetail("assignedUsers", assigned)
	}
	affected, err := h.svc.q.DeleteRole(r.Context(), dbgen.DeleteRoleParams{
		ID: role.ID, OrganizationID: &p.OrganizationID,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	if affected == 0 {
		return apierr.NotFound("Role")
	}
	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoleDeleted, ResourceType: "role",
		ResourceID: &role.ID, ResourcePublicID: role.PublicID,
		Before: map[string]any{"code": role.Code, "name": role.Name},
	}))
	return httpx.NoContent(w)
}

func (h *RoleHandler) listPermissions(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.svc.q.ListPermissions(r.Context())
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]PermissionView, 0, len(rows))
	for _, p := range rows {
		out = append(out, PermissionView{
			Code: p.Code, Resource: p.Resource, Action: p.Action,
			Module: p.Module, Description: p.Description,
		})
	}
	return httpx.OK(w, map[string]any{"data": out})
}

// loadCustomRole fetches a role and refuses to mutate a system role.
func (h *RoleHandler) loadCustomRole(r *http.Request, p *tenant.Principal) (dbgen.Role, error) {
	rolePublicID, err := httpx.PathPublicID(r, "roleId", publicid.PrefixRole, "Role")
	if err != nil {
		return dbgen.Role{}, err
	}
	role, err := h.svc.q.GetRoleByPublicID(r.Context(), dbgen.GetRoleByPublicIDParams{
		PublicID: rolePublicID, OrganizationID: &p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return dbgen.Role{}, apierr.NotFound("Role")
		}
		return dbgen.Role{}, apierr.Internal(err)
	}
	if role.IsSystem {
		return dbgen.Role{}, apierr.Conflict(apierr.CodeImmutableResource,
			"System roles are shared across tenants and cannot be modified. Create a custom role instead.")
	}
	return role, nil
}

// checkGrantablePermissions rejects unknown codes and blocks privilege
// escalation: an administrator may only put permissions into a custom role that
// they themselves hold.
func (h *RoleHandler) checkGrantablePermissions(r *http.Request, p *tenant.Principal, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	catalog, err := h.svc.q.ListPermissions(r.Context())
	if err != nil {
		return apierr.Internal(err)
	}
	known := make(map[string]struct{}, len(catalog))
	for _, c := range catalog {
		known[c.Code] = struct{}{}
	}
	var unknown, notHeld []string
	for _, code := range codes {
		if _, ok := known[code]; !ok {
			unknown = append(unknown, code)
			continue
		}
		if !p.IsSuperAdmin && !p.Can(code) {
			notHeld = append(notHeld, code)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return apierr.Validation("One or more permissions do not exist.",
			map[string]any{"unknownPermissions": unknown})
	}
	if len(notHeld) > 0 {
		slices.Sort(notHeld)
		return apierr.Forbidden("You cannot grant permissions that you do not hold yourself.").
			WithDetail("deniedPermissions", notHeld)
	}
	return nil
}

// invalidateRoleHolders clears cached authorization for every user holding a
// role whose permission set just changed.
func (h *RoleHandler) invalidateRoleHolders(r *http.Request, roleID int64) {
	userIDs, err := h.svc.q.ListUserIDsWithRole(r.Context(), roleID)
	if err != nil {
		h.svc.log.Warn("failed to enumerate role holders for cache invalidation")
		return
	}
	for _, id := range userIDs {
		h.svc.invalidateUserSessionCache(r.Context(), id)
	}
}
