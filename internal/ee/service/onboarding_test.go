package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
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

func TestOnboardingCreateEventRequestPopulatesMaxGroupByProperty(t *testing.T) {
	meter := &types.MeterInfo{
		EventName: "storage.usage",
		Aggregation: types.AggregationInfo{
			Type:    types.AggregationMax,
			Field:   "storage_gb",
			GroupBy: "region",
		},
	}

	event := (&onboardingService{}).createEventRequest(
		&types.OnboardingEventsMessage{CustomerExtID: "customer-1"}, meter,
	)

	value, ok := event.Properties[meter.Aggregation.GroupBy]
	if !ok || value == "" {
		t.Fatalf("grouped MAX event is missing a value for %q", meter.Aggregation.GroupBy)
	}
}

func TestOnboardingCreateEventRequestKeepsFilterValueOnAggregationFieldCollision(t *testing.T) {
	meter := &types.MeterInfo{
		EventName:   "storage.usage",
		Aggregation: types.AggregationInfo{Type: types.AggregationMax, Field: "storage_gb"},
		Filters:     []types.FilterInfo{{Key: "storage_gb", Values: []string{"10"}}},
	}

	event := (&onboardingService{}).createEventRequest(
		&types.OnboardingEventsMessage{CustomerExtID: "customer-1"}, meter,
	)

	if value := event.Properties[meter.Aggregation.Field]; value != "10" {
		t.Fatalf("filter value %q was overwritten with %v", "10", value)
	}
}

func TestCreateMeterInfoFromMeterPreservesGroupBy(t *testing.T) {
	meterInfo := createMeterInfoFromMeter(&dto.MeterResponse{
		Aggregation: meter.Aggregation{
			Type:    types.AggregationMax,
			Field:   "storage_gb",
			GroupBy: "region",
		},
	})

	if meterInfo.Aggregation.GroupBy != "region" {
		t.Fatalf("group_by = %q, want %q", meterInfo.Aggregation.GroupBy, "region")
	}
}
