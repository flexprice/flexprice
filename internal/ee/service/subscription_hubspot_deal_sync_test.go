package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

// HubSpotDealSyncSuite follows the same testutil.BaseServiceTestSuite pattern used by every
// other ee/service test (e.g. AlertSettingsServiceSuite in alert_test.go) -- an in-memory-store-
// backed ServiceParams via s.GetStores(), s.GetLogger(), s.GetDB(), s.GetContext().
type HubSpotDealSyncSuite struct {
	testutil.BaseServiceTestSuite
	svc *subscriptionService
}

func TestHubSpotDealSync(t *testing.T) {
	suite.Run(t, new(HubSpotDealSyncSuite))
}

func (s *HubSpotDealSyncSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = &subscriptionService{ServiceParams: ServiceParams{
		Logger:                       s.GetLogger(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
	}}
}

func (s *HubSpotDealSyncSuite) TestResolveHubSpotDealID_FallsBackToCustomerMetadataAndBackfills() {
	ctx := s.GetContext()

	cust := &customer.Customer{ID: "cust_1", Metadata: map[string]string{"hubspot_deal_id": "deal_legacy"}}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	dealID, err := s.svc.resolveHubSpotDealID(ctx, "sub_1", "cust_1")
	s.Require().NoError(err)
	s.Require().Equal("deal_legacy", dealID)

	// Second call should now hit the backfilled mapping row directly rather than falling
	// back to customer metadata again.
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = "sub_1"
	filter.EntityType = types.IntegrationEntityTypeSubscription
	mappings, err := s.GetStores().EntityIntegrationMappingRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(mappings, 1)
	s.Require().Equal("deal_legacy", mappings[0].ProviderEntityID)
}

func (s *HubSpotDealSyncSuite) TestResolveHubSpotDealID_NotLinkedReturnsEmpty() {
	ctx := s.GetContext()

	cust := &customer.Customer{ID: "cust_2", Metadata: map[string]string{}}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	dealID, err := s.svc.resolveHubSpotDealID(ctx, "sub_2", "cust_2")
	s.Require().NoError(err)
	s.Require().Empty(dealID)
}
