package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/config"
)

// runClickHouseMigrations applies every migrations/clickhouse/*.sql file (in
// filename order) against ClickHouse. All statements use `IF NOT EXISTS`, so
// re-runs are idempotent and no migration-tracking table is needed.
//
// It ensures the target database exists first, then executes each statement in
// each file individually (the native protocol runs one statement per Exec).
func runClickHouseMigrations(ctx context.Context, cfg *config.Configuration, dir string) error {
	opts := cfg.ClickHouse.GetClientOptions()
	db := cfg.ClickHouse.Database

	// Connect without pinning a database so we can CREATE DATABASE if missing.
	bootstrap := *opts
	bootstrap.Auth.Database = "default"
	conn, err := clickhouse_go.Open(&bootstrap)
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer conn.Close()

	if err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", db)); err != nil {
		return fmt.Errorf("create database %q: %w", db, err)
	}
	conn.Close()

	// Reconnect scoped to the target database so unqualified names resolve.
	dbConn, err := clickhouse_go.Open(opts)
	if err != nil {
		return fmt.Errorf("open clickhouse (db=%s): %w", db, err)
	}
	defer dbConn.Close()

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("glob %s: %w", dir, err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no .sql files found in %s", dir)
	}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		for i, stmt := range splitSQL(string(raw)) {
			if err := dbConn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("%s statement %d: %w\n---\n%s", filepath.Base(f), i+1, err, stmt)
			}
		}
		fmt.Printf(">>> applied %s\n", filepath.Base(f))
	}
	return nil
}

// splitSQL strips `--` line comments and `/* */` block comments, then splits on
// `;` into individual statements. The CH migration files contain no semicolons
// inside string literals, so a literal-aware scanner is unnecessary. Empty
// statements are dropped.
func splitSQL(sql string) []string {
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		// Block comment /* ... */
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				break // unterminated; ignore remainder
			}
			i += end + 3
			continue
		}
		// Line comment -- ... \n
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			nl := strings.IndexByte(sql[i:], '\n')
			if nl < 0 {
				break
			}
			i += nl
			b.WriteByte('\n')
			continue
		}
		b.WriteByte(sql[i])
	}

	var out []string
	for _, part := range strings.Split(b.String(), ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
