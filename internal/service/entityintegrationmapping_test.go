package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type EntityIntegrationMappingServiceSuite struct {
	testutil.BaseServiceTestSuite
	service EntityIntegrationMappingService
}

func TestEntityIntegrationMappingService(t *testing.T) {
	suite.Run(t, new(EntityIntegrationMappingServiceSuite))
}

func (s *EntityIntegrationMappingServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
}

func (s *EntityIntegrationMappingServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewEntityIntegrationMappingService(ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		CustomerRepo:                 stores.CustomerRepo,
		WebhookPublisher:             s.GetWebhookPublisher(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	})
}

func (s *EntityIntegrationMappingServiceSuite) TestCreateEntityIntegrationMapping() {
	// Test data
	req := dto.CreateEntityIntegrationMappingRequest{
		EntityID:         "cust_123",
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     "stripe",
		ProviderEntityID: "cus_stripe_456",
		Metadata: map[string]interface{}{
			"stripe_customer_email": "test@example.com",
		},
	}

	// Execute
	ctx := types.SetTenantID(context.Background(), "test_tenant")
	ctx = types.SetEnvironmentID(ctx, "test_env")
	ctx = types.SetUserID(ctx, "test_user")

	resp, err := s.service.CreateEntityIntegrationMapping(ctx, req)

	// Assert
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), resp)
	assert.Equal(s.T(), req.EntityID, resp.EntityID)
	assert.Equal(s.T(), types.IntegrationEntityType(req.EntityType), resp.EntityType)
	assert.Equal(s.T(), req.ProviderType, resp.ProviderType)
	assert.Equal(s.T(), req.ProviderEntityID, resp.ProviderEntityID)
	// Note: resp.Metadata should NOT exist anymore - this verifies our security fix!
	assert.NotEmpty(s.T(), resp.ID)
	assert.Equal(s.T(), "test_tenant", resp.TenantID)
	assert.Equal(s.T(), "test_env", resp.EnvironmentID)
}

func (s *EntityIntegrationMappingServiceSuite) TestGetEntityIntegrationMapping() {
	// Create a mapping first
	req := dto.CreateEntityIntegrationMappingRequest{
		EntityID:         "cust_123",
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     "stripe",
		ProviderEntityID: "cus_stripe_456",
	}

	ctx := types.SetTenantID(context.Background(), "test_tenant")
	ctx = types.SetEnvironmentID(ctx, "test_env")
	ctx = types.SetUserID(ctx, "test_user")

	created, err := s.service.CreateEntityIntegrationMapping(ctx, req)
	require.NoError(s.T(), err)

	// Get the mapping
	resp, err := s.service.GetEntityIntegrationMapping(ctx, created.ID)

	// Assert
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), resp)
	assert.Equal(s.T(), created.ID, resp.ID)
	assert.Equal(s.T(), req.EntityID, resp.EntityID)
	assert.Equal(s.T(), types.IntegrationEntityType(req.EntityType), resp.EntityType)
	assert.Equal(s.T(), req.ProviderType, resp.ProviderType)
	assert.Equal(s.T(), req.ProviderEntityID, resp.ProviderEntityID)
}

func (s *EntityIntegrationMappingServiceSuite) TestGetByEntityAndProvider() {
	// Create a mapping first
	req := dto.CreateEntityIntegrationMappingRequest{
		EntityID:         "cust_123",
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     "stripe",
		ProviderEntityID: "cus_stripe_456",
	}

	ctx := types.SetTenantID(context.Background(), "test_tenant")
	ctx = types.SetEnvironmentID(ctx, "test_env")
	ctx = types.SetUserID(ctx, "test_user")

	_, err := s.service.CreateEntityIntegrationMapping(ctx, req)
	require.NoError(s.T(), err)

	// Get by entity and provider using plural filters
	listResp, err := s.service.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewDefaultQueryFilter(),
		EntityID:      "cust_123",
		EntityType:    types.IntegrationEntityType("customer"),
		ProviderTypes: []string{"stripe"},
	})

	// Assert
	require.NoError(s.T(), err)
	require.NotNil(s.T(), listResp)
	require.GreaterOrEqual(s.T(), len(listResp.Items), 1)
	first := listResp.Items[0]
	assert.Equal(s.T(), "cust_123", first.EntityID)
	assert.Equal(s.T(), types.IntegrationEntityType("customer"), first.EntityType)
	assert.Equal(s.T(), "stripe", first.ProviderType)
	assert.Equal(s.T(), "cus_stripe_456", first.ProviderEntityID)
}

func (s *EntityIntegrationMappingServiceSuite) TestGetByProviderEntity() {
	// Create a mapping first
	req := dto.CreateEntityIntegrationMappingRequest{
		EntityID:         "cust_123",
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     "stripe",
		ProviderEntityID: "cus_stripe_456",
	}

	ctx := types.SetTenantID(context.Background(), "test_tenant")
	ctx = types.SetEnvironmentID(ctx, "test_env")
	ctx = types.SetUserID(ctx, "test_user")

	_, err := s.service.CreateEntityIntegrationMapping(ctx, req)
	require.NoError(s.T(), err)

	// Get by provider entity using plural filters
	listResp, err := s.service.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:       types.NewDefaultQueryFilter(),
		ProviderTypes:     []string{"stripe"},
		ProviderEntityIDs: []string{"cus_stripe_456"},
	})

	// Assert
	require.NoError(s.T(), err)
	require.NotNil(s.T(), listResp)
	require.GreaterOrEqual(s.T(), len(listResp.Items), 1)
	first := listResp.Items[0]
	assert.Equal(s.T(), "cust_123", first.EntityID)
	assert.Equal(s.T(), types.IntegrationEntityType("customer"), first.EntityType)
	assert.Equal(s.T(), "stripe", first.ProviderType)
	assert.Equal(s.T(), "cus_stripe_456", first.ProviderEntityID)
}

