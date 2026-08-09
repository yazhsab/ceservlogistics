// Command migrate applies, rolls back, inspects and validates database
// migrations.
//
//	migrate up                 apply every pending migration
//	migrate up --to 7          apply pending migrations up to version 7
//	migrate down --steps 1     roll back the most recent migration
//	migrate status             list applied and pending migrations
//	migrate validate           verify no applied migration has been edited
//	migrate seed --file demo   run a seed script from db/seeds
//	migrate bootstrap          create the platform operator and first admin
//	migrate demo               create a bookable demo tenant (non-production)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	appdb "github.com/ceserve/courier-os/db"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/provision"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	steps := fs.Int("steps", 1, "number of migrations to roll back (down)")
	to := fs.Int64("to", 0, "highest migration version to apply (up); 0 means all")
	seedFile := fs.String("file", "", "seed file name under db/seeds, without the .sql suffix")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr,
			"usage: migrate <up|down|status|validate|seed|bootstrap|demo> [flags]")
		fs.PrintDefaults()
	}
	if len(os.Args) < 2 {
		fs.Usage()
		return errors.New("a command is required")
	}
	command := os.Args[1]
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(logging.Config{Level: cfg.Log.Level, Format: cfg.Log.Format})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Migrations use a small dedicated pool: they are serialised by an advisory
	// lock, so extra connections would only compete with the running API.
	dbCfg := cfg.Database
	dbCfg.MaxConns = 4
	dbCfg.MinConns = 1
	dbCfg.StatementTimeout = 0 // a large index build must not be killed mid-way

	db, err := database.Open(ctx, dbCfg, log)
	if err != nil {
		return err
	}
	defer db.Close()

	m, err := database.NewMigrator(db.Pool, appdb.Migrations, "migrations", log)
	if err != nil {
		return err
	}

	switch command {
	case "up":
		var n int
		if *to > 0 {
			n, err = m.UpTo(ctx, *to)
		} else {
			n, err = m.Up(ctx)
		}
		if err != nil {
			return err
		}
		if n == 0 {
			log.Info("database is already up to date")
		} else {
			log.Info("migrations applied", slog.Int("count", n))
		}
		return nil

	case "down":
		n, err := m.Down(ctx, *steps)
		if err != nil {
			return err
		}
		log.Info("migrations rolled back", slog.Int("count", n))
		return nil

	case "status":
		applied, pending, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%-10s %-34s %-24s %s\n", "VERSION", "NAME", "APPLIED AT", "STATE")
		for _, a := range applied {
			fmt.Printf("%-10d %-34s %-24s %s\n", a.Version, a.Name, a.AppliedAt.Format(time.RFC3339), "applied")
		}
		for _, p := range pending {
			fmt.Printf("%-10d %-34s %-24s %s\n", p.Version, p.Name, "-", "pending")
		}
		fmt.Printf("\n%d applied, %d pending\n", len(applied), len(pending))
		if len(pending) > 0 {
			os.Exit(2) // non-zero so CI and deploy scripts can gate on it
		}
		return nil

	case "validate":
		if err := m.Validate(ctx); err != nil {
			return err
		}
		log.Info("migration checksums validated")
		return nil

	case "bootstrap":
		if err := provision.Bootstrap(ctx, db, cfg, log); err != nil {
			return err
		}
		return nil

	case "demo":
		result, err := provision.Demo(ctx, db, cfg, log)
		if err != nil {
			return err
		}
		fmt.Printf("\nDemo tenant ready:\n")
		fmt.Printf("  organization  %s\n", result.OrganizationCode)
		fmt.Printf("  email         %s\n", result.AdminEmail)
		fmt.Printf("  password      %s\n", result.AdminPassword)
		fmt.Printf("  customerId    %s\n", result.CustomerPublicID)
		fmt.Printf("  serviceCode   %s\n", result.ServiceCode)
		fmt.Printf("  lane          %s -> %s\n\n", result.OriginPincode, result.DestPincode)
		return nil

	case "seed":
		if *seedFile == "" {
			return errors.New("seed requires --file")
		}
		raw, err := readSeed(*seedFile)
		if err != nil {
			return err
		}
		if err := database.ExecScript(ctx, db.Pool, string(raw)); err != nil {
			return fmt.Errorf("seed %s: %w", *seedFile, err)
		}
		log.Info("seed applied", slog.String("file", *seedFile))
		return nil

	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func readSeed(name string) ([]byte, error) {
	raw, err := fs.ReadFile(appdb.Seeds, "seeds/"+name+".sql")
	if err != nil {
		entries, _ := fs.ReadDir(appdb.Seeds, "seeds")
		available := make([]string, 0, len(entries))
		for _, e := range entries {
			available = append(available, e.Name())
		}
		return nil, fmt.Errorf("seed %q not found (available: %v)", name, available)
	}
	return raw, nil
}
