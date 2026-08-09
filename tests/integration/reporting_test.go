package integration

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/reporting"
	"github.com/ceserve/courier-os/tests/harness"
)

// M31. The engine's whole reason to exist is that a report must not be built in
// RAM or in an HTTP request, so the tests care about the shape as much as the
// contents: is it queued rather than computed, does it stream, does the file
// expire, and can somebody export a tenant they do not belong to.

func queueReport(
	t *testing.T, env *harness.Env, token, reportType, from, to string,
) harness.Response {
	t.Helper()
	return env.Do(t, http.MethodPost, "/api/v1/reports", token, map[string]any{
		"reportType": reportType, "periodStart": from, "periodEnd": to,
	})
}

// runReports drains the report queue synchronously.
//
// The worker would do this on its own schedule; the test drives it directly so
// it is testing the engine rather than the queue's timing.
func runReports(t *testing.T, env *harness.Env, orgID int64) {
	t.Helper()
	rows, err := env.DB.Pool.Query(context.Background(),
		`SELECT id FROM report_runs WHERE organization_id = $1 AND status = 'QUEUED' ORDER BY id`,
		orgID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if err := env.App.Reporting.Generate(context.Background(), orgID, id); err != nil {
			t.Fatalf("generate report %d: %v", id, err)
		}
	}
}

func TestReportIsQueuedNotComputed(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	// 202, not 200: the response is a receipt. A client that treated it as the
	// result would be waiting for a file that does not exist yet.
	if resp.Status != http.StatusAccepted {
		t.Fatalf("queue: %d %s", resp.Status, resp.Raw)
	}
	if status, _ := resp.Body["status"].(string); status != reporting.StatusQueued {
		t.Fatalf("a fresh run is %q, want QUEUED", status)
	}
	if _, ok := resp.Body["pollUrl"]; !ok {
		t.Error("the receipt does not say where to poll")
	}
	// Nothing is downloadable yet, and the response must not pretend otherwise.
	if _, ok := resp.Body["downloadUrl"]; ok {
		t.Error("a queued report advertises a download URL")
	}

	id, _ := resp.Body["id"].(string)
	early := env.Do(t, http.MethodGet, "/api/v1/reports/"+id+"/download", tn.AdminAccessTok, nil)
	if early.Status != http.StatusConflict {
		t.Fatalf("downloading a queued report returned %d, want 409: %s", early.Status, early.Raw)
	}
	if early.ErrorCode() != "REPORT_NOT_READY" {
		t.Fatalf("error code = %s", early.ErrorCode())
	}
}

