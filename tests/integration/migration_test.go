package integration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	appdb "github.com/ceserve/courier-os/db"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/tests/harness"
)

// TestMigrationsApplyRollBackAndReapplyOnACleanDatabase is the migration proof
// required before release: a clean database must accept the full corpus, roll
// all of it back, and accept it again — leaving no residue either way.
//
// It runs against its own database so the shared test schema is untouched.
func TestMigrationsApplyRollBackAndReapplyOnACleanDatabase(t *testing.T) {
	env := harness.Start(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	const dbName = "courier_os_migration_check"
	admin := adminDSN(t, env.DSN)
	adminDB, err := database.Open(ctx, config.DatabaseConfig{
		URL: admin, MaxConns: 2, MinConns: 1,
	}, log)
	if err != nil {
		t.Skipf("cannot reach the maintenance database: %v", err)
	}
	defer adminDB.Close()

	if _, err := adminDB.Pool.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop scratch database: %v", err)
	}
	if _, err := adminDB.Pool.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Pool.Exec(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	})

	scratch, err := database.Open(ctx, config.DatabaseConfig{
		URL: replaceDatabase(env.DSN, dbName), MaxConns: 3, MinConns: 1,
	}, log)
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	defer scratch.Close()

	migrator, err := database.NewMigrator(scratch.Pool, appdb.Migrations, "migrations", log)
	if err != nil {
		t.Fatalf("build migrator: %v", err)
	}

	applied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply migrations on a clean database: %v", err)
	}
	if applied == 0 {
		t.Fatal("expected at least one migration to be applied")
	}
	t.Logf("applied %d migrations on a clean database", applied)

	// Every migration must be recorded, and re-running must be a no-op.
	if again, upErr := migrator.Up(ctx); upErr != nil || again != 0 {
		t.Fatalf("re-running migrations should be a no-op, got %d applied (%v)", again, upErr)
	}
	if err := migrator.Validate(ctx); err != nil {
		t.Fatalf("checksum validation failed: %v", err)
	}

	// The seeded RBAC catalogue must be present on a clean database.
	var permissions, roles int
	if err := scratch.Pool.QueryRow(ctx,
		`SELECT (SELECT count(*)::int FROM permissions), (SELECT count(*)::int FROM roles)`).
		Scan(&permissions, &roles); err != nil {
		t.Fatalf("read seeded catalogue: %v", err)
	}
	if permissions == 0 || roles != 12 {
		t.Errorf("expected the seeded catalogue (12 system roles, permissions), got %d roles and %d permissions",
			roles, permissions)
	}

	// Roll everything back.
	rolled, err := migrator.Down(ctx, applied)
	if err != nil {
		t.Fatalf("roll back migrations: %v", err)
	}
	if rolled != applied {
		t.Fatalf("rolled back %d of %d migrations", rolled, applied)
	}
	var remaining int
	if err := scratch.Pool.QueryRow(ctx, `
		SELECT count(*)::int FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining tables: %v", err)
	}
	if remaining != 0 {
		t.Errorf("rollback left %d tables behind", remaining)
	}

	// And apply them again.
	reapplied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("re-apply migrations after rollback: %v", err)
	}
	if reapplied != applied {
		t.Fatalf("re-applied %d of %d migrations", reapplied, applied)
	}
}

// TestMigrationChecksumDetectsTampering proves the guard against editing a
// migration that has already run in an environment.
func TestMigrationChecksumDetectsTampering(t *testing.T) {
	env := harness.Start(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator, err := database.NewMigrator(env.DB.Pool, appdb.Migrations, "migrations", log)
	if err != nil {
		t.Fatalf("build migrator: %v", err)
	}
	if err := migrator.Validate(ctx); err != nil {
		t.Fatalf("the applied schema should validate cleanly: %v", err)
	}

	// Simulate someone editing an already-applied migration.
	var original string
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT checksum FROM schema_migrations WHERE version = 1`).Scan(&original); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper with checksum: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.DB.Pool.Exec(context.Background(),
			`UPDATE schema_migrations SET checksum = $1 WHERE version = 1`, original)
	})

	err = migrator.Validate(ctx)
	if err == nil {
		t.Fatal("validation must fail when an applied migration no longer matches its file")
	}
	if !strings.Contains(err.Error(), "modified after being applied") {
		t.Errorf("the failure should name the problem, got: %v", err)
	}
}

// adminDSN points at the maintenance database so CREATE/DROP DATABASE can run.
func adminDSN(t *testing.T, dsn string) string {
	t.Helper()
	if override := os.Getenv("TEST_ADMIN_DATABASE_URL"); override != "" {
		return override
	}
	return replaceDatabase(dsn, "postgres")
}

// replaceDatabase swaps the database name in a PostgreSQL DSN.
func replaceDatabase(dsn, name string) string {
	head, tail, hasQuery := strings.Cut(dsn, "?")
	slash := strings.LastIndex(head, "/")
	if slash < 0 {
		return dsn
	}
	out := head[:slash+1] + name
	if hasQuery {
		out += "?" + tail
	}
	return out
}
