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
	"github.com/shopspring/decimal"
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

		// display_name is the name of the price
		field.String("display_name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),

		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Immutable().
			Default(decimal.Zero),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable().
			NotEmpty(),

		field.String("display_amount").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Immutable().
			Optional().
			NotEmpty(),

		// price_unit_type is the type of the price unit- Fiat, Custom
		field.String("price_unit_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			NotEmpty().
			Default(string(types.PRICE_UNIT_TYPE_FIAT)).
			GoType(types.PriceUnitType("")),

		// price_unit_id is the id of the price unit
		field.String("price_unit_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Optional().
			Nillable(),
		// price_unit is the code of the price unit
		field.String("price_unit").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable().
			Optional().
			Nillable(),

		// price_unit_amount is the amount of the price unit
		field.Other("price_unit_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Immutable().
			Optional().
			Nillable().
			Default(decimal.Zero),

		// display_price_unit_amount is the amount of the price unit in the display currency
		field.String("display_price_unit_amount").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Immutable().
			Optional().
			Nillable(),

		// conversion_rate is the conversion rate of the price unit to the fiat currency
		// amount in the fiat currency =  number of price units * conversion_rate (e.g. 1 BTC = 10000 USD)
		// ex if conversion_rate is 1, then 1 USD = 1 BTC
		// ex if conversion_rate is 2, then 1 USD = 0.5 BTC
		// ex if conversion_rate is 0.5, then 1 USD = 2 BTC
		field.Other("conversion_rate", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Immutable().
			Optional().
			Nillable().
			Default(decimal.NewFromInt(1)), // 1 USD = 1 BTC

		// min_quantity is the minimum quantity of the price
		field.Other("min_quantity", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Immutable().
			Optional().
			Nillable(),

		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			NotEmpty().
			GoType(types.PriceType("")),

		field.String("billing_period").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			NotEmpty().
			GoType(types.BillingPeriod("")),

		field.Int("billing_period_count").
			Immutable().
			NonNegative(),

		field.String("billing_model").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Immutable().
			GoType(types.BillingModel("")),

		field.String("billing_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			NotEmpty().
			GoType(types.BillingCadence("")),

		field.String("invoice_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional(). // TODO: Remove this once we have migrated all the data
			Immutable().
			GoType(types.InvoiceCadence("")),

		field.Int("trial_period").
			Default(0).
			Immutable(),

		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Optional().
			Nillable(),

		field.JSON("filter_values", map[string][]string{}).
			Optional(),

		field.String("tier_mode").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			Optional().
			Nillable().
			GoType(types.BillingTier("")),

		field.JSON("tiers", []*types.PriceTier{}).
			Immutable().
			Optional(),

		field.JSON("price_unit_tiers", []*types.PriceTier{}).
			Immutable().
			Optional(),

		field.JSON("transform_quantity", types.TransformQuantity{}).
			Immutable().
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

		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			NotEmpty().
			Default(string(types.PRICE_ENTITY_TYPE_PLAN)).
			GoType(types.PriceEntityType("")),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("parent_price_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),

		field.Time("start_date").
			Nillable().
			Default(time.Now).
			Optional().
			Immutable(),

		field.Time("end_date").
			Optional().
			Nillable(),

		field.String("group_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
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
			Immutable().
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
		index.Fields("tenant_id", "environment_id", "group_id"),
	}
}
