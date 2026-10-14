-- Lets self-billing's hourly count (filtered only on ingested_at) skip old granules;
-- events is partitioned by toYYYYMMDD(timestamp), so partition pruning cannot help that filter.
-- Apply in each deployment (this file or by hand). ADD INDEX only covers parts written afterwards;
-- index existing parts once, off-peak (background mutation, rewrites index files only, watch system.mutations):
--   ALTER TABLE flexprice.events MATERIALIZE INDEX mm_ingested_at;
ALTER TABLE flexprice.events
    ADD INDEX IF NOT EXISTS mm_ingested_at ingested_at TYPE minmax GRANULARITY 1;
