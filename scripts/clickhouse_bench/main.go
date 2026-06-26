// ClickHouse FINAL benchmark for meter_usage vs feature_usage.
//
// Usage:
//
//	cd <repo root>
//	# point .env at prod ClickHouse (vars listed in connect() below)
//	go run ./scripts/clickhouse_bench/
//
// The script:
//  1. Connects to ClickHouse using env vars (loads .env from cwd if present).
//  2. Resolves external_customer_id for each customer from feature_usage.
//  3. For every (customer × time_range × variant) cell, runs N iterations
//     interleaved so all cells see roughly the same cache state per round.
//  4. Tags every query with a structured log_comment so system.query_log
//     filtering is trivial later.
//  5. Waits for query_log to flush, then prints per-cell and aggregated
//     latency / memory / rows-read stats.
//
// Edit the constants at the top before running. Iterations / sleeps are also
// env-tunable so you don't need to edit the source to bump them.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/joho/godotenv"
)

// ============================================================================
// EDIT THESE
// ============================================================================

const (
	tenantID      = "tenant_01K1TJDVNSN7TWY8CZY870QMNV"
	environmentID = "env_01K1TJDVR40RB831YRTD42K77R"
)

// Customers to benchmark against. externalID may be left "" — the script will
// resolve it from feature_usage at startup.
var customers = []customerEntry{
	{label: "cust1", customerID: "cust_01K7GGF6Z6XV9ZPX7Z7111GJPB"},
	{label: "cust2", customerID: "cust_01KEKPQMM599755BBBJK511NFB"},
}

// Time windows. Edit to cover the latency profile you care about. Naming
// matters — it goes into the log_comment and shows up in the result table.
var timeRanges = []timeRangeEntry{
	{name: "1h", start: mustParse("2026-06-15T14:00:00Z"), end: mustParse("2026-06-15T15:00:00Z")},
	{name: "1d", start: mustParse("2026-06-14T00:00:00Z"), end: mustParse("2026-06-15T00:00:00Z")},
	{name: "1w", start: mustParse("2026-06-08T00:00:00Z"), end: mustParse("2026-06-15T00:00:00Z")},
	{name: "1m", start: mustParse("2026-05-16T00:00:00Z"), end: mustParse("2026-06-16T00:00:00Z")},
}

// ============================================================================
// Tunables (env-overridable)
// ============================================================================

var (
	iterations         = envInt("BENCH_ITERATIONS", 10)
	warmupRuns         = envInt("BENCH_WARMUP_RUNS", 3)
	interQuerySleepMs  = envInt("BENCH_INTER_QUERY_SLEEP_MS", 100)
	queryLogFlushSecs  = envInt("BENCH_QUERY_LOG_FLUSH_SECS", 12)
	perQueryTimeoutSec = envInt("BENCH_PER_QUERY_TIMEOUT_SEC", 120)
)

// Unique per-run tag so repeated invocations don't bleed into each other's
// result aggregation in system.query_log.
var benchRunID = time.Now().UTC().Format("20060102_150405")

// Pipelines/modes/phases. Used both to build log_comments and to drive the
// query plan.
var (
	pipelines  = []string{"meter", "feature"}
	finalModes = []string{"nofinal", "final"}
)

// localTimings holds wall-clock measurements collected per query as a fallback
// for environments where system.query_log isn't directly readable (e.g.
// ClickHouse Cloud without clusterAllReplicas permission). Server-side fields
// come from the native protocol's ProfileInfo + ProfileEvents packets and are
// populated even when query_log is disabled.
type localTiming struct {
	pipeline       string
	finalMode      string
	customer       string
	timeRange      string
	phase          string
	iteration      int
	durationMs     float64
	resultRows     uint64 // ProfileInfo.Rows — rows emitted by the pipeline (final output)
	scanRows       int64  // sum of ProfileEvents[SelectedRows]   — rows read from storage
	scanBytes      int64  // sum of ProfileEvents[SelectedBytes]  — uncompressed bytes read from storage
	readDiskBytes  int64  // sum of ProfileEvents[ReadCompressedBytes] — bytes read from disk after compression
	memPeakBytes   int64  // max of ProfileEvents[MemoryTrackerUsage]  — peak memory used
	err            error
}

var localTimings []localTiming

// queryMetrics collects per-query server-side stats observed via the native
// protocol callbacks. ProfileInfo fires once at the tail of the query with
// the output row/byte/block counts; ProfileEvents fires one or more times
// with name/value pairs sourced from the server's profile_events map. The
// distinction matters: ProfileInfo.Rows is the AGGREGATED OUTPUT (1 for our
// COUNT/SUM queries), whereas ProfileEvents.SelectedRows is the equivalent
// of system.query_log.read_rows — the rows actually scanned.
type queryMetrics struct {
	resultRows    uint64
	resultBytes   uint64
	resultBlocks  uint64
	scanRows      int64
	scanBytes     int64
	readDiskBytes int64
	memPeakBytes  int64
}

