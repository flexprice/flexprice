package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// An active, checkout-capable connection is a precondition for every create path
	// (the provider is resolved from the connection table, not hardcoded). Seed a
	// published Stripe connection bound to the test ctx's tenant+environment so
	// resolveCheckoutProvider returns it.
	s.seedStripeConnection()
}

// seedStripeConnection seeds a published Stripe connection for the test ctx's
// tenant+environment so resolveCheckoutProvider can resolve a provider.
func (s *CheckoutServiceTestSuite) seedStripeConnection() {
	ctx := s.GetContext()
	err := s.GetStores().ConnectionRepo.Create(ctx, &connection.Connection{
		ID:            "conn_stripe_checkout_test",
		Name:          "Stripe",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(ctx),
			Status:    types.StatusPublished,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
		},
	})
	require.NoError(s.T(), err)
}

// newCheckoutService constructs the concrete checkout service with the provider
// seam overridden so no real gateway is contacted.
func (s *CheckoutServiceTestSuite) newCheckoutService() *checkoutService {
	svc := &checkoutService{ServiceParams: s.params}
	svc.providerFn = func(ctx context.Context, provider types.CheckoutProvider) (checkout.CheckoutProvider, error) {
		return fakeCheckoutProvider{}, nil
	}
	return svc
}

// createTestCustomer seeds a customer in the in-memory store.
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

// createTestPlan seeds a plan + a single fixed monthly price.
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
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           types.CheckoutModePayment,
		Subscription: &dto.CreateSubscriptionRequest{
			CustomerID:    cust.ID,
			PlanID:        planID,
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
		SuccessURL: "https://app.test/success",
		CancelURL:  "https://app.test/cancel",
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
	pending, err := s.checkoutRepo.GetPendingByEntity(ctx, checkout.GetPendingByEntityParams{
		EntityType: types.CheckoutEntityTypeSubscription,
		EntityID:   newSubID,
		Mode:       types.CheckoutModePayment,
	})
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

func (s *CheckoutServiceTestSuite) TestCreate_SetupObjective() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Setup Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           types.CheckoutModeSetup,
		Subscription: &dto.CreateSubscriptionRequest{
			CustomerID:    cust.ID,
			PlanID:        planID,
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
		SuccessURL: "https://app.test/success",
		CancelURL:  "https://app.test/cancel",
	})

	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	assert.NotEmpty(s.T(), resp.ID)
	assert.Equal(s.T(), "https://stripe.test/cs_test", resp.CheckoutURL)
	assert.Equal(s.T(), string(types.CheckoutStatusPending), resp.Status)

	chk, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), chk)
	assert.Equal(s.T(), types.CheckoutModeSetup, chk.Mode)
	newSubID := chk.EntityID
	require.NotEmpty(s.T(), newSubID)

	pending, err := s.checkoutRepo.GetPendingByEntity(ctx, checkout.GetPendingByEntityParams{
		EntityType: types.CheckoutEntityTypeSubscription,
		EntityID:   newSubID,
		Mode:       types.CheckoutModeSetup,
	})
	require.NoError(s.T(), err)
	require.NotNil(s.T(), pending)
	assert.Equal(s.T(), resp.ID, pending.ID)

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, newSubID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.SubscriptionStatusDraft, sub.SubscriptionStatus)

	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter:    types.NewDefaultQueryFilter(),
		SubscriptionID: newSubID,
	})
	require.NoError(s.T(), err)
	assert.Empty(s.T(), invoices, "draft subscription must not raise an opening invoice")
}

func (s *CheckoutServiceTestSuite) TestComplete_Idempotent() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Complete Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           types.CheckoutModePayment,
		Subscription: &dto.CreateSubscriptionRequest{
			CustomerID:    cust.ID,
			PlanID:        planID,
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
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

func (s *CheckoutServiceTestSuite) TestComplete_SetupActivatesDraft() {
	ctx := s.GetContext()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("Setup Complete Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	resp, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           types.CheckoutModeSetup,
		Subscription: &dto.CreateSubscriptionRequest{
			CustomerID:    cust.ID,
			PlanID:        planID,
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
		SuccessURL: "https://app.test/success",
		CancelURL:  "https://app.test/cancel",
	})
	require.NoError(s.T(), err)

	chk, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	subID := chk.EntityID

	// Precondition: subscription is DRAFT.
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionStatusDraft, sub.SubscriptionStatus)

	// Complete -> activates the draft sub and marks the checkout completed.
	require.NoError(s.T(), svc.Complete(ctx, resp.ID))

	completed, err := s.checkoutRepo.Get(ctx, resp.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.CheckoutStatusCompleted, completed.Status)
	require.NotNil(s.T(), completed.CompletedAt)

	// The subscription is no longer DRAFT (activated; active or incomplete).
	sub, err = s.GetStores().SubscriptionRepo.Get(ctx, subID)
	require.NoError(s.T(), err)
	assert.NotEqual(s.T(), types.SubscriptionStatusDraft, sub.SubscriptionStatus)

	// Idempotent: a second Complete is a no-op and does not error.
	require.NoError(s.T(), svc.Complete(ctx, resp.ID))
}

// TestCreate_NoActiveConnection asserts that with no active checkout-capable
// connection, Create fails fast with an ErrNotFound (the provider can no longer be
// hardcoded — it must resolve from the connection table).
func (s *CheckoutServiceTestSuite) TestCreate_NoActiveConnection() {
	ctx := s.GetContext()

	// Remove the seeded Stripe connection so no checkout-capable provider resolves.
	s.GetStores().ConnectionRepo.(*testutil.InMemoryConnectionStore).Clear()

	cust := s.createTestCustomer()
	planID := s.createTestPlan("No Connection Plan", decimal.NewFromFloat(25.00))

	svc := s.newCheckoutService()
	_, err := svc.Create(ctx, dto.CreateCheckoutRequest{
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           types.CheckoutModePayment,
		Subscription: &dto.CreateSubscriptionRequest{
			CustomerID:    cust.ID,
			PlanID:        planID,
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
		SuccessURL: "https://app.test/success",
		CancelURL:  "https://app.test/cancel",
	})

	require.Error(s.T(), err)
	assert.True(s.T(), ierr.IsNotFound(err), "expected ErrNotFound when no active payment connection exists")
	assert.Contains(s.T(), err.Error(), "no active payment connection")
}
