package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MarketplaceServiceSuite struct {
	testutil.BaseServiceTestSuite
	service MarketplaceService
}

func TestMarketplaceService(t *testing.T) {
	suite.Run(t, new(MarketplaceServiceSuite))
}

func (s *MarketplaceServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
}

func (s *MarketplaceServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewMarketplaceService(ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      stores.SubscriptionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
	})
}

// createActiveSubscription creates a minimal active subscription owned by customerID/planID, so
// RegisterAgreement's existence/status/ownership checks pass.
func (s *MarketplaceServiceSuite) createActiveSubscription(id, customerID, planID string) *subscription.Subscription {
	ctx := s.GetContext()
	now := time.Now().UTC()
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         customerID,
		PlanID:             planID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingAnchor:      now,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *MarketplaceServiceSuite) gcpRequest(subID, custID, planID string) dto.RegisterMarketplaceAgreementRequest {
	return dto.RegisterMarketplaceAgreementRequest{
		Provider:       types.SecretProviderGCPMarketplace,
		SubscriptionID: subID,
		CustomerID:     custID,
		PlanID:         planID,
		GCP: &dto.GCPMarketplaceAgreement{
			ServiceName:      "my-service.endpoints.my-project.cloud.goog",
			UsageReportingID: "usage-reporting-id-1",
			MetricName:       "my-service.endpoints.my-project.cloud.goog/usage_fee",
			AccountID:        "buyer-account-1",
		},
	}
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_CreatesAllThreeMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_1", "cust_gcp_1", "plan_gcp_1")

	resp, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_1", "cust_gcp_1", "plan_gcp_1"))

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	assert.NotEmpty(s.T(), resp.PlanMappingID)
	assert.NotEmpty(s.T(), resp.SubscriptionMappingID)
	assert.NotEmpty(s.T(), resp.CustomerMappingID)
	assert.Equal(s.T(), "active", resp.Status)

	// The plan mapping stores service_name as the provider entity ID and metric_name in metadata,
	// matching what MarketplaceUsageReportActivity later reads via loadGCPMappings.
	planMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.PlanMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "my-service.endpoints.my-project.cloud.goog", planMapping.ProviderEntityID)
	assert.Equal(s.T(), "my-service.endpoints.my-project.cloud.goog/usage_fee", planMapping.Metadata["metric_name"])

	subMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.SubscriptionMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "usage-reporting-id-1", subMapping.ProviderEntityID)

	custMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.CustomerMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "buyer-account-1", custMapping.ProviderEntityID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_ReusesExistingPlanAndCustomerMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_2", "cust_gcp_shared", "plan_gcp_shared")
	s.createActiveSubscription("sub_gcp_3", "cust_gcp_shared", "plan_gcp_shared")

	// First buyer registers against the shared plan and customer.
	first, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_2", "cust_gcp_shared", "plan_gcp_shared"))
	require.NoError(s.T(), err)

	// Second buyer, same plan and customer, different subscription and usage_reporting_id.
	req := s.gcpRequest("sub_gcp_3", "cust_gcp_shared", "plan_gcp_shared")
	req.GCP.UsageReportingID = "usage-reporting-id-2"
	req.GCP.AccountID = "buyer-account-1" // same buyer registering a second entitlement
	second, err := s.service.RegisterAgreement(ctx, req)
	require.NoError(s.T(), err)

	// Plan mapping is created once and reused, never duplicated.
	assert.Equal(s.T(), first.PlanMappingID, second.PlanMappingID)
	// Customer mapping is reused too, since it's keyed on (entityID=customerID, provider), not on
	// the account_id passed this time.
	assert.Equal(s.T(), first.CustomerMappingID, second.CustomerMappingID)
	// Subscription mapping is always fresh: a new agreement is always a new subscription.
	assert.NotEqual(s.T(), first.SubscriptionMappingID, second.SubscriptionMappingID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_RejectsInactiveSubscription() {
	ctx := s.GetContext()
	sub := s.createActiveSubscription("sub_gcp_inactive", "cust_gcp_4", "plan_gcp_4")
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_inactive", "cust_gcp_4", "plan_gcp_4"))

	assert.ErrorContains(s.T(), err, "subscription is not active")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_RejectsCustomerMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_5", "cust_gcp_actual", "plan_gcp_5")

	_, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_5", "cust_gcp_wrong", "plan_gcp_5"))

	assert.ErrorContains(s.T(), err, "customer_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_RejectsPlanMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_6", "cust_gcp_6", "plan_gcp_actual")

	_, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_6", "cust_gcp_6", "plan_gcp_wrong"))

	assert.ErrorContains(s.T(), err, "plan_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_RejectsDuplicateUsageReportingID() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_7", "cust_gcp_7", "plan_gcp_7")
	s.createActiveSubscription("sub_gcp_8", "cust_gcp_8", "plan_gcp_8")

	_, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_7", "cust_gcp_7", "plan_gcp_7"))
	require.NoError(s.T(), err)

	// A different subscription tries to register with the same usage_reporting_id.
	_, err = s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_8", "cust_gcp_8", "plan_gcp_8"))

	assert.ErrorContains(s.T(), err, "agreement identifier already registered")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_GCP_RejectsResubmitWithDifferentAgreementID() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_gcp_9", "cust_gcp_9", "plan_gcp_9")

	_, err := s.service.RegisterAgreement(ctx, s.gcpRequest("sub_gcp_9", "cust_gcp_9", "plan_gcp_9"))
	require.NoError(s.T(), err)

	// Same subscription, re-registered with a different usage_reporting_id.
	req := s.gcpRequest("sub_gcp_9", "cust_gcp_9", "plan_gcp_9")
	req.GCP.UsageReportingID = "usage-reporting-id-different"
	_, err = s.service.RegisterAgreement(ctx, req)

	assert.ErrorContains(s.T(), err, "subscription already mapped to a different agreement identifier")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_RejectsBothProviderBlocksSet() {
	ctx := context.Background()
	req := s.gcpRequest("sub_x", "cust_x", "plan_x")
	req.AWS = &dto.AWSMarketplaceAgreement{
		ProductCode:          "code",
		LicenseArn:           "arn",
		CustomerAWSAccountID: "acct",
		Dimension:            "usage_fee",
	}

	_, err := s.service.RegisterAgreement(ctx, req)

	assert.ErrorContains(s.T(), err, "aws must not be set")
}
