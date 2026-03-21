package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

var Idx_tenant_environment_external_id_unique = "idx_tenant_environment_external_id_unique"

// Customer holds the schema definition for the Customer entity.
type Customer struct {
	ent.Schema
}

// Mixin of the Customer.
func (Customer) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
		baseMixin.MetadataMixin{},
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
		field.String("parent_customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Nillable().
			Optional(),
	}
}

// Edges of the Customer.
func (Customer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscriptions_with_usage", Subscription.Type).
			Ref("usage_customers").
			Comment("Subscriptions that aggregate this customer's usage"),
		edge.From("line_items_with_usage", SubscriptionLineItem.Type).
			Ref("usage_customers").
			Comment("Subscription line items that aggregate this customer's usage"),
	}
}

// Indexes of the Customer.
func (Customer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "external_id").
			Unique().
			Annotations(entsql.IndexWhere("(external_id IS NOT NULL AND external_id != '') AND status = 'published'")).
			StorageKey(Idx_tenant_environment_external_id_unique),
		index.Fields("tenant_id", "environment_id"),
		// Add email index for efficient email-based lookups
		index.Fields("tenant_id", "environment_id", "email").
			Annotations(entsql.IndexWhere("email IS NOT NULL AND email != '' AND status = 'published'")).
			StorageKey("idx_customer_tenant_environment_email"),
	}
}