// ============================================================================
// Types
// ============================================================================

type customerEntry struct {
	label      string
	customerID string
	externalID string
}

type timeRangeEntry struct {
	name  string
	start time.Time
	end   time.Time
}

// ============================================================================
// Main
// ============================================================================

func main() {
	_ = godotenv.Load() // .env is optional

	conn := connect()
	defer conn.Close()

	if err := conn.Ping(context.Background()); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	log.Printf("connected; bench_run_id=%s", benchRunID)

	// Resolve missing external_customer_ids from feature_usage so the meter
	// queries can filter on the right column.
	for i := range customers {
		if customers[i].externalID != "" {
			continue
		}
		ext, err := resolveExternalID(conn, customers[i].customerID)
		if err != nil {
			log.Fatalf("resolve external_customer_id for %s: %v", customers[i].customerID, err)
		}
		if ext == "" {
			log.Fatalf("no external_customer_id found for customer_id=%s in feature_usage (wrong tenant/env, or no data?)", customers[i].customerID)
		}
		customers[i].externalID = ext
		log.Printf("  resolved %s → external_customer_id=%s", customers[i].customerID, ext)
	}

	// Build the cell plan: every (customer × timeRange × pipeline × finalMode).
	type cell struct {
		pipeline    string
		finalMode   string
		customer    customerEntry
		timeRange   timeRangeEntry
		queryString string
	}
	var cells []cell
	for _, c := range customers {
		for _, tr := range timeRanges {
			for _, p := range pipelines {
				for _, fm := range finalModes {
					cells = append(cells, cell{
						pipeline:    p,
						finalMode:   fm,
						customer:    c,
						timeRange:   tr,
						queryString: buildQuery(p, fm, c, tr),
					})
				}
			}
		}
	}
	log.Printf("plan: %d cells × %d iterations = %d queries (warmup=%d, interleaved)", len(cells), iterations, len(cells)*iterations, warmupRuns)

	// Interleave: iteration outer, cell inner. Each cell gets one shot per
	// round, so cache state across cells is roughly equivalent for a given
	// iteration number.
	start := time.Now()
	for it := 1; it <= iterations; it++ {
		phase := "measure"
		if it <= warmupRuns {
			phase = "warmup"
		}
		for _, c := range cells {
			tag := buildLogComment(c.pipeline, c.finalMode, c.customer.label, c.timeRange.name, phase, it)
			t0 := time.Now()
			m, err := runQuery(conn, c.queryString, tag)
			dur := time.Since(t0)
			localTimings = append(localTimings, localTiming{
				pipeline:      c.pipeline,
				finalMode:     c.finalMode,
				customer:      c.customer.label,
				timeRange:     c.timeRange.name,
				phase:         phase,
				iteration:     it,
				durationMs:    float64(dur.Microseconds()) / 1000.0,
				resultRows:    m.resultRows,
				scanRows:      m.scanRows,
				scanBytes:     m.scanBytes,
				readDiskBytes: m.readDiskBytes,
				memPeakBytes:  m.memPeakBytes,
				err:           err,
			})
			if err != nil {
				log.Printf("[%s] FAILED: %v", tag, err)
			}
			time.Sleep(time.Duration(interQuerySleepMs) * time.Millisecond)
		}
		log.Printf("iteration %d/%d complete (elapsed: %s)", it, iterations, time.Since(start).Truncate(time.Second))
	}

	// Local-timing report — always works, regardless of server-side query_log
	// access. Wall-clock here INCLUDES network RTT, so absolute numbers should
	// not be read as "ClickHouse server time"; ratios between cells on the
	// same connection are the meaningful comparison.
	printLocalResults()

	// Server-side rows/bytes/memory captured via the native protocol's
	// ProfileInfo + ProfileEvents callbacks. These are real server numbers
	// (not wall-clock) and do NOT depend on system.query_log being readable.
	printServerSideResults()

	log.Printf("all runs done; waiting %ds for system.query_log to flush", queryLogFlushSecs)
	time.Sleep(time.Duration(queryLogFlushSecs) * time.Second)

	if err := tryReadQueryLog(conn); err != nil {
		log.Printf("query_log read failed: %v", err)
		log.Printf("  hint: set BENCH_QUERY_LOG_TABLE to the correct accessor for your")
		log.Printf("  cluster (e.g. \"clusterAllReplicas('default', system.query_log)\"")
		log.Printf("  or \"clusterAllReplicas('{cluster}', system.query_log)\"). Local")
		log.Printf("  wall-clock results above are still valid for FINAL vs no-FINAL ratios.")
	}
}

// ============================================================================
// Connection
// ============================================================================

