package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ─────────────────────────────────────────────
// Suite definition
// ─────────────────────────────────────────────

type CheckoutSessionServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc CheckoutSessionService
}

func TestCheckoutSessionServiceSuite(t *testing.T) {
	suite.Run(t, new(CheckoutSessionServiceSuite))
}

func (s *CheckoutSessionServiceSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *CheckoutSessionServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewCheckoutSessionService(s.buildServiceParams())
}

func (s *CheckoutSessionServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CheckoutSessionServiceSuite) buildServiceParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        stores.SubscriptionPhaseRepo,
		SubScheduleRepo:              stores.SubscriptionScheduleRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		PriceUnitRepo:                stores.PriceUnitRepo,
		EventRepo:                    stores.EventRepo,
		MeterRepo:                    stores.MeterRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		FeatureRepo:                  stores.FeatureRepo,
		TenantRepo:                   stores.TenantRepo,
		UserRepo:                     stores.UserRepo,
		AuthRepo:                     stores.AuthRepo,
		WalletRepo:                   stores.WalletRepo,
		PaymentRepo:                  stores.PaymentRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		AddonRepo:                    stores.AddonRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		SettingsRepo:                 stores.SettingsRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,
		PlanPriceSyncRepo:            stores.PlanPriceSyncRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		CreditNoteRepo:               stores.CreditNoteRepo,
		CreditNoteLineItemRepo:       stores.CreditNoteLineItemRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		FeatureUsageRepo:             stores.FeatureUsageRepo,
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
}

// ─────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────

func (s *CheckoutSessionServiceSuite) createCustomer() *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		BaseModel:  types.GetDefaultBaseModel(ctx),
		ExternalID: "ext_test_customer",
		Name:       "Test Customer",
		Email:      "test@example.com",
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

// createPlanWithPrice creates a plan and a $29 FLAT_FEE MONTHLY ADVANCE price linked to it.
// Returns (plan, price).
func (s *CheckoutSessionServiceSuite) createPlanWithPrice() (*plan.Plan, *price.Price) {
	ctx := s.GetContext()
	p := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Starter Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, p))

	pr := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(29),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           p.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
	return p, pr
}

// createCheckoutReq builds a standard CreateSubscription checkout request.
func (s *CheckoutSessionServiceSuite) createCheckoutReq(customerID, planID string) dto.CreateCheckoutSessionRequest {
	return dto.CreateCheckoutSessionRequest{
		CustomerID:      customerID,
		Action:          types.CheckoutActionCreateSubscription,
		PaymentProvider: types.CheckoutPaymentProviderStripe,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        planID,
				Currency:      "USD",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
	}
}

// ─────────────────────────────────────────────
// Group: Create
// ─────────────────────────────────────────────

func (s *CheckoutSessionServiceSuite) TestCreate_HappyPath() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()

	req := s.createCheckoutReq(cust.ID, pl.ID)
	resp, err := s.svc.Create(s.GetContext(), req)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.CheckoutStatusPending, resp.CheckoutStatus)
	s.Require().NotNil(resp.Result)
	s.Require().NotNil(resp.Result.CreateSubscriptionResult)
	s.NotEmpty(resp.Result.CreateSubscriptionResult.SubscriptionID)
	s.NotEmpty(resp.Result.CreateSubscriptionResult.InvoiceID)
	s.NotEmpty(resp.Result.CreateSubscriptionResult.PaymentID)
	s.NotNil(resp.CheckoutInvoiceID)
	s.NotNil(resp.CheckoutPaymentID)

	// Subscription should be DRAFT (not activated until CompleteCheckoutSession)
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), resp.Result.CreateSubscriptionResult.SubscriptionID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusDraft, sub.SubscriptionStatus)

	// Invoice should be DRAFT (not finalized until CompleteCheckoutSession)
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), resp.Result.CreateSubscriptionResult.InvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, inv.InvoiceStatus)
	s.True(inv.AmountDue.IsPositive(), "invoice amount_due should be > 0")

	// Payment should be INITIATED
	pay, err := s.GetStores().PaymentRepo.Get(s.GetContext(), resp.Result.CreateSubscriptionResult.PaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusInitiated, pay.PaymentStatus)
}

