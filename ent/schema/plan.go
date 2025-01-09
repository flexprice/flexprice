package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Plan holds the schema definition for the Plan entity.
type Plan struct {
	ent.Schema
}

// Mixin of the Plan.
func (Plan) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Plan.
func (Plan) Fields() []ent.Field {
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
			NotEmpty(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.Text("description").
			Optional(),
		field.String("invoice_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
		field.Int("trial_period").
			Default(0),
	}
}

// Edges of the Plan.
func (Plan) Edges() []ent.Edge {
	return nil
}

// Indexes of the Plan.
func (Plan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "lookup_key").
			Unique().
			Annotations(entsql.IndexWhere("status != 'deleted'")),
		index.Fields("tenant_id"),
	}
}
