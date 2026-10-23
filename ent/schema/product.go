package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Product holds the schema definition for the Product entity.
type Product struct {
	ent.Schema
}

const (
	ProductTenantIDEnvironmentIDLookupKeyConstraint = "product_tenant_id_environment_id_lookup_key"
)

// Mixin of the Product.
func (Product) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Product.
func (Product) Fields() []ent.Field {
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

		field.Text("description").
			Optional(),

		field.String("lookup_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Immutable().
			NotEmpty(),
	}
}

// Indexes of the Product.
func (Product) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "lookup_key").
			Unique().
			StorageKey(ProductTenantIDEnvironmentIDLookupKeyConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		index.Fields("tenant_id", "environment_id"),
	}
}
