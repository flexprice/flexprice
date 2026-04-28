package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type MeterUsageTrackingSuite struct {
	suite.Suite
	svc *meterUsageTrackingService
}

func TestMeterUsageTracking(t *testing.T) {
	suite.Run(t, new(MeterUsageTrackingSuite))
}

func (s *MeterUsageTrackingSuite) SetupTest() {
	s.svc = &meterUsageTrackingService{}
}

// --- checkMeterFilters tests ---

func (s *MeterUsageTrackingSuite) TestCheckMeterFilters_NoFilters() {
	event := &events.Event{Properties: map[string]interface{}{"key": "val"}}
	assert.True(s.T(), s.svc.checkMeterFilters(event, nil))
	assert.True(s.T(), s.svc.checkMeterFilters(event, []meter.Filter{}))
}

func (s *MeterUsageTrackingSuite) TestCheckMeterFilters_Match() {
	event := &events.Event{
		Properties: map[string]interface{}{
			"region": "us-east-1",
			"tier":   "premium",
		},
	}
	filters := []meter.Filter{
		{Key: "region", Values: []string{"us-east-1", "us-west-2"}},
		{Key: "tier", Values: []string{"premium"}},
	}
	assert.True(s.T(), s.svc.checkMeterFilters(event, filters))
}

func (s *MeterUsageTrackingSuite) TestCheckMeterFilters_NoMatch_MissingKey() {
	event := &events.Event{Properties: map[string]interface{}{"region": "us-east-1"}}
	filters := []meter.Filter{
		{Key: "tier", Values: []string{"premium"}},
	}
	assert.False(s.T(), s.svc.checkMeterFilters(event, filters))
}

func (s *MeterUsageTrackingSuite) TestCheckMeterFilters_NoMatch_WrongValue() {
	event := &events.Event{Properties: map[string]interface{}{"region": "eu-west-1"}}
	filters := []meter.Filter{
		{Key: "region", Values: []string{"us-east-1", "us-west-2"}},
	}
	assert.False(s.T(), s.svc.checkMeterFilters(event, filters))
}

// --- generateUniqueHash tests ---

func (s *MeterUsageTrackingSuite) TestGenerateUniqueHash_NonCountUnique() {
	event := &events.Event{ID: "evt_123", EventName: "api_call"}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "duration"}}

	hash := s.svc.generateUniqueHash(event, m)
	assert.Empty(s.T(), hash) // Non-CountUnique meters return empty hash
}

func (s *MeterUsageTrackingSuite) TestGenerateUniqueHash_CountUnique() {
	event := &events.Event{
		ID:        "evt_123",
		EventName: "api_call",
		Properties: map[string]interface{}{
			"user_id": "user_456",
		},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "user_id"}}

	hash := s.svc.generateUniqueHash(event, m)

	// Different event with same user_id should produce same hash (field-based)
	event2 := &events.Event{
		ID:         "evt_789",
		EventName:  "api_call",
		Properties: map[string]interface{}{"user_id": "user_456"},
	}
	hash2 := s.svc.generateUniqueHash(event2, m)
	assert.Equal(s.T(), hash, hash2)

	// Different user_id should produce different hash
	event3 := &events.Event{
		ID:         "evt_789",
		EventName:  "api_call",
		Properties: map[string]interface{}{"user_id": "user_000"},
	}
	hash3 := s.svc.generateUniqueHash(event3, m)
	assert.NotEqual(s.T(), hash, hash3)
}