// connect reads ClickHouse credentials from env. Honored vars:
//
//	CLICKHOUSE_ADDR        host:port (preferred; e.g. "ch.example.com:9440")
//	CLICKHOUSE_HOST        used when CLICKHOUSE_ADDR is unset
//	CLICKHOUSE_PORT        used when CLICKHOUSE_ADDR is unset (default 9000)
//	CLICKHOUSE_USERNAME    default: "default"
//	CLICKHOUSE_PASSWORD    default: ""
//	CLICKHOUSE_DATABASE    default: "default"
//	CLICKHOUSE_TLS         "true"/"1" to enable native TLS (default off)
//	CLICKHOUSE_TLS_INSECURE_SKIP_VERIFY  "true"/"1" — skip cert verify (dev only)
func connect() driver.Conn {
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		host := envStr("CLICKHOUSE_HOST", "localhost")
		port := envStr("CLICKHOUSE_PORT", "9000")
		addr = fmt.Sprintf("%s:%s", host, port)
	}

	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: envStr("CLICKHOUSE_DATABASE", "flexprice"),
			Username: envStr("CLICKHOUSE_USERNAME", "default"),
			Password: os.Getenv("CLICKHOUSE_PASSWORD"),
		},
		DialTimeout:     10 * time.Second,
		ConnMaxLifetime: time.Hour,
		Settings: clickhouse.Settings{
			"max_memory_usage": uint64(90 * 1024 * 1024 * 1024), // 90 GB
		},
	}
	if envBool("CLICKHOUSE_TLS", false) {
		opts.TLS = &tls.Config{
			InsecureSkipVerify: envBool("CLICKHOUSE_TLS_INSECURE_SKIP_VERIFY", false),
		}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		log.Fatalf("clickhouse.Open: %v", err)
	}
	return conn
}

// ============================================================================
// Queries
// ============================================================================

func buildQuery(pipeline, finalMode string, c customerEntry, tr timeRangeEntry) string {
	startStr := tr.start.UTC().Format("2006-01-02 15:04:05")
	endStr := tr.end.UTC().Format("2006-01-02 15:04:05")

	finalKW := ""
	if finalMode == "final" {
		finalKW = "FINAL"
	}

	switch pipeline {
	case "meter":
		return fmt.Sprintf(`
			SELECT
				count() AS rows_returned,
				countDistinct(id) AS distinct_ids,
				sum(qty_total) AS sum_qty,
				groupUniqArray(source) AS sources
			FROM meter_usage %s
			WHERE tenant_id            = '%s'
			  AND environment_id       = '%s'
			  AND external_customer_id = '%s'
			  AND timestamp           >= '%s'
			  AND timestamp           <  '%s'
		`, finalKW, tenantID, environmentID, c.externalID, startStr, endStr)

	case "feature":
		// feature_usage uses customer_id (not external) and needs sign != 0
		// when querying without FINAL to skip tombstones.
		signFilter := ""
		if finalMode == "nofinal" {
			signFilter = "AND sign != 0"
		}
		return fmt.Sprintf(`
			SELECT
				count() AS rows_returned,
				countDistinct(id) AS distinct_ids,
				sum(qty_total) AS sum_qty,
				groupUniqArray(source) AS sources
			FROM feature_usage %s
			WHERE tenant_id      = '%s'
			  AND environment_id = '%s'
			  AND customer_id    = '%s'
			  AND timestamp     >= '%s'
			  AND timestamp     <  '%s'
			  %s
		`, finalKW, tenantID, environmentID, c.customerID, startStr, endStr, signFilter)
	}

	panic("unknown pipeline: " + pipeline)
}

// buildLogComment produces a pipe-separated tag we can parse back out in the
// query_log aggregation. Pipe avoids collisions with underscores inside
// pipeline / mode names.
func buildLogComment(pipeline, finalMode, customer, timeRangeName, phase string, iteration int) string {
	return fmt.Sprintf("bench|%s|%s|%s|%s|%s|run%d|%s",
		pipeline, finalMode, customer, timeRangeName, phase, iteration, benchRunID)
}

func runQuery(conn driver.Conn, query, logComment string) (queryMetrics, error) {
	var m queryMetrics
	ctx := clickhouse.Context(context.Background(),
		clickhouse.WithSettings(clickhouse.Settings{
			"log_comment": logComment,
			// Belt-and-suspenders: ensure this query is eligible for query_log
			// even if the user/profile default has log_queries=0.
			"log_queries": uint8(1),
		}),
		clickhouse.WithProfileInfo(func(p *clickhouse.ProfileInfo) {
			// ProfileInfo reports the OUTPUT of the pipeline (the rows the
			// server actually returns to the client). Take max in case it
			// fires more than once. For COUNT/SUM queries this is ~1 row;
			// the "rows scanned" number comes from ProfileEvents below.
			if p == nil {
				return
			}
			if p.Rows > m.resultRows {
				m.resultRows = p.Rows
			}
			if p.Bytes > m.resultBytes {
				m.resultBytes = p.Bytes
			}
			if p.Blocks > m.resultBlocks {
				m.resultBlocks = p.Blocks
			}
		}),
		clickhouse.WithProfileEvents(func(events []clickhouse.ProfileEvent) {
			// Counters (SelectedRows, SelectedBytes, ReadCompressedBytes)
			// arrive as per-thread, per-snapshot increments — summing all
			// values for a given Name across the entire query yields the
			// same total query_log would record.
			//
			// MemoryTrackerUsage is a gauge: each snapshot reports the
			// query's current memory footprint. The max across all
			// snapshots approximates peak usage (mirrors query_log.memory_usage).
			for _, e := range events {
				switch e.Name {
				case "SelectedRows":
					m.scanRows += e.Value
				case "SelectedBytes":
					m.scanBytes += e.Value
				case "ReadCompressedBytes":
					m.readDiskBytes += e.Value
				case "MemoryTrackerUsage":
					if e.Value > m.memPeakBytes {
						m.memPeakBytes = e.Value
					}
				}
			}
		}),
	)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(perQueryTimeoutSec)*time.Second)
	defer cancel()

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	// Drain rows so CH sees the full query lifecycle. Result values discarded.
	for rows.Next() {
	}
	return m, rows.Err()
}

