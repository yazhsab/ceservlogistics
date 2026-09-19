package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler exposes the authentication endpoints.
type Handler struct {
	svc *Service
	cfg config.AuthConfig
	// exposeResetToken is enabled only outside production so that development
	// and integration tests can complete a reset without an email transport.
	// The production configuration keeps it false and the token is delivered
	// exclusively by the notification worker in Release 4.
	exposeResetToken bool
}

// NewHandler builds the authentication handler.
func NewHandler(svc *Service, cfg config.AuthConfig, isProduction bool) *Handler {
	return &Handler{svc: svc, cfg: cfg, exposeResetToken: !isProduction}
}

// ---- DTOs ------------------------------------------------------------------

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type logoutRequest struct {
	RefreshToken string `json:"refreshToken,omitempty"`
	AllDevices   bool   `json:"allDevices,omitempty"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// UserProfile is the authenticated user's own view of themselves.
type UserProfile struct {
	ID                        string          `json:"id"`
	Email                     string          `json:"email"`
	FullName                  string          `json:"fullName"`
	Status                    string          `json:"status"`
	IsSuperAdmin              bool            `json:"isSuperAdmin"`
	MustChangePassword        bool            `json:"mustChangePassword"`
	LastLoginAt               *time.Time      `json:"lastLoginAt,omitempty"`
	Organization              OrganizationRef `json:"organization"`
	Roles                     []string        `json:"roles"`
	Permissions               []string        `json:"permissions"`
	OperatingUnits            []string        `json:"operatingUnitIds"`
	HasOrganizationWideAccess bool            `json:"hasOrganizationWideAccess"`

	// Portal is the session's *subject* binding: which customer accounts or
	// franchise this login acts for.
	//
	// Without it a portal client has to infer "which franchise am I" from a
	// list of operating units, which is backend authorization logic running in
	// a browser. Roles and permissions say what somebody may do; this says who
	// they are.
	Portal PortalSubject `json:"portal"`
}

// PortalSubject binds a session to the accounts it represents.
type PortalSubject struct {
	// IsCustomerUser is true when the login is linked to customer accounts and
	// may use /api/v1/portal/customer. A staff login is false even if its role
	// happens to carry portal.customer.
	IsCustomerUser bool `json:"isCustomerUser"`
	// Customers are the accounts a customer login may act for. Empty for staff.
	Customers []CustomerRef `json:"customers"`
	// Franchise is the one this login's operating units belong to, when there
	// is exactly one. Null for head office, and null when the units span
	// several franchises — which is a configuration error the franchise portal
	// reports rather than resolving arbitrarily.
	Franchise *FranchiseRef `json:"franchise"`
}

// CustomerRef identifies a customer account a portal login represents.
type CustomerRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// FranchiseRef identifies the franchise behind a session.
type FranchiseRef struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	UnitCode string `json:"operatingUnitCode"`
}

// OrganizationRef is the tenant summary embedded in the profile.
type OrganizationRef struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Currency  string `json:"currency"`
	Timezone  string `json:"timezone"`
	AWBPrefix string `json:"awbPrefix"`
}

type loginResponse struct {
	Tokens TokenPair   `json:"tokens"`
	User   UserProfile `json:"user"`
}

// ---- routes ----------------------------------------------------------------

// Routes mounts the public authentication endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/login", httpx.Wrap(h.login))
	r.Post("/refresh", httpx.Wrap(h.refresh))
	r.Post("/forgot-password", httpx.Wrap(h.forgotPassword))
	r.Post("/reset-password", httpx.Wrap(h.resetPassword))
}

// AuthenticatedRoutes mounts endpoints that require a valid access token.
func (h *Handler) AuthenticatedRoutes(r chi.Router) {
	r.Post("/logout", httpx.Wrap(h.logout))
	r.Post("/change-password", httpx.Wrap(h.changePassword))
	r.Get("/me", httpx.Wrap(h.me))
	r.Get("/sessions", httpx.Wrap(h.listSessions))
}

// ---- handlers --------------------------------------------------------------

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	email := v.Email("email", req.Email)
	if req.Password == "" {
		v.Add("password", "This field is required.")
	}
	if err := v.Err(); err != nil {
		return err
	}

	pair, user, err := h.svc.Login(r.Context(), LoginInput{
		Email:     email,
		Password:  req.Password,
		UserAgent: r.UserAgent(),
		ClientIP:  httpx.ClientIP(r.Context()),
	})
	if err != nil {
		return err
	}

	snapshot, err := h.svc.resolveSession(r.Context(), pair.SessionID)
	if err != nil {
		return apierr.Internal(err)
	}
	org, err := h.svc.q.GetOrganizationByID(r.Context(), user.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}

	return httpx.OK(w, loginResponse{
		Tokens: *pair,
		User: UserProfile{
			ID:                 user.PublicID,
			Email:              user.Email,
			FullName:           user.FullName,
			Status:             user.Status,
			IsSuperAdmin:       user.IsSuperAdmin,
			MustChangePassword: user.MustChangePassword,
			LastLoginAt:        user.LastLoginAt,
			Organization: OrganizationRef{
				ID: org.PublicID, Code: org.Code, Name: org.Name,
				Currency: org.Currency, Timezone: org.Timezone, AWBPrefix: org.AwbPrefix,
			},
			Roles:                     snapshot.Roles,
			Permissions:               snapshot.Permissions,
			OperatingUnits:            snapshot.ScopedUnitPublicIDs,
			HasOrganizationWideAccess: snapshot.HasUnscopedRole,
			Portal:                    h.portalSubject(r.Context(), snapshot.toPrincipal(pair.SessionID)),
		},
	})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return apierr.Validation("refreshToken is required.", nil)
	}
	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken, r.UserAgent(), httpx.ClientIP(r.Context()))
	if err != nil {
		return err
	}
	return httpx.OK(w, pair)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req logoutRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
	}
	if err := h.svc.Logout(r.Context(), p.SessionPublicID, req.RefreshToken, req.AllDevices); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	var req forgotPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	email := v.Email("email", req.Email)
	if err := v.Err(); err != nil {
		return err
	}

	reset, err := h.svc.RequestPasswordReset(r.Context(), email, httpx.ClientIP(r.Context()))
	if err != nil {
		return err
	}

	// The response is identical whether or not the address matched, so this
	// endpoint cannot be used to discover which addresses have accounts.
	body := map[string]any{
		"message": "If an account exists for that address, a password reset link has been sent.",
	}
	if h.exposeResetToken && reset != nil {
		body["resetToken"] = reset.Token
		body["expiresAt"] = reset.ExpiresAt
		body["note"] = "resetToken is returned only outside production, for local development and tests."
	}
	return httpx.OK(w, body)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) error {
	var req resetPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	v.Required("token", req.Token)
	v.Password("newPassword", req.NewPassword, h.cfg.PasswordMinLength)
	if err := v.Err(); err != nil {
		return err
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		return err
	}
	return httpx.OK(w, map[string]string{
		"message": "Your password has been updated. Please sign in with your new password.",
	})
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req changePasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	v.Required("currentPassword", req.CurrentPassword)
	v.Password("newPassword", req.NewPassword, h.cfg.PasswordMinLength)
	if err := v.Err(); err != nil {
		return err
	}
	if err := h.svc.ChangePassword(r.Context(), p, req.CurrentPassword, req.NewPassword); err != nil {
		return err
	}
	return httpx.OK(w, map[string]string{
		"message": "Your password has been updated. All other sessions have been signed out.",
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	user, err := h.svc.q.GetUserByID(r.Context(), p.UserID)
	if err != nil {
		return apierr.Internal(err)
	}
	org, err := h.svc.q.GetOrganizationByID(r.Context(), p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, UserProfile{
		ID:                 p.UserPublicID,
		Email:              p.Email,
		FullName:           p.FullName,
		Status:             user.Status,
		IsSuperAdmin:       p.IsSuperAdmin,
		MustChangePassword: user.MustChangePassword,
		LastLoginAt:        user.LastLoginAt,
		Organization: OrganizationRef{
			ID: p.OrganizationPublicID, Code: p.OrganizationCode, Name: org.Name,
			Currency: p.OrganizationCurrency, Timezone: p.OrganizationTimezone,
			AWBPrefix: p.OrganizationAWBPrefix,
		},
		Roles:                     p.Roles,
		Permissions:               p.Permissions(),
		OperatingUnits:            p.ScopedUnitPublicIDs,
		HasOrganizationWideAccess: p.HasUnscopedRole,
		Portal:                    h.portalSubject(r.Context(), p),
	})
}

// portalSubject resolves who this session represents.
//
// Best-effort: a lookup failure degrades to an empty subject rather than
// failing the profile call. Somebody who cannot load their own profile cannot
// use the application at all, and the portal binding is not what they logged in
// for.
func (h *Handler) portalSubject(ctx context.Context, p *tenant.Principal) PortalSubject {
	out := PortalSubject{
		IsCustomerUser: p.IsPortalUser,
		Customers:      []CustomerRef{},
	}

	if p.IsPortalUser {
		rows, err := h.svc.q.ListCustomersForPortalUser(ctx,
			dbgen.ListCustomersForPortalUserParams{
				OrganizationID: p.OrganizationID, UserID: p.UserID,
			})
		if err != nil {
			h.svc.log.Warn("could not resolve a portal user's customers",
				"userId", p.UserPublicID, "error", err)
		}
		for _, c := range rows {
			out.Customers = append(out.Customers,
				CustomerRef{ID: c.PublicID, Code: c.Code, Name: c.Name})
		}
	}

	// A franchise subject only exists for a principal scoped to units. Head
	// office covers the network and is not "a franchise".
	scope := p.UnitScope()
	if len(scope) == 0 {
		return out
	}
	rows, err := h.svc.q.ListFranchisesForUnits(ctx, dbgen.ListFranchisesForUnitsParams{
		OrganizationID: p.OrganizationID, UnitIds: scope,
	})
	if err != nil {
		h.svc.log.Warn("could not resolve a session's franchise",
			"userId", p.UserPublicID, "error", err)
		return out
	}
	// Exactly one, or none. Units spanning two franchises is a configuration
	// error, and picking whichever sorted first would hide it.
	if len(rows) == 1 {
		out.Franchise = &FranchiseRef{
			ID: rows[0].PublicID, Code: rows[0].Code,
			Name: rows[0].Name, UnitCode: rows[0].UnitCode,
		}
	}
	return out
}

// sessionSummary is the safe view of a session; no token material is exposed.
type sessionSummary struct {
	ID         string     `json:"id"`
	IssuedAt   time.Time  `json:"issuedAt"`
	LastUsedAt time.Time  `json:"lastUsedAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	UserAgent  string     `json:"userAgent,omitempty"`
	ClientIP   string     `json:"clientIp,omitempty"`
	Current    bool       `json:"current"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListActiveSessionsForUser(r.Context(), p.UserID)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]sessionSummary, 0, len(rows))
	for _, s := range rows {
		summary := sessionSummary{
			ID: s.PublicID, IssuedAt: s.IssuedAt, LastUsedAt: s.LastUsedAt,
			ExpiresAt: s.ExpiresAt, Current: s.PublicID == p.SessionPublicID,
			RevokedAt: s.RevokedAt,
		}
		if s.UserAgent != nil {
			summary.UserAgent = *s.UserAgent
		}
		if s.ClientIp != nil {
			summary.ClientIP = *s.ClientIp
		}
		out = append(out, summary)
	}
	return httpx.OK(w, map[string]any{"data": out})
}
