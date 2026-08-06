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

func (s *MarketplaceServiceSuite) awsRequest(subID, custID, planID string) dto.RegisterMarketplaceAgreementRequest {
	return dto.RegisterMarketplaceAgreementRequest{
		Provider:       types.SecretProviderAWSMarketplace,
		SubscriptionID: subID,
		CustomerID:     custID,
		PlanID:         planID,
		AWS: &dto.AWSMarketplaceAgreement{
			ProductCode:          "product-code-1",
			LicenseArn:           "license-arn-1",
			CustomerAWSAccountID: "aws-account-1",
			Dimension:            "usage_fee",
		},
	}
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_CreatesAllThreeMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_1", "cust_aws_1", "plan_aws_1")

	resp, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_1", "cust_aws_1", "plan_aws_1"))

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	assert.NotEmpty(s.T(), resp.PlanMappingID)
	assert.NotEmpty(s.T(), resp.SubscriptionMappingID)
	assert.NotEmpty(s.T(), resp.CustomerMappingID)
	assert.Equal(s.T(), "active", resp.Status)

	// The plan mapping stores product_code as the provider entity ID and dimension/concurrent_agreements
	// in metadata, matching what MarketplaceUsageReportActivity later reads via loadAWSMappings.
	planMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.PlanMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "product-code-1", planMapping.ProviderEntityID)
	assert.Equal(s.T(), "usage_fee", planMapping.Metadata["dimension"])
	assert.Equal(s.T(), false, planMapping.Metadata["concurrent_agreements"])

	subMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.SubscriptionMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "license-arn-1", subMapping.ProviderEntityID)

	custMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.CustomerMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "aws-account-1", custMapping.ProviderEntityID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_ReusesExistingPlanAndCustomerMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_2", "cust_aws_shared", "plan_aws_shared")
	s.createActiveSubscription("sub_aws_3", "cust_aws_shared", "plan_aws_shared")

	// First buyer registers against the shared plan and customer.
	first, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_2", "cust_aws_shared", "plan_aws_shared"))
	require.NoError(s.T(), err)

	// Second buyer, same plan and customer, different subscription and license_arn.
	req := s.awsRequest("sub_aws_3", "cust_aws_shared", "plan_aws_shared")
	req.AWS.LicenseArn = "license-arn-2"
	req.AWS.CustomerAWSAccountID = "aws-account-1" // same buyer registering a second entitlement
	second, err := s.service.RegisterAgreement(ctx, req)
	require.NoError(s.T(), err)

	// Plan mapping is created once and reused, never duplicated.
	assert.Equal(s.T(), first.PlanMappingID, second.PlanMappingID)
	// Customer mapping is reused too, since it's keyed on (entityID=customerID, provider), not on
	// the customer_aws_account_id passed this time.
	assert.Equal(s.T(), first.CustomerMappingID, second.CustomerMappingID)
	// Subscription mapping is always fresh: a new agreement is always a new subscription.
	assert.NotEqual(s.T(), first.SubscriptionMappingID, second.SubscriptionMappingID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_RejectsInactiveSubscription() {
	ctx := s.GetContext()
	sub := s.createActiveSubscription("sub_aws_inactive", "cust_aws_4", "plan_aws_4")
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_inactive", "cust_aws_4", "plan_aws_4"))

	assert.ErrorContains(s.T(), err, "subscription is not active")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_RejectsCustomerMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_5", "cust_aws_actual", "plan_aws_5")

	_, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_5", "cust_aws_wrong", "plan_aws_5"))

	assert.ErrorContains(s.T(), err, "customer_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_RejectsPlanMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_6", "cust_aws_6", "plan_aws_actual")

	_, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_6", "cust_aws_6", "plan_aws_wrong"))

	assert.ErrorContains(s.T(), err, "plan_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_RejectsDuplicateLicenseArn() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_7", "cust_aws_7", "plan_aws_7")
	s.createActiveSubscription("sub_aws_8", "cust_aws_8", "plan_aws_8")

	_, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_7", "cust_aws_7", "plan_aws_7"))
	require.NoError(s.T(), err)

	// A different subscription tries to register with the same license_arn.
	_, err = s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_8", "cust_aws_8", "plan_aws_8"))

	assert.ErrorContains(s.T(), err, "agreement identifier already registered")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_AWS_RejectsResubmitWithDifferentAgreementID() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_aws_9", "cust_aws_9", "plan_aws_9")

	_, err := s.service.RegisterAgreement(ctx, s.awsRequest("sub_aws_9", "cust_aws_9", "plan_aws_9"))
	require.NoError(s.T(), err)

	// Same subscription, re-registered with a different license_arn.
	req := s.awsRequest("sub_aws_9", "cust_aws_9", "plan_aws_9")
	req.AWS.LicenseArn = "license-arn-different"
	_, err = s.service.RegisterAgreement(ctx, req)

	assert.ErrorContains(s.T(), err, "subscription already mapped to a different agreement identifier")
}

func (s *MarketplaceServiceSuite) azureRequest(subID, custID, planID string) dto.RegisterMarketplaceAgreementRequest {
	return dto.RegisterMarketplaceAgreementRequest{
		Provider:       types.SecretProviderAzureMarketplace,
		SubscriptionID: subID,
		CustomerID:     custID,
		PlanID:         planID,
		Azure: &dto.AzureMarketplaceAgreement{
			PlanID:               "azure-plan-1",
			Dimension:            "usage_fee",
			ResourceID:           "resource-id-1",
			BeneficiaryAccountID: "beneficiary-account-1",
		},
	}
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_CreatesAllThreeMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_1", "cust_azure_1", "plan_azure_1")

	resp, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_1", "cust_azure_1", "plan_azure_1"))

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	assert.NotEmpty(s.T(), resp.PlanMappingID)
	assert.NotEmpty(s.T(), resp.SubscriptionMappingID)
	assert.NotEmpty(s.T(), resp.CustomerMappingID)
	assert.Equal(s.T(), "active", resp.Status)

	// The plan mapping stores Azure's plan_id as the provider entity ID and dimension in metadata,
	// matching what MarketplaceUsageReportActivity later reads via loadAzureMappings.
	planMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.PlanMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "azure-plan-1", planMapping.ProviderEntityID)
	assert.Equal(s.T(), "usage_fee", planMapping.Metadata["dimension"])

	subMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.SubscriptionMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "resource-id-1", subMapping.ProviderEntityID)

	custMapping, err := s.GetStores().EntityIntegrationMappingRepo.Get(ctx, resp.CustomerMappingID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "beneficiary-account-1", custMapping.ProviderEntityID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_ReusesExistingPlanAndCustomerMappings() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_2", "cust_azure_shared", "plan_azure_shared")
	s.createActiveSubscription("sub_azure_3", "cust_azure_shared", "plan_azure_shared")

	// First buyer registers against the shared plan and customer.
	first, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_2", "cust_azure_shared", "plan_azure_shared"))
	require.NoError(s.T(), err)

	// Second buyer, same plan and customer, different subscription and resource_id.
	req := s.azureRequest("sub_azure_3", "cust_azure_shared", "plan_azure_shared")
	req.Azure.ResourceID = "resource-id-2"
	req.Azure.BeneficiaryAccountID = "beneficiary-account-1" // same buyer registering a second entitlement
	second, err := s.service.RegisterAgreement(ctx, req)
	require.NoError(s.T(), err)

	// Plan mapping is created once and reused, never duplicated.
	assert.Equal(s.T(), first.PlanMappingID, second.PlanMappingID)
	// Customer mapping is reused too, since it's keyed on (entityID=customerID, provider), not on
	// the beneficiary_account_id passed this time.
	assert.Equal(s.T(), first.CustomerMappingID, second.CustomerMappingID)
	// Subscription mapping is always fresh: a new agreement is always a new subscription.
	assert.NotEqual(s.T(), first.SubscriptionMappingID, second.SubscriptionMappingID)
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_RejectsInactiveSubscription() {
	ctx := s.GetContext()
	sub := s.createActiveSubscription("sub_azure_inactive", "cust_azure_4", "plan_azure_4")
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_inactive", "cust_azure_4", "plan_azure_4"))

	assert.ErrorContains(s.T(), err, "subscription is not active")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_RejectsCustomerMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_5", "cust_azure_actual", "plan_azure_5")

	_, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_5", "cust_azure_wrong", "plan_azure_5"))

	assert.ErrorContains(s.T(), err, "customer_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_RejectsPlanMismatch() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_6", "cust_azure_6", "plan_azure_actual")

	_, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_6", "cust_azure_6", "plan_azure_wrong"))

	assert.ErrorContains(s.T(), err, "plan_id does not match subscription")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_RejectsDuplicateResourceID() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_7", "cust_azure_7", "plan_azure_7")
	s.createActiveSubscription("sub_azure_8", "cust_azure_8", "plan_azure_8")

	_, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_7", "cust_azure_7", "plan_azure_7"))
	require.NoError(s.T(), err)

	// A different subscription tries to register with the same resource_id.
	_, err = s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_8", "cust_azure_8", "plan_azure_8"))

	assert.ErrorContains(s.T(), err, "agreement identifier already registered")
}

func (s *MarketplaceServiceSuite) TestRegisterAgreement_Azure_RejectsResubmitWithDifferentAgreementID() {
	ctx := s.GetContext()
	s.createActiveSubscription("sub_azure_9", "cust_azure_9", "plan_azure_9")

	_, err := s.service.RegisterAgreement(ctx, s.azureRequest("sub_azure_9", "cust_azure_9", "plan_azure_9"))
	require.NoError(s.T(), err)

	// Same subscription, re-registered with a different resource_id.
	req := s.azureRequest("sub_azure_9", "cust_azure_9", "plan_azure_9")
	req.Azure.ResourceID = "resource-id-different"
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

	assert.ErrorContains(s.T(), err, "only gcp must be set")
}
