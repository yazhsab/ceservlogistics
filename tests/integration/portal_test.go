package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// M27, M28, M29. Each of these surfaces exists to show one audience their own
// slice, so almost every test here is the same question asked three ways: can
// the asker see something that is not theirs?

// portalUser creates a CUSTOMER-role login linked to a customer account.
//
// The link is what makes them a portal user; the role alone would leave them a
// staff account with a misleading name.
func portalUser(t *testing.T, env *harness.Env, tn *harness.Tenant, email string, customerID int64) string {
	t.Helper()
	userID, _, _ := env.NewUser(t, tn.OrgID, email, "CUSTOMER", nil)
	env.MustExec(t,
		`INSERT INTO customer_users (organization_id, customer_id, user_id, portal_role, status)
		 VALUES ($1, $2, $3, 'OWNER', 'ACTIVE')`,
		tn.OrgID, customerID, userID)
	// The principal snapshot is built at login, so the link must exist first.
	return env.Login(t, email, "UserPassw0rd!x")
}

// ---------------------------------------------------------------------------
// M27 Customer portal
// ---------------------------------------------------------------------------

func TestCustomerPortalShowsOnlyItsOwnAccount(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// A second customer in the same tenant, with its own shipment.
	var otherID int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`INSERT INTO customers (public_id, organization_id, code, customer_type, name,
		                        phone, status)
		 VALUES (gen_seed_public_id('cus'), $1, 'OTHERCO', 'BUSINESS', 'Other Co',
		         '9000000001', 'ACTIVE')
		 RETURNING id`, tn.OrgID).Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	// Two shipments for the harness customer, one for the other.
	bookOne(t, env, tn)
	bookOne(t, env, tn)
	other := env.Do(t, http.MethodPost, "/api/v1/shipments", tn.AdminAccessTok,
		tn.BookingBody(map[string]any{"customerId": customerPublicID(t, env, otherID)}),
		[2]string{"Idempotency-Key", harness.RandomKey()})
	if other.Status != http.StatusCreated {
		t.Fatalf("other customer booking: %d %s", other.Status, other.Raw)
	}
	otherShipment, _ := other.Body["id"].(string)

	token := portalUser(t, env, tn, "portal@example.com", tn.CustomerID)

	summary := env.Do(t, http.MethodGet, "/api/v1/portal/customer/summary", token, nil)
	if summary.Status != http.StatusOK {
		t.Fatalf("summary: %d %s", summary.Status, summary.Raw)
	}
	if got := int64(summary.Body["total"].(float64)); got != 2 {
		t.Fatalf("the portal reports %d shipments, want 2 — the third belongs to another account", got)
	}

	list := env.Do(t, http.MethodGet, "/api/v1/portal/customer/shipments", token, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("shipments: %d %s", list.Status, list.Raw)
	}
	items, _ := list.Body["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("the portal lists %d shipments, want 2", len(items))
	}

	// The other account's parcel is a 404, not a 403: a portal that
	// distinguished them would confirm the identifier exists.
	direct := env.Do(t, http.MethodGet,
		"/api/v1/portal/customer/shipments/"+otherShipment, token, nil)
	if direct.Status != http.StatusNotFound {
		t.Fatalf("reading another account's shipment returned %d, want 404: %s",
			direct.Status, direct.Raw)
	}
	track := env.Do(t, http.MethodGet,
		"/api/v1/portal/customer/shipments/"+otherShipment+"/track", token, nil)
	if track.Status != http.StatusNotFound {
		t.Fatalf("tracking another account's shipment returned %d, want 404", track.Status)
	}
}

func TestCustomerPortalRefusesAStaffLogin(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// The org admin holds every permission, including portal.customer if a role
	// grant were sloppy. It must still be refused: CustomerScope returns nil for
	// a staff principal meaning *unrestricted*, and treating that as "everything"
	// would hand one screen the tenant's whole book.
	env.MustExec(t,
		`INSERT INTO role_permissions (role_id, permission_id)
		 SELECT r.id, p.id FROM roles r, permissions p
		  WHERE r.code = 'ORG_ADMIN' AND p.code = 'portal.customer'
		 ON CONFLICT DO NOTHING`)
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)

	for _, path := range []string{
		"/api/v1/portal/customer/summary",
		"/api/v1/portal/customer/accounts",
		"/api/v1/portal/customer/shipments",
		"/api/v1/portal/customer/invoices",
	} {
		resp := env.Do(t, http.MethodGet, path, token, nil)
		if resp.Status != http.StatusForbidden {
			t.Errorf("GET %s as staff returned %d, want 403: %s", path, resp.Status, resp.Raw)
		}
	}
}

func TestCustomerPortalIsTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})
	bookOne(t, env, beta)

	token := portalUser(t, env, alpha, "alpha-portal@example.com", alpha.CustomerID)
	summary := env.Do(t, http.MethodGet, "/api/v1/portal/customer/summary", token, nil)
	if summary.Status != http.StatusOK {
		t.Fatalf("summary: %d %s", summary.Status, summary.Raw)
	}
	if got := int64(summary.Body["total"].(float64)); got != 0 {
		t.Fatalf("alpha's customer sees %d of beta's shipments", got)
	}
}

func TestCustomerPortalAccountsExposeCreditWithoutInternalIds(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	token := portalUser(t, env, tn, "credit@example.com", tn.CustomerID)

	resp := env.Do(t, http.MethodGet, "/api/v1/portal/customer/accounts", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("accounts: %d %s", resp.Status, resp.Raw)
	}
	items, _ := resp.Body["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("the login is linked to %d accounts, want 1", len(items))
	}
	account, _ := items[0].(map[string]any)
	id, _ := account["id"].(string)
	if !strings.HasPrefix(id, "cus_") {
		t.Fatalf("account id %q is not a public identifier", id)
	}
	// An internal key must never appear in a customer-facing payload (§11).
	raw, _ := json.Marshal(account)
	if strings.Contains(string(raw), `"customerId":`) {
		t.Fatalf("the account payload carries an internal id: %s", raw)
	}
}

// ---------------------------------------------------------------------------
// M28 Franchise portal
// ---------------------------------------------------------------------------

// franchiseUser creates a franchise and an operator assigned to its unit.
func franchiseUser(
	t *testing.T, env *harness.Env, tn *harness.Tenant, code string, unitID int64,
) (franchiseID int64, token string) {
	t.Helper()
	if err := env.DB.Pool.QueryRow(context.Background(),
		`INSERT INTO franchises (public_id, organization_id, code, name, operating_unit_id,
		                         owner_name, owner_phone, status)
		 VALUES (gen_seed_public_id('frn'), $1, $2, $3, $4, 'Owner', '9000000002', 'ACTIVE')
		 RETURNING id`, tn.OrgID, code, code+" Franchise", unitID).Scan(&franchiseID); err != nil {
		t.Fatal(err)
	}
	_, _, token = env.NewUser(t, tn.OrgID,
		strings.ToLower(code)+"-owner@example.com", "FRANCHISE_OWNER", &unitID)
	return franchiseID, token
}

func TestFranchisePortalSeesOnlyItsOwnUnit(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// Everything the harness books lands at the origin branch. A franchise that
	// owns the *destination* branch must therefore see none of it.
	_, token := franchiseUser(t, env, tn, "FRNDEST", tn.DestBranchID)
	for i := 0; i < 3; i++ {
		bookOne(t, env, tn)
	}

	resp := env.Do(t, http.MethodGet, "/api/v1/portal/franchise/summary", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("summary: %d %s", resp.Status, resp.Raw)
	}
	if got := int64(resp.Body["booked"].(float64)); got != 0 {
		t.Fatalf("the destination franchise sees %d origin bookings", got)
	}

	// The default listing is ORIGIN — what this franchise booked — so it agrees
	// with the summary above it. Two numbers on one screen that disagree is
	// worse than either being absent.
	list := env.Do(t, http.MethodGet, "/api/v1/portal/franchise/shipments", token, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("shipments: %d %s", list.Status, list.Raw)
	}
	if role, _ := list.Body["role"].(string); role != "ORIGIN" {
		t.Fatalf("the default relationship is %q, want ORIGIN", role)
	}
	items, _ := list.Body["data"].([]any)
	if len(items) != 0 {
		t.Fatalf("the destination franchise lists %d shipments it did not book", len(items))
	}

	// Asked for its inbound workload, it does see them — and every row says
	// why it is there.
	inbound := env.Do(t, http.MethodGet,
		"/api/v1/portal/franchise/shipments?role=DESTINATION", token, nil)
	if inbound.Status != http.StatusOK {
		t.Fatalf("inbound: %d %s", inbound.Status, inbound.Raw)
	}
	inboundItems, _ := inbound.Body["data"].([]any)
	if len(inboundItems) != 3 {
		t.Fatalf("the destination franchise sees %d inbound parcels, want 3", len(inboundItems))
	}
	first, _ := inboundItems[0].(map[string]any)
	if role, _ := first["role"].(string); role != "DESTINATION" {
		t.Fatalf("an inbound row is labelled %q", role)
	}
}

