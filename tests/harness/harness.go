// Package harness boots a real PostgreSQL and Redis for integration tests.
//
// Constitution §8 and §40: no mock persistence. Every integration test in this
// repository runs against a real database with the real migrations applied, so
// a passing test is evidence about production behaviour rather than about a
// stub.
//
// The containers are started once per test binary and shared; each test gets an
// isolated tenant instead of an isolated database, which is both faster and a
// better model of production, where tenants genuinely share tables and rely on
// organization_id predicates for isolation.
package harness

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/platform/storage"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	appdb "github.com/ceserve/courier-os/db"
	"github.com/ceserve/courier-os/internal/api"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
)

// Env is the shared infrastructure for one test binary.
type Env struct {
	DB      *database.DB
	Cache   *cache.Cache
	Queries *dbgen.Queries
	Config  *config.Config
	App     *api.Application
	Server  *httptest.Server
	DSN     string
}

var (
	shared   *Env
	sharedMu sync.Mutex
)

// Start returns the shared environment, booting it on first use.
//
// TEST_DATABASE_URL and TEST_REDIS_URL short-circuit container startup, which
// makes the local edit-run loop far faster; CI leaves them unset so the suite
// is fully self-contained.
func Start(t *testing.T) *Env {
	t.Helper()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if shared != nil {
		return shared
	}

	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	redisURL := os.Getenv("TEST_REDIS_URL")

	if dsn == "" {
		pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("courier_os_test"),
			tcpostgres.WithUsername("courier"),
			tcpostgres.WithPassword("courier"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		)
		if err != nil {
			t.Skipf("skipping integration tests: cannot start PostgreSQL container (%v); "+
				"set TEST_DATABASE_URL to use an existing database", err)
		}
		dsn, err = pg.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("postgres connection string: %v", err)
		}
	}
	if redisURL == "" {
		rd, err := tcredis.Run(ctx, "redis:7-alpine")
		if err != nil {
			t.Skipf("skipping integration tests: cannot start Redis container (%v)", err)
		}
		redisURL, err = rd.ConnectionString(ctx)
		if err != nil {
			t.Fatalf("redis connection string: %v", err)
		}
	}

	setTestEnv(t, dsn, redisURL)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}

	// Test logs go to io.Discard by default; TEST_VERBOSE_LOGS turns them on
	// when a failure needs the detail.
	logOut := io.Discard
	if os.Getenv("TEST_VERBOSE_LOGS") != "" {
		logOut = os.Stderr
	}
	log := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db, err := database.Open(ctx, cfg.Database, log)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	migrator, err := database.NewMigrator(db.Pool, appdb.Migrations, "migrations", log)
	if err != nil {
		t.Fatalf("build migrator: %v", err)
	}
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	rd, err := cache.Open(ctx, cfg.Redis, log)
	if err != nil {
		t.Fatalf("open test cache: %v", err)
	}
	tel, err := telemetry.Setup(ctx, cfg, log)
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}

	// POD artifacts land in a temp directory: the tests exercise real upload,
	// checksum and retrieval rather than a stub, without needing MinIO.
	objectDir, err := os.MkdirTemp("", "courier-objects-*")
	if err != nil {
		panic(err)
	}
	objects, err := storage.NewFSStore(objectDir, "courier-os-test")
	if err != nil {
		panic(err)
	}

	app := api.New(api.Dependencies{
		Config: cfg, DB: db, Cache: rd, Logger: log, Telemetry: tel, Storage: objects,
	})
	server := httptest.NewServer(app.Handler())

	shared = &Env{
		DB: db, Cache: rd, Queries: dbgen.New(db.Pool),
		Config: cfg, App: app, Server: server, DSN: dsn,
	}
	return shared
}

func setTestEnv(t *testing.T, dsn, redisURL string) {
	t.Helper()
	rateLimitEnabled := "false"
	if os.Getenv("TEST_RATE_LIMIT_ENABLED") == "true" {
		rateLimitEnabled = "true"
	}
	vars := map[string]string{
		"APP_ENV":      config.EnvTest,
		"DATABASE_URL": dsn,
		"REDIS_URL":    redisURL,
		"JWT_SECRET":   "test-secret-value-that-is-at-least-32-bytes-long",
		"LOG_LEVEL":    "warn",
		"LOG_FORMAT":   "text",
		// Argon2 at production cost would add ~60 ms to every login in the
		// suite. The parameters are still exercised by TestPasswordHashing,
		// which uses the real defaults.
		"ARGON2_TIME":          "1",
		"ARGON2_MEMORY_KIB":    "16384",
		"DATABASE_MAX_CONNS":   "20",
		"RATE_LIMIT_ENABLED":   rateLimitEnabled,
		"REDIS_REQUIRED":       "true",
		"CORS_ALLOWED_ORIGINS": "http://localhost:3000",
	}
	for k, v := range vars {
		t.Setenv(k, v)
		// t.Setenv restores on test completion, but the shared Env outlives the
		// first test, so the value is also set for the process.
		_ = os.Setenv(k, v)
	}
}

