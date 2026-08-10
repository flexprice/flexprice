package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestOnboardingCreateEventRequestPopulatesMaxAggregationField(t *testing.T) {
	meter := &types.MeterInfo{
		EventName: "storage.usage",
		Aggregation: types.AggregationInfo{
			Type:  types.AggregationMax,
			Field: "storage_gb",
		},
	}

	event := (&onboardingService{}).createEventRequest(
		&types.OnboardingEventsMessage{CustomerExtID: "customer-1"},
		meter,
	)

	value, ok := event.Properties[meter.Aggregation.Field]
	if !ok {
		t.Fatalf("MAX aggregation event is missing %q", meter.Aggregation.Field)
	}

	intValue, ok := value.(int64)
	if !ok {
		t.Fatalf("MAX aggregation value has type %T, want int64", value)
	}
	if intValue < 1 || intValue > 100 {
		t.Fatalf("MAX aggregation value %d is outside the onboarding dummy-data range", intValue)
	}
}
