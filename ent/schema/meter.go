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

// Edges of the Meter.
func (Meter) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("costsheet", Costsheet.Type),
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
	Multiplier *decimal.Decimal      `json:"multiplier,omitempty"`
	BucketSize types.WindowSize      `json:"bucket_size,omitempty"`
}
