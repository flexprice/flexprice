package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Customer holds the schema definition for the Customer entity.
type Customer struct {
	ent.Schema
}

// Mixin of the Customer.
func (Customer) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Customer.
func (Customer) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("external_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("email").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
		// Address fields
		field.String("address_line1").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
		field.String("address_line2").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
		field.String("address_city").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional(),
		field.String("address_state").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional(),
		field.String("address_postal_code").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional(),
		field.String("address_country").
			SchemaType(map[string]string{
				"postgres": "varchar(2)",
			}).
			Optional(),
		// Metadata as JSON field
		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

// Edges of the Customer.
func (Customer) Edges() []ent.Edge {
	return nil
}

// Indexes of the Customer.
func (Customer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "external_id").
			Unique().
			Annotations(entsql.IndexWhere("status != 'deleted'" + " AND external_id != ''")),
		index.Fields("tenant_id"),
	}
}