func TestReportGeneratesADownloadableFile(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	const shipments = 5
	for i := 0; i < shipments; i++ {
		bookOne(t, env, tn)
	}

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	if resp.Status != http.StatusAccepted {
		t.Fatalf("queue: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Body["id"].(string)

	runReports(t, env, tn.OrgID)

	status := env.Do(t, http.MethodGet, "/api/v1/reports/"+id, tn.AdminAccessTok, nil)
	if status.Status != http.StatusOK {
		t.Fatalf("status: %d %s", status.Status, status.Raw)
	}
	if got, _ := status.Body["status"].(string); got != reporting.StatusCompleted {
		t.Fatalf("run status = %q: %s", got, status.Raw)
	}
	if rows := int64(status.Body["rowCount"].(float64)); rows != shipments {
		t.Fatalf("the report covered %d rows, want %d", rows, shipments)
	}
	if size := int64(status.Body["byteSize"].(float64)); size <= 0 {
		t.Fatal("the report has no size")
	}
	// A finished report says when its download window closes, because it is
	// customer data and the window is deliberately short.
	if _, ok := status.Body["expiresAt"]; !ok {
		t.Error("a completed report has no expiry")
	}

	// The filesystem store cannot presign, so the API streams. Either way the
	// bytes must be a real CSV with one row per shipment.
	download := env.Do(t, http.MethodGet, "/api/v1/reports/"+id+"/download",
		tn.AdminAccessTok, nil)
	if download.Status != http.StatusOK && download.Status != http.StatusFound {
		t.Fatalf("download: %d %s", download.Status, download.Raw)
	}
	if download.Status == http.StatusOK {
		if ct := download.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("Content-Type = %q", ct)
		}
		// An export URL is a short-lived credential; it must not be cached.
		if cc := download.Headers.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		records, err := csv.NewReader(strings.NewReader(download.Raw)).ReadAll()
		if err != nil {
			t.Fatalf("the download is not valid CSV: %v", err)
		}
		if len(records) != shipments+1 {
			t.Fatalf("the CSV has %d lines, want %d rows plus a header",
				len(records), shipments)
		}
		if records[0][0] != "awb" {
			t.Fatalf("the first column is %q, want awb", records[0][0])
		}
	}
}

func TestReportStreamsRatherThanBuffering(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// More rows than one chunk (2000), so the engine has to loop and resume from
	// its cursor. A buffered implementation would pass the row count too, but a
	// mishandled chunk boundary would drop or repeat rows.
	//
	// The rollup is unique per (org, date, unit, service), so breadth has to
	// come from the unit dimension: twelve synthetic branches across 300 days.
	env.MustExec(t, `
		INSERT INTO operating_units (public_id, organization_id, code, name, unit_type,
		                             address_line1, pincode, status)
		SELECT gen_seed_public_id('ou'), $1, 'RPT' || g, 'Report Unit ' || g, 'COMPANY_BRANCH',
		       'Report Street', $2, 'ACTIVE'
		FROM generate_series(1, 12) g`, tn.OrgID, tn.OriginPincode)
	env.MustExec(t, `
		INSERT INTO shipment_daily_stats
		    (organization_id, stat_date, operating_unit_id, service_id,
		     booked_count, delivered_count, revenue_minor)
		SELECT $1, (CURRENT_DATE - d)::date, u.id, $2, d % 50, d % 40, d * 100
		FROM generate_series(0, 299) d
		CROSS JOIN (SELECT id FROM operating_units
		             WHERE organization_id = $1 AND code LIKE 'RPT%') u
		ON CONFLICT DO NOTHING`, tn.OrgID, tn.ServiceID)

	var seeded int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM shipment_daily_stats WHERE organization_id = $1`,
		tn.OrgID).Scan(&seeded); err != nil {
		t.Fatal(err)
	}

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeBranchPerformance,
		time.Now().AddDate(0, 0, -320).Format("2006-01-02"),
		time.Now().Format("2006-01-02"))
	if resp.Status != http.StatusAccepted {
		t.Fatalf("queue: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Body["id"].(string)
	runReports(t, env, tn.OrgID)

	status := env.Do(t, http.MethodGet, "/api/v1/reports/"+id, tn.AdminAccessTok, nil)
	if got, _ := status.Body["status"].(string); got != reporting.StatusCompleted {
		t.Fatalf("run status = %q: %s", got, status.Raw)
	}
	rows := int64(status.Body["rowCount"].(float64))
	if rows != int64(seeded) {
		t.Fatalf("the report covered %d rows, want %d — a chunk boundary was mishandled",
			rows, seeded)
	}

	download := env.Do(t, http.MethodGet, "/api/v1/reports/"+id+"/download",
		tn.AdminAccessTok, nil)
	if download.Status == http.StatusOK {
		records, err := csv.NewReader(strings.NewReader(download.Raw)).ReadAll()
		if err != nil {
			t.Fatalf("the multi-chunk download is not valid CSV: %v", err)
		}
		if len(records) != seeded+1 {
			t.Fatalf("the CSV has %d lines, want %d plus a header", len(records), seeded)
		}
		// The header appears once, not once per chunk.
		headers := 0
		for _, rec := range records {
			if rec[0] == "date" {
				headers++
			}
		}
		if headers != 1 {
			t.Fatalf("the CSV carries %d header rows", headers)
		}
	}
}

func TestReportsAreTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})
	for i := 0; i < 3; i++ {
		bookOne(t, env, beta)
	}

	// Alpha runs the same report and gets its own (empty) data, not beta's.
	resp := queueReport(t, env, alpha.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	if resp.Status != http.StatusAccepted {
		t.Fatalf("queue: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Body["id"].(string)
	runReports(t, env, alpha.OrgID)

	status := env.Do(t, http.MethodGet, "/api/v1/reports/"+id, alpha.AdminAccessTok, nil)
	if rows := int64(status.Body["rowCount"].(float64)); rows != 0 {
		t.Fatalf("alpha's report covered %d of beta's shipments", rows)
	}

	// And beta's run is invisible to alpha entirely.
	betaResp := queueReport(t, env, beta.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	betaID, _ := betaResp.Body["id"].(string)

	cross := env.Do(t, http.MethodGet, "/api/v1/reports/"+betaID, alpha.AdminAccessTok, nil)
	if cross.Status != http.StatusNotFound {
		t.Fatalf("alpha read beta's report run: %d %s", cross.Status, cross.Raw)
	}
	crossDownload := env.Do(t, http.MethodGet,
		"/api/v1/reports/"+betaID+"/download", alpha.AdminAccessTok, nil)
	if crossDownload.Status != http.StatusNotFound {
		t.Fatalf("alpha downloaded beta's report: %d", crossDownload.Status)
	}

	list := env.Do(t, http.MethodGet, "/api/v1/reports", alpha.AdminAccessTok, nil)
	items, _ := list.Body["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("alpha's listing shows %d runs, want only its own", len(items))
	}
}

func TestFinanceReportsNeedTheFinancePermission(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// A hub manager may export their own volume. Commission is somebody's
	// income and somebody else's liability, so it needs report.finance.
	_, _, token := env.NewUser(t, tn.OrgID, "hub-mgr@example.com",
		"HUB_MANAGER", &tn.HubID)

	ok := queueReport(t, env, token, reporting.TypeShipmentVolume, "2026-01-01", "2026-03-31")
	if ok.Status != http.StatusAccepted {
		t.Fatalf("an operational report was refused: %d %s", ok.Status, ok.Raw)
	}

	for _, restricted := range []string{
		reporting.TypeCommission, reporting.TypeSettlement,
		reporting.TypeRevenue, reporting.TypeCODAging,
	} {
		denied := queueReport(t, env, token, restricted, "2026-01-01", "2026-03-31")
		if denied.Status != http.StatusForbidden {
			t.Errorf("%s without report.finance returned %d", restricted, denied.Status)
		}
	}

	// The catalogue says which is which, so a UI can grey them out rather than
	// letting somebody discover it by being refused.
	types := env.Do(t, http.MethodGet, "/api/v1/reports/types", token, nil)
	if types.Status != http.StatusOK {
		t.Fatalf("types: %d %s", types.Status, types.Raw)
	}
	if !strings.Contains(types.Raw, "requiresFinancePermission") {
		t.Error("the report catalogue does not say which types are restricted")
	}
}

func TestReportPeriodIsBounded(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	long := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2000-01-01", "2030-01-01")
	if long.Status != http.StatusUnprocessableEntity && long.Status != http.StatusBadRequest {
		t.Fatalf("a 30-year report was accepted: %d %s", long.Status, long.Raw)
	}

	backwards := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-06-01", "2026-01-01")
	if backwards.Status != http.StatusUnprocessableEntity && backwards.Status != http.StatusBadRequest {
		t.Fatalf("a backwards period was accepted: %d %s", backwards.Status, backwards.Raw)
	}

	unknown := queueReport(t, env, tn.AdminAccessTok,
		"NOT_A_REPORT", "2026-01-01", "2026-02-01")
	if unknown.Status != http.StatusUnprocessableEntity && unknown.Status != http.StatusBadRequest {
		t.Fatalf("an unknown report type was accepted: %d %s", unknown.Status, unknown.Raw)
	}
}

func TestExpiredReportIsRefusedAndItsFileDeleted(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	id, _ := resp.Body["id"].(string)
	runReports(t, env, tn.OrgID)

	// Bring the expiry forward rather than waiting a day.
	env.MustExec(t,
		`UPDATE report_runs SET expires_at = now() - interval '1 hour'
		  WHERE organization_id = $1 AND public_id = $2`, tn.OrgID, id)

	n, err := env.App.Reporting.ExpireRuns(context.Background())
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d runs, want 1", n)
	}

	after := env.Do(t, http.MethodGet, "/api/v1/reports/"+id+"/download",
		tn.AdminAccessTok, nil)
	if after.Status != http.StatusConflict {
		t.Fatalf("downloading an expired report returned %d, want 409: %s",
			after.Status, after.Raw)
	}
	if after.ErrorCode() != "REPORT_EXPIRED" {
		t.Fatalf("error code = %s", after.ErrorCode())
	}

	// The file goes with the record. Expiring one and leaving the other would
	// leave exported customer data in the bucket indefinitely.
	var objectKey string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT o.object_key FROM report_runs r JOIN stored_objects o ON o.id = r.object_id
		  WHERE r.public_id = $1`, id).Scan(&objectKey); err != nil {
		t.Fatal(err)
	}
	if _, _, gErr := env.App.Storage.Get(context.Background(), objectKey); gErr == nil {
		t.Fatal("the expired report's file is still in storage")
	}
}

