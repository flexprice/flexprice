package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// MeterUsageCheckpointWatermark is how far a customer's usage has been added to their period totals
// (meter_usage_checkpoints rows with period_start).
type MeterUsageCheckpointWatermark struct {
	ent.Schema
}

func (MeterUsageCheckpointWatermark) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (MeterUsageCheckpointWatermark) Fields() []ent.Field {
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

func (MeterUsageCheckpointWatermark) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id").
			Unique().
			StorageKey("meterusagecheckpointwatermark_customer"),
	}
}
