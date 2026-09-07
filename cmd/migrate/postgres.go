package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
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
	var allowIndexChanges bool
	var file string

	cmd := &cobra.Command{
		Use:   "postgres",
		Short: "Run Ent/PostgreSQL schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runPostgresSQLFile(file, dryRun, timeout)
			}
			return runPostgresMigration(dryRun, timeout, allowIndexChanges)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print migration SQL without executing it")
	cmd.Flags().IntVar(&timeout, "timeout", 300, "Timeout in seconds for the migration")
	// CI only. Production keeps ModifyIndex in the skip set (RCA 2026-06-25), which
	// means a changed index is silently never applied and never reported. The schema
	// sync check needs to SEE those changes to fail a PR that forgot the migration,
	// so it runs against a throwaway database with this flag set.
	cmd.Flags().BoolVar(&allowIndexChanges, "allow-index-changes", false,
		"Include index modifications in the diff (CI/draft only; requires FLEXPRICE_MIGRATE_UNSAFE=1)")
	cmd.Flags().StringVar(&file, "file", "", "Apply a raw .sql file (e.g. a baseline) instead of Ent auto-migration")

	// `migrate postgres up` is the deploy path. Bare `migrate postgres` remains
	// Ent auto-migration so a rollback is a values flip, not an image rebuild.
	cmd.AddCommand(newPostgresUpCmd())

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

	raw, err := os.ReadFile(file) // #nosec G304 -- operator-supplied --file flag, CLI tool not a request handler
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	if dryRun {
		l.Info(ctx, "Dry run mode - printing SQL file without executing", "file", file)
		fmt.Print(string(raw))
		fmt.Fprintln(os.Stderr, "Migration process completed")
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
	fmt.Fprintln(os.Stderr, "Migration process completed")
	return nil
}

func runPostgresMigration(dryRun bool, timeout int, allowIndexChanges bool) error {
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
	if allowIndexChanges && os.Getenv("FLEXPRICE_MIGRATE_UNSAFE") != "1" {
		// A flag alone is too easy to reach in a deploy. Removing ModifyIndex from
		// the skip set lets Ent drop and recreate predicate-sensitive indexes under
		// exclusive locks — the 2026-06-25 incident. Requiring a second, deliberate
		// signal means it cannot be turned on by editing a command line.
		return fmt.Errorf("--allow-index-changes requires FLEXPRICE_MIGRATE_UNSAFE=1; " +
			"it is for CI verification against throwaway databases, never a real deployment")
	}
	skip := schema.DropIndex | schema.DropColumn | schema.ModifyIndex
	if allowIndexChanges {
		// CI / draft path only: unskip ModifyIndex so a changed index predicate or
		// column list is visible. Production keeps it skipped (RCA 2026-06-25).
		//
		// DropIndex stays skipped even here. Unskipping it makes Ent propose
		// dropping every index the database has that the Ent schema does not
		// declare — and production has many, some deliberate. That is a report for
		// a human, not DDL to auto-draft. See ORPHANED INDEXES in
		// migrations/versioned/README.md.
		skip = schema.DropIndex | schema.DropColumn
	}
	migrateOpts := []schema.MigrateOption{schema.WithSkipChanges(skip)}

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

	fmt.Fprintln(os.Stderr, "Migration process completed")
	return nil
}

// Connection options the migration files rely on but cannot set themselves.
//
//	lock_timeout=3s      a blocked ALTER gives up rather than queueing every
//	                     query behind it (RCA: prod DDL-lock incident 2026-06-25)
//	statement_timeout=0  a CREATE INDEX CONCURRENTLY killed by a timeout leaves
//	                     an INVALID index that nothing retries
//
// These cannot live in the SQL: a `transaction:false` migration may contain
// exactly one statement, leaving no room for a SET. They have to come from the
// connection, which is why applying by hand with a bare `dbmate up` is not
// equivalent. Kept identical to scripts/migrations/apply.sh.
const migrationConnOptions = "-c lock_timeout=3s -c statement_timeout=0"

