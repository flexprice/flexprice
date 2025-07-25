package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// StripeTenantConfig holds the schema definition for the StripeTenantConfig entity.
type StripeTenantConfig struct {
	ent.Schema
}

// Mixin of the StripeTenantConfig.
func (StripeTenantConfig) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the StripeTenantConfig.
func (StripeTenantConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.Text("api_key_encrypted").
			NotEmpty(),
		field.Bool("sync_enabled").
			Default(true),
		field.Int("aggregation_window_minutes").
			Default(60),
		field.JSON("webhook_config", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the StripeTenantConfig.
func (StripeTenantConfig) Edges() []ent.Edge {
	return nil
}

// Indexes of the StripeTenantConfig.
func (StripeTenantConfig) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup pattern: one config per tenant + environment
		index.Fields("tenant_id", "environment_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Sync enabled filtering for active configurations
		index.Fields("tenant_id", "environment_id", "sync_enabled", "status"),
	}
}
