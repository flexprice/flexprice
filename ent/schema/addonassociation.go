package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// AddonAssociation holds the schema definition for the AddonAssociation entity.
type AddonAssociation struct {
	ent.Schema
}

// Mixin of the AddonAssociation.
func (AddonAssociation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the AddonAssociation.
func (AddonAssociation) Fields() []ent.Field {
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

		field.String("addon_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.Time("start_date").
			Optional().
			Nillable().
			Default(time.Now),

		field.Time("end_date").
			Optional().
			Nillable(),

		// Lifecycle management
		field.String("addon_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default(string(types.AddonStatusActive)),

		field.String("cancellation_reason").
			Optional().
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}),

		field.Time("cancelled_at").
			Optional().
			Nillable(),

		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the AddonAssociation.
func (AddonAssociation) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Indexes of the AddonAssociation.
func (AddonAssociation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "entity_id", "entity_type", "addon_id"),
	}
}
