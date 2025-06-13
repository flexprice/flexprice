package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// MeterProviderMapping holds the schema definition for the MeterProviderMapping entity.
type MeterProviderMapping struct {
	ent.Schema
}

// Mixin of the MeterProviderMapping.
func (MeterProviderMapping) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the MeterProviderMapping.
func (MeterProviderMapping) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("meter_id").
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
		field.String("provider_meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable(),
		field.Bool("sync_enabled").
			Default(true),
		field.JSON("configuration", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the MeterProviderMapping.
func (MeterProviderMapping) Edges() []ent.Edge {
	return nil
}

// Indexes of the MeterProviderMapping.
func (MeterProviderMapping) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup pattern: tenant + environment + meter + provider
		index.Fields("tenant_id", "environment_id", "meter_id", "provider_type").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Reverse lookup pattern: provider meter ID to FlexPrice meter
		index.Fields("tenant_id", "environment_id", "provider_type", "provider_meter_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Sync enabled filtering for active mappings
		index.Fields("tenant_id", "environment_id", "sync_enabled", "status"),
		// General tenant/environment filtering
		index.Fields("tenant_id", "environment_id", "status"),
	}
}
