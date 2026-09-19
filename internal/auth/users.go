package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// UserHandler exposes user administration.
type UserHandler struct {
	svc *Service
}

// NewUserHandler builds the user administration handler.
func NewUserHandler(svc *Service) *UserHandler { return &UserHandler{svc: svc} }

// Routes mounts user administration under /users.
func (h *UserHandler) Routes(r chi.Router) {
	r.With(RequirePermission("user.read")).Get("/", httpx.Wrap(h.list))
	r.With(RequirePermission("user.create")).Post("/", httpx.Wrap(h.create))
	r.With(RequirePermission("user.read")).Get("/{userId}", httpx.Wrap(h.get))
	r.With(RequirePermission("user.update")).Patch("/{userId}", httpx.Wrap(h.update))
	r.With(RequirePermission("user.deactivate")).Post("/{userId}/status", httpx.Wrap(h.setStatus))
	r.With(RequirePermission("user.assign_role")).Get("/{userId}/roles", httpx.Wrap(h.listRoles))
	r.With(RequirePermission("user.assign_role")).Post("/{userId}/roles", httpx.Wrap(h.assignRole))
	r.With(RequirePermission("user.assign_role")).Delete("/{userId}/roles", httpx.Wrap(h.revokeRole))
	r.With(RequirePermission("user.revoke_session")).Post("/{userId}/revoke-sessions", httpx.Wrap(h.revokeSessions))
}

// ---- DTOs ------------------------------------------------------------------

// UserSummary is the list/detail representation of a user.
type UserSummary struct {
	ID                 string            `json:"id"`
	Email              string            `json:"email"`
	FullName           string            `json:"fullName"`
	Phone              string            `json:"phone,omitempty"`
	Status             string            `json:"status"`
	IsSuperAdmin       bool              `json:"isSuperAdmin"`
	MustChangePassword bool              `json:"mustChangePassword"`
	LastLoginAt        *time.Time        `json:"lastLoginAt,omitempty"`
	LockedUntil        *time.Time        `json:"lockedUntil,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	Roles              *[]RoleAssignment `json:"roles,omitempty"`
}

// RoleAssignment describes one grant of a role to a user.
type RoleAssignment struct {
	RoleID        string    `json:"roleId"`
	RoleCode      string    `json:"roleCode"`
	RoleName      string    `json:"roleName"`
	OperatingUnit *UnitRef  `json:"operatingUnit,omitempty"`
	GrantedAt     time.Time `json:"grantedAt"`
	GrantedBy     string    `json:"grantedBy,omitempty"`
}

// UnitRef identifies an operating unit in a nested response.
type UnitRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type createUserRequest struct {
	Email              string             `json:"email"`
	FullName           string             `json:"fullName"`
	Phone              string             `json:"phone,omitempty"`
	Password           string             `json:"password"`
	MustChangePassword *bool              `json:"mustChangePassword,omitempty"`
	Roles              []roleGrantRequest `json:"roles,omitempty"`
}

type roleGrantRequest struct {
	RoleCode        string `json:"roleCode"`
	OperatingUnitID string `json:"operatingUnitId,omitempty"`
}

type updateUserRequest struct {
	FullName *string `json:"fullName,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Email    *string `json:"email,omitempty"`
}

type setStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// ---- handlers --------------------------------------------------------------

func (h *UserHandler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 10_000)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE", "LOCKED"})
	if err != nil {
		return err
	}

	params := dbgen.ListUsersParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit),
		RowOffset:      int32((page - 1) * limit),
	}
	if status != "" {
		params.Status = &status
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	if rc := httpx.Query(r, "roleCode"); rc != "" {
		params.RoleCode = &rc
	}

	rows, err := h.svc.q.ListUsers(r.Context(), params)
	if err != nil {
		return apierr.Internal(fmt.Errorf("list users: %w", err))
	}
	userIDs := make([]int64, 0, len(rows))
	for _, u := range rows {
		userIDs = append(userIDs, u.ID)
	}
	assignments, err := h.svc.q.ListRoleAssignmentsForUsers(r.Context(), dbgen.ListRoleAssignmentsForUsersParams{
		OrganizationID: p.OrganizationID, UserIds: userIDs,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("list user assignments: %w", err))
	}
	byUser := make(map[int64][]dbgen.ListUserRoleAssignmentsRow)
	for _, a := range assignments {
		byUser[a.UserID] = append(byUser[a.UserID], dbgen.ListUserRoleAssignmentsRow{
			GrantedAt: a.GrantedAt, RolePublicID: a.RolePublicID,
			RoleCode: a.RoleCode, RoleName: a.RoleName,
			OperatingUnitPublicID: a.OperatingUnitPublicID, OperatingUnitCode: a.OperatingUnitCode,
			OperatingUnitName: a.OperatingUnitName, OperatingUnitType: a.OperatingUnitType,
			GrantedByName: a.GrantedByName,
		})
	}
	var total int64
	items := make([]UserSummary, 0, len(rows))
	for _, u := range rows {
		total = u.TotalCount
		summary := toUserSummary(dbgen.User{
			PublicID: u.PublicID, Email: u.Email, FullName: u.FullName, Phone: u.Phone,
			Status: u.Status, IsSuperAdmin: u.IsSuperAdmin, MustChangePassword: u.MustChangePassword,
			LastLoginAt: u.LastLoginAt, LockedUntil: u.LockedUntil,
			CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		})
		roles := toRoleAssignments(byUser[u.ID])
		summary.Roles = &roles
		items = append(items, summary)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "fullName", "asc"))
}

