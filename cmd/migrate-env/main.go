// Command migrate-env runs Ent schema migrations using a DSN from the environment
// or -dsn flag. Intended for CI/CD only; local dev should use cmd/migrate with config.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	_ "github.com/lib/pq"
)

const dsnEnvVar = "ENT_MIGRATE_POSTGRES_DSN"

func main() {
	dsnFlag := flag.String("dsn", "", "Postgres DSN (overrides "+dsnEnvVar+")")
	dryRun := flag.Bool("dry-run", false, "Print migration SQL without executing it")
	timeout := flag.Int("timeout", 300, "Timeout in seconds for the migration")
	flag.Parse()

	dsn := strings.TrimSpace(*dsnFlag)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv(dsnEnvVar))
	}
	if dsn == "" {
		log.Fatalf("missing DSN: set -dsn flag or %s", dsnEnvVar)
	}

	host := dsnHostForLog(dsn)
	log.Printf("connecting to postgres host=%s", host)

	client, err := ent.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	if *dryRun {
		log.Print("dry run: printing migration SQL")
		if err := client.Schema.WriteTo(ctx, os.Stdout); err != nil {
			log.Fatalf("generate migration SQL: %v", err)
		}
	} else {
		log.Print("running database migrations")
		if err := client.Schema.Create(ctx); err != nil {
			log.Fatalf("create schema: %v", err)
		}
		log.Print("migration completed successfully")
	}

	fmt.Println("Migration process completed")
}

func dsnHostForLog(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil && u.Host != "" {
			return u.Hostname()
		}
	}
	for _, field := range strings.Fields(dsn) {
		if strings.HasPrefix(field, "host=") {
			return strings.TrimPrefix(field, "host=")
		}
	}
	return "(redacted)"
}
