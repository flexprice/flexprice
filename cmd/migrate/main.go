package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	_ "github.com/lib/pq"
)

func main() {
	// Parse command line flags
	dryRun := flag.Bool("dry-run", false, "Print migration SQL without executing it")
	timeout := flag.Int("timeout", 300, "Timeout in seconds for the migration")
	flag.Parse()

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Get DSN from config
	dsn := cfg.Postgres.GetDSN()
	logger.Info(context.Background(), "Connecting to database", "host", cfg.Postgres.Host)

	// Create Ent client
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		logger.Fatal(context.Background(), "Failed to connect to postgres", "error", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	// Run auto migration
	logger.Info(ctx, "Running database migrations...")

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

	// Check if we're in dry-run mode
	if *dryRun {
		logger.Info(ctx, "Dry run mode - printing migration SQL without executing")
		// In dry-run mode, we just print the SQL that would be executed
		err = client.Schema.WriteTo(ctx, os.Stdout, migrateOpts...)
		if err != nil {
			logger.Fatal(ctx, "Failed to generate migration SQL", "error", err)
		}
	} else {
		// Run the actual migration
		err = client.Schema.Create(ctx, migrateOpts...)
		if err != nil {
			logger.Fatal(ctx, "Failed to create schema resources", "error", err)
		}
		logger.Info(ctx, "Migration completed successfully")
	}

	fmt.Println("Migration process completed")
}