func (h *UserHandler) create(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	email := v.Email("email", req.Email)
	fullName := v.Text("fullName", req.FullName, 1, 160, true)
	phone := v.Phone("phone", req.Phone, false)
	v.Password("password", req.Password, h.svc.cfg.PasswordMinLength)
	for i, g := range req.Roles {
		// Existence is verified against the role catalogue in grantRole; this only
		// rejects obviously malformed input before a database round trip.
		v.Code(fmt.Sprintf("roles[%d].roleCode", i), g.RoleCode)
		if g.OperatingUnitID != "" {
			v.PublicID(fmt.Sprintf("roles[%d].operatingUnitId", i), g.OperatingUnitID, publicid.PrefixOperatingUnit, false)
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	hash, err := h.svc.hasher.Hash(req.Password)
	if err != nil {
		return apierr.Internal(err)
	}
	mustChange := true
	if req.MustChangePassword != nil {
		mustChange = *req.MustChangePassword
	}

	var created dbgen.User
	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.svc.q.WithTx(tx)
		var cErr error
		created, cErr = qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
			PublicID:       publicid.New(publicid.PrefixUser),
			OrganizationID: p.OrganizationID,
			Email:          email,
			PasswordHash:   hash,
			FullName:       fullName,
			Phone:          optional(phone),
			Status:         "ACTIVE",
			// is_super_admin is never settable through the API. Platform
			// operators are provisioned by the bootstrap path only, so a tenant
			// administrator cannot escalate to cross-tenant access.
			IsSuperAdmin:       false,
			MustChangePassword: mustChange,
			CreatedBy:          &p.UserID,
		})
		if cErr != nil {
			if database.IsUniqueViolation(cErr, "users_email_unique_per_org") {
				return apierr.Duplicate("A user with this email address already exists in your organization.")
			}
			return apierr.Internal(fmt.Errorf("create user: %w", cErr))
		}
		for _, g := range req.Roles {
			if gErr := h.grantRole(r.Context(), qtx, p, created.ID, g); gErr != nil {
				return gErr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionUserCreated, ResourceType: "user",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		After: map[string]any{"email": created.Email, "fullName": created.FullName, "roles": req.Roles},
	}))
	return httpx.Created(w, "/api/v1/users/"+created.PublicID, toUserSummary(created))
}

func (h *UserHandler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}
	summary := toUserSummary(user)
	assignments, err := h.svc.q.ListUserRoleAssignments(r.Context(), user.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	roles := toRoleAssignments(assignments)
	summary.Roles = &roles
	return httpx.OK(w, summary)
}

func (h *UserHandler) update(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	var req updateUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	existing, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}

	v := validate.New()
	params := dbgen.UpdateUserParams{ID: existing.ID, OrganizationID: p.OrganizationID}
	if req.FullName != nil {
		name := v.Text("fullName", *req.FullName, 1, 160, true)
		params.FullName = &name
	}
	if req.Phone != nil {
		phone := v.Phone("phone", *req.Phone, false)
		params.Phone = &phone
	}
	if req.Email != nil {
		email := v.Email("email", *req.Email)
		params.Email = &email
	}
	if err := v.Err(); err != nil {
		return err
	}

	updated, err := h.svc.q.UpdateUser(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "users_email_unique_per_org") {
			return apierr.Duplicate("A user with this email address already exists in your organization.")
		}
		return apierr.Internal(err)
	}
	// An email change alters the login identity, so cached sessions must be
	// rebuilt rather than serving the old address in audit records.
	h.svc.invalidateUserSessionCache(r.Context(), updated.ID)

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionUserUpdated, ResourceType: "user",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
		Before: map[string]any{"email": existing.Email, "fullName": existing.FullName, "phone": existing.Phone},
		After:  map[string]any{"email": updated.Email, "fullName": updated.FullName, "phone": updated.Phone},
	}))
	return httpx.OK(w, toUserSummary(updated))
}

