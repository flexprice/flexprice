package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// CheckoutCreateDraftSubscriptionSuite exercises createDraftSubscription end-to-end —
// the create-subscription checkout path that computes the draft invoice and applies taxes
// before payment. Regression coverage for the stale-invoice / zero AmountDue bug.
type CheckoutCreateDraftSubscriptionSuite struct {
	testutil.BaseServiceTestSuite
	svc      *checkoutSessionService
	planID   string
	charge   decimal.Decimal
	customer *customer.Customer
}

func TestCheckoutCreateDraftSubscriptionSuite(t *testing.T) {
	suite.Run(t, new(CheckoutCreateDraftSubscriptionSuite))
}

func (s *CheckoutCreateDraftSubscriptionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.charge = decimal.NewFromInt(20)
	s.svc = NewCheckoutSessionService(s.buildParams()).(*checkoutSessionService)
	s.setupPlanAndCustomer()
}

func (s *CheckoutCreateDraftSubscriptionSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CheckoutCreateDraftSubscriptionSuite) buildParams() ServiceParams {
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		PriceUnitRepo:                s.GetStores().PriceUnitRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		UserRepo:                     s.GetStores().UserRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonRepo:                    s.GetStores().AddonRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		CheckoutSessionRepo:          s.GetStores().CheckoutSessionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
		PlanPriceSyncRepo:            s.GetStores().PlanPriceSyncRepo,
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
}

func (s *CheckoutCreateDraftSubscriptionSuite) setupPlanAndCustomer() {
	ctx := s.GetContext()

	s.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_checkout_draft_tax",
		Name:       "Checkout Draft Tax Customer",
		Email:      "checkout-draft-tax@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.customer))

	p := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Checkout Draft Tax Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, p))
	s.planID = p.ID

	fixedPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             s.charge,
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
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, fixedPrice))
}

func (s *CheckoutCreateDraftSubscriptionSuite) createCustomerTaxAssociation(pct int64) {
	ctx := s.GetContext()
	pctDec := decimal.NewFromInt(pct)
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "Checkout Create Draft Tax",
		Code:            "checkout_create_draft_tax_" + types.GenerateUUIDWithPrefix("code"),
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pctDec,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))

	assoc := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     tr.ID,
		EntityType:    types.TaxRateEntityTypeCustomer,
		EntityID:      s.customer.ID,
		Priority:      100,
		AutoApply:     true,
		Currency:      "usd",
		StartDate:     time.Now().UTC().Add(-24 * time.Hour),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))
}

func (s *CheckoutCreateDraftSubscriptionSuite) newSession() *domainCheckout.CheckoutSession {
	ctx := s.GetContext()
	return &domainCheckout.CheckoutSession{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		CustomerID:     s.customer.ID,
		Action:         types.CheckoutActionCreateSubscription,
		CheckoutStatus: types.CheckoutStatusInitiated,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        s.planID,
				Currency:      "usd",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		}),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func (s *CheckoutCreateDraftSubscriptionSuite) TestCreateDraftSubscription_WithCustomerTax_AmountDueIncludesTax() {
	s.createCustomerTaxAssociation(10)

	_, invResp, err := s.svc.createDraftSubscription(s.GetContext(), s.newSession())
	s.Require().NoError(err)
	s.Require().NotNil(invResp)

	expectedTax := types.RoundToCurrencyPrecision(s.charge.Mul(decimal.NewFromInt(10)).Div(decimal.NewFromInt(100)), "usd")
	expectedDue := s.charge.Add(expectedTax)

	s.True(expectedTax.Equal(invResp.TotalTax), "expected total_tax %s, got %s", expectedTax, invResp.TotalTax)
	s.True(expectedDue.Equal(invResp.AmountDue),
		"expected amount_due %s (checkout payment fails if zero), got %s", expectedDue, invResp.AmountDue)
	s.True(invResp.AmountDue.GreaterThan(decimal.Zero))
}

func (s *CheckoutCreateDraftSubscriptionSuite) TestCreateDraftSubscription_NoTax_AmountDueEqualsCharge() {
	_, invResp, err := s.svc.createDraftSubscription(s.GetContext(), s.newSession())
	s.Require().NoError(err)
	s.Require().NotNil(invResp)

	s.True(invResp.TotalTax.IsZero(), "expected zero total_tax, got %s", invResp.TotalTax)
	s.True(s.charge.Equal(invResp.AmountDue),
		"expected amount_due %s, got %s", s.charge, invResp.AmountDue)
}
