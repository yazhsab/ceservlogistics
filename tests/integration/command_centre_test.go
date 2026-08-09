package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

// M30. The command centre's whole reason to exist is that it must answer these
// questions without scanning shipments, so the tests care about two things:
// that the rollup agrees with reality, and that a scoped operator cannot read
// the network's figures off it.

func commandTenant(t *testing.T, env *harness.Env) *harness.Tenant {
	t.Helper()
	return env.NewTenant(t, env.Geography(t), harness.TenantOptions{})
}

func overview(t *testing.T, env *harness.Env, token, query string) harness.Response {
	t.Helper()
	path := "/api/v1/command-centre"
	if query != "" {
		path += "?" + query
	}
	resp := env.Do(t, http.MethodGet, path, token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("command centre: %d %s", resp.Status, resp.Raw)
	}
	return resp
}

func section(t *testing.T, resp harness.Response, name string) map[string]any {
	t.Helper()
	s, ok := resp.Body[name].(map[string]any)
	if !ok {
		t.Fatalf("the response has no %q section: %s", name, resp.Raw)
	}
	return s
}

func num(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s is missing or not a number: %v", key, m[key])
	}
	return int64(v)
}

// ---------------------------------------------------------------------------
// The rollup must agree with the shipments it summarises
// ---------------------------------------------------------------------------

func TestRollupCountsEveryBooking(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	const n = 7
	for i := 0; i < n; i++ {
		bookOne(t, env, tn)
	}

	// The trigger runs in the booking transaction, so the rollup is already
	// correct — no sleep, no polling. That is the property being tested.
	var booked, revenue int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(booked_count),0), COALESCE(SUM(revenue_minor),0)
		   FROM shipment_daily_stats WHERE organization_id = $1`,
		tn.OrgID).Scan(&booked, &revenue); err != nil {
		t.Fatal(err)
	}
	if booked != n {
		t.Fatalf("rollup booked_count = %d, want %d", booked, n)
	}

	// And it agrees with the source it summarises.
	var actual, actualRevenue int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*), COALESCE(SUM(total_amount_minor),0)
		   FROM shipments WHERE organization_id = $1`,
		tn.OrgID).Scan(&actual, &actualRevenue); err != nil {
		t.Fatal(err)
	}
	if booked != actual {
		t.Fatalf("rollup says %d bookings, shipments says %d", booked, actual)
	}
	if revenue != actualRevenue {
		t.Fatalf("rollup revenue %d != shipments revenue %d", revenue, actualRevenue)
	}

	resp := overview(t, env, tn.AdminAccessTok, "")
	period := section(t, resp, "period")
	if got := num(t, period, "booked"); got != n {
		t.Fatalf("the API reports %d bookings, want %d", got, n)
	}
	if got := num(t, period, "revenueMinor"); got != actualRevenue {
		t.Fatalf("the API reports revenue %d, want %d", got, actualRevenue)
	}
}

func TestRollupFollowsAStatusChange(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	before := num(t, section(t, overview(t, env, tn.AdminAccessTok, ""), "period"), "cancelled")

	var shipmentID string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM shipments WHERE organization_id = $1`, tn.OrgID).
		Scan(&shipmentID); err != nil {
		t.Fatal(err)
	}
	resp := env.Do(t, http.MethodPost, "/api/v1/shipments/"+shipmentID+"/cancel",
		tn.AdminAccessTok, map[string]any{"reason": "customer changed their mind"})
	if resp.Status != http.StatusOK {
		t.Fatalf("cancel: %d %s", resp.Status, resp.Raw)
	}

	after := num(t, section(t, overview(t, env, tn.AdminAccessTok, ""), "period"), "cancelled")
	if after != before+1 {
		t.Fatalf("cancelled count went %d -> %d, want +1", before, after)
	}
}

func TestLiveBacklogExcludesTerminalStates(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)
	bookOne(t, env, tn)

	live := section(t, overview(t, env, tn.AdminAccessTok, ""), "live")
	if got := num(t, live, "total"); got != 2 {
		t.Fatalf("live backlog = %d, want 2", got)
	}

	// Cancelling removes it from the backlog — a cancelled parcel is nobody's
	// work — while the period total still records that it happened.
	var shipmentID string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id FROM shipments WHERE organization_id = $1 ORDER BY id LIMIT 1`,
		tn.OrgID).Scan(&shipmentID); err != nil {
		t.Fatal(err)
	}
	if r := env.Do(t, http.MethodPost, "/api/v1/shipments/"+shipmentID+"/cancel",
		tn.AdminAccessTok, map[string]any{"reason": "no longer required"}); r.Status != http.StatusOK {
		t.Fatalf("cancel: %d %s", r.Status, r.Raw)
	}

	resp := overview(t, env, tn.AdminAccessTok, "")
	if got := num(t, section(t, resp, "live"), "total"); got != 1 {
		t.Fatalf("live backlog after cancellation = %d, want 1", got)
	}
	if got := num(t, section(t, resp, "period"), "booked"); got != 2 {
		t.Fatalf("period booked = %d, want 2 — cancelling must not rewrite history", got)
	}
}