func resolveExternalID(conn driver.Conn, customerID string) (string, error) {
	q := `
		SELECT external_customer_id
		FROM feature_usage
		WHERE tenant_id      = ?
		  AND environment_id = ?
		  AND customer_id    = ?
		LIMIT 1
	`
	rows, err := conn.Query(context.Background(), q, tenantID, environmentID, customerID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return "", err
		}
		return v, nil
	}
	return "", rows.Err()
}

// ============================================================================
// Reporting
// ============================================================================

// printLocalResults prints latency aggregations using wall-clock timings the
// Go client captured itself. Always available — no server-side permissions
// needed. Includes network RTT, so absolute values are not pure CH server
// time; ratios between cells on the same connection are the comparable metric.
func printLocalResults() {
	type key struct {
		pipeline, finalMode, customer, timeRange string
	}
	buckets := make(map[key][]float64)
	failures := make(map[key]int)
	for _, t := range localTimings {
		if t.phase != "measure" {
			continue
		}
		k := key{t.pipeline, t.finalMode, t.customer, t.timeRange}
		if t.err != nil {
			failures[k]++
			continue
		}
		buckets[k] = append(buckets[k], t.durationMs)
	}

	type row struct {
		key     key
		samples int
		failed  int
		p50     float64
		p95     float64
		p99     float64
		minV    float64
		maxV    float64
		mean    float64
	}
	rows := make([]row, 0, len(buckets))
	for k, vs := range buckets {
		rows = append(rows, row{
			key:     k,
			samples: len(vs),
			failed:  failures[k],
			p50:     quantile(vs, 0.50),
			p95:     quantile(vs, 0.95),
			p99:     quantile(vs, 0.99),
			minV:    sliceMin(vs),
			maxV:    sliceMax(vs),
			mean:    sliceMean(vs),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, bi := rows[i].key, rows[j].key
		if ai.customer != bi.customer {
			return ai.customer < bi.customer
		}
		if ai.timeRange != bi.timeRange {
			return cmpTimeRange(ai.timeRange, bi.timeRange) < 0
		}
		if ai.pipeline != bi.pipeline {
			return ai.pipeline < bi.pipeline
		}
		return ai.finalMode < bi.finalMode
	})

	fmt.Println()
	fmt.Println("LOCAL wall-clock timings (always-available; includes network RTT):")
	fmt.Println(strings.Repeat("-", 110))
	fmt.Printf("%-8s %-6s %-8s %-8s %7s %6s %8s %8s %8s %8s %8s %8s\n",
		"customer", "range", "pipeline", "final", "samples", "fails", "mean", "p50", "p95", "p99", "min", "max")
	fmt.Println(strings.Repeat("-", 110))
	for _, r := range rows {
		fmt.Printf("%-8s %-6s %-8s %-8s %7d %6d %8.1f %8.1f %8.1f %8.1f %8.1f %8.1f\n",
			r.key.customer, r.key.timeRange, r.key.pipeline, r.key.finalMode,
			r.samples, r.failed, r.mean, r.p50, r.p95, r.p99, r.minV, r.maxV)
	}

	// Aggregated FINAL-vs-no-FINAL ratios, rolled up across customers.
	type aggKey struct {
		pipeline, finalMode, timeRange string
	}
	aggBuckets := make(map[aggKey][]float64)
	for _, t := range localTimings {
		if t.phase != "measure" || t.err != nil {
			continue
		}
		k := aggKey{pipeline: t.pipeline, finalMode: t.finalMode, timeRange: t.timeRange}
		aggBuckets[k] = append(aggBuckets[k], t.durationMs)
	}

	type aggRow struct {
		pipeline, finalMode, timeRange string
		p50, p95, mean                 float64
	}
	aggRows := make([]aggRow, 0, len(aggBuckets))
	for k, vals := range aggBuckets {
		aggRows = append(aggRows, aggRow{
			pipeline:  k.pipeline,
			finalMode: k.finalMode,
			timeRange: k.timeRange,
			p50:       quantile(vals, 0.50),
			p95:       quantile(vals, 0.95),
			mean:      sliceMean(vals),
		})
	}
	sort.Slice(aggRows, func(i, j int) bool {
		if aggRows[i].pipeline != aggRows[j].pipeline {
			return aggRows[i].pipeline < aggRows[j].pipeline
		}
		if aggRows[i].timeRange != aggRows[j].timeRange {
			return cmpTimeRange(aggRows[i].timeRange, aggRows[j].timeRange) < 0
		}
		return aggRows[i].finalMode < aggRows[j].finalMode
	})

	fmt.Println()
	fmt.Println("LOCAL aggregated (across customers) — primary FINAL-vs-no-FINAL view:")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("%-8s %-6s %-8s %8s %8s %8s\n", "pipeline", "range", "final", "mean", "p50", "p95")
	fmt.Println(strings.Repeat("-", 70))
	for i, r := range aggRows {
		fmt.Printf("%-8s %-6s %-8s %8.1f %8.1f %8.1f", r.pipeline, r.timeRange, r.finalMode, r.mean, r.p50, r.p95)
		// Inline FINAL/no-FINAL ratio when this is the "final" row paired with
		// the prior "nofinal" row (sort order: nofinal before final per range).
		if r.finalMode == "final" && i > 0 {
			prev := aggRows[i-1]
			if prev.pipeline == r.pipeline && prev.timeRange == r.timeRange && prev.finalMode == "nofinal" && prev.p95 > 0 {
				fmt.Printf("   ← p95 ratio vs no-FINAL: %.2fx", r.p95/prev.p95)
			}
		}
		fmt.Println()
	}
	fmt.Println()
}

// printServerSideResults summarises the work each query did, captured via
// the native protocol callbacks (ProfileInfo + ProfileEvents). These are
// real server numbers populated even when system.query_log is disabled.
//
// Columns:
//   - scan_rows  : ProfileEvents[SelectedRows]        ≈ query_log.read_rows
//   - scan_mb    : ProfileEvents[SelectedBytes] / MiB ≈ query_log.read_bytes (uncompressed)
//   - disk_mb    : ProfileEvents[ReadCompressedBytes] / MiB (post-compression disk reads)
//   - mem_peak_mb: max(ProfileEvents[MemoryTrackerUsage]) / MiB ≈ query_log.memory_usage
func printServerSideResults() {
	type key struct {
		pipeline, finalMode, customer, timeRange string
	}
	type agg struct {
		samples                                                       int
		sumScanRows, sumScanBytes, sumReadDisk                        int64
		maxScanRows, maxScanBytes, maxReadDisk, maxMem                int64
		sumMem                                                        int64
		anyProfileObserved                                            bool
	}
	buckets := make(map[key]*agg)
	for _, t := range localTimings {
		if t.phase != "measure" || t.err != nil {
			continue
		}
		k := key{t.pipeline, t.finalMode, t.customer, t.timeRange}
		a := buckets[k]
		if a == nil {
			a = &agg{}
			buckets[k] = a
		}
		a.samples++
		a.sumScanRows += t.scanRows
		a.sumScanBytes += t.scanBytes
		a.sumReadDisk += t.readDiskBytes
		a.sumMem += t.memPeakBytes
		if t.scanRows > a.maxScanRows {
			a.maxScanRows = t.scanRows
		}
		if t.scanBytes > a.maxScanBytes {
			a.maxScanBytes = t.scanBytes
		}
		if t.readDiskBytes > a.maxReadDisk {
			a.maxReadDisk = t.readDiskBytes
		}
		if t.memPeakBytes > a.maxMem {
			a.maxMem = t.memPeakBytes
		}
		if t.scanRows > 0 || t.scanBytes > 0 || t.memPeakBytes > 0 {
			a.anyProfileObserved = true
		}
	}

	type row struct {
		k                                                                 key
		samples                                                           int
		meanScanRows, meanScanMB, meanDiskMB, meanMemMB                   float64
		maxScanRows, maxScanMB, maxDiskMB, maxMemMB                       float64
		anyObs                                                            bool
	}
	rows := make([]row, 0, len(buckets))
	mib := float64(1024 * 1024)
	for k, a := range buckets {
		n := float64(a.samples)
		rows = append(rows, row{
			k:            k,
			samples:      a.samples,
			meanScanRows: float64(a.sumScanRows) / n,
			meanScanMB:   float64(a.sumScanBytes) / n / mib,
			meanDiskMB:   float64(a.sumReadDisk) / n / mib,
			meanMemMB:    float64(a.sumMem) / n / mib,
			maxScanRows:  float64(a.maxScanRows),
			maxScanMB:    float64(a.maxScanBytes) / mib,
			maxDiskMB:    float64(a.maxReadDisk) / mib,
			maxMemMB:     float64(a.maxMem) / mib,
			anyObs:       a.anyProfileObserved,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, bi := rows[i].k, rows[j].k
		if ai.customer != bi.customer {
			return ai.customer < bi.customer
		}
		if ai.timeRange != bi.timeRange {
			return cmpTimeRange(ai.timeRange, bi.timeRange) < 0
		}
		if ai.pipeline != bi.pipeline {
			return ai.pipeline < bi.pipeline
		}
		return ai.finalMode < bi.finalMode
	})

	fmt.Println()
	fmt.Println("SERVER-SIDE per-cell metrics (from native-protocol ProfileInfo / ProfileEvents):")
	fmt.Println(strings.Repeat("-", 140))
	fmt.Printf("%-8s %-6s %-8s %-8s %7s %14s %12s %12s %12s %14s %12s %12s\n",
		"customer", "range", "pipeline", "final", "samples",
		"mean_scan_rows", "mean_scan_mb", "max_scan_mb",
		"mean_disk_mb", "max_scan_rows", "mean_mem_mb", "max_mem_mb")
	fmt.Println(strings.Repeat("-", 140))
	anyObservedOverall := false
	for _, r := range rows {
		if r.anyObs {
			anyObservedOverall = true
		}
		fmt.Printf("%-8s %-6s %-8s %-8s %7d %14.0f %12.2f %12.2f %12.2f %14.0f %12.2f %12.2f\n",
			r.k.customer, r.k.timeRange, r.k.pipeline, r.k.finalMode,
			r.samples, r.meanScanRows, r.meanScanMB, r.maxScanMB,
			r.meanDiskMB, r.maxScanRows, r.meanMemMB, r.maxMemMB)
	}
	if !anyObservedOverall {
		fmt.Println()
		fmt.Println("  WARNING: all server-side counters are zero. The clickhouse-go ProfileInfo /")
		fmt.Println("  ProfileEvents callbacks did not receive any packets from the server. This")
		fmt.Println("  usually means the connection is not using the native protocol, or the")
		fmt.Println("  server is unusually old. Wall-clock numbers above are still valid.")
	}
	fmt.Println()

	// Aggregated FINAL-vs-no-FINAL view of the work done. This is the
	// most useful summary for "did FINAL make the query do meaningfully
	// more I/O / memory than no-FINAL?"
	type aggKey struct {
		pipeline, finalMode, timeRange string
	}
	type aggVal struct {
		samples                                  int
		scanRows, scanBytes, readDisk, mem       int64
	}
	aggBuckets := make(map[aggKey]*aggVal)
	for _, t := range localTimings {
		if t.phase != "measure" || t.err != nil {
			continue
		}
		k := aggKey{t.pipeline, t.finalMode, t.timeRange}
		v := aggBuckets[k]
		if v == nil {
			v = &aggVal{}
			aggBuckets[k] = v
		}
		v.samples++
		v.scanRows += t.scanRows
		v.scanBytes += t.scanBytes
		v.readDisk += t.readDiskBytes
		v.mem += t.memPeakBytes
	}
	type aggRow struct {
		pipeline, finalMode, timeRange       string
		samples                              int
		meanScanRows, meanScanMB, meanMemMB  float64
	}
	aggRows := make([]aggRow, 0, len(aggBuckets))
	for k, v := range aggBuckets {
		n := float64(v.samples)
		aggRows = append(aggRows, aggRow{
			pipeline:     k.pipeline,
			finalMode:    k.finalMode,
			timeRange:    k.timeRange,
			samples:      v.samples,
			meanScanRows: float64(v.scanRows) / n,
			meanScanMB:   float64(v.scanBytes) / n / mib,
			meanMemMB:    float64(v.mem) / n / mib,
		})
	}
	sort.Slice(aggRows, func(i, j int) bool {
		if aggRows[i].pipeline != aggRows[j].pipeline {
			return aggRows[i].pipeline < aggRows[j].pipeline
		}
		if aggRows[i].timeRange != aggRows[j].timeRange {
			return cmpTimeRange(aggRows[i].timeRange, aggRows[j].timeRange) < 0
		}
		return aggRows[i].finalMode < aggRows[j].finalMode
	})
	fmt.Println("SERVER-SIDE aggregated (across customers) — FINAL-vs-no-FINAL work:")
	fmt.Println(strings.Repeat("-", 100))
	fmt.Printf("%-8s %-6s %-8s %14s %12s %12s\n",
		"pipeline", "range", "final", "mean_scan_rows", "mean_scan_mb", "mean_mem_mb")
	fmt.Println(strings.Repeat("-", 100))
	for i, r := range aggRows {
		fmt.Printf("%-8s %-6s %-8s %14.0f %12.2f %12.2f",
			r.pipeline, r.timeRange, r.finalMode, r.meanScanRows, r.meanScanMB, r.meanMemMB)
		if r.finalMode == "final" && i > 0 {
			prev := aggRows[i-1]
			if prev.pipeline == r.pipeline && prev.timeRange == r.timeRange && prev.finalMode == "nofinal" && prev.meanScanRows > 0 {
				fmt.Printf("   ← scan_rows ratio vs no-FINAL: %.2fx", r.meanScanRows/prev.meanScanRows)
			}
		}
		fmt.Println()
	}
	fmt.Println()
}

// tryReadQueryLog attempts to fetch server-side query metrics. Different CH
// deployments expose system.query_log differently:
//   - single-node:           FROM system.query_log
//   - replicated / Cloud:    FROM clusterAllReplicas('<cluster>', system.query_log)
//
// Discovery: we probe system.clusters and system.tables on the coordinator
// to build the candidate list dynamically. The BENCH_QUERY_LOG_TABLE override
// is always tried first.
func tryReadQueryLog(conn driver.Conn) error {
	candidates := discoverQueryLogAccessors(conn)
	if len(candidates) == 0 {
		return fmt.Errorf("no query_log accessors to try (discovery returned nothing)")
	}

	var lastErr error
	for _, tbl := range candidates {
		log.Printf("trying query_log accessor: %s", tbl)
		if err := readPerCell(conn, tbl); err != nil {
			lastErr = err
			log.Printf("  failed: %v", err)
			continue
		}
		// First call succeeded; the aggregate read uses the same accessor.
		if err := readAggregated(conn, tbl); err != nil {
			log.Printf("  aggregated read failed (per-cell succeeded): %v", err)
			return err
		}
		return nil
	}
	return lastErr
}

// discoverQueryLogAccessors builds an ordered list of FROM-clause expressions
// to probe. We try, in order:
//  1. BENCH_QUERY_LOG_TABLE env override
//  2. plain system.query_log if it exists on the coordinator
//  3. clusterAllReplicas('<name>', system.query_log) for each cluster seen
//     in system.clusters
//  4. the original hardcoded defaults as a last resort
//
// All failures during discovery are logged but non-fatal — the caller still
// gets a candidate list (possibly just the static fallbacks) to try.
func discoverQueryLogAccessors(conn driver.Conn) []string {
	var candidates []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		candidates = append(candidates, s)
	}

	if env := os.Getenv("BENCH_QUERY_LOG_TABLE"); env != "" {
		log.Printf("query_log discovery: honoring BENCH_QUERY_LOG_TABLE=%s", env)
		add(env)
	}

	log.Printf("query_log discovery: probing system.tables for system.query_log")
	exists, err := tableExists(conn, "system", "query_log")
	switch {
	case err != nil:
		log.Printf("  system.tables probe failed: %v", err)
	case exists:
		log.Printf("  system.query_log present on coordinator")
		add("system.query_log")
	default:
		log.Printf("  system.query_log NOT present on coordinator (likely disabled or replicated-only)")
	}

	log.Printf("query_log discovery: listing clusters from system.clusters")
	clusters, err := listClusters(conn)
	if err != nil {
		log.Printf("  system.clusters probe failed: %v", err)
	} else {
		log.Printf("  clusters: %v", clusters)
		for _, c := range clusters {
			add(fmt.Sprintf("clusterAllReplicas('%s', system.query_log)", c))
		}
	}

	// Static fallbacks (in case discovery missed something).
	add("clusterAllReplicas('default', system.query_log)")
	add("system.query_log")

	return candidates
}

func tableExists(conn driver.Conn, database, name string) (bool, error) {
	q := `SELECT count() FROM system.tables WHERE database = ? AND name = ?`
	rows, err := conn.Query(context.Background(), q, database, name)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var n uint64
		if err := rows.Scan(&n); err != nil {
			return false, err
		}
		return n > 0, nil
	}
	return false, rows.Err()
}

