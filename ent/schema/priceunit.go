package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// PriceUnit holds the schema definition for the PriceUnit entity.
type PriceUnit struct {
	ent.Schema
}

// Mixin of the PriceUnit.
func (PriceUnit) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
		baseMixin.MetadataMixin{},
	}
}

// Fields of the PriceUnit.
func (PriceUnit) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),

		field.String("code").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable().
			NotEmpty(),

		field.String("symbol").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),

		field.String("base_currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable().
			NotEmpty(),

		field.Other("conversion_rate", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(10,5)",
			}).
			Default(decimal.NewFromInt(1)).
			Immutable(),
	}
}

// Edges of the PriceUnit.
func (PriceUnit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("prices", Price.Type).
			Ref("price_unit_edge"),
	}
}

// Indexes of the PriceUnit.
func (PriceUnit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "code").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
	}
}