// ---------------------------------------------------------------------------
// Tenant and operating-unit isolation
// ---------------------------------------------------------------------------

func TestCommandCentreIsTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})

	for i := 0; i < 3; i++ {
		bookOne(t, env, beta)
	}

	period := section(t, overview(t, env, alpha.AdminAccessTok, ""), "period")
	if got := num(t, period, "booked"); got != 0 {
		t.Fatalf("alpha sees %d of beta's bookings", got)
	}
	live := section(t, overview(t, env, alpha.AdminAccessTok, ""), "live")
	if got := num(t, live, "total"); got != 0 {
		t.Fatalf("alpha sees %d of beta's backlog", got)
	}

	// Beta sees its own.
	betaPeriod := section(t, overview(t, env, beta.AdminAccessTok, ""), "period")
	if got := num(t, betaPeriod, "booked"); got != 3 {
		t.Fatalf("beta sees %d of its own bookings, want 3", got)
	}
}

func TestScopedOperatorCannotReadTheWholeNetwork(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	for i := 0; i < 4; i++ {
		bookOne(t, env, tn)
	}

	// A branch manager assigned to the destination branch. The bookings above
	// were all taken at the origin branch, so a correctly scoped view shows
	// them nothing — and, critically, not the network total.
	_, _, token := env.NewUser(t, tn.OrgID, "branch-mgr@example.com",
		"BRANCH_MANAGER", &tn.DestBranchID)

	resp := env.Do(t, http.MethodGet, "/api/v1/command-centre", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("command centre for a scoped user: %d %s", resp.Status, resp.Raw)
	}
	period := section(t, resp, "period")
	if got := num(t, period, "booked"); got != 0 {
		t.Fatalf("a destination-branch manager sees %d origin-branch bookings", got)
	}

	// And naming somebody else's unit is refused rather than answered.
	other := env.Do(t, http.MethodGet,
		"/api/v1/command-centre?unitId="+tn.OriginBranchPubID, token, nil)
	if other.Status != http.StatusForbidden {
		t.Fatalf("a scoped manager read another unit's figures: %d %s",
			other.Status, other.Raw)
	}
}

func TestCommandCentreRequiresThePermission(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// A delivery agent has no business reading network KPIs.
	_, _, token := env.NewUser(t, tn.OrgID, "agent@example.com",
		"DELIVERY_AGENT", &tn.DestBranchID)

	for _, path := range []string{
		"/api/v1/command-centre",
		"/api/v1/command-centre/trend",
		"/api/v1/command-centre/units",
		"/api/v1/command-centre/services",
		"/api/v1/command-centre/backlog",
		"/api/v1/command-centre/snapshots",
	} {
		resp := env.Do(t, http.MethodGet, path, token, nil)
		if resp.Status != http.StatusForbidden {
			t.Errorf("GET %s without command.read returned %d", path, resp.Status)
		}
		if !strings.Contains(resp.Raw, "command.read") {
			t.Errorf("GET %s does not name the missing permission: %s", path, resp.Raw)
		}
	}
}

