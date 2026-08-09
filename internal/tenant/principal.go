// Package tenant defines the authenticated principal and the tenant/scope
// context every request carries.
//
// Constitution §9: the tenant is derived server-side from the authenticated
// user and is never read from the request body or from a client-supplied
// header. A Principal is constructed only by the authentication middleware.
package tenant

import (
	"context"
	"net/http"
	"slices"

	"github.com/ceserve/courier-os/internal/platform/apierr"
)

type ctxKey int

const principalKey ctxKey = iota

// Principal is the authenticated actor for one request.
//
// It is immutable once built. Handlers read it; nothing mutates it.
type Principal struct {
	UserID       int64
	UserPublicID string
	Email        string
	FullName     string

	OrganizationID       int64
	OrganizationPublicID string
	OrganizationCode     string
	OrganizationCurrency string
	OrganizationTimezone string
	// OrganizationCountry is ISO-3166 alpha-2. It decides how a tax identifier
	// is validated and which label a market expects — a Nigerian operator must
	// not be asked for a GSTIN.
	OrganizationCountry   string
	OrganizationAWBPrefix string
	IsPlatformOrg         bool

	// IsSuperAdmin marks a platform operator. It permits acting inside another
	// tenant via the X-Organization-Context header, which is always audited.
	IsSuperAdmin bool
	// ImpersonatedOrg records that this request is operating on a tenant other
	// than the principal's own, so audit and logs can say so.
	ImpersonatedOrg bool

	SessionID       int64
	SessionPublicID string
	SessionChainID  string

	Roles       []string
	permissions map[string]struct{}

	// ScopedUnitIDs lists the operating units this principal's role grants name.
	ScopedUnitIDs       []int64
	ScopedUnitPublicIDs []string
	// HasUnscopedRole is true when at least one role is granted without an
	// operating unit, which lifts the operating-unit restriction entirely.
	HasUnscopedRole bool

	// CustomerIDs restricts a portal principal to its own customer records.
	// Empty for staff principals.
	CustomerIDs  []int64
	IsPortalUser bool

	// IsPartner marks a request authenticated by an API key rather than a user
	// session. A partner principal has no user row behind it, so UserID is zero
	// and every column referencing users(id) must go through ActorUserID.
	//
	// Partner authorization is by *scope*, held on the key and checked by the
	// partner transport. It is deliberately not expressed as permissions: a
	// partner integration must not be able to acquire a person's rights by
	// holding a credential.
	IsPartner      bool
	PartnerKeyID   int64
	PartnerKeyName string
}

// ActorUserID returns the acting user's id, or nil when the request was made by
// an API key rather than a person.
//
// Every column that references users(id) must use this rather than &p.UserID:
// a partner principal has no user row, and a pointer to zero would violate the
// foreign key at insert time.
func (p *Principal) ActorUserID() *int64 {
	if p == nil || p.UserID == 0 {
		return nil
	}
	id := p.UserID
	return &id
}

// NewPrincipal builds a Principal with an indexed permission set.
func NewPrincipal(p Principal, permissionCodes []string) *Principal {
	p.permissions = make(map[string]struct{}, len(permissionCodes))
	for _, c := range permissionCodes {
		p.permissions[c] = struct{}{}
	}
	return &p
}

// Permissions returns the permission codes held, sorted, for API responses.
func (p *Principal) Permissions() []string {
	out := make([]string, 0, len(p.permissions))
	for c := range p.permissions {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Can reports whether the principal holds a permission.
//
// SUPER_ADMIN is not special-cased here: the seed grants it every permission
// explicitly, so a permission introduced later is covered by data rather than
// by a bypass in code that could drift from the catalogue.
func (p *Principal) Can(permission string) bool {
	if p == nil {
		return false
	}
	_, ok := p.permissions[permission]
	return ok
}

// CanAny reports whether the principal holds at least one of the permissions.
func (p *Principal) CanAny(permissions ...string) bool {
	for _, perm := range permissions {
		if p.Can(perm) {
			return true
		}
	}
	return false
}

// HasRole reports whether the principal holds a role code.
func (p *Principal) HasRole(code string) bool {
	return p != nil && slices.Contains(p.Roles, code)
}

// Require returns a 403 unless the principal holds the permission.
func (p *Principal) Require(permission string) error {
	if p.Can(permission) {
		return nil
	}
	return apierr.Forbidden("You do not have permission to perform this action.").
		WithDetail("requiredPermission", permission)
}

// UnitScope returns the operating-unit IDs a listing must be restricted to, or
// nil when the principal may see the whole tenant.
//
// A principal sees everything when it holds an unscoped role, or when it holds
// an explicit override permission such as shipment.read_all. Otherwise it sees
// only the units named by its scoped grants — and a principal with no grants at
// all sees nothing, which is represented by a non-nil empty slice so callers
// cannot mistake it for "unrestricted".
func (p *Principal) UnitScope(overridePermissions ...string) []int64 {
	if p == nil {
		return []int64{}
	}
	if p.HasUnscopedRole {
		return nil
	}
	for _, perm := range overridePermissions {
		if p.Can(perm) {
			return nil
		}
	}
	if p.ScopedUnitIDs == nil {
		return []int64{}
	}
	return p.ScopedUnitIDs
}

// IsUnitInScope reports whether the principal may act at an operating unit.
func (p *Principal) IsUnitInScope(unitID int64) bool {
	if p == nil {
		return false
	}
	if p.HasUnscopedRole {
		return true
	}
	return slices.Contains(p.ScopedUnitIDs, unitID)
}

// RequireUnitInScope returns a 403 when the principal may not act at a unit.
func (p *Principal) RequireUnitInScope(unitID int64) error {
	if p.IsUnitInScope(unitID) {
		return nil
	}
	return apierr.Forbidden("You are not assigned to the selected operating unit.")
}

// CustomerScope returns the customer IDs a portal principal is limited to, or
// nil for staff principals who are not customer-restricted.
func (p *Principal) CustomerScope() []int64 {
	if p == nil || !p.IsPortalUser {
		return nil
	}
	if p.CustomerIDs == nil {
		return []int64{}
	}
	return p.CustomerIDs
}

// IsCustomerInScope reports whether a portal principal may access a customer.
func (p *Principal) IsCustomerInScope(customerID int64) bool {
	if p == nil {
		return false
	}
	if !p.IsPortalUser {
		return true
	}
	return slices.Contains(p.CustomerIDs, customerID)
}

// LogAttrs returns the safe subset of identity to attach to logs. Email and
// name are omitted: request logs must not accumulate personal data.
func (p *Principal) LogAttrs() (organization, user, session string) {
	if p == nil {
		return "", "", ""
	}
	return p.OrganizationPublicID, p.UserPublicID, p.SessionPublicID
}

// WithPrincipal stores the principal on a context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext returns the principal, or nil for anonymous requests.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}

// Require returns the principal or a 401. Handlers behind the authentication
// middleware can rely on it being present, but this keeps the failure explicit
// rather than a nil dereference if a route is ever mounted unprotected.
func Require(r *http.Request) (*Principal, error) {
	p := FromContext(r.Context())
	if p == nil {
		return nil, apierr.Unauthorized(apierr.CodeUnauthorized, "Authentication is required.")
	}
	return p, nil
}
