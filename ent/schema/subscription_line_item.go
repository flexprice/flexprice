package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// SubscriptionLineItem holds the schema definition for the SubscriptionLineItem entity.
type SubscriptionLineItem struct {
	ent.Schema
}

// Mixin of the SubscriptionLineItem.
func (SubscriptionLineItem) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the SubscriptionLineItem.
func (SubscriptionLineItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.InvoiceLineItemEntityTypePlan)).
			Immutable(),
		field.String("plan_display_name").
			Optional().
			Nillable(),
		field.String("price_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("price_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("meter_display_name").
			Optional().
			Nillable(),
		field.String("price_unit_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("price_unit").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Optional().
			Nillable(),
		field.String("display_name").
			Optional().
			Nillable(),
		field.Other("quantity", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),
		field.String("billing_period").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("invoice_cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Immutable().
			Optional(), // TODO: Remove this once we have migrated all the data
		field.Int("trial_period").
			Default(0),
		field.Time("start_date").
			Optional().
			Nillable(),
		field.Time("end_date").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the SubscriptionLineItem.
func (SubscriptionLineItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", Subscription.Type).
			Ref("line_items").
			Field("subscription_id").
			Unique().
			Required().
			Immutable(),
		edge.To("coupon_associations", CouponAssociation.Type).
			Comment("Subscription line item can have multiple coupon associations"),
	}
}

// Indexes of the SubscriptionLineItem.
func (SubscriptionLineItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "subscription_id", "status"),
		index.Fields("tenant_id", "environment_id", "customer_id", "status"),
		index.Fields("tenant_id", "environment_id", "entity_id", "entity_type", "status"),
		index.Fields("tenant_id", "environment_id", "price_id", "status"),
		index.Fields("tenant_id", "environment_id", "meter_id", "status"),
		index.Fields("start_date", "end_date"),
	}
}
