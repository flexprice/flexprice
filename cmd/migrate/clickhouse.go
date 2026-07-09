package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

// validCHIdent guards database identifiers interpolated into DDL. ClickHouse
// does not support parameterized identifiers, so an allowlist is the only way
// to keep a misconfigured value from producing broken/unexpected DDL.
var validCHIdent = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// runClickHouseMigrations applies every migrations/clickhouse/*.sql file (in
// filename order) against ClickHouse. All statements use `IF NOT EXISTS`, so
// re-runs are idempotent and no migration-tracking table is needed.
//
// It ensures the target database exists first, then executes each statement in
// each file individually (the native protocol runs one statement per Exec).
func runClickHouseMigrations(ctx context.Context, cfg *config.Configuration, dir string, log *logger.Logger) error {
	opts := cfg.ClickHouse.GetClientOptions()
	db := cfg.ClickHouse.Database

	if !validCHIdent.MatchString(db) {
		return fmt.Errorf("invalid clickhouse database name %q", db)
	}

	// Connect without pinning a database so we can CREATE DATABASE if missing.
	// The native protocol works over ClickHouse Cloud PrivateLink now that
	// GetClientOptions sets a DialTimeout (without it, the in-order dial to a
	// PrivateLink AZ ENI could block forever).
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
		stmts, err := splitSQL(string(raw))
		if err != nil {
			return fmt.Errorf("split %s: %w", filepath.Base(f), err)
		}
		for i, stmt := range stmts {
			if err := execWithRetry(ctx, dbConn, stmt); err != nil {
				return fmt.Errorf("%s statement %d: %w\n---\n%s", filepath.Base(f), i+1, err, stmt)
			}
		}
		log.Info(ctx, "applied clickhouse migration", "file", filepath.Base(f))
	}
	return nil
}

// execWithRetry runs a statement, retrying on ClickHouse Cloud's transient
// SharedMergeTree replication races. Rapid consecutive ALTERs (e.g. several
// ADD INDEX on one table) can outrun cross-replica metadata sync, yielding
// "code: 517 ... doesn't catchup with latest ALTER ... Please retry". These are
// self-healing once replicas converge, so a short backoff-retry clears them.
func execWithRetry(ctx context.Context, conn driver.Conn, stmt string) error {
	const maxAttempts = 8
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = conn.Exec(ctx, stmt); err == nil {
			return nil
		}
		msg := err.Error()
		transient := strings.Contains(msg, "code: 517") ||
			strings.Contains(msg, "CANNOT_ASSIGN_ALTER") ||
			strings.Contains(msg, "doesn't catchup")
		if !transient || attempt == maxAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return err
}

// splitSQL strips `--` line comments and `/* */` block comments, then splits on
// `;` into individual statements. Empty statements are dropped.
//
// It is NOT a literal-aware scanner: it does not treat `;`, `--`, or `/* */`
// specially when they appear inside a quoted string literal. The current CH
// migration set contains none, so this is safe today. To keep that assumption
// honest for future authors, splitSQL fails loudly (returns an error) if it
// detects any of those sequences inside a `'`- or `` ` ``-quoted literal rather
// than silently mis-splitting or truncating a statement.
func splitSQL(sql string) ([]string, error) {
	if err := checkNoMarkersInLiterals(sql); err != nil {
		return nil, err
	}

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
	return out, nil
}

// checkNoMarkersInLiterals scans for `;`, `--`, and `/* */` occurring inside a
// single-quoted or backtick-quoted literal, returning an error if found. This
// guards the non-literal-aware splitSQL against silent mis-splitting. `''` and
// backslash escapes inside single quotes are handled so escaped quotes don't
// prematurely close a literal.
func checkNoMarkersInLiterals(sql string) error {
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if c != '\'' && c != '`' {
			continue
		}
		quote := c
		i++
		for i < len(sql) {
			if quote == '\'' && sql[i] == '\\' {
				i += 2 // skip escaped char
				continue
			}
			if sql[i] == quote {
				// doubled quote ('' or ``) = escaped quote, stay in literal
				if i+1 < len(sql) && sql[i+1] == quote {
					i += 2
					continue
				}
				break // closing quote
			}
			switch {
			case sql[i] == ';':
				return fmt.Errorf("splitSQL: %q inside a string literal is not supported", ";")
			case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
				return fmt.Errorf("splitSQL: %q inside a string literal is not supported", "--")
			case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
				return fmt.Errorf("splitSQL: %q inside a string literal is not supported", "/*")
			}
			i++
		}
	}
	return nil
}
