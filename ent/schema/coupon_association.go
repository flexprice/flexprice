package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// CouponAssociation holds the schema definition for the CouponAssociation entity.
type CouponAssociation struct {
	ent.Schema
}

// Mixin of the CouponAssociation.
func (CouponAssociation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CouponAssociation.
func (CouponAssociation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("coupon_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("subscription_line_item_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("subscription_phase_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.Time("start_date").
			Immutable().
			Default(time.Now),
		field.Time("end_date").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			Comment("Additional metadata for coupon association"),
	}
}

// Edges of the CouponAssociation.
func (CouponAssociation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("coupon", Coupon.Type).
			Ref("coupon_associations").
			Field("coupon_id").
			Unique().
			Immutable().
			Required(),
		edge.From("subscription", Subscription.Type).
			Ref("coupon_associations").
			Field("subscription_id").
			Unique().
			Immutable().
			Required(),
		edge.From("subscription_line_item", SubscriptionLineItem.Type).
			Ref("coupon_associations").
			Field("subscription_line_item_id").
			Unique(),
		edge.To("coupon_applications", CouponApplication.Type).
			Comment("CouponAssociation can have multiple coupon applications"),
	}
}

// Indexes of the CouponAssociation.
func (CouponAssociation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
		index.Fields("tenant_id", "environment_id", "coupon_id"),
		index.Fields("tenant_id", "environment_id", "subscription_id"),
		index.Fields("tenant_id", "environment_id", "subscription_id", "subscription_line_item_id"),
	}
}