// Reset truncates every business table between tests that need a clean slate.
//
// TRUNCATE organizations CASCADE also empties `roles`, because system roles
// carry a nullable organization_id foreign key. The RBAC catalogue from
// migration 0012 is therefore re-applied afterwards, so every test starts from
// exactly the state a freshly migrated database has.
func (e *Env) Reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := e.DB.Pool.Exec(ctx, `
		TRUNCATE
			webhook_attempts, webhook_deliveries, webhook_subscriptions, webhook_endpoints,
			api_key_requests, api_keys,
			report_runs, operational_snapshots, shipment_daily_stats,
			notification_attempts, notifications, notification_subscriptions,
			notification_preferences, notification_templates,
			invoice_payments, credit_note_lines, credit_notes, tax_components,
			invoice_shipments, invoice_lines, invoices, billing_runs, invoice_sequences,
			settlement_payments, settlement_approvals, settlement_adjustments,
			settlement_lines, settlements,
			cod_disputes, cod_adjustments, cod_remittance_items, cod_remittances,
			cod_reconciliation_items, cod_reconciliations,
			cod_custody_transfer_items, cod_custody_transfers, cod_collections, cod_obligations,
			commission_entries, commission_calculations, commission_rule_versions,
			commission_rules, commission_schemes,
			journal_entries, journal_transactions, ledger_accounts, accounting_periods,
			pod_artifacts, proof_of_delivery, object_access_log, stored_objects,
			rto_legs, rto_cases, ndr_actions, ndr_attempts, ndr_cases, ndr_reasons,
			delivery_otps, delivery_attempts, delivery_run_items, delivery_runs,
			shipment_holds, reconciliation_items, reconciliations, operational_exceptions,
			trip_events, trip_assignments, trip_legs, trips, drivers, vehicles, carriers,
			manifest_events, manifest_loose_shipments, manifest_bags, manifests,
			bag_events, bag_seals, bag_items, bags,
			pickup_attempts, pickup_assignments, pickup_runs, pickup_request_shipments,
			pickup_requests, operational_sequences, scan_events,
			shipment_events, shipment_route_snapshots, shipment_charge_snapshots,
			shipment_address_snapshots, shipment_packages, shipments, awb_sequences,
			customer_credit_entries, customer_users, credit_profiles, billing_profiles,
			customer_addresses, customer_contacts, business_accounts, customers,
			discount_rules, surcharge_rules, weight_slabs, zone_rates,
			rate_card_versions, rate_cards, tax_rules,
			temporary_closures, routing_overrides, route_legs, route_definitions,
			serviceability_rules, service_areas,
			courier_services,
			franchise_agreements, franchises, operating_unit_capabilities, operating_units, regions,
			pincode_zone_mappings, zones, localities, pincodes, cities, districts, states, countries,
			import_rows, import_jobs, jobs, idempotency_keys,
			audit_events, password_reset_tokens, sessions, user_roles, users,
			role_permissions, roles, permissions, organizations
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
	// Cached postal codes, zones and sessions would otherwise survive the
	// truncation and make the next test read data that no longer exists.
	e.Cache.DeletePrefix(ctx, e.Cache.Key(""))
	e.reseedRBAC(t)
}

// reseedRBAC restores the permission and system-role catalogue by replaying the
// migration that owns it, rather than by duplicating its contents here.
// reseedRBAC replays the permission catalogue after a truncate.
//
// TRUNCATE organizations CASCADE also empties roles, because a role's
// organization_id is nullable and the system roles hang off no tenant. Every
// migration that seeds permissions or grants therefore has to be replayed here,
// or the next test starts with an ORG_ADMIN who cannot do anything.
func (e *Env) reseedRBAC(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"migrations/0012_rbac_catalog.up.sql",
		"migrations/0019_rbac_release2.up.sql",
		"migrations/0025_rbac_finance.up.sql",
		"migrations/0029_rbac_release4.up.sql",
	} {
		raw, err := appdb.Migrations.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := e.DB.Pool.Exec(context.Background(), string(raw)); err != nil {
			t.Fatalf("reseed %s: %v", name, err)
		}
	}
	e.reseedTrackingMilestones(t)
}

// reseedTrackingMilestones restores the platform tracking defaults.
//
// They live inside 0018, which also creates tables, so the whole migration
// cannot be replayed. The INSERT is extracted from the migration text rather
// than copied here, so the defaults cannot drift between the schema and the
// tests.
func (e *Env) reseedTrackingMilestones(t *testing.T) {
	t.Helper()
	raw, err := appdb.Migrations.ReadFile("migrations/0018_pod_storage.up.sql")
	if err != nil {
		t.Fatalf("read pod storage migration: %v", err)
	}
	body := string(raw)
	start := strings.Index(body, "INSERT INTO tracking_milestones")
	if start < 0 {
		t.Fatal("0018 no longer seeds tracking milestones; update the harness")
	}
	end := strings.Index(body[start:], ";")
	if end < 0 {
		t.Fatal("could not find the end of the tracking milestone seed")
	}
	if _, err := e.DB.Pool.Exec(context.Background(), body[start:start+end+1]); err != nil {
		t.Fatalf("reseed tracking milestones: %v", err)
	}
}

// URL builds an absolute test-server URL.
func (e *Env) URL(path string) string { return e.Server.URL + path }

// MustExec runs a statement and fails the test on error.
func (e *Env) MustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.DB.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", truncate(sql, 120), err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = fmt.Sprintf
