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
	logger.Infow("Connecting to database", "host", cfg.Postgres.Host)

	// Create Ent client
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		logger.Fatalw("Failed to connect to postgres", "error", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run auto migration
	logger.Info("Running database migrations...")

	// Check if we're in dry-run mode
	if *dryRun {
		logger.Info("Dry run mode - printing migration SQL without executing")
		// In dry-run mode, we just print the SQL that would be executed
		err = client.Schema.WriteTo(ctx, os.Stdout)
		if err != nil {
			logger.Fatalw("Failed to generate migration SQL", "error", err)
		}
	} else {
		// Run the actual migration
		err = client.Schema.Create(ctx)
		if err != nil {
			logger.Fatalw("Failed to create schema resources", "error", err)
		}
		logger.Info("Migration completed successfully")
	}

	fmt.Println("Migration process completed")
}
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	_ "github.com/lib/pq"
)

func main() {
	//  Pyroscope integration start
	_, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: "flexprice.migrate",
		ServerAddress:   "http://localhost:4040",
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
		},
	})
	if err != nil {
		panic(err)
	}
	//  Pyroscope integration end

	// Parse command line flags
	dryRun := flag.Bool("dry-run", false, "Print migration SQL without executing it")
	flag.Parse()

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load config: %v", err))
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}

	// Get DSN from config
	dsn := cfg.Postgres.GetDSN()
	logger.Infow("Connecting to database", "host", cfg.Postgres.Host)

	// Create Ent client
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		logger.Fatalw("Failed to connect to postgres", "error", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run auto migration
	logger.Info("Running database migrations...")

	if *dryRun {
		logger.Info("Dry run mode - printing migration SQL without executing")
		err = client.Schema.WriteTo(ctx, os.Stdout)
		if err != nil {
			logger.Fatalw("Failed to generate migration SQL", "error", err)
		}
	} else {
		err = client.Schema.Create(ctx)
		if err != nil {
			logger.Fatalw("Failed to create schema resources", "error", err)
		}
		logger.Info("Migration completed successfully")
	}

	fmt.Println("Migration process completed")
}