func TestFranchisePortalCannotReadAnotherFranchise(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	_, tokenA := franchiseUser(t, env, tn, "FRNAAA", tn.OriginBranchID)
	otherID, _ := franchiseUser(t, env, tn, "FRNBBB", tn.DestBranchID)

	var otherPublic string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM franchises WHERE id = $1`, otherID).Scan(&otherPublic); err != nil {
		t.Fatal(err)
	}

	// Naming somebody else's franchise is a 404, not their numbers.
	resp := env.Do(t, http.MethodGet,
		"/api/v1/portal/franchise/summary?franchiseId="+otherPublic, tokenA, nil)
	if resp.Status != http.StatusNotFound {
		t.Fatalf("reading another franchise returned %d, want 404: %s", resp.Status, resp.Raw)
	}

	// Its settlements likewise.
	settle := env.Do(t, http.MethodGet,
		"/api/v1/portal/franchise/settlements?franchiseId="+otherPublic, tokenA, nil)
	if settle.Status != http.StatusNotFound {
		t.Fatalf("reading another franchise's settlements returned %d", settle.Status)
	}
}

func TestFranchisePortalResolvesTheCallersOwnFranchise(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	_, token := franchiseUser(t, env, tn, "FRNOWN", tn.OriginBranchID)
	for i := 0; i < 2; i++ {
		bookOne(t, env, tn)
	}

	// No franchiseId supplied: the surface derives it from the operator's unit
	// grant rather than asking them who they are.
	resp := env.Do(t, http.MethodGet, "/api/v1/portal/franchise/summary", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("summary: %d %s", resp.Status, resp.Raw)
	}
	f, _ := resp.Body["franchise"].(map[string]any)
	if code, _ := f["code"].(string); code != "FRNOWN" {
		t.Fatalf("resolved franchise %q, want FRNOWN", code)
	}
	if got := int64(resp.Body["booked"].(float64)); got != 2 {
		t.Fatalf("the origin franchise sees %d bookings, want 2", got)
	}
	// Commission and COD are on the same payload, because a franchise owner's
	// first question is what they are owed.
	for _, key := range []string{
		"commissionEarnedMinor", "commissionOutstandingMinor",
		"codInCustodyMinor", "deliveryRateBasisPoints",
	} {
		if _, ok := resp.Body[key]; !ok {
			t.Errorf("the franchise summary omits %s", key)
		}
	}
}

// ---------------------------------------------------------------------------
// M29 Console
// ---------------------------------------------------------------------------

func TestConsoleLookupIsCompact(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	awb := bookOne(t, env, tn)

	resp := env.Do(t, http.MethodGet, "/api/v1/console/lookup/"+awb, tn.AdminAccessTok, nil,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if resp.Status != http.StatusOK {
		t.Fatalf("lookup: %d %s", resp.Status, resp.Raw)
	}
	if got, _ := resp.Body["awb"].(string); got != awb {
		t.Fatalf("lookup returned %q, want %s", got, awb)
	}

	// The whole point of the surface: a scanner gets what it shows and nothing
	// else. A full shipment payload here would be roughly fifteen times larger
	// over the worst network in the platform.
	for _, absent := range []string{
		"charges", "packages", "addresses", "route", "customer",
		"allowedTransitions", "lineItems",
	} {
		if _, present := resp.Body[absent]; present {
			t.Errorf("the console payload carries %q, which a scanner does not show", absent)
		}
	}
	if len(resp.Raw) > 800 {
		t.Errorf("the console lookup payload is %d bytes; it is meant to be compact:\n%s",
			len(resp.Raw), resp.Raw)
	}
}

func TestConsoleIsTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})
	betaAWB := bookOne(t, env, beta)

	resp := env.Do(t, http.MethodGet, "/api/v1/console/lookup/"+betaAWB, alpha.AdminAccessTok, nil,
		[2]string{"X-Operating-Unit", alpha.OriginBranchPubID})
	if resp.Status != http.StatusNotFound {
		t.Fatalf("a cross-tenant barcode lookup returned %d, want 404: %s",
			resp.Status, resp.Raw)
	}
	if strings.Contains(resp.Raw, betaAWB) {
		t.Fatalf("the 404 echoed beta's AWB back: %s", resp.Raw)
	}
}

func TestConsoleQueueAndSummary(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	for i := 0; i < 3; i++ {
		bookOne(t, env, tn)
	}

	// A freshly booked parcel is in the booking unit's custody.
	summary := env.Do(t, http.MethodGet, "/api/v1/console/summary", tn.AdminAccessTok, nil,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if summary.Status != http.StatusOK {
		t.Fatalf("summary: %d %s", summary.Status, summary.Raw)
	}
	if got := int64(summary.Body["inCustody"].(float64)); got != 3 {
		t.Fatalf("the origin branch holds %d parcels, want 3", got)
	}

	queue := env.Do(t, http.MethodGet, "/api/v1/console/queue?limit=2", tn.AdminAccessTok, nil,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if queue.Status != http.StatusOK {
		t.Fatalf("queue: %d %s", queue.Status, queue.Raw)
	}
	items, _ := queue.Body["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("the queue returned %d items for limit=2", len(items))
	}
	// A bounded page must say how to get the next one.
	if _, ok := queue.Body["nextCursor"]; !ok {
		t.Error("a full page carries no nextCursor")
	}

	// A different facility holds none of them.
	elsewhere := env.Do(t, http.MethodGet, "/api/v1/console/summary", tn.AdminAccessTok, nil,
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})
	if got := int64(elsewhere.Body["inCustody"].(float64)); got != 0 {
		t.Fatalf("the destination branch reports %d parcels in custody", got)
	}
}

func TestConsoleRequiresTheFacilityAndThePermission(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// No facility header: the console has no idea where it is, and guessing
	// would show an operator somebody else's queue.
	resp := env.Do(t, http.MethodGet, "/api/v1/console/queue", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusBadRequest && resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a console request with no facility returned %d: %s", resp.Status, resp.Raw)
	}

	// A delivery agent *does* hold portal.console — the scanner surface is
	// exactly who it is for. A finance manager does not, and that is the
	// boundary worth pinning: the console shows operational custody, not
	// something a back-office role should be browsing.
	_, _, finance := env.NewUser(t, tn.OrgID, "console-finance@example.com",
		"FINANCE_MANAGER", nil)
	denied := env.Do(t, http.MethodGet, "/api/v1/console/summary", finance, nil,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if denied.Status != http.StatusForbidden {
		t.Fatalf("a finance manager reached the console: %d %s", denied.Status, denied.Raw)
	}
	if !strings.Contains(denied.Raw, "portal.console") {
		t.Errorf("the refusal does not name the missing permission: %s", denied.Raw)
	}
}

func TestConsoleCannotReadAnotherBranchesQueue(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	// A branch manager scoped to the destination branch, asking for the origin
	// branch's queue.
	_, _, token := env.NewUser(t, tn.OrgID, "dest-mgr@example.com",
		"BRANCH_MANAGER", &tn.DestBranchID)

	resp := env.Do(t, http.MethodGet, "/api/v1/console/queue", token, nil,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if resp.Status != http.StatusForbidden && resp.Status != http.StatusNotFound {
		t.Fatalf("a scoped manager read another branch's queue: %d %s", resp.Status, resp.Raw)
	}
}

func customerPublicID(t *testing.T, env *harness.Env, id int64) string {
	t.Helper()
	var publicID string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM customers WHERE id = $1`, id).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	return publicID
}
