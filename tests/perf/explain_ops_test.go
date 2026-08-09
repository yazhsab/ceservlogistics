//go:build explain

package perf

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

const opsEvidencePath = "../../docs/releases/explain-analyze-operations.md"

// TestExplainOperationalHotQueries records plans for the Release 2 paths that
// run at operational volume.
//
// Run with:
//
//	go test -tags explain ./tests/perf/ -run TestExplainOperationalHotQueries -v
//
// The scanner endpoint is the one that matters most: a sorting hub can push
// thousands of scans an hour through ResolveScanBarcode, and a sequential scan
// there would take the whole facility down at peak.
func TestExplainOperationalHotQueries(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "OPSPERF"})

	seedVolume(t, env, tn, geo)
	seedOperationalVolume(t, env, tn)

	queries := []struct {
		name string
		why  string
		sql  string
		args []any
	}{
		{
			name: "Scanner barcode resolution",
			why: "The hottest read in the release: every scan starts here, and a hub " +
				"can run thousands an hour from handheld devices.",
			sql: `SELECT s.id, s.public_id, s.awb, s.current_status, s.movement_direction,
			             s.current_custody_unit_id, s.current_custody_user_id, s.current_bag_id,
			             s.origin_branch_id, s.destination_branch_id, s.is_held,
			             p.id AS package_id
			      FROM shipments s
			      LEFT JOIN shipment_packages p
			             ON p.piece_barcode = $1 AND p.shipment_id = s.id
			      WHERE s.organization_id = $2
			        AND (s.awb = $1
			             OR s.id = (SELECT shipment_id FROM shipment_packages
			                         WHERE piece_barcode = $1 LIMIT 1))`,
			args: []any{scanBarcode(t, env, tn), tn.OrgID},
		},
		{
			name: "Delivery-ready queue at a branch",
			why: "Rendered every time a supervisor plans a delivery run, filtered to " +
				"one branch out of the whole network.",
			sql: `SELECT s.public_id, s.awb, s.current_status, s.status_changed_at,
			             s.promised_delivery_at, s.is_held
			      FROM shipments s
			      WHERE s.organization_id = $1
			        AND s.destination_branch_id = $2
			        AND s.current_status IN ('DESTINATION_BRANCH_RECEIVED','NDR','DELIVERY_FAILED')
			        AND s.is_held = false
			      ORDER BY s.promised_delivery_at NULLS LAST, s.status_changed_at, s.id
			      LIMIT 50`,
			args: []any{tn.OrgID, tn.DestBranchID},
		},
		{
			name: "Facility custody stocktake",
			why:  "What is physically at a hub right now; the busiest hub dashboard panel.",
			sql: `SELECT s.public_id, s.awb, s.current_status, s.status_changed_at
			      FROM shipments s
			      WHERE s.organization_id = $1
			        AND s.current_custody_unit_id = $2
			        AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
			      ORDER BY s.status_changed_at DESC, s.id DESC
			      LIMIT 50`,
			args: []any{tn.OrgID, tn.OriginBranchID},
		},
		{
			name: "Bag contents",
			why:  "Read on every bag screen and on every reconciliation.",
			sql: `SELECT bi.status, bi.piece_count, s.awb, s.current_status
			      FROM bag_items bi
			      JOIN shipments s ON s.id = bi.shipment_id
			      WHERE bi.bag_id = $1
			      ORDER BY bi.added_at, bi.id`,
			args: []any{firstBagID(t, env, tn)},
		},
		{
			name: "Expected inbound manifests",
			why:  "The hub inbound board, polled continuously during a shift.",
			sql: `SELECT m.public_id, m.manifest_code, m.bag_count, m.total_shipment_count,
			             m.dispatched_at
			      FROM manifests m
			      WHERE m.organization_id = $1
			        AND m.destination_unit_id = $2
			        AND m.status = 'DISPATCHED'
			      ORDER BY m.dispatched_at, m.id
			      LIMIT 50`,
			args: []any{tn.OrgID, tn.DestBranchID},
		},
		{
			name: "Scan history by facility (deep page)",
			why: "Keyset paginated; the plan must not degrade as an operator pages " +
				"back through a busy day.",
			sql: `SELECT se.id, se.public_id, se.raw_barcode, se.scan_type, se.outcome, se.occurred_at
			      FROM scan_events se
			      WHERE se.organization_id = $1
			        AND se.operating_unit_id = $2
			        AND (se.occurred_at, se.id) < ($3, $4)
			      ORDER BY se.occurred_at DESC, se.id DESC
			      LIMIT 50`,
			args: deepScanCursor(t, env, tn),
		},
		{
			name: "Public tracking by AWB",
			why: "Unauthenticated and heavily polled by consignees; must be a single " +
				"unique-index probe.",
			sql: `SELECT s.awb, s.current_status, s.status_changed_at, s.piece_count,
			             s.promised_delivery_at, s.booked_at, s.id
			      FROM shipments s
			      WHERE s.awb = $1`,
			args: []any{scanBarcode(t, env, tn)},
		},
		{
			name: "Public tracking timeline",
			why:  "The second query of every public tracking request.",
			sql: `SELECT e.event_type, e.to_status, e.occurred_at, e.description
			      FROM shipment_events e
			      WHERE e.shipment_id = $1 AND e.event_type <> 'REMARK'
			      ORDER BY e.occurred_at ASC, e.sequence ASC
			      LIMIT 100`,
			args: []any{firstShipmentID(t, env, tn)},
		},
		{
			name: "Open exceptions at a facility",
			why:  "The hub exception panel, filtered to one facility.",
			sql: `SELECT e.public_id, e.exception_type, e.severity, e.raised_at
			      FROM operational_exceptions e
			      WHERE e.organization_id = $1
			        AND e.operating_unit_id = $2
			        AND e.status IN ('OPEN','INVESTIGATING')
			      ORDER BY e.created_at DESC, e.id DESC
			      LIMIT 50`,
			args: []any{tn.OrgID, tn.OriginBranchID},
		},
		{
			name: "NDR cases due for reattempt",
			why:  "Swept by the worker and rendered on the branch NDR board.",
			sql: `SELECT n.public_id, n.case_code, n.next_attempt_at, n.attempt_count
			      FROM ndr_cases n
			      WHERE n.organization_id = $1
			        AND n.status IN ('OPEN','SCHEDULED')
			        AND n.next_attempt_at IS NOT NULL
			        AND n.next_attempt_at <= now()
			      ORDER BY n.next_attempt_at
			      LIMIT 50`,
			args: []any{tn.OrgID},
		},
	}

	var report strings.Builder
	report.WriteString("# Release 2 — operational query plans\n\n")
	report.WriteString(fmt.Sprintf(
		"Captured %s against PostgreSQL 16 with a seeded operational dataset.\n\n",
		time.Now().Format("2 January 2006")))
	report.WriteString(datasetSummary(t, env, tn))
	report.WriteString("\nEvery query below is asserted to avoid a sequential scan on a " +
		"high-volume table; the test fails the build if one appears.\n\n")

	for _, q := range queries {
		plan := explain(t, env, q.sql, q.args...)
		fmt.Fprintf(&report, "## %s\n\n%s\n\n```sql\n%s\n```\n\n```\n%s\n```\n\n",
			q.name, q.why, strings.TrimSpace(dedent(q.sql)), plan)

		for _, table := range []string{
			"shipments", "shipment_events", "scan_events", "bag_items",
			"manifests", "operational_exceptions", "ndr_cases",
		} {
			if strings.Contains(plan, "Seq Scan on "+table) {
				t.Errorf("%s sequentially scans %s:\n%s", q.name, table, plan)
			}
		}
		t.Logf("%s: %s", q.name, firstLine(plan))
	}

	if err := os.WriteFile(opsEvidencePath, []byte(report.String()), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	t.Logf("wrote %s", opsEvidencePath)
}