// ---------------------------------------------------------------------------
// Bounds and honesty
// ---------------------------------------------------------------------------

func TestWindowIsBounded(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// A dashboard is not a reporting engine. An unbounded range would read the
	// whole rollup, which is exactly the cost this module exists to avoid.
	resp := env.Do(t, http.MethodGet,
		"/api/v1/command-centre?from=2000-01-01&to=2030-01-01", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
		t.Fatalf("a 30-year window was accepted: %d %s", resp.Status, resp.Raw)
	}

	// A backwards range is a client bug, not an empty result.
	backwards := env.Do(t, http.MethodGet,
		"/api/v1/command-centre?from=2026-08-01&to=2026-07-01", tn.AdminAccessTok, nil)
	if backwards.Status != http.StatusUnprocessableEntity && backwards.Status != http.StatusBadRequest {
		t.Fatalf("a backwards window was accepted: %d %s", backwards.Status, backwards.Raw)
	}
}

func TestResponseStatesWhichFiguresAreExact(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	// A dashboard that cannot tell an exact number from a sampled one will
	// present both as live, and somebody will make a decision on the wrong one.
	c := section(t, overview(t, env, tn.AdminAccessTok, ""), "consistency")
	for _, key := range []string{"periodTotals", "liveBacklog", "snapshots"} {
		if v, _ := c[key].(string); v == "" {
			t.Errorf("consistency.%s is not stated", key)
		}
	}
	if v, _ := c["periodTotals"].(string); !strings.Contains(v, "exact") {
		t.Errorf("the rollup is exact but is not described as such: %q", v)
	}
	if v, _ := c["snapshots"].(string); !strings.Contains(v, "capturedAt") {
		t.Errorf("the snapshot series does not point at its own age field: %q", v)
	}
}

func TestRatesAreIntegerBasisPoints(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	period := section(t, overview(t, env, tn.AdminAccessTok, ""), "period")
	// Nothing delivered yet, so the rate is zero rather than absent or NaN —
	// division by a zero denominator is the classic dashboard crash.
	if got := num(t, period, "deliveryRateBasisPoints"); got != 0 {
		t.Fatalf("delivery rate with no deliveries = %d, want 0", got)
	}

	// An empty tenant must not divide by zero either.
	empty := commandTenant(t, env)
	emptyPeriod := section(t, overview(t, env, empty.AdminAccessTok, ""), "period")
	if got := num(t, emptyPeriod, "deliveryRateBasisPoints"); got != 0 {
		t.Fatalf("delivery rate on an empty tenant = %d, want 0", got)
	}
	if got := num(t, emptyPeriod, "booked"); got != 0 {
		t.Fatalf("an empty tenant reports %d bookings", got)
	}
}

// ---------------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------------

func TestSnapshotCaptureAndAppendOnly(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)
	bookOne(t, env, tn)

	if err := env.App.Analytics.CaptureSnapshot(context.Background(), tn.OrgID); err != nil {
		t.Fatalf("capture: %v", err)
	}

	resp := env.Do(t, http.MethodGet, "/api/v1/command-centre/snapshots",
		tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("snapshots: %d %s", resp.Status, resp.Raw)
	}
	points, _ := resp.Body["data"].([]any)
	if len(points) != 1 {
		t.Fatalf("snapshots returned %d points, want 1", len(points))
	}
	point, _ := points[0].(map[string]any)
	if got := num(t, point, "inCustody"); got != 2 {
		t.Fatalf("snapshot inCustody = %d, want 2", got)
	}
	// The age of the reading must be on the reading, not implied by when it was
	// fetched.
	if ts, _ := point["capturedAt"].(string); ts == "" {
		t.Fatal("a snapshot point carries no capturedAt")
	}

	// History is history. A trend chart whose past can be edited is not evidence
	// of anything.
	if _, err := env.DB.Pool.Exec(context.Background(),
		`UPDATE operational_snapshots SET in_custody_count = 999 WHERE organization_id = $1`,
		tn.OrgID); err == nil {
		t.Fatal("an operational snapshot was edited")
	}
	if _, err := env.DB.Pool.Exec(context.Background(),
		`DELETE FROM operational_snapshots WHERE organization_id = $1`, tn.OrgID); err == nil {
		t.Fatal("an operational snapshot was deleted")
	}
}

