-- Lets the self-billing hourly count (filtered only on ingested_at) skip old granules;
-- events is partitioned by toYYYYMMDD(timestamp), so partition pruning cannot help that filter.
-- ADD INDEX only covers parts written afterwards; index existing parts once, off-peak, per deployment
-- (background mutation that rewrites index files only; watch system.mutations):
--   ALTER TABLE flexprice.events MATERIALIZE INDEX mm_ingested_at;
-- No apostrophes in these comments: splitSQL treats them as string literals.
ALTER TABLE flexprice.events
    ADD INDEX IF NOT EXISTS mm_ingested_at ingested_at TYPE minmax GRANULARITY 1;