func (s *CheckoutSessionServiceSuite) TestCreate_NonExistentPlan() {
	cust := s.createCustomer()
	req := s.createCheckoutReq(cust.ID, "plan_does_not_exist")

	resp, err := s.svc.Create(s.GetContext(), req)

	s.Require().Error(err)
	s.Nil(resp)

	// The session must have been created and then marked as failed by CleanupCheckoutSession.
	filter := types.NewDefaultCheckoutSessionFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	sessions, listErr := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), filter)
	s.Require().NoError(listErr)
	s.Require().Len(sessions, 1, "one session should have been persisted")
	s.Equal(types.CheckoutStatusFailed, sessions[0].CheckoutStatus)
	s.NotNil(sessions[0].FailureReason)
}

func (s *CheckoutSessionServiceSuite) TestCreate_ValidationError_NoPlanID() {
	cust := s.createCustomer()
	req := dto.CreateCheckoutSessionRequest{
		CustomerID:      cust.ID,
		Action:          types.CheckoutActionCreateSubscription,
		PaymentProvider: types.CheckoutPaymentProviderStripe,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				Currency:      "USD",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
				// PlanID intentionally omitted
			},
		},
	}

	resp, err := s.svc.Create(s.GetContext(), req)

	s.Require().Error(err)
	s.Nil(resp)

	// Validation fires before the session is persisted — nothing in the store.
	filter := types.NewDefaultCheckoutSessionFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	sessions, listErr := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), filter)
	s.Require().NoError(listErr)
	s.Empty(sessions, "no session should be created on validation failure")
}

// ─────────────────────────────────────────────
// Group: CompleteCheckoutSession
// ─────────────────────────────────────────────

func (s *CheckoutSessionServiceSuite) TestComplete_HappyPath() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)

	err = s.svc.CompleteCheckoutSession(s.GetContext(), resp.ID, nil)
	s.Require().NoError(err)

	// Session should be completed
	session, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, session.CheckoutStatus)
	s.NotNil(session.CompletedAt)

	res := resp.Result.CreateSubscriptionResult

	// Subscription should now be active
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), res.SubscriptionID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, sub.SubscriptionStatus)

	// Invoice should be finalized
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), res.InvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)

	// Payment should be succeeded
	pay, err := s.GetStores().PaymentRepo.Get(s.GetContext(), res.PaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusSucceeded, pay.PaymentStatus)
}

func (s *CheckoutSessionServiceSuite) TestComplete_WithProviderResult() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)

	providerResult := &types.CheckoutProviderResult{
		ProviderSessionID:       "cs_stripe_session_abc",
		ProviderPaymentIntentID: "pi_abc123",
	}
	err = s.svc.CompleteCheckoutSession(s.GetContext(), resp.ID, providerResult)
	s.Require().NoError(err)

	// Gateway payment ID should be stored on the payment
	res := resp.Result.CreateSubscriptionResult
	pay, err := s.GetStores().PaymentRepo.Get(s.GetContext(), res.PaymentID)
	s.Require().NoError(err)
	s.Require().NotNil(pay.GatewayPaymentID)
	s.Equal("pi_abc123", *pay.GatewayPaymentID)
}

func (s *CheckoutSessionServiceSuite) TestComplete_SessionNotFound() {
	err := s.svc.CompleteCheckoutSession(s.GetContext(), "sess_does_not_exist", nil)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "expected not_found error, got: %v", err)
}

func (s *CheckoutSessionServiceSuite) TestComplete_AlreadyCompleted() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)

	// Complete once
	s.Require().NoError(s.svc.CompleteCheckoutSession(s.GetContext(), resp.ID, nil))

	// Complete again — should fail with already-exists (terminal state guard)
	err = s.svc.CompleteCheckoutSession(s.GetContext(), resp.ID, nil)
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected already-exists error, got: %v", err)
}

func (s *CheckoutSessionServiceSuite) TestComplete_FailedSession() {
	cust := s.createCustomer()
	// Use non-existent plan so Create returns a failed session
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, "plan_missing"))
	s.Require().Error(err)
	s.Nil(resp)

	// Find the failed session
	filter := types.NewDefaultCheckoutSessionFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	sessions, _ := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), filter)
	s.Require().Len(sessions, 1)
	failedSessionID := sessions[0].ID

	err = s.svc.CompleteCheckoutSession(s.GetContext(), failedSessionID, nil)
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected already-exists error for failed session, got: %v", err)
}

// ─────────────────────────────────────────────
// Group: CleanupCheckoutSession
// ─────────────────────────────────────────────