// --- extractQuantity tests ---

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Count() {
	event := &events.Event{}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCount}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromInt(1).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Sum_Float() {
	event := &events.Event{
		Properties: map[string]interface{}{"tokens": float64(42.5)},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromFloat(42.5).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Sum_String() {
	event := &events.Event{
		Properties: map[string]interface{}{"tokens": "100.25"},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	expected, _ := decimal.NewFromString("100.25")
	assert.True(s.T(), expected.Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Sum_MissingField() {
	event := &events.Event{Properties: map[string]interface{}{}}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.Zero.Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Sum_EmptyField() {
	event := &events.Event{Properties: map[string]interface{}{"tokens": float64(5)}}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: ""}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.Zero.Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_CountUnique() {
	event := &events.Event{
		Properties: map[string]interface{}{"user_id": "u_1"},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "user_id"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromInt(1).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_CountUnique_MissingField() {
	event := &events.Event{Properties: map[string]interface{}{}}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "user_id"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.Zero.Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Max() {
	event := &events.Event{
		Properties: map[string]interface{}{"memory_gb": float64(16.5)},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "memory_gb"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromFloat(16.5).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_Avg() {
	event := &events.Event{
		Properties: map[string]interface{}{"latency_ms": float64(250)},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationAvg, Field: "latency_ms"}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromFloat(250).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_SumWithMultiplier() {
	multiplier := decimal.NewFromFloat(1.5)
	event := &events.Event{
		Properties: map[string]interface{}{"tokens": float64(100)},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{
		Type:       types.AggregationSumWithMultiplier,
		Field:      "tokens",
		Multiplier: &multiplier,
	}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromFloat(150).Equal(qty))
}

func (s *MeterUsageTrackingSuite) TestExtractQuantity_SumWithMultiplier_NilMultiplier() {
	event := &events.Event{
		Properties: map[string]interface{}{"tokens": float64(100)},
	}
	m := &meter.Meter{Aggregation: meter.Aggregation{
		Type:  types.AggregationSumWithMultiplier,
		Field: "tokens",
	}}

	qty, err := s.svc.extractQuantity(event, m)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.Zero.Equal(qty))
}

// --- convertToDecimal tests ---

func (s *MeterUsageTrackingSuite) TestConvertToDecimal_AllTypes() {
	tests := []struct {
		name     string
		input    interface{}
		expected decimal.Decimal
	}{
		{"float64", float64(3.14), decimal.NewFromFloat(3.14)},
		{"float32", float32(2.5), decimal.NewFromFloat32(2.5)},
		{"int", int(42), decimal.NewFromInt(42)},
		{"int64", int64(100), decimal.NewFromInt(100)},
		{"int32", int32(50), decimal.NewFromInt(50)},
		{"uint", uint(7), decimal.NewFromInt(7)},
		{"uint64", uint64(999), decimal.NewFromInt(999)},
		{"string", "123.456", decimal.RequireFromString("123.456")},
		{"invalid_string", "not_a_number", decimal.Zero},
		{"bool", true, decimal.Zero},
		{"nil", nil, decimal.Zero},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := s.svc.convertToDecimal(tt.input)
			assert.True(s.T(), tt.expected.Equal(result), "expected %s, got %s", tt.expected, result)
		})
	}
}

// --- processEvent integration test ---

func (s *MeterUsageTrackingSuite) TestProcessEvent_MatchesMeters() {
	// This test verifies the MeterUsage record building logic
	// by testing the helper methods work together correctly

	event := &events.Event{
		ID:                 "evt_test_001",
		TenantID:           types.DefaultTenantID,
		EnvironmentID:      "env_sandbox",
		ExternalCustomerID: "cust_ext_1",
		EventName:          "api_call",
		Timestamp:          time.Now().UTC(),
		Source:             "sdk",
		Properties: map[string]interface{}{
			"tokens":  float64(500),
			"model":   "gpt-4",
			"user_id": "usr_abc",
		},
	}

	// Meter 1: SUM of tokens
	m1 := &meter.Meter{
		ID:        "mtr_sum",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "tokens",
		},
	}

	// Meter 2: COUNT_UNIQUE of user_id (with filter on model)
	m2 := &meter.Meter{
		ID:        "mtr_unique",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationCountUnique,
			Field: "user_id",
		},
		Filters: []meter.Filter{
			{Key: "model", Values: []string{"gpt-4", "gpt-3.5"}},
		},
	}

	// Meter 3: SUM but filter doesn't match
	m3 := &meter.Meter{
		ID:        "mtr_no_match",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "tokens",
		},
		Filters: []meter.Filter{
			{Key: "model", Values: []string{"claude-3"}},
		},
	}

	// Verify m1 matches
	assert.True(s.T(), s.svc.checkMeterFilters(event, m1.Filters))
	qty1, err := s.svc.extractQuantity(event, m1)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromFloat(500).Equal(qty1))

	// Verify m2 matches (filter passes)
	assert.True(s.T(), s.svc.checkMeterFilters(event, m2.Filters))
	qty2, err := s.svc.extractQuantity(event, m2)
	assert.NoError(s.T(), err)
	assert.True(s.T(), decimal.NewFromInt(1).Equal(qty2))

	// Verify m3 does NOT match (filter fails)
	assert.False(s.T(), s.svc.checkMeterFilters(event, m3.Filters))

	// Verify unique hash is field-based for COUNT_UNIQUE
	hash2 := s.svc.generateUniqueHash(event, m2)
	assert.NotEmpty(s.T(), hash2)

	// Verify unique hash is empty for non-CountUnique (SUM)
	hash1 := s.svc.generateUniqueHash(event, m1)
	assert.Empty(s.T(), hash1)
}
