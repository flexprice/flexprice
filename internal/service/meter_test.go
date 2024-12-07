package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type MeterServiceSuite struct {
	suite.Suite
	ctx     context.Context
	service MeterService
	store   *testutil.InMemoryMeterStore
}

func TestMeterService(t *testing.T) {
	suite.Run(t, new(MeterServiceSuite))
}

func (s *MeterServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.store = testutil.NewInMemoryMeterStore()
	s.service = NewMeterService(s.store)
}

func (s *MeterServiceSuite) TestCreateMeter() {
	testCases := []struct {
		name          string
		input         *dto.CreateMeterRequest
		expectedError bool
	}{
		{
			name: "successful_meter_creation",
			input: &dto.CreateMeterRequest{
				Name:      "API Usage Counter",
				EventName: "api_request",
				Aggregation: meter.Aggregation{
					Type:  types.AggregationSum,
					Field: "duration_ms",
				},
				Filters:    []meter.Filter{},
				ResetUsage: types.ResetUsageBillingPeriod,
			},
			expectedError: false,
		},
		{
			name:          "nil_meter",
			input:         nil,
			expectedError: true,
		},
		{
			name: "invalid_meter_missing_name",
			input: &dto.CreateMeterRequest{
				EventName: "api_request",
				Aggregation: meter.Aggregation{
					Type: types.AggregationSum,
				},
			},
			expectedError: true,
		},
		{
			name: "invalid_meter_missing_event_name",
			input: &dto.CreateMeterRequest{
				Name: "Test Meter",
				Aggregation: meter.Aggregation{
					Type: types.AggregationSum,
				},
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			meter, err := s.service.CreateMeter(s.ctx, tc.input)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)

			if tc.input != nil {
				stored, err := s.store.GetMeter(s.ctx, meter.ID)
				s.NoError(err)
				s.Equal(tc.input.Name, stored.Name)
				s.Equal(tc.input.EventName, stored.EventName)
			}
		})
	}
}

func (s *MeterServiceSuite) TestGetMeter() {
	// Create test meter
	testMeter := meter.NewMeter("Test API Meter", "tenant-1", "test-user")
	testMeter.EventName = "api_request"
	testMeter.Aggregation = meter.Aggregation{
		Type:  types.AggregationSum,
		Field: "duration_ms",
	}
	testMeter.Filters = []meter.Filter{
		{
			Key:    "status",
			Values: []string{"success", "error"},
		},
		{
			Key:    "region",
			Values: []string{"us-east-1"},
		},
	}
	testMeter.BaseModel.Status = types.StatusActive
	testMeter.ResetUsage = types.ResetUsageBillingPeriod

	err := s.store.CreateMeter(s.ctx, testMeter)
	s.NoError(err)

	result, err := s.service.GetMeter(s.ctx, testMeter.ID)
	s.NoError(err)
	s.Equal(testMeter.ID, result.ID)
	s.Equal(testMeter.Name, result.Name)
	s.Equal(testMeter.EventName, result.EventName)
	s.Equal(testMeter.Aggregation, result.Aggregation)
	s.Equal(testMeter.Filters, result.Filters)
}

func (s *MeterServiceSuite) TestGetAllMeters() {
	// Create test meters
	meters := []*meter.Meter{
		meter.NewMeter("First API Meter", "tenant-1", "test-user-1"),
		meter.NewMeter("Second API Meter", "tenant-1", "test-user-2"),
	}

	// Set required fields
	for _, m := range meters {
		m.EventName = "api_request"
		m.Aggregation = meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "duration_ms",
		}
		m.Filters = []meter.Filter{
			{
				Key:    "status",
				Values: []string{"success"},
			},
		}
		m.BaseModel.Status = types.StatusActive
		m.ResetUsage = types.ResetUsageBillingPeriod

		err := s.store.CreateMeter(s.ctx, m)
		s.NoError(err)
	}

	result, err := s.service.GetAllMeters(s.ctx)
	s.NoError(err)
	s.Len(result, 2)
	for i, m := range result {
		s.Equal(meters[i].Name, m.Name)
		s.Equal("api_request", m.EventName)
		s.NotEmpty(m.Filters)
		s.Equal("status", m.Filters[0].Key)
	}
}

func (s *MeterServiceSuite) TestDisableMeter() {
	// Create test meter with a name
	testMeter := meter.NewMeter("Test Disable Meter", "tenant-1", "test-user") // Added name
	testMeter.EventName = "api_request"
	testMeter.BaseModel = types.BaseModel{
		Status: types.StatusActive,
	}
	testMeter.Aggregation = meter.Aggregation{ // Add required fields
		Type:  types.AggregationSum,
		Field: "duration_ms",
	}
	testMeter.Filters = []meter.Filter{} // Initialize empty filters
	testMeter.ResetUsage = types.ResetUsageBillingPeriod

	err := s.store.CreateMeter(s.ctx, testMeter)
	s.NoError(err)

	testCases := []struct {
		name          string
		id            string
		expectedError bool
	}{
		{
			name:          "successful_disable",
			id:            testMeter.ID,
			expectedError: false,
		},
		{
			name:          "empty_id",
			id:            "",
			expectedError: true,
		},
		{
			name:          "non_existent_meter",
			id:            "non-existent",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.DisableMeter(s.ctx, tc.id)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)

			// Verify meter is disabled
			meter, err := s.store.GetMeter(s.ctx, tc.id)
			s.NoError(err)
			s.Equal(types.StatusDeleted, meter.BaseModel.Status)
		})
	}
}
