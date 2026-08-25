package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

func newPostgresCmd() *cobra.Command {
	var dryRun bool
	var timeout int
	var file string

	cmd := &cobra.Command{
		Use:   "postgres",
		Short: "Run Ent/PostgreSQL schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runPostgresSQLFile(file, dryRun, timeout)
			}
			return runPostgresMigration(dryRun, timeout)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print migration SQL without executing it")
	cmd.Flags().IntVar(&timeout, "timeout", 300, "Timeout in seconds for the migration")
	cmd.Flags().StringVar(&file, "file", "", "Apply a raw .sql file (e.g. a baseline) instead of Ent auto-migration")

	return cmd
}

// runPostgresSQLFile applies a raw .sql file (e.g. the schema baseline) instead
// of running Ent auto-migration. The file is executed as a single query so
// pg_dump artifacts -- dollar-quoted function bodies, multi-statement bodies --
// replay exactly as `psql -f` would, without a fragile statement splitter.
func runPostgresSQLFile(file string, dryRun bool, timeout int) error {
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	l, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	if dryRun {
		l.Info(ctx, "Dry run mode - printing SQL file without executing", "file", file)
		fmt.Print(string(raw))
		fmt.Println("Migration process completed")
		return nil
	}

	l.Info(ctx, "Applying PostgreSQL SQL file", "host", cfg.Postgres.Host, "file", file)

	db, err := sql.Open("postgres", cfg.Postgres.GetDSN())
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer db.Close()

	// The baseline is a from-empty snapshot with no IF NOT EXISTS on CREATE TABLE,
	// so it must not be replayed over an existing schema. Refuse rather than fail
	// half-way through the file. Mirrors migrations/baseline/apply.sh.
	var existing int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'",
	).Scan(&existing); err != nil {
		return fmt.Errorf("check existing schema: %w", err)
	}
	if existing != 0 {
		return fmt.Errorf("target database already has %d tables in schema public; the baseline expects an empty database (point at a fresh one or drop the schema first)", existing)
	}

	// A single Exec of the whole file uses lib/pq's simple query protocol, which
	// runs every statement in one implicit transaction -- so a failure part-way
	// rolls the whole file back rather than half-applying it.
	if _, err := db.ExecContext(ctx, string(raw)); err != nil {
		return fmt.Errorf("apply %s: %w", file, err)
	}

	l.Info(ctx, "PostgreSQL SQL file applied successfully", "file", file)
	fmt.Println("Migration process completed")
	return nil
}

func runPostgresMigration(dryRun bool, timeout int) error {
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	l, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	dsn := cfg.Postgres.GetDSN()
	l.Info(context.Background(), "Connecting to database", "host", cfg.Postgres.Host)

	client, err := ent.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	l.Info(ctx, "Running database migrations...")

	// Safety guard: never let auto-migration drop or recreate (modify) an index.
	// Ent/Atlas's partial-index predicate comparison is lossy (it false-detects
	// drift between `status = 'published'` and Postgres' canonical
	// `((status)::text = 'published'::text)`), which previously caused ~30 indexes
	// on hot tables to be dropped+recreated on every run, taking exclusive table
	// locks. Adding ModifyIndex to the skip set makes index DDL deliberate (handled
	// via reviewed/versioned migrations), not an auto-migrate side effect. New
	// indexes (AddIndex) are still created. See RCA: prod DDL-lock incident 2026-06-25.
	//
	// IMPORTANT: WithSkipChanges OVERWRITES Ent's default skip set, which is
	// (DropIndex | DropColumn). We MUST re-include both here, or auto-migrate would
	// start dropping columns (data loss). So the set is default + ModifyIndex.
	migrateOpts := []schema.MigrateOption{
		schema.WithSkipChanges(schema.DropIndex | schema.DropColumn | schema.ModifyIndex),
	}

	if dryRun {
		l.Info(ctx, "Dry run mode - printing migration SQL without executing")
		if err := client.Schema.WriteTo(ctx, os.Stdout, migrateOpts...); err != nil {
			return fmt.Errorf("failed to generate migration SQL: %w", err)
		}
	} else {
		if err := client.Schema.Create(ctx, migrateOpts...); err != nil {
			return fmt.Errorf("failed to create schema resources: %w", err)
		}
		l.Info(ctx, "Migration completed successfully")
	}

	fmt.Println("Migration process completed")
	return nil
}
