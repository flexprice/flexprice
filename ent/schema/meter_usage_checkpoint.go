package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// MeterUsageCheckpoint is a SIGNED_SUM meter's running total for one customer as of a
// point in time. Reads add the changes since the latest checkpoint.
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

		// Signed: a SIGNED_SUM total can be negative when the customer sends bad data.
		field.Other("total", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(34,15)",
			}),

		field.Time("aggregated_until").
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
		// backward scan on aggregated_until with no sort.
		index.Fields("tenant_id", "environment_id", "customer_id", "meter_id", "aggregated_until").
			Unique().
			StorageKey("meterusagecheckpoint_customer_meter_aggregated_until"),
	}
}