func listClusters(conn driver.Conn) ([]string, error) {
	rows, err := conn.Query(context.Background(),
		`SELECT cluster FROM system.clusters GROUP BY cluster ORDER BY cluster`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func readPerCell(conn driver.Conn, tbl string) error {
	q := fmt.Sprintf(`
		SELECT
			splitByChar('|', log_comment)[2] AS pipeline,
			splitByChar('|', log_comment)[3] AS final_mode,
			splitByChar('|', log_comment)[4] AS customer,
			splitByChar('|', log_comment)[5] AS time_range,
			count() AS samples,
			round(quantile(0.50)(query_duration_ms), 1) AS p50_ms,
			round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
			round(quantile(0.99)(query_duration_ms), 1) AS p99_ms,
			round(min(query_duration_ms), 1)            AS min_ms,
			round(max(query_duration_ms), 1)            AS max_ms,
			round(avg(memory_usage) / 1024 / 1024, 1)   AS avg_mem_mb,
			round(avg(read_rows))                       AS avg_rows_read,
			round(avg(read_bytes) / 1024 / 1024, 1)     AS avg_mb_read
		FROM %s
		WHERE log_comment LIKE 'bench|%%|%%|%%|%%|measure|%%|%s'
		  AND type        = 'QueryFinish'
		  AND event_time  > now() - INTERVAL 2 HOUR
		GROUP BY pipeline, final_mode, customer, time_range
		ORDER BY customer, time_range, pipeline, final_mode
	`, tbl, benchRunID)

	rows, err := conn.Query(context.Background(), q)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println()
	fmt.Printf("SERVER-SIDE per-cell results (from %s):\n", tbl)
	fmt.Println(strings.Repeat("-", 140))
	fmt.Printf("%-8s %-6s %-8s %-8s %7s %8s %8s %8s %8s %8s %10s %12s %10s\n",
		"customer", "range", "pipeline", "final", "samples",
		"p50_ms", "p95_ms", "p99_ms", "min_ms", "max_ms",
		"mem_mb", "rows_read", "mb_read")
	fmt.Println(strings.Repeat("-", 140))

	any := false
	for rows.Next() {
		var pipeline, finalMode, customer, timeRange string
		var samples uint64
		var p50, p95, p99, minMs, maxMs, avgMem, avgRows, avgMB float64
		if err := rows.Scan(&pipeline, &finalMode, &customer, &timeRange, &samples,
			&p50, &p95, &p99, &minMs, &maxMs, &avgMem, &avgRows, &avgMB); err != nil {
			return err
		}
		fmt.Printf("%-8s %-6s %-8s %-8s %7d %8.1f %8.1f %8.1f %8.1f %8.1f %10.1f %12.0f %10.1f\n",
			customer, timeRange, pipeline, finalMode, samples,
			p50, p95, p99, minMs, maxMs, avgMem, avgRows, avgMB)
		any = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !any {
		return fmt.Errorf("no rows in query_log for this run (table reachable but log_comment filter matched nothing — check user has log_comment SETTINGS allowed)")
	}
	return nil
}

func readAggregated(conn driver.Conn, tbl string) error {
	q := fmt.Sprintf(`
		SELECT
			splitByChar('|', log_comment)[2] AS pipeline,
			splitByChar('|', log_comment)[3] AS final_mode,
			splitByChar('|', log_comment)[5] AS time_range,
			count() AS samples,
			round(quantile(0.50)(query_duration_ms), 1) AS p50_ms,
			round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
			round(avg(memory_usage) / 1024 / 1024, 1)   AS avg_mem_mb,
			round(avg(read_rows))                       AS avg_rows_read
		FROM %s
		WHERE log_comment LIKE 'bench|%%|%%|%%|%%|measure|%%|%s'
		  AND type        = 'QueryFinish'
		  AND event_time  > now() - INTERVAL 2 HOUR
		GROUP BY pipeline, final_mode, time_range
		ORDER BY pipeline, time_range, final_mode
	`, tbl, benchRunID)

	rows, err := conn.Query(context.Background(), q)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println()
	fmt.Println("SERVER-SIDE aggregated:")
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("%-8s %-6s %-8s %7s %8s %8s %10s %12s\n",
		"pipeline", "range", "final", "samples", "p50_ms", "p95_ms", "mem_mb", "rows_read")
	fmt.Println(strings.Repeat("-", 90))

	for rows.Next() {
		var pipeline, finalMode, timeRange string
		var samples uint64
		var p50, p95, avgMem, avgRows float64
		if err := rows.Scan(&pipeline, &finalMode, &timeRange, &samples, &p50, &p95, &avgMem, &avgRows); err != nil {
			return err
		}
		fmt.Printf("%-8s %-6s %-8s %7d %8.1f %8.1f %10.1f %12.0f\n",
			pipeline, timeRange, finalMode, samples, p50, p95, avgMem, avgRows)
	}
	return rows.Err()
}

// quantile returns the value at quantile q in [0,1] from a copy of the input.
func quantile(in []float64, q float64) float64 {
	if len(in) == 0 {
		return 0
	}
	v := make([]float64, len(in))
	copy(v, in)
	sort.Float64s(v)
	idx := int(float64(len(v)-1) * q)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(v) {
		idx = len(v) - 1
	}
	return v[idx]
}

func sliceMean(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	var s float64
	for _, v := range in {
		s += v
	}
	return s / float64(len(in))
}

func sliceMin(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	m := in[0]
	for _, v := range in[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func sliceMax(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	m := in[0]
	for _, v := range in[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// cmpTimeRange orders 1h < 1d < 1w < 1m by the unit suffix.
func cmpTimeRange(a, b string) int {
	order := map[string]int{"1h": 0, "1d": 1, "1w": 2, "1m": 3}
	ai, ok := order[a]
	if !ok {
		ai = 99
	}
	bi, ok := order[b]
	if !ok {
		bi = 99
	}
	switch {
	case ai < bi:
		return -1
	case ai > bi:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// ============================================================================
// Helpers
// ============================================================================

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		log.Fatalf("invalid time %q: %v", s, err)
	}
	return t
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("warn: %s=%q not an int; using default %d", k, v, def)
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