func (h *UserHandler) setStatus(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	var req setStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	status := v.Enum("status", req.Status, []string{"ACTIVE", "INACTIVE"}, true)
	if err := v.Err(); err != nil {
		return err
	}

	existing, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}
	if existing.ID == p.UserID && status == "INACTIVE" {
		return apierr.Conflict(apierr.CodeConflict, "You cannot deactivate your own account.")
	}
	if status == "INACTIVE" {
		// Refuse to remove the last active administrator, which would leave the
		// tenant unmanageable and require operator intervention to recover.
		admins, cErr := h.svc.q.CountOrganizationAdmins(r.Context(), p.OrganizationID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		assignments, aErr := h.svc.q.ListUserRoleAssignments(r.Context(), existing.ID)
		if aErr != nil {
			return apierr.Internal(aErr)
		}
		if admins <= 1 && hasAdminRole(assignments) {
			return apierr.Conflict(apierr.CodeConflict,
				"This is the last active administrator; assign another administrator before deactivating.")
		}
	}

	updated, err := h.svc.q.SetUserStatus(r.Context(), dbgen.SetUserStatusParams{
		ID: existing.ID, OrganizationID: p.OrganizationID, Status: status,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	if status == "INACTIVE" {
		reason := "USER_DEACTIVATED"
		if _, rErr := h.svc.q.RevokeUserSessions(r.Context(), dbgen.RevokeUserSessionsParams{
			UserID: existing.ID, Reason: &reason,
		}); rErr != nil {
			return apierr.Internal(rErr)
		}
	}
	h.svc.invalidateUserSessionCache(r.Context(), existing.ID)

	action := audit.ActionUserActivated
	if status == "INACTIVE" {
		action = audit.ActionUserDeactivated
	}
	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: action, ResourceType: "user",
		ResourceID: &updated.ID, ResourcePublicID: updated.PublicID, Reason: req.Reason,
		Before: map[string]any{"status": existing.Status},
		After:  map[string]any{"status": updated.Status},
	}))
	return httpx.OK(w, toUserSummary(updated))
}

func (h *UserHandler) listRoles(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}
	assignments, err := h.svc.q.ListUserRoleAssignments(r.Context(), user.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, map[string]any{"data": toRoleAssignments(assignments)})
}

func (h *UserHandler) assignRole(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	var req roleGrantRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}

	err = h.svc.db.InTx(r.Context(), func(tx pgx.Tx) error {
		return h.grantRole(r.Context(), h.svc.q.WithTx(tx), p, user.ID, req)
	})
	if err != nil {
		return err
	}
	h.svc.invalidateUserSessionCache(r.Context(), user.ID)

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoleAssigned, ResourceType: "user",
		ResourceID: &user.ID, ResourcePublicID: user.PublicID,
		After: map[string]any{"roleCode": req.RoleCode, "operatingUnitId": req.OperatingUnitID},
	}))
	assignments, err := h.svc.q.ListUserRoleAssignments(r.Context(), user.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, map[string]any{"data": toRoleAssignments(assignments)})
}

// grantRole validates and inserts one role assignment.
//
// Two escalation controls live here: SUPER_ADMIN can only be granted by a
// platform operator, and a scoped role must name an operating unit that exists
// inside the caller's tenant.
func (h *UserHandler) grantRole(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, userID int64, g roleGrantRequest,
) error {
	if g.RoleCode == "SUPER_ADMIN" && !p.IsSuperAdmin {
		return apierr.Forbidden("Only platform operators may grant the SUPER_ADMIN role.")
	}
	role, err := q.GetRoleByCodeForOrganization(ctx, dbgen.GetRoleByCodeForOrganizationParams{
		Code: g.RoleCode, OrganizationID: &p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Validation("The requested role does not exist.",
				map[string]any{"roleCode": g.RoleCode})
		}
		return apierr.Internal(err)
	}

	var unitID *int64
	if g.OperatingUnitID != "" {
		unit, uErr := q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: g.OperatingUnitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			if database.IsNoRows(uErr) {
				return apierr.Validation("The operating unit does not exist in your organization.",
					map[string]any{"operatingUnitId": g.OperatingUnitID})
			}
			return apierr.Internal(uErr)
		}
		unitID = &unit.ID
	}
	if role.ScopeRequired && unitID == nil {
		return apierr.Validation("This role must be granted at a specific operating unit.",
			map[string]any{"roleCode": g.RoleCode})
	}

	_, err = q.AssignUserRole(ctx, dbgen.AssignUserRoleParams{
		OrganizationID:  p.OrganizationID,
		UserID:          userID,
		RoleID:          role.ID,
		OperatingUnitID: unitID,
		GrantedBy:       &p.UserID,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return apierr.Duplicate("This role is already assigned to the user at that scope.")
		}
		return apierr.Internal(fmt.Errorf("assign role: %w", err))
	}
	return nil
}

