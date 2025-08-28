package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Price holds the schema definition for the Price entity.
type Price struct {
	ent.Schema
}

// Mixin of the Price.
func (Price) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Price.
func (Price) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.Float("amount").
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			NotEmpty(),

		field.String("display_amount").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),

		// price_unit_type is the type of the price unit- Fiat, Custom
		field.String("price_unit_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default(string(types.PRICE_UNIT_TYPE_FIAT)),

		// price_unit_id is the id of the price unit
		field.String("price_unit_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		// price_unit is the code of the price unit
		field.String("price_unit").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Optional(),

		// price_unit_amount is the amount of the price unit
		field.Float("price_unit_amount").
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Optional(),

		// display_price_unit_amount is the amount of the price unit in the display currency
		field.String("display_price_unit_amount").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),

		// conversion_rate is the conversion rate of the price unit to the fiat currency
		field.Float("conversion_rate").
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Optional(),

		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.String("billing_period").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.Int("billing_period_count").
			NonNegative(),

		field.String("billing_model").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.String("billing_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.String("invoice_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional(). // TODO: Remove this once we have migrated all the data
			Immutable(),

		field.Int("trial_period").
			Default(0).
			Immutable(),

		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),

		field.JSON("filter_values", map[string][]string{}).
			Optional(),
		field.String("tier_mode").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional().
			Nillable(),

		field.JSON("tiers", []*types.PriceTier{}).
			Optional(),

		field.JSON("price_unit_tiers", []*types.PriceTier{}).
			Optional(),

		field.JSON("transform_quantity", types.TransformQuantity{}).
			Optional(),

		field.String("lookup_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),

		field.Text("description").
			Optional(),

		field.JSON("metadata", map[string]string{}).
			Optional(),

		// Price override fields
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			Nillable().
			Default(string(types.PRICE_ENTITY_TYPE_PLAN)).
			Optional(),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Nillable().
			Optional(),

		field.String("parent_price_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),

		field.Time("start_date").
			Default(time.Now).
			Nillable().
			Immutable(),

		field.Time("end_date").
			Optional().
			Nillable(),
	}
}

// Edges of the Price.
func (Price) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("costsheet", Costsheet.Type),
		edge.To("price_unit_edge", PriceUnit.Type).
			Field("price_unit_id").
			Unique(),
	}
}

// Indexes of the Price.
func (Price) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "lookup_key").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published' AND lookup_key IS NOT NULL AND lookup_key != ''")),
		index.Fields("tenant_id", "environment_id"),
		index.Fields("start_date", "end_date"),
	}
}
