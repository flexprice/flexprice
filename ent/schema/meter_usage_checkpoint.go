package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// MeterUsageCheckpoint is a meter's running aggregate for one customer up to a point in
// time. Reads combine the latest checkpoint with the changes since. A row with period_start
// covers one billing period and is advanced in place; a row without one is a running total.
type MeterUsageCheckpoint struct {
	ent.Schema
}

func (MeterUsageCheckpoint) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (MeterUsageCheckpoint) Fields() []ent.Field {
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

		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// How the total was aggregated. A checkpoint is only read back for the same type.
		field.String("aggregation_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			GoType(types.AggregationType("")),

		// Raw signed usage, independent of billable quantities and grant availability.
		field.Other("total", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(34,15)",
			}),

		field.Time("aggregated_until"),

		// Set on per-period rows: the billing period start the total counts from.
		field.Time("period_start").
			Optional().
			Nillable().
			Immutable(),

		// The invoice whose period end this checkpoint records, when it came from one.
		field.String("invoice_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
	}
}

func (MeterUsageCheckpoint) Indexes() []ent.Index {
	return []ent.Index{
		// One checkpoint per instant, and the latest-at-or-before lookup is a
		// backward scan on aggregated_until with no sort. Period rows never share a meter with
		// running totals (the checkpoint job skips never-reset SUM meters), so they cannot collide.
		index.Fields("tenant_id", "environment_id", "customer_id", "meter_id", "aggregated_until").
			Unique().
			StorageKey("meterusagecheckpoint_customer_meter_aggregated_until"),

		// One per-period row per customer, meter and billing period.
		index.Fields("tenant_id", "environment_id", "customer_id", "meter_id", "period_start").
			Unique().
			Annotations(entsql.IndexWhere("(period_start IS NOT NULL)")).
			StorageKey("meterusagecheckpoint_period_total"),
	}
}
