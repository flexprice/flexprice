package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// CustomerUsageWatermark is how far a customer's usage has been added to their period totals
// (meter_usage_checkpoints rows with period_start).
type CustomerUsageWatermark struct {
	ent.Schema
}

func (CustomerUsageWatermark) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CustomerUsageWatermark) Fields() []ent.Field {
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
			NotEmpty().
			Immutable(),

		field.Time("aggregated_until"),
	}
}

func (CustomerUsageWatermark) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id").
			Unique().
			StorageKey("customerusagewatermark_customer"),
	}
}