func (h *UserHandler) revokeRole(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	var req roleGrantRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}
	role, err := h.svc.q.GetRoleByCodeForOrganization(r.Context(), dbgen.GetRoleByCodeForOrganizationParams{
		Code: req.RoleCode, OrganizationID: &p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Role")
		}
		return apierr.Internal(err)
	}

	params := dbgen.RevokeUserRoleParams{
		UserID: user.ID, RoleID: role.ID, OrganizationID: p.OrganizationID,
	}
	if req.OperatingUnitID != "" {
		unit, uErr := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: req.OperatingUnitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			return apierr.NotFound("Operating unit")
		}
		params.OperatingUnitID = &unit.ID
	}

	affected, err := h.svc.q.RevokeUserRole(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	if affected == 0 {
		return apierr.NotFound("Role assignment")
	}
	h.svc.invalidateUserSessionCache(r.Context(), user.ID)

	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoleRevoked, ResourceType: "user",
		ResourceID: &user.ID, ResourcePublicID: user.PublicID,
		Before: map[string]any{"roleCode": req.RoleCode, "operatingUnitId": req.OperatingUnitID},
	}))
	return httpx.NoContent(w)
}

func (h *UserHandler) revokeSessions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	userPublicID, err := httpx.PathPublicID(r, "userId", publicid.PrefixUser, "User")
	if err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
		PublicID: userPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("User")
		}
		return apierr.Internal(err)
	}
	reason := "ADMIN_REVOKED"
	count, err := h.svc.q.RevokeUserSessions(r.Context(), dbgen.RevokeUserSessionsParams{
		UserID: user.ID, Reason: &reason,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	h.svc.invalidateUserSessionCache(r.Context(), user.ID)
	h.svc.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionSessionRevoked, ResourceType: "user",
		ResourceID: &user.ID, ResourcePublicID: user.PublicID,
		Metadata: map[string]any{"revokedSessions": count},
	}))
	return httpx.OK(w, map[string]any{"revokedSessions": count})
}

// ---- mapping ---------------------------------------------------------------

func toUserSummary(u dbgen.User) UserSummary {
	s := UserSummary{
		ID: u.PublicID, Email: u.Email, FullName: u.FullName, Status: u.Status,
		IsSuperAdmin: u.IsSuperAdmin, MustChangePassword: u.MustChangePassword,
		LastLoginAt: u.LastLoginAt, LockedUntil: u.LockedUntil,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
	if u.Phone != nil {
		s.Phone = *u.Phone
	}
	return s
}

func toRoleAssignments(rows []dbgen.ListUserRoleAssignmentsRow) []RoleAssignment {
	out := make([]RoleAssignment, 0, len(rows))
	for _, a := range rows {
		item := RoleAssignment{
			RoleID: a.RolePublicID, RoleCode: a.RoleCode, RoleName: a.RoleName, GrantedAt: a.GrantedAt,
		}
		if a.OperatingUnitPublicID != nil {
			item.OperatingUnit = &UnitRef{
				ID: *a.OperatingUnitPublicID, Code: derefString(a.OperatingUnitCode),
				Name: derefString(a.OperatingUnitName), Type: derefString(a.OperatingUnitType),
			}
		}
		if a.GrantedByName != nil {
			item.GrantedBy = *a.GrantedByName
		}
		out = append(out, item)
	}
	return out
}

func hasAdminRole(rows []dbgen.ListUserRoleAssignmentsRow) bool {
	for _, a := range rows {
		if a.RoleCode == "ORG_ADMIN" || a.RoleCode == "SUPER_ADMIN" {
			return true
		}
	}
	return false
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
