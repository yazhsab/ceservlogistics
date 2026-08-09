package integration

import (
	"net/http"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// TestCrossTenantAccessIsImpossible is the Constitution §9 proof: organization A
// must never reach organization B's data, by any route.
//
// Every object type reachable by public id is probed with a valid token from
// the wrong tenant. A 404 (not 403) is required, so the endpoint cannot even
// confirm that the identifier exists elsewhere.
func TestCrossTenantAccessIsImpossible(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)

	tenantA := env.NewTenant(t, geo, harness.TenantOptions{Code: "ISOA"})
	tenantB := env.NewTenant(t, geo, harness.TenantOptions{Code: "ISOB"})

	// Tenant A books a shipment and creates a customer address.
	booking := env.Do(t, "POST", "/api/v1/shipments", tenantA.AdminAccessTok, tenantA.BookingBody(nil))
	if booking.Status != http.StatusCreated {
		t.Fatalf("tenant A booking failed: %d %s", booking.Status, booking.Raw)
	}
	shipmentID, _ := booking.Body["id"].(string)
	awb, _ := booking.Body["awb"].(string)

	addr := env.Do(t, "POST",
		"/api/v1/customers/"+tenantA.CustomerPublicID+"/addresses", tenantA.AdminAccessTok,
		map[string]any{
			"label": "Warehouse", "addressType": "PICKUP",
			"contactName": "Ops", "contactPhone": "+919800000009",
			"line1": "9 Warehouse Lane", "pincode": tenantA.OriginPincode,
		})
	if addr.Status != http.StatusCreated {
		t.Fatalf("tenant A address creation failed: %d %s", addr.Status, addr.Raw)
	}
	addressID, _ := addr.Body["id"].(string)

	probes := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"shipment detail", "GET", "/api/v1/shipments/" + shipmentID, nil},
		{"shipment events", "GET", "/api/v1/shipments/" + shipmentID + "/events", nil},
		{"shipment label", "GET", "/api/v1/shipments/" + shipmentID + "/label", nil},
		{"shipment cancel", "POST", "/api/v1/shipments/" + shipmentID + "/cancel",
			map[string]any{"reason": "cross tenant attempt"}},
		{"customer detail", "GET", "/api/v1/customers/" + tenantA.CustomerPublicID, nil},
		{"customer addresses", "GET", "/api/v1/customers/" + tenantA.CustomerPublicID + "/addresses", nil},
		{"customer address update", "PATCH",
			"/api/v1/customers/" + tenantA.CustomerPublicID + "/addresses/" + addressID,
			map[string]any{"label": "Hijacked"}},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			resp := env.Do(t, probe.method, probe.path, tenantB.AdminAccessTok, probe.body)
			if resp.Status != http.StatusNotFound {
				t.Fatalf("tenant B reached tenant A's %s: expected 404, got %d: %s",
					probe.name, resp.Status, resp.Raw)
			}
		})
	}

	// Listings must not leak either.
	t.Run("shipment list", func(t *testing.T) {
		resp := env.Do(t, "GET", "/api/v1/shipments", tenantB.AdminAccessTok, nil)
		if resp.Status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Status, resp.Raw)
		}
		data, _ := resp.Body["data"].([]any)
		if len(data) != 0 {
			t.Fatalf("tenant B's shipment list must be empty, got %d rows: %s", len(data), resp.Raw)
		}
	})
	t.Run("customer list", func(t *testing.T) {
		resp := env.Do(t, "GET", "/api/v1/customers", tenantB.AdminAccessTok, nil)
		data, _ := resp.Body["data"].([]any)
		for _, item := range data {
			row, _ := item.(map[string]any)
			if row["id"] == tenantA.CustomerPublicID {
				t.Fatal("tenant A's customer appeared in tenant B's list")
			}
		}
	})
	t.Run("search by awb", func(t *testing.T) {
		resp := env.Do(t, "GET", "/api/v1/shipments?search="+awb, tenantB.AdminAccessTok, nil)
		data, _ := resp.Body["data"].([]any)
		if len(data) != 0 {
			t.Fatal("searching tenant A's AWB from tenant B returned results")
		}
	})
}

// TestBookingRejectsForeignCustomer proves a client cannot book against another
// tenant's customer by supplying its public id.
func TestBookingRejectsForeignCustomer(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)

	tenantA := env.NewTenant(t, geo, harness.TenantOptions{Code: "FGNA"})
	tenantB := env.NewTenant(t, geo, harness.TenantOptions{Code: "FGNB"})

	body := tenantB.BookingBody(map[string]any{"customerId": tenantA.CustomerPublicID})
	resp := env.Do(t, "POST", "/api/v1/shipments", tenantB.AdminAccessTok, body)
	if resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404 when booking against a foreign customer, got %d: %s",
			resp.Status, resp.Raw)
	}
}

// TestOrganizationContextHeaderRequiresSuperAdmin proves the impersonation
// header cannot be used for lateral movement by an ordinary tenant admin.
func TestOrganizationContextHeaderRequiresSuperAdmin(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)

	tenantA := env.NewTenant(t, geo, harness.TenantOptions{Code: "IMPA"})
	tenantB := env.NewTenant(t, geo, harness.TenantOptions{Code: "IMPB"})

	resp := env.Do(t, "GET", "/api/v1/customers", tenantB.AdminAccessTok, nil,
		[2]string{"X-Organization-Context", tenantA.OrgPublicID})
	if resp.Status != http.StatusForbidden {
		t.Fatalf("expected 403 when a tenant admin supplies an organization context, got %d: %s",
			resp.Status, resp.Raw)
	}
}

// TestMassAssignmentIsRejected proves a client cannot smuggle server-owned
// fields into a request body.
func TestMassAssignmentIsRejected(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "MASS"})

	cases := map[string]map[string]any{
		"organizationId":   {"organizationId": 1},
		"awb":              {"awb": "HACK260808000001"},
		"status":           {"status": "DELIVERED"},
		"totalAmountMinor": {"totalAmountMinor": 0},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			body := tn.BookingBody(extra)
			resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
			if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
				t.Fatalf("expected the unknown field %q to be rejected, got %d: %s",
					name, resp.Status, resp.Raw)
			}
		})
	}
}
