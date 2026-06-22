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

// Coupon holds the schema definition for the Coupon entity.
type Coupon struct {
	ent.Schema
}

// Mixin of the Coupon.
func (Coupon) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Coupon.
func (Coupon) Fields() []ent.Field {
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
			NotEmpty().
			Comment("Coupon name"),
		field.Time("redeem_after").
			Optional().
			Nillable().
			Comment("Coupon redeem after date"),
		field.Time("redeem_before").
			Optional().
			Nillable().
			Comment("Coupon redeem before date"),
		field.Int("max_redemptions").
			Optional().
			Nillable().
			Comment("Coupon max redemptions"),
		field.Int("total_redemptions").
			Default(0).
			Comment("Coupon total redemptions"),
		field.JSON("rules", map[string]interface{}{}).
			Optional().
			Comment("Rule engine configuration for discount application"),
		field.Other("amount_off", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Optional().
			Default(decimal.Zero).
			Comment("Coupon amount off"),
		field.Other("percentage_off", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(7,4)",
			}).
			Optional().
			Default(decimal.Zero).
			Comment("Coupon percentage off"),
		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default("fixed").
			Comment("Coupon type: fixed or percentage"),
		field.String("cadence").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default("once").
			Comment("Coupon cadence: once, repeated, forever"),
		field.Int("duration_in_periods").
			Optional().
			Nillable().
			Comment("Coupon duration in periods"),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Optional().
			Nillable().
			Comment("Coupon currency"),
		field.JSON("metadata", map[string]string{}).
			Optional().
			Comment("Additional metadata for coupon"),
		field.String("coupon_code").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional().
			Nillable().
			Comment("Human-readable coupon code (e.g. SUMMER20). Stored lowercase. Unique per tenant+environment when published."),
	}
}

// Edges of the Coupon.
func (Coupon) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("coupon_associations", CouponAssociation.Type).
			Comment("Coupon can be associated with multiple coupon associations"),
		edge.To("coupon_applications", CouponApplication.Type).
			Comment("Coupon can have multiple coupon applications"),
	}
}

// Indexes of the Coupon.
func (Coupon) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
		index.Fields("tenant_id", "environment_id", "coupon_code").
			Unique().
			Annotations(entsql.IndexWhere("coupon_code IS NOT NULL AND coupon_code != '' AND status = 'published'")).
			StorageKey("idx_coupon_tenant_environment_coupon_code_unique"),
	}
}
