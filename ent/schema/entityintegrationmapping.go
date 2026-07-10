package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

var Idx_entity_integration_mapping_unique = "idx_entity_integration_mapping_unique"

// EntityIntegrationMapping holds the schema definition for the EntityIntegrationMapping entity.
type EntityIntegrationMapping struct {
	ent.Schema
}

// Mixin of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("provider_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("provider_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		// Metadata as JSON field for provider-specific data
		field.JSON("metadata", map[string]interface{}{}).
			Optional(),
	}
}

// Edges of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Edges() []ent.Edge {
	return nil
}

// Indexes of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Indexes() []ent.Index {
	return []ent.Index{
		// Primary composite index with optimal column order for PostgreSQL
		// tenant_id, environment_id, entity_type, entity_id, provider_type
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "provider_type").
			Unique().
			StorageKey(Idx_entity_integration_mapping_unique).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		// Index for provider entity lookups (not covered by primary index)
		index.Fields("provider_type", "provider_entity_id"),
	}
}