func TestSnapshotSweepCoversEveryTenant(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})
	bookOne(t, env, alpha)
	bookOne(t, env, beta)

	n, err := env.App.Analytics.CaptureAll(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n < 2 {
		t.Fatalf("the sweep captured %d tenants, want at least 2", n)
	}

	for _, tn := range []*harness.Tenant{alpha, beta} {
		var count int
		if err := env.DB.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM operational_snapshots WHERE organization_id = $1`,
			tn.OrgID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("tenant %s has %d snapshots, want 1", tn.OrgCode, count)
		}
	}
}

func TestSnapshotRetention(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// Two readings, one backdated past the retention window. Inserted directly
	// because the guard blocks changing captured_at afterwards.
	env.MustExec(t,
		`INSERT INTO operational_snapshots (organization_id, captured_at, in_custody_count)
		 VALUES ($1, now() - interval '200 days', 5), ($1, now(), 7)`, tn.OrgID)

	purged, err := env.App.Analytics.PurgeSnapshots(context.Background(), 180*24*time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged %d snapshots, want 1", purged)
	}

	var remaining int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM operational_snapshots WHERE organization_id = $1`,
		tn.OrgID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("%d snapshots survived, want 1", remaining)
	}
}

func TestRetentionCannotReachRecentHistory(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	env.MustExec(t,
		`INSERT INTO operational_snapshots (organization_id, captured_at, in_custody_count)
		 VALUES ($1, now() - interval '3 days', 5)`, tn.OrgID)

	// A caller asking to purge everything from the last day must not be able to.
	// The floor is in the database precisely so a mistake — or a deliberate
	// attempt to delete an inconvenient week — cannot shorten it.
	if _, err := env.App.Analytics.PurgeSnapshots(context.Background(), time.Hour); err == nil {
		t.Fatal("a three-day-old snapshot was purged inside the retention floor")
	}

	var remaining int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM operational_snapshots WHERE organization_id = $1`,
		tn.OrgID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("%d snapshots survived, want 1", remaining)
	}
}

// ---------------------------------------------------------------------------
// League tables
// ---------------------------------------------------------------------------

func TestUnitAndServiceTables(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	for i := 0; i < 3; i++ {
		bookOne(t, env, tn)
	}

	units := env.Do(t, http.MethodGet, "/api/v1/command-centre/units", tn.AdminAccessTok, nil)
	if units.Status != http.StatusOK {
		t.Fatalf("units: %d %s", units.Status, units.Raw)
	}
	rows, _ := units.Body["data"].([]any)
	if len(rows) == 0 {
		t.Fatal("the unit table is empty after three bookings")
	}
	row, _ := rows[0].(map[string]any)
	if got := num(t, row, "booked"); got != 3 {
		t.Fatalf("the booking branch shows %d bookings, want 3", got)
	}
	// A public id, never an internal key (§11).
	if id, _ := row["unitId"].(string); !strings.HasPrefix(id, "ou_") {
		t.Fatalf("unitId %q is not a public identifier", id)
	}

	services := env.Do(t, http.MethodGet, "/api/v1/command-centre/services",
		tn.AdminAccessTok, nil)
	if services.Status != http.StatusOK {
		t.Fatalf("services: %d %s", services.Status, services.Raw)
	}
	svcRows, _ := services.Body["data"].([]any)
	if len(svcRows) == 0 {
		t.Fatal("the service table is empty after three bookings")
	}
}

func TestBacklogByUnitIsLive(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	resp := env.Do(t, http.MethodGet, "/api/v1/command-centre/backlog",
		tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("backlog: %d %s", resp.Status, resp.Raw)
	}
	if note, _ := resp.Body["consistency"].(string); !strings.Contains(note, "live") {
		t.Fatalf("the backlog does not declare itself live: %q", note)
	}
}
