package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

const (
	Idx_code_tenant_id_environment_id = "idx_code_tenant_id_environment_id"
)

// TaxRate holds the schema definition for the TaxRate entity.
type TaxRate struct {
	ent.Schema
}

// Mixin of the TaxRate.
func (TaxRate) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the TaxRate.
func (TaxRate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("code").
			NotEmpty().
			Comment("e.g. CGST, SGST, etc."),
		field.Other("percentage", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(9,6)",
			}).
			Default(decimal.Zero),
		field.Other("fixed_value", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(9,6)",
			}).
			Default(decimal.Zero),
		field.Bool("is_compound").
			Default(false),
		field.Time("valid_from").
			Optional().
			Nillable(),
		field.Time("valid_to").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the TaxRate.
func (TaxRate) Edges() []ent.Edge {
	return nil
}

// Indexes of the TaxRate.
func (TaxRate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code", "tenant_id", "environment_id").
			Unique().
			StorageKey(Idx_code_tenant_id_environment_id).
			Annotations(entsql.IndexWhere("(code IS NOT NULL AND code != '' and status = 'published')")),

		index.Fields("code").
			Annotations(entsql.IndexWhere("(code IS NOT NULL AND code != '')")),
	}
}

func (TaxRate) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "tax_rates",
			Checks: map[string]string{
				// Exactly one of percentage or fixed_value must be present
				"percentage_fixed_value_check": "(percentage IS NOT NULL) <> (fixed_value IS NOT NULL)",

				// Percentage, if given, must not exceed 100 %
				"percentage_check": "(percentage IS NULL OR percentage <= 100)",
			},
		},
	}
}
