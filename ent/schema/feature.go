package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Feature holds the schema definition for the Feature entity.
type Feature struct {
	ent.Schema
}

// Mixin of the Feature.
func (Feature) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (Feature) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("lookup_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Immutable(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.Text("description").
			Optional().
			Nillable(),
		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.String("unit_singular").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("unit_plural").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.JSON("alert_settings", types.AlertSettings{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Indexes of the Feature.
func (Feature) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "lookup_key").
			Unique().
			StorageKey("idx_feature_tenant_env_lookup_key_unique").
			Annotations(entsql.IndexWhere("(lookup_key IS NOT NULL AND lookup_key != '') AND status = 'published'")),
		index.Fields("tenant_id", "environment_id", "meter_id").
			StorageKey("idx_feature_tenant_env_meter_id").
			Annotations(entsql.IndexWhere("meter_id IS NOT NULL")),
		index.Fields("tenant_id", "environment_id", "type").
			StorageKey("idx_feature_tenant_env_type"),
		index.Fields("tenant_id", "environment_id", "status").
			StorageKey("idx_feature_tenant_env_status"),
		index.Fields("tenant_id", "environment_id", "created_at").
			StorageKey("idx_feature_tenant_env_created_at"),
	}
}