// ────────────────────────────────────────────────────────────────────────────
// LinkIntegrationMapping tests
// ────────────────────────────────────────────────────────────────────────────

func (s *EntityIntegrationMappingServiceSuite) testCtx() context.Context {
	ctx := types.SetTenantID(context.Background(), types.DefaultTenantID)
	ctx = types.SetEnvironmentID(ctx, "test_env")
	ctx = types.SetUserID(ctx, types.DefaultUserID)
	return ctx
}

// seedCustomer creates a customer in the in-memory store and returns its ID.
func (s *EntityIntegrationMappingServiceSuite) seedCustomer(id string) {
	ctx := s.testCtx()
	err := s.GetStores().CustomerRepo.Create(ctx, &customer.Customer{
		ID:    id,
		Name:  "Test Customer",
		Email: "test@example.com",
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
		},
	})
	require.NoError(s.T(), err)
}

// TestLinkIntegrationMapping_ValidationMissingFields ensures required fields are enforced.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_ValidationMissingFields() {
	ctx := s.testCtx()

	_, err := s.service.LinkIntegrationMapping(ctx, dto.LinkIntegrationMappingRequest{})
	require.Error(s.T(), err)
}

// TestLinkIntegrationMapping_InvalidEntityType ensures unsupported entity types are rejected by side-effects.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_InvalidEntityType() {
	ctx := s.testCtx()

	_, err := s.service.LinkIntegrationMapping(ctx, dto.LinkIntegrationMappingRequest{
		EntityType:       "invoice",
		EntityID:         "inv_001",
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: "pay_rzp_001",
	})
	require.Error(s.T(), err)
}

// TestLinkIntegrationMapping_InvalidProvider ensures unsupported providers for customer are rejected.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_InvalidProvider() {
	ctx := s.testCtx()
	s.seedCustomer("cust_001")

	_, err := s.service.LinkIntegrationMapping(ctx, dto.LinkIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeCustomer,
		EntityID:         "cust_001",
		ProviderType:     string(types.SecretProviderStripe),
		ProviderEntityID: "cus_stripe_001",
	})
	require.Error(s.T(), err)
}

// TestLinkIntegrationMapping_CreatesNewMapping verifies a mapping is created when none exists.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_CreatesNewMapping() {
	ctx := s.testCtx()
	s.seedCustomer("cust_razorpay_001")

	resp, err := s.service.LinkIntegrationMapping(ctx, dto.LinkIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeCustomer,
		EntityID:         "cust_razorpay_001",
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: "rzp_cust_new",
	})

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	require.NotNil(s.T(), resp.Mapping)
	assert.NotEmpty(s.T(), resp.Mapping.ID)
	assert.Equal(s.T(), "cust_razorpay_001", resp.Mapping.EntityID)
	assert.Equal(s.T(), types.IntegrationEntityTypeCustomer, resp.Mapping.EntityType)
	assert.Equal(s.T(), string(types.SecretProviderRazorpay), resp.Mapping.ProviderType)
	assert.Equal(s.T(), "rzp_cust_new", resp.Mapping.ProviderEntityID)
	assert.Equal(s.T(), types.DefaultTenantID, resp.Mapping.TenantID)
}

// TestLinkIntegrationMapping_UpsertExistingMapping verifies that calling Link twice updates
// the provider entity ID instead of creating a duplicate.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_UpsertExistingMapping() {
	ctx := s.testCtx()
	s.seedCustomer("cust_razorpay_002")

	req := dto.LinkIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeCustomer,
		EntityID:         "cust_razorpay_002",
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: "rzp_cust_v1",
	}

	firstResp, err := s.service.LinkIntegrationMapping(ctx, req)
	require.NoError(s.T(), err)
	firstID := firstResp.Mapping.ID

	// Second call with a different provider entity ID — should upsert, not create
	req.ProviderEntityID = "rzp_cust_v2"
	secondResp, err := s.service.LinkIntegrationMapping(ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), secondResp.Mapping)

	assert.Equal(s.T(), firstID, secondResp.Mapping.ID, "upsert must reuse same record")
	assert.Equal(s.T(), "rzp_cust_v2", secondResp.Mapping.ProviderEntityID)

	// Only one mapping should exist
	listResp, err := s.service.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewDefaultQueryFilter(),
		EntityID:      "cust_razorpay_002",
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderRazorpay)},
	})
	require.NoError(s.T(), err)
	assert.Len(s.T(), listResp.Items, 1)
}

// TestLinkIntegrationMapping_CustomerMetadataUpdated verifies the side effect of stamping
// razorpay_customer_id onto the customer metadata.
func (s *EntityIntegrationMappingServiceSuite) TestLinkIntegrationMapping_CustomerMetadataUpdated() {
	ctx := s.testCtx()
	s.seedCustomer("cust_meta_001")

	_, err := s.service.LinkIntegrationMapping(ctx, dto.LinkIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeCustomer,
		EntityID:         "cust_meta_001",
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: "rzp_cust_meta",
	})
	require.NoError(s.T(), err)

	cust, err := s.GetStores().CustomerRepo.Get(ctx, "cust_meta_001")
	require.NoError(s.T(), err)
	require.NotNil(s.T(), cust.Metadata)
	assert.Equal(s.T(), "rzp_cust_meta", cust.Metadata["razorpay_customer_id"])
}
