package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	_ "github.com/lib/pq"
)

func main() {
	// Parse command line flags
	dryRun := flag.Bool("dry-run", false, "Print migration SQL without executing it")
	timeout := flag.Int("timeout", 300, "Timeout in seconds for the migration")
	clickhouse := flag.Bool("clickhouse", false, "Run ClickHouse migrations (migrations/clickhouse/*.sql) instead of Postgres")
	chDir := flag.String("clickhouse-dir", "migrations/clickhouse", "Directory of ClickHouse .sql migration files")
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

	// ClickHouse migrations: separate path, idempotent .sql files.
	if *clickhouse {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
		defer cancel()
		logger.Info(ctx, "Running ClickHouse migrations...", "address", cfg.ClickHouse.Address, "database", cfg.ClickHouse.Database)
		if err := runClickHouseMigrations(ctx, cfg, *chDir); err != nil {
			logger.Fatal(ctx, "ClickHouse migration failed", "error", err)
		}
		logger.Info(ctx, "ClickHouse migrations completed successfully")
		fmt.Println("Migration process completed")
		return
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

	// Check if we're in dry-run mode
	if *dryRun {
		logger.Info(ctx, "Dry run mode - printing migration SQL without executing")
		// In dry-run mode, we just print the SQL that would be executed
		err = client.Schema.WriteTo(ctx, os.Stdout)
		if err != nil {
			logger.Fatal(ctx, "Failed to generate migration SQL", "error", err)
		}
	} else {
		// Run the actual migration
		err = client.Schema.Create(ctx)
		if err != nil {
			logger.Fatal(ctx, "Failed to create schema resources", "error", err)
		}
		logger.Info(ctx, "Migration completed successfully")
	}

	fmt.Println("Migration process completed")
}