func (s *CheckoutSessionServiceSuite) TestCleanup_HappyPath() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)
	s.Require().Equal(types.CheckoutStatusPending, resp.CheckoutStatus)

	res := resp.Result.CreateSubscriptionResult

	session, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)

	err = s.svc.CleanupCheckoutSession(s.GetContext(), session, nil)
	s.Require().NoError(err)

	// Session should be expired (cleanup with no reason = natural expiry)
	updated, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, updated.CheckoutStatus)
	s.Nil(updated.FailureReason)

	// In-memory stores hard-delete; verify all three entities were removed.
	_, err = s.GetStores().PaymentRepo.Get(s.GetContext(), res.PaymentID)
	s.True(ierr.IsNotFound(err), "payment should have been deleted on cleanup")

	_, err = s.GetStores().InvoiceRepo.Get(s.GetContext(), res.InvoiceID)
	s.True(ierr.IsNotFound(err), "invoice should have been deleted on cleanup")

	_, err = s.GetStores().SubscriptionRepo.Get(s.GetContext(), res.SubscriptionID)
	s.True(ierr.IsNotFound(err), "subscription should have been deleted on cleanup")
}

func (s *CheckoutSessionServiceSuite) TestCleanup_WithFailureReason() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)

	session, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)

	reason := ierr.NewError("payment_failed: card was declined").Mark(ierr.ErrValidation)
	err = s.svc.CleanupCheckoutSession(s.GetContext(), session, reason)
	s.Require().NoError(err)

	updated, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusFailed, updated.CheckoutStatus)
	s.Require().NotNil(updated.FailureReason)
	s.Contains(*updated.FailureReason, "card was declined")
}

func (s *CheckoutSessionServiceSuite) TestCleanup_NoEntities() {
	// A session with no result (e.g. created but fulfillment never ran) has nothing to archive.
	ctx := s.GetContext()
	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, "non_existent")
	_ = session
	s.Require().Error(err) // ensure baseline

	// Build a minimal session directly in the repo with no result set.
	cust := s.createCustomer()
	fakeSession := &dto.CreateCheckoutSessionRequest{
		CustomerID:      cust.ID,
		Action:          types.CheckoutActionCreateSubscription,
		PaymentProvider: types.CheckoutPaymentProviderStripe,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        "any_plan",
				Currency:      "USD",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
	}
	domSess := fakeSession.ToCheckoutSession(ctx)
	domSess.CheckoutStatus = types.CheckoutStatusInitiated
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, domSess))

	err = s.svc.CleanupCheckoutSession(ctx, domSess, nil)
	s.Require().NoError(err)

	updated, err := s.GetStores().CheckoutSessionRepo.Get(ctx, domSess.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, updated.CheckoutStatus)
}

// ─────────────────────────────────────────────
// Group: Get / List
// ─────────────────────────────────────────────

func (s *CheckoutSessionServiceSuite) TestGet_Existing() {
	cust := s.createCustomer()
	pl, _ := s.createPlanWithPrice()
	resp, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(cust.ID, pl.ID))
	s.Require().NoError(err)

	got, err := s.svc.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.Equal(resp.ID, got.ID)
	s.Equal(types.CheckoutStatusPending, got.CheckoutStatus)
}

func (s *CheckoutSessionServiceSuite) TestGet_NotFound() {
	_, err := s.svc.Get(s.GetContext(), "sess_does_not_exist")
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "expected not_found error, got: %v", err)
}

func (s *CheckoutSessionServiceSuite) TestList_ByCustomerID() {
	custA := s.createCustomer()
	custB := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		ExternalID: "ext_customer_b",
		Name:       "Customer B",
		Email:      "b@example.com",
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), custB))

	pl, _ := s.createPlanWithPrice()

	// Create two sessions for custA and one for custB
	_, err := s.svc.Create(s.GetContext(), s.createCheckoutReq(custA.ID, pl.ID))
	s.Require().NoError(err)
	_, err = s.svc.Create(s.GetContext(), s.createCheckoutReq(custA.ID, pl.ID))
	s.Require().NoError(err)

	// custB session: also needs its own plan+price (no idempotency key set, but same plan is fine)
	pl2, _ := s.createPlanWithPrice()
	_, err = s.svc.Create(s.GetContext(), s.createCheckoutReq(custB.ID, pl2.ID))
	s.Require().NoError(err)

	filter := types.NewDefaultCheckoutSessionFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	filter.CustomerIDs = []string{custA.ID}

	result, err := s.svc.List(s.GetContext(), filter)
	s.Require().NoError(err)
	s.Require().Equal(2, result.Pagination.Total)
	for _, sess := range result.Items {
		s.Equal(custA.ID, sess.CustomerID)
	}
}

