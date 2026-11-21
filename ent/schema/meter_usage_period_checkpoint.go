package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// MeterUsagePeriodCheckpoint is how far one customer's per-period meter totals
// (meter_usage_checkpoints rows with period_start) have been aggregated.
type MeterUsagePeriodCheckpoint struct {
	ent.Schema
}

func (MeterUsagePeriodCheckpoint) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (MeterUsagePeriodCheckpoint) Fields() []ent.Field {
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

		field.Time("period_start").
			Immutable(),

		field.Time("aggregated_until"),
	}
}

func (MeterUsagePeriodCheckpoint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id", "period_start").
			Unique().
			StorageKey("meterusageperiodcheckpoint_customer_period"),
	}
}
