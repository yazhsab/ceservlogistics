package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// TestPermissionsGateEveryMutation walks the role matrix: a franchise operator
// can book but must not be able to reconfigure the network, price cards or
// other people's accounts.
func TestPermissionsGateEveryMutation(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "RBAC1"})

	_, _, operator := env.NewUser(t, tn.OrgID, "operator@rbac1.test",
		"FRANCHISE_OPERATOR", &tn.OriginBranchID)

	denied := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create operating unit", "POST", "/api/v1/network/operating-units", map[string]any{
			"code": "NEW", "name": "New", "unitType": "COMPANY_BRANCH",
			"addressLine1": "1 Road", "pincode": tn.OriginPincode,
		}},
		{"create courier service", "POST", "/api/v1/courier-services", map[string]any{
			"code": "NEW", "name": "New", "mode": "AIR",
			"maxWeightGrams": 1000, "slaTransitHours": 24,
		}},
		{"create rate card", "POST", "/api/v1/rate-cards", map[string]any{
			"code": "NEW", "name": "New", "scope": "RETAIL",
		}},
		{"create user", "POST", "/api/v1/users", map[string]any{
			"email": "x@y.test", "fullName": "X", "password": "Passw0rd!Passw0rd",
		}},
		{"read audit trail", "GET", "/api/v1/audit-events", nil},
		{"update organization", "PATCH", "/api/v1/organization", map[string]any{"name": "Renamed"}},
		{"declare a closure", "POST", "/api/v1/serviceability/closures", map[string]any{
			"operatingUnitCode": tn.OriginBranch, "closureType": "FULL",
			"reasonCode": "WEATHER", "reason": "attempted without permission",
			"startsAt": "2026-01-01T00:00:00Z", "endsAt": "2026-01-02T00:00:00Z",
		}},
		{"cancel a shipment", "POST", "/api/v1/shipments/shp_01ARZ3NDEKTSV4RRFFQ69G5FAV/cancel",
			map[string]any{"reason": "attempted without permission"}},
	}
	for _, tc := range denied {
		t.Run("denied: "+tc.name, func(t *testing.T) {
			resp := env.Do(t, tc.method, tc.path, operator, tc.body)
			if resp.Status != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", resp.Status, resp.Raw)
			}
			if resp.ErrorCode() != "FORBIDDEN" {
				t.Errorf("expected FORBIDDEN, got %q", resp.ErrorCode())
			}
		})
	}

	allowed := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"quote a price", "POST", "/api/v1/pricing/quote", map[string]any{
			"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
			"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
			"packages": []map[string]any{{"actualWeightGrams": 500}},
		}},
		{"check serviceability", "POST", "/api/v1/serviceability/check", map[string]any{
			"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
			"serviceCode": tn.ServiceCode,
		}},
		{"book a shipment", "POST", "/api/v1/shipments", tn.BookingBody(nil)},
		{"list customers", "GET", "/api/v1/customers", nil},
	}
	for _, tc := range allowed {
		t.Run("allowed: "+tc.name, func(t *testing.T) {
			resp := env.Do(t, tc.method, tc.path, operator, tc.body)
			if resp.Status >= 400 {
				t.Fatalf("expected success, got %d: %s", resp.Status, resp.Raw)
			}
		})
	}
}

