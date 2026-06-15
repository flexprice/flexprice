package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// fakeCheckoutProvider is a stand-in for a real (e.g. Stripe) checkout provider
// so the service test never reaches out to an external gateway.
type fakeCheckoutProvider struct{}

func (fakeCheckoutProvider) CreateCheckoutSession(ctx context.Context, req checkout.CheckoutSessionRequest) (*checkout.CheckoutSessionResponse, error) {
	return &checkout.CheckoutSessionResponse{SessionID: "sess_test", URL: "https://stripe.test/cs_test"}, nil
}

type CheckoutServiceTestSuite struct {
	testutil.BaseServiceTestSuite
	checkoutRepo *testutil.InMemoryCheckoutStore
	params       ServiceParams
	planService  *planService
	priceService *priceService
}

func TestCheckoutService(t *testing.T) {
	suite.Run(t, new(CheckoutServiceTestSuite))
}

func (s *CheckoutServiceTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupServices()
}

func (s *CheckoutServiceTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CheckoutServiceTestSuite) setupServices() {
	// CheckoutRepo is not part of the shared testutil Stores, so we construct the
	// in-memory checkout store directly and wire it into ServiceParams.
	s.checkoutRepo = testutil.NewInMemoryCheckoutStore()

	s.params = ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		UserRepo:                     s.GetStores().UserRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		TaskRepo:                     s.GetStores().TaskRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:             s.GetStores().FeatureUsageRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
		PlanPriceSyncRepo:            s.GetStores().PlanPriceSyncRepo,
		CheckoutRepo:                 s.checkoutRepo,
	}

	s.planService = NewPlanService(s.params).(*planService)
	s.priceService = NewPriceService(s.params).(*priceService)
}

// newCheckoutService constructs the concrete checkout service with the provider
// seam overridden so no real gateway is contacted.
func (s *CheckoutServiceTestSuite) newCheckoutService() *checkoutService {
	svc := &checkoutService{ServiceParams: s.params}
	svc.providerFn = func(ctx context.Context, provider string) (checkout.CheckoutProvider, error) {
		return fakeCheckoutProvider{}, nil
	}
	return svc
}

// createTestCustomer seeds a customer in the in-memory store. Mirrors the
// subscription_change test setup.
func (s *CheckoutServiceTestSuite) createTestCustomer() *customer.Customer {
	ctx := s.GetContext()
	cust := &customer.Customer{
		ID:         s.GetUUID(),
		ExternalID: "ext_" + s.GetUUID(),
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().CustomerRepo.Create(ctx, cust))
	return cust
}

// createTestPlan seeds a plan + a single fixed monthly price. Mirrors the
// subscription_change test setup so CreateSubscription succeeds in-memory.
func (s *CheckoutServiceTestSuite) createTestPlan(name string, amount decimal.Decimal) string {
	ctx := s.GetContext()

	planResponse, err := s.planService.CreatePlan(ctx, dto.CreatePlanRequest{
		Name:        name,
		Description: "Test plan for checkout",
	})
	require.NoError(s.T(), err)

	amt := amount
	_, err = s.priceService.CreatePrice(ctx, dto.CreatePriceRequest{
		Amount:             &amt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planResponse.Plan.ID,
	})
	require.NoError(s.T(), err)

	return planResponse.Plan.ID
}

func (s *CheckoutServiceTestSuite) TestCreate_PaymentObjective() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Checkout Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CustomerID:    cust.ID,
		PlanID:        planID,
		Currency:      "usd",
		Objective:     types.CheckoutObjectivePayment,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		SuccessURL:    "https://app.test/success",
		CancelURL:     "https://app.test/cancel",
	})

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	assert.NotEmpty(s.T(), resp.ID)
	assert.NotEmpty(s.T(), resp.CheckoutURL)
	assert.Equal(s.T(), "https://stripe.test/cs_test", resp.CheckoutURL)
	assert.Equal(s.T(), string(types.CheckoutStatusPending), resp.Status)

	// The persisted checkout binds to a newly created subscription. Find it.
	chk, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), chk)
	newSubID := chk.EntityID
	assert.NotEmpty(s.T(), newSubID)
	assert.Equal(s.T(), types.CheckoutEntityTypeSubscription, chk.EntityType)

	// A pending checkout exists for (subscription, newSubID, payment).
	pending, err := s.checkoutRepo.GetPendingByEntity(
		ctx, types.CheckoutEntityTypeSubscription, newSubID, types.CheckoutObjectivePayment)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), pending)
	assert.Equal(s.T(), resp.ID, pending.ID)

	// The created subscription is incomplete (deferred activation pending payment).
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, newSubID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.SubscriptionStatusIncomplete, sub.SubscriptionStatus)

	// An opening invoice exists for the subscription.
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter:    types.NewDefaultQueryFilter(),
		SubscriptionID: newSubID,
	})
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), invoices, "expected an opening invoice for the subscription")
}

func (s *CheckoutServiceTestSuite) TestCreate_SetupObjectiveUnsupported() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Setup Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CustomerID:    cust.ID,
		PlanID:        planID,
		Currency:      "usd",
		Objective:     types.CheckoutObjectiveSetup,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY,
	})

	require.Error(s.T(), err)
	assert.Nil(s.T(), resp)
}

func (s *CheckoutServiceTestSuite) TestComplete_Idempotent() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Complete Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CustomerID:    cust.ID,
		PlanID:        planID,
		Currency:      "usd",
		Objective:     types.CheckoutObjectivePayment,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY,
	})
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)

	// First complete -> transitions to completed.
	require.NoError(s.T(), svc.Complete(ctx, resp.ID))
	chk, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.CheckoutStatusCompleted, chk.Status)
	require.NotNil(s.T(), chk.CompletedAt)

	// Second complete -> idempotent no-op, stays completed.
	require.NoError(s.T(), svc.Complete(ctx, resp.ID))
	chk2, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.CheckoutStatusCompleted, chk2.Status)
}
