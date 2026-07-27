package invoice

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type InvoiceActivitiesSuite struct {
	testutil.BaseServiceTestSuite
	activities *InvoiceActivities
}

func TestInvoiceActivities(t *testing.T) {
	suite.Run(t, new(InvoiceActivitiesSuite))
}

// GetContext returns context with environment ID set, matching InvoiceServiceSuite's override —
// the activity's Validate() requires a non-empty environment_id.
func (s *InvoiceActivitiesSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *InvoiceActivitiesSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	params := service.ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		UserRepo:                     s.GetStores().UserRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
	s.activities = NewInvoiceActivities(params, s.GetLogger())
}

// setupFinalizedSubscription creates a customer, plan, subscription with a finalized invoice
// covering the subscription's current period, and returns the subscription ID.
func (s *InvoiceActivitiesSuite) setupFinalizedSubscription() string {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         "cust_skip_test",
		ExternalID: "ext_cust_skip_test",
		Name:       "Skip Test Customer",
		Email:      "skip_test@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        "plan_skip_test",
		Name:      "Skip Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	now := time.Now().UTC()
	periodStart := now.AddDate(0, 0, -10)
	periodEnd := now.AddDate(0, 0, 20)

	sub := &subscription.Subscription{
		ID:                 "sub_skip_test",
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, nil))

	billingPeriodStr := string(sub.BillingPeriod)
	finalizedInvoice := &invoice.Invoice{
		ID:              "inv_skip_test",
		CustomerID:      cust.ID,
		SubscriptionID:  &sub.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        sub.Currency,
		BillingPeriod:   &billingPeriodStr,
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		AmountDue:       decimal.Zero,
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.Zero,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(ctx, finalizedInvoice))

	return sub.ID
}

func (s *InvoiceActivitiesSuite) TestCreateDraftForCurrentSubscriptionPeriodActivity_SkipIfAlreadyInvoiced() {
	subID := s.setupFinalizedSubscription()
	ctx := s.GetContext()

	s.Run("SkipIfAlreadyInvoiced=false returns an error, matching today's behavior", func() {
		_, err := s.activities.CreateDraftForCurrentSubscriptionPeriodActivity(ctx, invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput{
			SubscriptionID: subID,
			TenantID:       types.GetTenantID(ctx),
			EnvironmentID:  types.GetEnvironmentID(ctx),
			UserID:         "user_test",
		})
		s.Require().Error(err)
		s.Require().True(ierr.IsAlreadyExists(err))
	})

	s.Run("SkipIfAlreadyInvoiced=true returns Skipped=true, no error", func() {
		out, err := s.activities.CreateDraftForCurrentSubscriptionPeriodActivity(ctx, invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput{
			SubscriptionID:        subID,
			TenantID:              types.GetTenantID(ctx),
			EnvironmentID:         types.GetEnvironmentID(ctx),
			UserID:                "user_test",
			SkipIfAlreadyInvoiced: true,
		})
		s.Require().NoError(err)
		s.Require().True(out.Skipped)
		s.Require().Empty(out.InvoiceID)
	})
}
