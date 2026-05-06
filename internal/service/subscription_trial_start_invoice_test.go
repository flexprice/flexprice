package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionTrialStartInvoiceSuite struct {
	testutil.BaseServiceTestSuite
	subSvc SubscriptionService
}

func TestSubscriptionTrialStartInvoice(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialStartInvoiceSuite))
}

func (s *SubscriptionTrialStartInvoiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.subSvc = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

// setupTrialFixtures creates a customer, plan, and recurring fixed price with 14-day trial.
func (s *SubscriptionTrialStartInvoiceSuite) setupTrialFixtures() (custID, planID string) {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Start Test Customer",
		Email:     "trialstart@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Start Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	pr := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Type:               types.PRICE_TYPE_FIXED,
		TrialPeriodDays:    14,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))

	return cust.ID, pl.ID
}

// findTrialStartInvoice returns the SUBSCRIPTION_TRIAL_START invoice for a subscription, or nil.
func (s *SubscriptionTrialStartInvoiceSuite) findTrialStartInvoice(subID string) *domaininvoice.Invoice {
	ctx := s.GetContext()
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	for _, inv := range invoices {
		if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) {
			return inv
		}
	}
	return nil
}

// TestTrialStartInvoice_CreatedFinalizedPaid verifies that creating a trialing subscription
// produces a SUBSCRIPTION_TRIAL_START invoice that is FINALIZED, paid, with invoice number,
// and that the subscription stays TRIALING.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_CreatedFinalizedPaid() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	now := time.Now().UTC()
	sub, err := s.subSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        &now,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorAllowIncomplete),
	})
	s.Require().NoError(err)
	s.Require().NotNil(sub)
	s.Equal(types.SubscriptionStatusTrialing, sub.SubscriptionStatus)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Require().NotNil(inv, "SUBSCRIPTION_TRIAL_START invoice must be created")
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus, "must be FINALIZED")
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus, "must be paid")
	s.True(inv.Subtotal.IsZero(), "subtotal must be $0")
	s.True(inv.AmountDue.IsZero(), "amount_due must be $0")
	s.NotNil(inv.InvoiceNumber, "invoice number must be assigned")
	s.NotEmpty(lo.FromPtr(inv.InvoiceNumber), "invoice number must not be empty")

	// Critical: subscription must stay TRIALING — not activated
	updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, updatedSub.SubscriptionStatus,
		"subscription must stay TRIALING after $0 trial-start invoice is paid")
}

// TestTrialStartInvoice_NotCreatedForNonTrialing verifies a non-trial subscription does not
// produce a SUBSCRIPTION_TRIAL_START invoice.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_NotCreatedForNonTrialing() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	now2 := time.Now().UTC()
	sub, err := s.subSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        &now2,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorDefaultActive),
		TrialPeriodDays:  lo.ToPtr(0), // explicitly no trial
	})
	s.Require().NoError(err)
	s.Require().NotNil(sub)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Nil(inv, "SUBSCRIPTION_TRIAL_START invoice must NOT exist for non-trialing subscription")
}

// TestTrialStartInvoice_HasLineItems verifies the trial-start invoice contains line items
// showing what the customer would pay after trial ends.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_HasLineItems() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	now3 := time.Now().UTC()
	sub, err := s.subSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        &now3,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorAllowIncomplete),
	})
	s.Require().NoError(err)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Require().NotNil(inv, "SUBSCRIPTION_TRIAL_START invoice must exist")

	lineItems, err := s.GetStores().InvoiceLineItemRepo.List(ctx, &types.InvoiceLineItemFilter{
		InvoiceIDs: []string{inv.ID},
	})
	s.Require().NoError(err)
	s.NotEmpty(lineItems, "trial-start invoice must have line items showing post-trial charges")
}
