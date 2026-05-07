package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionTrialPaymentMatrixSuite documents trial-end converting-invoice behavior for
// collection_method × payment_behavior × payment outcome (design spec 2026-04-23).
//
// Preconditions: trialing subscription with current period = trial window; simulates the
// HandlePaymentBehavior step used by ProcessDraftInvoice after trial-end cron (InvoiceFlowRenewal).
//
// Without a Stripe connection and with amount_due > 0, card charge is a no-op → payment fails.
// With amount_due == 0, processPayment short-circuits as success (no gateway).
type SubscriptionTrialPaymentMatrixSuite struct {
	testutil.BaseServiceTestSuite
	proc SubscriptionPaymentProcessor
}

func TestSubscriptionTrialPaymentMatrix(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialPaymentMatrixSuite))
}

func (s *SubscriptionTrialPaymentMatrixSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := &ServiceParams{
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
	}
	s.proc = NewSubscriptionPaymentProcessor(params)
}

func (s *SubscriptionTrialPaymentMatrixSuite) TestMatrix_HandlePaymentBehavior_RenewalFlow() {
	ctx := s.GetContext()
	trialStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Matrix Customer",
		Email:     "matrix@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Matrix Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// Period is already advanced to the first real billing window by processSubscriptionTrialEnd.
	firstPeriodEnd, err := types.NextBillingDate(trialEnd, trialStart, 1, types.BILLING_PERIOD_MONTHLY, nil)
	s.Require().NoError(err)

	tests := []struct {
		name             string
		collectionMethod types.CollectionMethod
		paymentBehavior  types.PaymentBehavior
		amountDue        decimal.Decimal
		wantStatus       types.SubscriptionStatus
		notes            string
	}{
		{
			name:             "charge_automatically_allow_incomplete_payment_fails",
			collectionMethod: types.CollectionMethodChargeAutomatically,
			paymentBehavior:  types.PaymentBehaviorAllowIncomplete,
			amountDue:        decimal.NewFromInt(25),
			wantStatus:       types.SubscriptionStatusIncomplete,
			notes:            "Unpaid invoice → stays incomplete; period already advanced at trial end.",
		},
		{
			name:             "charge_automatically_default_active_payment_fails",
			collectionMethod: types.CollectionMethodChargeAutomatically,
			paymentBehavior:  types.PaymentBehaviorDefaultActive,
			amountDue:        decimal.NewFromInt(25),
			wantStatus:       types.SubscriptionStatusActive,
			notes:            "default_active: always activates regardless of payment; period already correct.",
		},
		{
			name:             "charge_automatically_error_if_incomplete_payment_fails_renewal",
			collectionMethod: types.CollectionMethodChargeAutomatically,
			paymentBehavior:  types.PaymentBehaviorErrorIfIncomplete,
			amountDue:        decimal.NewFromInt(25),
			wantStatus:       types.SubscriptionStatusIncomplete,
			notes:            "Renewal flow: payment failure does not change status; stays incomplete.",
		},
		{
			name:             "charge_automatically_allow_incomplete_zero_due_treated_paid",
			collectionMethod: types.CollectionMethodChargeAutomatically,
			paymentBehavior:  types.PaymentBehaviorAllowIncomplete,
			amountDue:        decimal.Zero,
			wantStatus:       types.SubscriptionStatusActive,
			notes:            "Zero-amount: marks active immediately.",
		},
		{
			name:             "send_invoice_default_active",
			collectionMethod: types.CollectionMethodSendInvoice,
			paymentBehavior:  types.PaymentBehaviorDefaultActive,
			amountDue:        decimal.NewFromInt(40),
			wantStatus:       types.SubscriptionStatusActive,
			notes:            "send_invoice + default_active: activates without waiting for payment.",
		},
		{
			name:             "send_invoice_default_incomplete",
			collectionMethod: types.CollectionMethodSendInvoice,
			paymentBehavior:  types.PaymentBehaviorDefaultIncomplete,
			amountDue:        decimal.NewFromInt(40),
			wantStatus:       types.SubscriptionStatusIncomplete,
			notes:            "Unpaid invoice → stays incomplete.",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Subscription arrives at HandlePaymentBehavior already in incomplete state with
			// period advanced to [trialEnd, firstPeriodEnd] by processSubscriptionTrialEnd.
			sub := &subscription.Subscription{
				ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
				CustomerID:         cust.ID,
				PlanID:             pl.ID,
				SubscriptionStatus: types.SubscriptionStatusIncomplete,
				Currency:           "usd",
				BillingAnchor:      trialStart,
				BillingCycle:       types.BillingCycleAnniversary,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				StartDate:          trialStart,
				CurrentPeriodStart: trialEnd,
				CurrentPeriodEnd:   firstPeriodEnd,
				TrialStart:         &trialStart,
				TrialEnd:           &trialEnd,
				CollectionMethod:   string(tt.collectionMethod),
				PaymentBehavior:    string(tt.paymentBehavior),
				BaseModel:          types.GetDefaultBaseModel(ctx),
			}
			s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

			invID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE)
			amtDue := tt.amountDue
			amtRem := tt.amountDue
			inv := &invoice.Invoice{
				ID:              invID,
				CustomerID:      cust.ID,
				SubscriptionID:  &sub.ID,
				InvoiceType:     types.InvoiceTypeSubscription,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        "usd",
				AmountDue:       amtDue,
				AmountRemaining: amtRem,
				AmountPaid:      decimal.Zero,
				Total:           amtDue,
				Subtotal:        amtDue,
				BillingReason:   string(types.InvoiceBillingReasonSubscriptionTrialEnd),
				BaseModel:       types.GetDefaultBaseModel(ctx),
				LineItems:       []*invoice.InvoiceLineItem{},
			}
			s.Require().NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))

			invResp := dto.NewInvoiceResponse(inv)
			s.Require().NoError(s.proc.HandlePaymentBehavior(ctx, sub, invResp, tt.paymentBehavior, types.InvoiceFlowRenewal))

			updated, gerr := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(gerr)
			s.Require().Equal(tt.wantStatus, updated.SubscriptionStatus, tt.notes)

			// Period must always remain at the first real billing window — HandlePaymentBehavior
			// does not touch periods; they were set correctly by processSubscriptionTrialEnd.
			s.True(updated.CurrentPeriodStart.Equal(trialEnd), "period start should be at trial end")
			s.True(updated.CurrentPeriodEnd.Equal(firstPeriodEnd), "period end should be at first period end")

			invAfter, ierr := s.GetStores().InvoiceRepo.Get(ctx, invID)
			s.Require().NoError(ierr)
			s.Equal(types.PaymentStatusPending, invAfter.PaymentStatus, "HandlePaymentBehavior does not mark invoice paid")
		})
	}
}