// seedOperationalVolume creates enough operational history that the planner
// makes realistic choices.
func seedOperationalVolume(t *testing.T, env *harness.Env, tn *harness.Tenant) {
	t.Helper()
	start := time.Now()

	// Put a slice of the seeded shipments into a realistic operational state so
	// the partial indexes on custody and the delivery queue are exercised.
	env.MustExec(t, `
		UPDATE shipments
		   SET current_custody_unit_id = $2,
		       current_status = 'DESTINATION_BRANCH_RECEIVED',
		       destination_branch_id = $3,
		       promised_delivery_at = now() + (id % 5) * interval '1 day'
		 WHERE organization_id = $1 AND id % 7 = 0`,
		tn.OrgID, tn.OriginBranchID, tn.DestBranchID)

	// Six events per shipment: the append-only history a real parcel accumulates
	// between booking and delivery. Without this the tracking timeline query
	// would be measured against an empty table and prove nothing.
	env.MustExec(t, `
		INSERT INTO shipment_events (
			public_id, organization_id, shipment_id, sequence, event_type,
			to_status, occurred_at, actor_type, description, source
		)
		SELECT 'evt_' || upper(substr(md5(s.id::text || n::text || 'perfevt'), 1, 26)),
		       $1, s.id, n,
		       (ARRAY['BOOKED','SCAN','BAG','MANIFEST','TRIP','DELIVERY'])[n],
		       (ARRAY['BOOKED','ORIGIN_BRANCH_RECEIVED','ORIGIN_BAGGED',
		              'ORIGIN_DISPATCHED','IN_TRANSIT','DELIVERED'])[n],
		       s.created_at + n * interval '3 hours',
		       'USER', 'Seeded operational event', 'API'
		FROM shipments s
		CROSS JOIN generate_series(1, 6) n
		WHERE s.organization_id = $1
		ON CONFLICT DO NOTHING`, tn.OrgID)
	env.MustExec(t, `
		UPDATE shipments SET event_sequence = 6 WHERE organization_id = $1`, tn.OrgID)

	// 60,000 scan events across two facilities: a busy day at a sorting hub.
	env.MustExec(t, `
		INSERT INTO scan_events (
			public_id, organization_id, raw_barcode, shipment_id, scan_type,
			operating_unit_id, source, occurred_at, outcome, rejection_code
		)
		SELECT 'scn_' || upper(substr(md5(g::text || 'perfscan'), 1, 26)),
		       $1, 'PRF' || lpad(g::text, 6, '0'), NULL,
		       (ARRAY['RECEIVE','ARRIVAL','DEPARTURE','SORT'])[1 + (g % 4)],
		       CASE WHEN g % 2 = 0 THEN $2::bigint ELSE $3::bigint END,
		       'SCANNER',
		       now() - (g % 1440) * interval '1 minute',
		       CASE WHEN g % 50 = 0 THEN 'REJECTED' ELSE 'ACCEPTED' END,
		       -- The CHECK constraint requires a code on a rejection, which is
		       -- the point: a refusal with no reason is not a useful record.
		       CASE WHEN g % 50 = 0 THEN 'UNKNOWN_BARCODE' ELSE NULL END
		FROM generate_series(1, 60000) g`,
		tn.OrgID, tn.OriginBranchID, tn.DestBranchID)

	// 5,000 bags, each holding one shipment.
	//
	// The bags are seeded OPEN and closed afterwards, because bag_items_guard
	// refuses an insert into a closed bag — even from a bulk seed, which is a
	// small proof that the trigger is doing its job.
	env.MustExec(t, `
		INSERT INTO bags (
			public_id, organization_id, bag_code, barcode, status,
			origin_unit_id, destination_unit_id, shipment_count, piece_count, total_weight_grams,
			created_at
		)
		SELECT 'bag_' || upper(substr(md5(g::text || 'perfbag'), 1, 26)),
		       $1, 'PERF-BAG-' || lpad(g::text, 6, '0'), 'PERFBAG' || lpad(g::text, 6, '0'),
		       'OPEN', $2, $3, 1, 1, 1000,
		       now() - (g % 30) * interval '1 day'
		FROM generate_series(1, 5000) g`,
		tn.OrgID, tn.OriginBranchID, tn.DestBranchID)

	env.MustExec(t, `
		INSERT INTO bag_items (organization_id, bag_id, shipment_id, piece_count, weight_grams)
		SELECT $1, b.id, s.id, 1, 1000
		FROM (SELECT id, row_number() OVER (ORDER BY id) rn FROM bags
		       WHERE organization_id = $1 LIMIT 5000) b
		JOIN (SELECT id, row_number() OVER (ORDER BY id) rn FROM shipments
		       WHERE organization_id = $1 LIMIT 5000) s ON s.rn = b.rn
		ON CONFLICT DO NOTHING`, tn.OrgID)

	env.MustExec(t, `
		UPDATE bags SET status = 'CLOSED', closed_at = now(),
		                closed_contents = '{"items":[]}'::jsonb
		 WHERE organization_id = $1 AND status = 'OPEN'`, tn.OrgID)

	// 3,000 manifests, a fifth of them in flight.
	env.MustExec(t, `
		INSERT INTO manifests (
			public_id, organization_id, manifest_code, status,
			origin_unit_id, destination_unit_id, bag_count, total_shipment_count,
			total_piece_count, total_weight_grams, closed_at, closed_contents,
			dispatched_at, created_at
		)
		SELECT 'mft_' || upper(substr(md5(g::text || 'perfmft'), 1, 26)),
		       $1, 'PERF-MFT-' || lpad(g::text, 6, '0'),
		       CASE WHEN g % 5 = 0 THEN 'DISPATCHED' ELSE 'RECONCILED' END,
		       $2, $3, 5, 50, 50, 50000, now(), '{"bags":[]}'::jsonb,
		       now() - (g % 20) * interval '1 hour', now() - (g % 30) * interval '1 day'
		FROM generate_series(1, 3000) g`,
		tn.OrgID, tn.OriginBranchID, tn.DestBranchID)

	// 8,000 exceptions and 4,000 NDR cases.
	env.MustExec(t, `
		INSERT INTO operational_exceptions (
			public_id, organization_id, exception_code, exception_type, severity,
			status, operating_unit_id, description, raised_at, created_at,
			resolved_at, resolution_action, resolution_notes
		)
		SELECT 'exc_' || upper(substr(md5(g::text || 'perfexc'), 1, 26)),
		       $1, 'PERF-EXC-' || lpad(g::text, 6, '0'),
		       (ARRAY['MISSING','EXCESS','DAMAGED','MISROUTED'])[1 + (g % 4)],
		       'MEDIUM',
		       CASE WHEN g % 4 = 0 THEN 'OPEN' ELSE 'RESOLVED' END,
		       CASE WHEN g % 2 = 0 THEN $2::bigint ELSE $3::bigint END,
		       'Seeded exception', now() - (g % 30) * interval '1 day',
		       now() - (g % 30) * interval '1 day',
		       -- A resolved exception must carry its resolution: the CHECK
		       -- constraint refuses "resolved" with no explanation.
		       CASE WHEN g % 4 = 0 THEN NULL ELSE now() END,
		       CASE WHEN g % 4 = 0 THEN NULL ELSE 'FOUND' END,
		       CASE WHEN g % 4 = 0 THEN NULL ELSE 'Seeded resolution' END
		FROM generate_series(1, 8000) g`,
		tn.OrgID, tn.OriginBranchID, tn.DestBranchID)

	env.MustExec(t, `
		INSERT INTO ndr_cases (
			public_id, organization_id, case_code, shipment_id, current_reason_code,
			status, attempt_count, max_attempts, branch_id, next_attempt_at, created_at,
			resolved_at
		)
		SELECT 'ndr_' || upper(substr(md5(g::text || 'perfndr'), 1, 26)),
		       $1, 'PERF-NDR-' || lpad(g::text, 6, '0'), s.id, 'CUSTOMER_NOT_AVAILABLE',
		       CASE WHEN g % 3 = 0 THEN 'SCHEDULED' ELSE 'CLOSED' END,
		       1, 3, $2,
		       CASE WHEN g % 3 = 0 THEN now() - interval '1 hour' ELSE NULL END,
		       now() - (g % 30) * interval '1 day',
		       CASE WHEN g % 3 = 0 THEN NULL ELSE now() END
		FROM generate_series(1, 4000) g
		JOIN LATERAL (
			SELECT id FROM shipments WHERE organization_id = $1 OFFSET g LIMIT 1
		) s ON true`, tn.OrgID, tn.DestBranchID)

	env.MustExec(t, `ANALYZE`)
	t.Logf("seeded operational volume in %s", time.Since(start).Round(time.Millisecond))
}

