package internal

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
)

func TestDummyEventGeneratorsPopulateMaxAggregationField(t *testing.T) {
	testMeter := &meter.Meter{
		EventName: "storage.usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationMax,
			Field: "storage_gb",
		},
		Filters: []meter.Filter{{
			Key:    "storage_gb",
			Values: []string{"not-a-number"},
		}},
	}

	tests := []struct {
		name       string
		properties func() map[string]interface{}
	}{
		{
			name: "seed events by meters",
			properties: func() map[string]interface{} {
				generator := NewEventGenerator(
					[]*meter.Meter{testMeter},
					[]*customer.Customer{{ExternalID: "customer-1"}},
					nil,
				)
				return generator.generateEvent(0).Properties
			},
		},
		{
			name: "setup dummy billing customer",
			properties: func() map[string]interface{} {
				return eventPropertiesForMeter(testMeter)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := tt.properties()[testMeter.Aggregation.Field]
			if !ok {
				t.Fatalf("MAX aggregation event is missing %q", testMeter.Aggregation.Field)
			}
			intValue, ok := value.(int64)
			if !ok {
				t.Fatalf("MAX aggregation value has type %T, want int64", value)
			}
			if intValue < 1 || intValue > 1000 {
				t.Fatalf("MAX aggregation value %v is outside the dummy-data range", intValue)
			}
		})
	}
}
