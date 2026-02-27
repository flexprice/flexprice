package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Meter holds the schema definition for the Meter entity.
type Meter struct {
	ent.Schema
}

// Mixin of the Meter.
func (Meter) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Meter.
func (Meter) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("event_name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.JSON("aggregation", MeterAggregation{}).
			Default(MeterAggregation{
				Type:  types.AggregationCount,
				Field: "",
			}),
		field.JSON("filters", []MeterFilter{}).
			Default([]MeterFilter{}),
		field.String("reset_usage").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.ResetUsageBillingPeriod)),
	}
}

// Indexes of the Meter.
func (Meter) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
	}
}

// Additional types needed for JSON fields
type MeterFilter struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// MeterAggregation defines the aggregation configuration for a meter
type MeterAggregation struct {
	Type       types.AggregationType `json:"type"`
	Field      string                `json:"field,omitempty"`
	// Expression is an optional CEL expression to compute per-event quantity from event.properties.
	// When set, it replaces Field-based extraction. Property names are used directly (e.g., token * duration * pixel).
	Expression string                `json:"expression,omitempty"`
	Multiplier *decimal.Decimal      `json:"multiplier,omitempty"`
	BucketSize types.WindowSize      `json:"bucket_size,omitempty"`
	// GroupBy is the property name in event.properties to group by before aggregating.
	// Currently only supported for MAX aggregation with bucket_size.
	// When set, aggregation is applied per unique value of this property within each bucket,
	// then the per-group results are summed to produce the bucket total.
	GroupBy string `json:"group_by,omitempty"`
}
