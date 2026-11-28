package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// PerformanceObligation holds the schema definition for the PerformanceObligation entity.
type PerformanceObligation struct {
	ent.Schema
}

// Mixin of the PerformanceObligation.
func (PerformanceObligation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the PerformanceObligation.
func (PerformanceObligation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("contract_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),

		field.String("recognition_method").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),

		field.String("amortization_method").
			SchemaType(map[string]string{
				"postgres": "varchar(30)",
			}).
			NotEmpty(),

		field.Time("service_start"),

		field.Time("service_end").
			Optional().
			Nillable(),

		field.String("pob_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
	}
}

// Indexes of the PerformanceObligation.
func (PerformanceObligation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "contract_id"),
	}
}