// dbmate ships as a binary in the image rather than as a Go dependency: its
// module pulls in a driver set the application has no use for (gorm, BigQuery,
// MySQL, SQLite) and would bloat go.mod for every build. The Dockerfile
// installs a pinned version into /usr/local/bin.
var dbmateSearchPath = []string{"dbmate", "/usr/local/bin/dbmate", "/app/dbmate"}

func newPostgresUpCmd() *cobra.Command {
	var dir string
	var statusOnly bool

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply pending versioned SQL migrations (dbmate)",
		Long: "Apply the migrations in migrations/versioned/postgres that this database\n" +
			"has not recorded yet. Replaces Ent auto-migration as the deploy mechanism:\n" +
			"every change is a reviewed .sql file, and what ran is recorded in\n" +
			"schema_migrations rather than inferred from a live schema diff.",
		// An unreachable database or an unadopted one is an operational failure,
		// not a usage error; printing the flag list under it buries the message
		// that says what to do. main() already reports the error.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersionedMigrations(dir, statusOnly)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "migrations/versioned/postgres",
		"Directory holding the versioned migrations")
	cmd.Flags().BoolVar(&statusOnly, "status", false,
		"List applied and pending migrations, then exit without applying anything")

	return cmd
}

// postgresMigrationURL renders the connection as a URL. GetDSN() returns
// keyword/value form, which dbmate does not accept. url.UserPassword escapes the
// password, so a secret containing @ / : / ? cannot truncate the URL.
func postgresMigrationURL(c config.PostgresConfig) string {
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	q.Set("options", migrationConnOptions)

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     c.Host + ":" + strconv.Itoa(c.Port),
		Path:     "/" + c.DBName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// assertAdopted refuses to touch a database that has tables but no ledger.
//
// The first migration in the timeline is the baseline, which CREATEs every table
// with no IF NOT EXISTS. Running it against a populated database fails on the
// first collision anyway — but with a Postgres error naming an arbitrary table,
// which reads like a bug in the migration rather than a missing setup step. An
// existing deployment must be adopted once (`make migrate-adopt`) so its history
// is recorded and only NEW migrations run against it.
func assertAdopted(ctx context.Context, db *sql.DB) error {
	var tables int
	var hasLedger bool
	err := db.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM information_schema.tables
		     WHERE table_schema = 'public' AND table_type = 'BASE TABLE'),
		  (to_regclass('public.schema_migrations') IS NOT NULL)`,
	).Scan(&tables, &hasLedger)
	if err != nil {
		return fmt.Errorf("inspect target database: %w", err)
	}

	if hasLedger || tables == 0 {
		return nil
	}

	return fmt.Errorf(
		"database has %d tables but no schema_migrations ledger — it has never been adopted.\n"+
			"Adopting records the migrations written so far as already applied and executes zero DDL,\n"+
			"so only new migrations ever run here. From a checkout of this repo:\n\n"+
			"    make migrate-adopt url=\"postgres://USER:PASS@HOST:PORT/DBNAME?sslmode=require\" dry=1\n"+
			"    make migrate-adopt url=\"postgres://USER:PASS@HOST:PORT/DBNAME?sslmode=require\"\n\n"+
			"Refusing to continue: applying the baseline over an existing schema would fail\n"+
			"part-way and leave the ledger empty", tables)
}

// assertNoInvalidIndexes refuses to run while a half-built index exists.
//
// CREATE INDEX CONCURRENTLY that is interrupted -- a killed Job pod, a dropped
// connection, a cancelled backend -- leaves the index in place and marked
// invalid. Postgres will not use it for queries, and, critically,
// `CREATE INDEX CONCURRENTLY IF NOT EXISTS` SKIPS it: a re-run reports success,
// dbmate records the migration as applied, and the index stays invalid forever
// with nothing reporting it.
//
// Failing here turns that silent outcome into a blocked deploy naming the index.
// Rebuilding is a DROP followed by re-running the migration; both are online.
func assertNoInvalidIndexes(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT n.nspname, c.relname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE (NOT i.indisvalid OR NOT i.indisready)
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY 1, 2`)
	if err != nil {
		return fmt.Errorf("check for invalid indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var broken []string
	for rows.Next() {
		var schemaName, indexName string
		if err := rows.Scan(&schemaName, &indexName); err != nil {
			return fmt.Errorf("check for invalid indexes: %w", err)
		}
		broken = append(broken, fmt.Sprintf("%s.%s", schemaName, indexName))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check for invalid indexes: %w", err)
	}
	if len(broken) == 0 {
		return nil
	}

	msg := fmt.Sprintf("%d index(es) are INVALID -- a CREATE INDEX CONCURRENTLY was interrupted:\n", len(broken))
	for _, b := range broken {
		msg += fmt.Sprintf("    %s\n", b)
	}
	msg += "\nPostgres does not use an invalid index, and CREATE INDEX CONCURRENTLY IF NOT EXISTS\n" +
		"skips it silently -- so continuing would record the migration as applied and leave\n" +
		"the index broken. Drop each one, then re-run this deploy:\n\n"
	for _, b := range broken {
		msg += fmt.Sprintf("    DROP INDEX CONCURRENTLY IF EXISTS %s;\n", b)
	}
	msg += "\nDROP INDEX CONCURRENTLY takes no blocking lock and is safe on a live database."
	return fmt.Errorf("%s", msg)
}

func resolveDbmate() (string, error) {
	for _, candidate := range dbmateSearchPath {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("dbmate not found (looked for %v); the deployment image installs it at /usr/local/bin/dbmate", dbmateSearchPath)
}

func runVersionedMigrations(dir string, statusOnly bool) error {
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	bin, err := resolveDbmate()
	if err != nil {
		return err
	}

	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("migrations directory %s: %w", dir, err)
	}

	dsn := postgresMigrationURL(cfg.Postgres)

	// The pre-flight connection is separate from dbmate's and short-lived; a
	// deployment that cannot reach the database should say so in seconds rather
	// than sit until the Job's activeDeadlineSeconds expires.
	checkCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(checkCtx); err != nil {
		return fmt.Errorf("connect to %s:%d/%s: %w", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.DBName, err)
	}
	if err := assertAdopted(checkCtx, db); err != nil {
		return err
	}
	if err := assertNoInvalidIndexes(checkCtx, db); err != nil {
		return err
	}

	action := "up"
	if statusOnly {
		action = "status"
	}

	fmt.Fprintf(os.Stderr, "dbmate %s against %s:%d/%s (dir %s)\n",
		action, cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.DBName, dir)

	// --no-dump-schema: dbmate writes schema.sql after a successful run by
	// default, which needs pg_dump on PATH and a writable tree. Neither holds in
	// the deployment image, and the dump is a local development convenience.
	// No timeout on the migration itself, so the context is never cancelled: a
	// CREATE INDEX CONCURRENTLY on a large table legitimately runs for a long
	// time, and killing it mid-build leaves an INVALID index. The Job's
	// activeDeadlineSeconds (Helm) or the workflow's wait (ECS) is the bound.
	//
	// `bin` is resolved by resolveDbmate from the fixed dbmateSearchPath list,
	// never from input. `dir` is an operator-supplied flag passed as its own argv
	// element, not through a shell, so neither can inject a command.
	c := exec.CommandContext(context.Background(), bin, "--migrations-dir", dir, "--no-dump-schema", action) // #nosec G204 -- bin from fixed candidate list, argv not shell // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	c.Env = append(os.Environ(), "DATABASE_URL="+dsn)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// No timeout on the migration itself. A CREATE INDEX CONCURRENTLY on a large
	// table legitimately runs for a long time, and killing it mid-build leaves an
	// INVALID index. The Job's activeDeadlineSeconds is the bound that matters.
	if err := c.Run(); err != nil {
		return fmt.Errorf("dbmate %s: %w", action, err)
	}
	return nil
}