func datasetSummary(t *testing.T, env *harness.Env, tn *harness.Tenant) string {
	t.Helper()
	var shipments, scans, bags, items, manifests, exceptions, ndr, events int64
	row := env.DB.Pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM shipments WHERE organization_id = $1),
		       (SELECT count(*) FROM scan_events WHERE organization_id = $1),
		       (SELECT count(*) FROM bags WHERE organization_id = $1),
		       (SELECT count(*) FROM bag_items WHERE organization_id = $1),
		       (SELECT count(*) FROM manifests WHERE organization_id = $1),
		       (SELECT count(*) FROM operational_exceptions WHERE organization_id = $1),
		       (SELECT count(*) FROM ndr_cases WHERE organization_id = $1),
		       (SELECT count(*) FROM shipment_events WHERE organization_id = $1)`, tn.OrgID)
	if err := row.Scan(&shipments, &scans, &bags, &items, &manifests, &exceptions, &ndr, &events); err != nil {
		t.Fatalf("dataset summary: %v", err)
	}
	return fmt.Sprintf(
		"Dataset: %d shipments, %d shipment events, %d scan events, %d bags holding %d items, "+
			"%d manifests, %d operational exceptions, %d NDR cases.\n",
		shipments, events, scans, bags, items, manifests, exceptions, ndr)
}

func scanBarcode(t *testing.T, env *harness.Env, tn *harness.Tenant) string {
	t.Helper()
	var awb string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT awb FROM shipments WHERE organization_id = $1 ORDER BY id LIMIT 1`,
		tn.OrgID).Scan(&awb); err != nil {
		t.Fatalf("pick an awb: %v", err)
	}
	return awb
}