func (s *SubscriptionTrialPaymentMatrixSuite) TestFullPayAfterBehavior_ActivatesFromIncomplete() {
	ctx := s.GetContext()
	trialStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Pay After Customer",
		Email:     "payafter@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Pay After Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// processSubscriptionTrialEnd already advanced the period to [trialEnd, firstPeriodEnd]
	// and set status to incomplete before creating the invoice.
	firstPeriodEnd, err := types.NextBillingDate(trialEnd, trialStart, 1, types.BILLING_PERIOD_MONTHLY, nil)
	s.Require().NoError(err)

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
		Currency:           "usd",
		BillingAnchor:      trialStart,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          trialStart,
		CurrentPeriodStart: trialEnd,
		CurrentPeriodEnd:   firstPeriodEnd,
		TrialStart:         &trialStart,
		TrialEnd:           &trialEnd,
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:    string(types.PaymentBehaviorAllowIncomplete),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	inv := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      cust.ID,
		SubscriptionID:  &sub.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusSucceeded,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(10),
		AmountRemaining: decimal.Zero,
		AmountPaid:      decimal.NewFromInt(10),
		Total:           decimal.NewFromInt(10),
		Subtotal:        decimal.NewFromInt(10),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PaidAt:          &paidAt,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	svc := NewSubscriptionService(ServiceParams{
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

	s.Require().NoError(svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))

	updated, gerr := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(gerr)
	// Period was already advanced at trial end; paying the invoice only flips status to active.
	s.Equal(types.SubscriptionStatusActive, updated.SubscriptionStatus)
	s.True(updated.CurrentPeriodStart.Equal(trialEnd), "period start remains at trial end")
	s.True(updated.CurrentPeriodEnd.Equal(firstPeriodEnd), "period end remains at first period end")
}
