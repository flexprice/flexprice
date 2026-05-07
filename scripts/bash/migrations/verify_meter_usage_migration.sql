-- =============================================================================
-- verify_meter_usage_migration.sql
--
-- Run these queries after the migration to check correctness.
-- All counts / sums are computed on deduplicated (FINAL) views.
-- Each query is bounded to 90 GB memory per CLAUDE.md guidelines.
-- =============================================================================

-- ─────────────────────────────────────────────────────────────────────────────
-- 1. ROW COUNT COMPARISON  (per day)
--    Source counts only rows that would have been migrated
--    (meter_id IS NOT NULL AND sign = 1, deduplicated via FINAL).
-- ─────────────────────────────────────────────────────────────────────────────
SELECT
    toYYYYMMDD(day)                 AS partition_day,
    src_rows,
    dst_rows,
    dst_rows - src_rows             AS delta,
    if(src_rows = 0, NULL,
       round(dst_rows / src_rows * 100, 2)) AS pct
FROM (
    SELECT
        toStartOfDay(timestamp)     AS day,
        count()                     AS src_rows
    FROM flexprice.feature_usage FINAL
    WHERE meter_id IS NOT NULL
      AND sign = 1
    GROUP BY day
) src
FULL OUTER JOIN (
    SELECT
        toStartOfDay(timestamp)     AS day,
        count()                     AS dst_rows
    FROM flexprice.meter_usage FINAL
    GROUP BY day
) dst USING (day)
ORDER BY partition_day
SETTINGS max_memory_usage = 90000000000;


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. QUANTITY SUM COMPARISON  (total, per day)
--    Small differences are expected only at the Decimal(25,15) → Decimal(18,8)
--    rounding boundary.  Anything beyond ~1e-8 per row warrants investigation.
-- ─────────────────────────────────────────────────────────────────────────────
SELECT
    toYYYYMMDD(day)                             AS partition_day,
    round(src_sum, 8)                           AS src_qty_sum,
    dst_sum                                     AS dst_qty_sum,
    round(abs(toFloat64(src_sum) - toFloat64(dst_sum)), 8) AS abs_diff
FROM (
    SELECT
        toStartOfDay(timestamp)    AS day,
        sum(qty_total)             AS src_sum
    FROM flexprice.feature_usage FINAL
    WHERE meter_id IS NOT NULL
      AND sign = 1
    GROUP BY day
) src
JOIN (
    SELECT
        toStartOfDay(timestamp)    AS day,
        sum(qty_total)             AS dst_sum
    FROM flexprice.meter_usage FINAL
    GROUP BY day
) dst USING (day)
ORDER BY partition_day
SETTINGS max_memory_usage = 90000000000;


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. SPOT-CHECK: rows in destination that have no match in source
--    (should be 0 for any migrated id)
--    Uses LEFT JOIN anti-join pattern — avoids expensive NOT EXISTS correlated subquery.
-- ─────────────────────────────────────────────────────────────────────────────
SELECT count() AS orphan_count
FROM flexprice.meter_usage FINAL mu
LEFT JOIN flexprice.feature_usage FINAL fu ON fu.id = mu.id
WHERE fu.id = ''
SETTINGS max_memory_usage = 90000000000;


-- ─────────────────────────────────────────────────────────────────────────────
-- 4. NULL HYGIENE CHECK on destination
--    All these should return 0 — destination defines them as NOT NULL.
-- ─────────────────────────────────────────────────────────────────────────────
SELECT
    countIf(meter_id    = '')   AS blank_meter_id,
    countIf(unique_hash IS NULL) AS null_unique_hash,   -- can't happen, but defensive
    countIf(source      IS NULL) AS null_source
FROM flexprice.meter_usage FINAL
WHERE toYYYYMMDD(timestamp) >= 20240101   -- restrict to migrated range
SETTINGS max_memory_usage = 90000000000;


-- ─────────────────────────────────────────────────────────────────────────────
-- 5. PROGRESS TRACKER  (run any time during the migration)
--    Shows how many days have been populated so far.
-- ─────────────────────────────────────────────────────────────────────────────
SELECT
    toYYYYMMDD(timestamp)   AS partition_day,
    count()                 AS rows_in_dst,
    sum(qty_total)          AS total_qty,
    min(timestamp)          AS earliest,
    max(timestamp)          AS latest
FROM flexprice.meter_usage
-- No FINAL here — we just want a fast partition-level overview
GROUP BY partition_day
ORDER BY partition_day DESC
LIMIT 60;
