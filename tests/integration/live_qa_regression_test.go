package integration

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

func TestLoginCarriesSamePortalSubjectAndOrganizationAsRestoredSession(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "LOGINQA"})
	portalUser(t, env, tn, "qa-customer@example.test", tn.CustomerID)
	franchiseUser(t, env, tn, "QAFRN", tn.OriginBranchID)

	for _, tc := range []struct {
		name, email, password string
		customer, franchise   bool
	}{
		{"customer", "qa-customer@example.test", "UserPassw0rd!x", true, false},
		{"franchise", "qafrn-owner@example.com", "UserPassw0rd!x", false, true},
		{"staff", tn.AdminEmail, tn.AdminPassword, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			login := env.Do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": tc.email, "password": tc.password})
			if login.Status != http.StatusOK {
				t.Fatalf("login: %d %s", login.Status, login.Raw)
			}
			profile := login.Body["user"].(map[string]any)
			token := login.Body["tokens"].(map[string]any)["accessToken"].(string)
			me := env.Do(t, http.MethodGet, "/api/v1/auth/me", token, nil)
			if me.Status != http.StatusOK {
				t.Fatalf("me: %d %s", me.Status, me.Raw)
			}
			for _, field := range []string{"portal", "organization", "roles", "permissions", "operatingUnitIds", "hasOrganizationWideAccess"} {
				if !reflect.DeepEqual(profile[field], me.Body[field]) {
					t.Errorf("%s differs between login and restored session: %#v vs %#v", field, profile[field], me.Body[field])
				}
			}
			subject := profile["portal"].(map[string]any)
			if subject["isCustomerUser"] != tc.customer {
				t.Errorf("customer binding: %#v", subject)
			}
			if (subject["franchise"] != nil) != tc.franchise {
				t.Errorf("franchise binding: %#v", subject)
			}
		})
	}
}

func TestUserListIncludesActualAssignmentsAndEmptyArraysWithoutCrossTenantUsers(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "USERQA"})
	other := env.NewTenant(t, geo, harness.TenantOptions{Code: "OTHERQA"})
	_, branchID, _ := env.NewUser(t, tn.OrgID, "qa-branch@example.test", "BRANCH_MANAGER", &tn.OriginBranchID)
	empty := env.Do(t, http.MethodPost, "/api/v1/users", tn.AdminAccessTok, map[string]any{
		"email": "qa-none@example.test", "fullName": "QA No access", "password": "UserPassw0rd!x", "mustChangePassword": false,
	})
	if empty.Status != http.StatusCreated {
		t.Fatalf("create unassigned: %d %s", empty.Status, empty.Raw)
	}
	resp := env.Do(t, http.MethodGet, "/api/v1/users?limit=100", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("list: %d %s", resp.Status, resp.Raw)
	}
	seen := map[string]bool{}
	for _, row := range resp.Body["data"].([]any) {
		item := row.(map[string]any)
		id := item["id"].(string)
		seen[id] = true
		if item["email"] == other.AdminEmail {
			t.Fatal("cross-tenant user leaked")
		}
		roles, ok := item["roles"].([]any)
		if !ok {
			t.Fatalf("roles must be an explicit array for %s: %#v", id, item["roles"])
		}
		detail := env.Do(t, http.MethodGet, "/api/v1/users/"+id, tn.AdminAccessTok, nil)
		if detail.Status != http.StatusOK || !reflect.DeepEqual(roles, detail.Body["roles"]) {
			t.Errorf("list assignments differ from detail for %s", id)
		}
		if id == branchID {
			if len(roles) != 1 {
				t.Fatalf("branch assignment count: %d", len(roles))
			}
			assignment := roles[0].(map[string]any)
			if assignment["roleCode"] != "BRANCH_MANAGER" || assignment["operatingUnit"] == nil {
				t.Fatalf("missing scoped assignment: %#v", assignment)
			}
		}
		if id == empty.Body["id"] && len(roles) != 0 {
			t.Fatal("unassigned user has invented roles")
		}
	}
	if !seen[branchID] || !seen[empty.Body["id"].(string)] {
		t.Fatal("expected users absent from list")
	}
}