func TestReportGenerationIsClaimedOnce(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	id, _ := resp.Body["id"].(string)

	var runID int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT id FROM report_runs WHERE public_id = $1`, id).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	// The first call generates; the second finds nothing to claim and does
	// nothing. Two workers producing the same object key would both write, and
	// the loser would leave a truncated file behind a COMPLETED record.
	if err := env.App.Reporting.Generate(context.Background(), tn.OrgID, runID); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if err := env.App.Reporting.Generate(context.Background(), tn.OrgID, runID); err != nil {
		t.Fatalf("a second generate should be a no-op, got: %v", err)
	}

	var objects int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stored_objects WHERE organization_id = $1 AND purpose = 'EXPORT'`,
		tn.OrgID).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if objects != 1 {
		t.Fatalf("%d report objects were written, want 1", objects)
	}
}

func TestReportRunCanBeCancelledBeforeItStarts(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	resp := queueReport(t, env, tn.AdminAccessTok,
		reporting.TypeShipmentVolume, "2026-01-01", "2026-12-31")
	id, _ := resp.Body["id"].(string)

	cancel := env.Do(t, http.MethodPost, "/api/v1/reports/"+id+"/cancel",
		tn.AdminAccessTok, nil)
	if cancel.Status != http.StatusOK {
		t.Fatalf("cancel: %d %s", cancel.Status, cancel.Raw)
	}
	if got, _ := cancel.Body["status"].(string); got != reporting.StatusCancelled {
		t.Fatalf("status after cancel = %q", got)
	}

	// A cancelled run is not picked up.
	runReports(t, env, tn.OrgID)
	status := env.Do(t, http.MethodGet, "/api/v1/reports/"+id, tn.AdminAccessTok, nil)
	if got, _ := status.Body["status"].(string); got != reporting.StatusCancelled {
		t.Fatalf("a cancelled run was generated anyway: %q", got)
	}
}
