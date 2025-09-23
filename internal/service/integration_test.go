package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type IntegrationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service IntegrationService
}

func TestIntegrationService(t *testing.T) {
	suite.Run(t, new(IntegrationServiceSuite))
}

func (s *IntegrationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewIntegrationService(ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		CustomerRepo:                 s.GetStores().CustomerRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
	})
}

func (s *IntegrationServiceSuite) TestIntegrationServiceCreation() {
	// Simple test to verify the service can be created
	s.NotNil(s.service)
}

func (s *IntegrationServiceSuite) TestSyncMeterToProviders() {
	// Test that meter sync is supported
	ctx := s.GetContext()

	// Create a test meter
	meterService := NewMeterServiceWithParams(ServiceParams{
		Logger:                       s.GetLogger(),
		DB:                           s.GetDB(),
		MeterRepo:                    s.GetStores().MeterRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
	})

	createReq := &dto.CreateMeterRequest{
		EventName: "test_event",
		Name:      "Test Meter",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "value",
		},
		ResetUsage: types.ResetUsageBillingPeriod,
	}

	meter, err := meterService.CreateMeter(ctx, createReq)
	s.NoError(err)
	s.NotNil(meter)

	// Test syncing meter to providers (this will fail gracefully since no Stripe connection is configured)
	err = s.service.SyncEntityToProviders(ctx, types.IntegrationEntityTypeMeter, meter.ID)
	// Should not error even if no providers are configured
	s.NoError(err)
}
