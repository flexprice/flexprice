package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

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
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("provider_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("provider_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable(),
		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Edges() []ent.Edge {
	return nil
}

// Indexes of the EntityIntegrationMapping.
func (EntityIntegrationMapping) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup pattern: unique mapping per entity + provider combination
		index.Fields("tenant_id", "environment_id", "entity_id", "entity_type", "provider_type").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Reverse lookup pattern: provider entity ID to FlexPrice entity
		index.Fields("tenant_id", "environment_id", "provider_type", "provider_entity_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Entity type filtering for queries by specific entity types
		index.Fields("tenant_id", "environment_id", "entity_type", "status"),
		// Provider type filtering for queries by specific providers
		index.Fields("tenant_id", "environment_id", "provider_type", "status"),
		// General tenant/environment filtering
		index.Fields("tenant_id", "environment_id", "status"),
	}
}
