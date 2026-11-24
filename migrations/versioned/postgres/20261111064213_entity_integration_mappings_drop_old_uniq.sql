-- migrate:up transaction:false
-- The connection-aware key replaces it. Pods on the previous release name no unique index
-- when inserting, so they keep working once this is gone.
DROP INDEX CONCURRENTLY IF EXISTS "idx_entity_integration_mapping_unique";

-- migrate:down transaction:false
-- Fails if two connections already hold a mapping for the same entity and provider.
CREATE UNIQUE INDEX CONCURRENTLY "idx_entity_integration_mapping_unique" ON "entity_integration_mappings" ("tenant_id", "environment_id", "entity_type", "entity_id", "provider_type") WHERE ((status)::text = 'published'::text);