// TestOperatingUnitScopeLimitsVisibility proves that a branch-scoped principal
// sees only its own branch's shipments, and that a 404 (not 403) is returned so
// the endpoint reveals nothing about other branches.
func TestOperatingUnitScopeLimitsVisibility(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SCOPE"})

	// A second branch under the same hub, with its own manager.
	otherUnit := env.Do(t, "POST", "/api/v1/network/operating-units", tn.AdminAccessTok, map[string]any{
		"code": "SCOPE-OTHER", "name": "Other Branch", "unitType": "COMPANY_BRANCH",
		"addressLine1": "7 Other Road", "pincode": tn.OriginPincode,
	})
	if otherUnit.Status != http.StatusCreated {
		t.Fatalf("create other unit failed: %d %s", otherUnit.Status, otherUnit.Raw)
	}
	var otherUnitID int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT id FROM operating_units WHERE organization_id = $1 AND code = 'SCOPE-OTHER'`,
		tn.OrgID).Scan(&otherUnitID); err != nil {
		t.Fatalf("read other unit: %v", err)
	}
	_, _, otherManager := env.NewUser(t, tn.OrgID, "manager@scope-other.test",
		"BRANCH_MANAGER", &otherUnitID)
	_, _, originManager := env.NewUser(t, tn.OrgID, "manager@scope-origin.test",
		"BRANCH_MANAGER", &tn.OriginBranchID)

	// The origin-branch manager books; the shipment belongs to their branch.
	booked := env.Do(t, "POST", "/api/v1/shipments", originManager, tn.BookingBody(nil))
	if booked.Status != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", booked.Status, booked.Raw)
	}
	shipmentID, _ := booked.Body["id"].(string)

	// The other branch's manager must not see it.
	if resp := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID, otherManager, nil); resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404 for an out-of-scope shipment, got %d: %s", resp.Status, resp.Raw)
	}
	list := env.Do(t, "GET", "/api/v1/shipments", otherManager, nil)
	if data, _ := list.Body["data"].([]any); len(data) != 0 {
		t.Fatalf("an out-of-scope branch manager must see no shipments, got %d", len(data))
	}

	// The owning manager sees it, and so does the unscoped organization admin.
	if resp := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID, originManager, nil); resp.Status != http.StatusOK {
		t.Fatalf("the owning branch manager must see the shipment, got %d", resp.Status)
	}
	if resp := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID, tn.AdminAccessTok, nil); resp.Status != http.StatusOK {
		t.Fatalf("an organization admin must see every shipment, got %d", resp.Status)
	}
}

// TestPrivilegeEscalationIsBlocked proves an administrator cannot grant
// themselves — or anyone else — capabilities they do not hold.
func TestPrivilegeEscalationIsBlocked(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "ESCAL"})

	// A tenant admin cannot mint a platform operator.
	created := env.Do(t, "POST", "/api/v1/users", tn.AdminAccessTok, map[string]any{
		"email": "wannabe@escal.test", "fullName": "Wannabe",
		"password": "Passw0rd!Passw0rd",
		"roles":    []map[string]any{{"roleCode": "SUPER_ADMIN"}},
	})
	if created.Status != http.StatusForbidden {
		t.Fatalf("expected 403 when granting SUPER_ADMIN, got %d: %s", created.Status, created.Raw)
	}

	// is_super_admin is not an accepted field at all.
	sneaky := env.Do(t, "POST", "/api/v1/users", tn.AdminAccessTok, map[string]any{
		"email": "sneaky@escal.test", "fullName": "Sneaky",
		"password": "Passw0rd!Passw0rd", "isSuperAdmin": true,
	})
	if sneaky.Status != http.StatusUnprocessableEntity && sneaky.Status != http.StatusBadRequest {
		t.Fatalf("expected the unknown isSuperAdmin field to be rejected, got %d: %s",
			sneaky.Status, sneaky.Raw)
	}

	// A custom role may not carry a permission the creator lacks. A finance
	// manager has no operating_unit.manage.
	_, _, finance := env.NewUser(t, tn.OrgID, "finance@escal.test", "FINANCE_MANAGER", nil)
	role := env.Do(t, "POST", "/api/v1/roles", finance, map[string]any{
		"code": "SNEAKY", "name": "Sneaky role",
		"permissions": []string{"operating_unit.manage"},
	})
	if role.Status != http.StatusForbidden {
		t.Fatalf("expected 403 when granting an unheld permission, got %d: %s", role.Status, role.Raw)
	}
}

// TestSystemRolesAreImmutable proves the shared catalogue cannot be edited by a
// tenant, which would change behaviour for every other tenant.
func TestSystemRolesAreImmutable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SYSROLE"})

	roles := env.Do(t, "GET", "/api/v1/roles", tn.AdminAccessTok, nil)
	if roles.Status != http.StatusOK {
		t.Fatalf("list roles failed: %d %s", roles.Status, roles.Raw)
	}
	var orgAdminID string
	for _, raw := range roles.Body["data"].([]any) {
		role, _ := raw.(map[string]any)
		if role["code"] == "ORG_ADMIN" {
			orgAdminID, _ = role["id"].(string)
			if role["isSystem"] != true {
				t.Error("ORG_ADMIN should be flagged as a system role")
			}
		}
	}
	if orgAdminID == "" {
		t.Fatal("ORG_ADMIN was not listed")
	}

	for _, probe := range []struct {
		method string
		path   string
		body   any
	}{
		{"PATCH", "/api/v1/roles/" + orgAdminID, map[string]any{"name": "Hijacked"}},
		{"PUT", "/api/v1/roles/" + orgAdminID + "/permissions", map[string]any{"permissions": []string{}}},
		{"DELETE", "/api/v1/roles/" + orgAdminID, nil},
	} {
		resp := env.Do(t, probe.method, probe.path, tn.AdminAccessTok, probe.body)
		if resp.Status != http.StatusConflict {
			t.Errorf("%s %s: expected 409 for a system role, got %d: %s",
				probe.method, probe.path, resp.Status, resp.Raw)
		}
	}
}

// TestDeactivatedUserLosesAccessImmediately proves the session cache does not
// delay a revocation.
func TestDeactivatedUserLosesAccessImmediately(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "DEACT"})

	_, userPublicID, token := env.NewUser(t, tn.OrgID, "victim@deact.test",
		"BRANCH_MANAGER", &tn.OriginBranchID)
	if resp := env.Do(t, "GET", "/api/v1/auth/me", token, nil); resp.Status != http.StatusOK {
		t.Fatalf("expected the new user to authenticate, got %d", resp.Status)
	}

	deactivate := env.Do(t, "POST", "/api/v1/users/"+userPublicID+"/status", tn.AdminAccessTok,
		map[string]any{"status": "INACTIVE", "reason": "Left the company"})
	if deactivate.Status != http.StatusOK {
		t.Fatalf("deactivation failed: %d %s", deactivate.Status, deactivate.Raw)
	}

	after := env.Do(t, "GET", "/api/v1/auth/me", token, nil)
	if after.Status != http.StatusUnauthorized {
		t.Fatalf("a deactivated user must lose access immediately, got %d: %s", after.Status, after.Raw)
	}
	if env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": "victim@deact.test", "password": "UserPassw0rd!x",
	}).Status == http.StatusOK {
		t.Error("a deactivated user must not be able to sign in again")
	}
}

// TestLastAdministratorCannotBeRemoved prevents a tenant locking itself out.
func TestLastAdministratorCannotBeRemoved(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "LASTADM"})

	adminPublicID := ""
	users := env.Do(t, "GET", "/api/v1/users", tn.AdminAccessTok, nil)
	for _, raw := range users.Body["data"].([]any) {
		user, _ := raw.(map[string]any)
		if user["email"] == tn.AdminEmail {
			adminPublicID, _ = user["id"].(string)
		}
	}
	if adminPublicID == "" {
		t.Fatal("could not find the admin user")
	}

	// Deactivating yourself is refused outright.
	resp := env.Do(t, "POST", "/api/v1/users/"+adminPublicID+"/status", tn.AdminAccessTok,
		map[string]any{"status": "INACTIVE", "reason": "self deactivation attempt"})
	if resp.Status != http.StatusConflict {
		t.Fatalf("expected 409 for self-deactivation, got %d: %s", resp.Status, resp.Raw)
	}
}

// TestPermissionCatalogueIsComplete guards the seeded catalogue against drift.
func TestPermissionCatalogueIsComplete(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PERMCAT"})

	resp := env.Do(t, "GET", "/api/v1/permissions", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("list permissions failed: %d %s", resp.Status, resp.Raw)
	}
	present := map[string]bool{}
	for _, raw := range resp.Body["data"].([]any) {
		p, _ := raw.(map[string]any)
		code, _ := p["code"].(string)
		present[code] = true
		if p["module"] == "" || p["description"] == "" {
			t.Errorf("permission %s is missing module or description", code)
		}
	}
	// Every permission the router enforces must exist in the catalogue,
	// otherwise a route would be permanently unreachable.
	for _, required := range []string{
		"shipment.create", "shipment.read", "shipment.read_all", "shipment.cancel", "shipment.label",
		"pricing.quote", "rate_card.manage", "rate_card.activate", "tax_rule.manage",
		"serviceability.check", "route.manage", "routing.debug", "closure.manage",
		"operating_unit.manage", "franchise.manage", "zone.manage", "pincode.manage",
		"customer.create", "customer_credit.manage", "user.assign_role", "audit.read",
	} {
		if !present[required] {
			t.Errorf("permission %q is enforced by a route but missing from the catalogue", required)
		}
	}
}
