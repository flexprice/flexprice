package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Contract holds the schema definition for the Contract entity.
type Contract struct {
	ent.Schema
}

// Mixin of the Contract.
func (Contract) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Contract.
func (Contract) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Optional(),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable().
			Optional(),

		field.Time("term_start").
			Optional().
			Nillable(),

		field.Time("term_end").
			Optional().
			Nillable(),

		field.Time("ended_at").
			Optional().
			Nillable(),

		field.String("contract_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.JSON("document", &types.ContractDocument{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),

		field.String("extraction_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional().
			Nillable(),

		field.JSON("extraction", &types.ContractExtraction{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),

		field.Text("extraction_error").
			Optional().
			Nillable(),
	}
}

// Indexes of the Contract.
func (Contract) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id"),
	}
}
