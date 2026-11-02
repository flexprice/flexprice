-- migrate:up transaction:false
-- entity_integration_mappings is live, so the build is CONCURRENTLY. statement_timeout must be 0
-- on the connection; a build killed by a timeout leaves an INVALID index behind. No IF NOT EXISTS,
-- so a retry after such a failure fails loudly. Check first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE UNIQUE INDEX CONCURRENTLY "idx_entity_integration_mapping_connection_unique" ON "entity_integration_mappings" ("tenant_id", "environment_id", "entity_type", "entity_id", "provider_type", "connection_id") WHERE ((status)::text = 'published'::text);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_entity_integration_mapping_connection_unique";