func firstShipmentID(t *testing.T, env *harness.Env, tn *harness.Tenant) int64 {
	t.Helper()
	var id int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT id FROM shipments WHERE organization_id = $1 ORDER BY id LIMIT 1`,
		tn.OrgID).Scan(&id); err != nil {
		t.Fatalf("pick a shipment: %v", err)
	}
	return id
}

func firstBagID(t *testing.T, env *harness.Env, tn *harness.Tenant) int64 {
	t.Helper()
	var id int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT id FROM bags WHERE organization_id = $1 ORDER BY id LIMIT 1`,
		tn.OrgID).Scan(&id); err != nil {
		t.Fatalf("pick a bag: %v", err)
	}
	return id
}

// deepScanCursor returns a cursor 30,000 rows into the scan log, so the keyset
// plan is measured at depth rather than at the first page.
func deepScanCursor(t *testing.T, env *harness.Env, tn *harness.Tenant) []any {
	t.Helper()
	var occurredAt time.Time
	var id int64
	if err := env.DB.Pool.QueryRow(context.Background(), `
		SELECT occurred_at, id FROM scan_events
		 WHERE organization_id = $1
		 ORDER BY occurred_at DESC, id DESC
		 OFFSET 30000 LIMIT 1`, tn.OrgID).Scan(&occurredAt, &id); err != nil {
		t.Fatalf("deep scan cursor: %v", err)
	}
	return []any{tn.OrgID, tn.OriginBranchID, occurredAt, id}
}
