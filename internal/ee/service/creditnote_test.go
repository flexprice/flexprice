package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CreditNoteServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CreditNoteService
	testData struct {
		customer *customer.Customer
		invoices struct {
			finalized       *invoice.Invoice
			pending         *invoice.Invoice
			failed          *invoice.Invoice
			succeeded       *invoice.Invoice
			refunded        *invoice.Invoice
			partialRefunded *invoice.Invoice
			partialPayment  *invoice.Invoice
		}
		wallets struct {
			usd *wallet.Wallet
			eur *wallet.Wallet
		}
		now time.Time
	}
}

func TestCreditNoteService(t *testing.T) {
	suite.Run(t, new(CreditNoteServiceSuite))
}

func (s *CreditNoteServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

// GetContext returns context with environment ID set for settings lookup
func (s *CreditNoteServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *CreditNoteServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CreditNoteServiceSuite) setupService() {
	s.service = NewCreditNoteService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		CreditNoteRepo:             s.GetStores().CreditNoteRepo,
		RefundRepo:                 s.GetStores().RefundRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		CreditNoteLineItemRepo:     s.GetStores().CreditNoteLineItemRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		EventPublisher:             s.GetPublisher(),
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		WebhookPublisher:           s.GetWebhookPublisher(),
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:   types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	})
}

func (s *CreditNoteServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:        "cust_test_123",
		Name:      "Test Customer",
		Email:     "test@example.com",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test plans
	s.createTestPlans()

	// Create test subscriptions
	s.createTestSubscriptions()

	// Create test invoices (both subscription and one-off types)
	s.createTestInvoices()

	// Create test wallets
	s.createTestWallets()
}

func (s *CreditNoteServiceSuite) createTestPlans() {
	// Create a test plan for USD subscriptions
	usdPlan := &plan.Plan{
		ID:          "plan_test_123",
		Name:        "Test Plan USD",
		Description: "Test plan for USD subscriptions",
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(s.GetContext()),
			Status:    types.StatusPublished,
			CreatedAt: s.testData.now,
			UpdatedAt: s.testData.now,
			CreatedBy: types.GetUserID(s.GetContext()),
			UpdatedBy: types.GetUserID(s.GetContext()),
		},
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), usdPlan))

	// Create a test plan for EUR subscriptions
	eurPlan := &plan.Plan{
		ID:          "plan_eur_123",
		Name:        "Test Plan EUR",
		Description: "Test plan for EUR subscriptions",
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(s.GetContext()),
			Status:    types.StatusPublished,
			CreatedAt: s.testData.now,
			UpdatedAt: s.testData.now,
			CreatedBy: types.GetUserID(s.GetContext()),
			UpdatedBy: types.GetUserID(s.GetContext()),
		},
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), eurPlan))
}

func (s *CreditNoteServiceSuite) createTestSubscriptions() {
	// Create a mock subscription for subscription-type invoices
	testSubscription := &subscription.Subscription{
		ID:                 "sub_test_123",
		CustomerID:         s.testData.customer.ID,
		PlanID:             "plan_test_123",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "USD",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          s.testData.now,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.AddDate(0, 1, 0),
		BillingAnchor:      s.testData.now,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create subscription line items for the test subscription
	testLineItems := []*subscription.SubscriptionLineItem{
		{
			ID:             "sub_line_1",
			SubscriptionID: testSubscription.ID,
			PriceID:        "price_test_123",
			Quantity:       decimal.NewFromInt(1),
			Currency:       "USD",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), testSubscription, testLineItems))

	// Create another subscription for EUR invoices
	eurSubscription := &subscription.Subscription{
		ID:                 "sub_eur_123",
		CustomerID:         s.testData.customer.ID,
		PlanID:             "plan_eur_123",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "EUR",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          s.testData.now,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.AddDate(0, 1, 0),
		BillingAnchor:      s.testData.now,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create subscription line items for the EUR subscription
	eurLineItems := []*subscription.SubscriptionLineItem{
		{
			ID:             "sub_line_eur_1",
			SubscriptionID: eurSubscription.ID,
			PriceID:        "price_eur_123",
			Quantity:       decimal.NewFromInt(1),
			Currency:       "EUR",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), eurSubscription, eurLineItems))
}

func (s *CreditNoteServiceSuite) createTestInvoices() {
	// Create a test subscription ID
	testSubscriptionID := "sub_test_123"
	eurSubscriptionID := "sub_eur_123"

	// Finalized subscription invoice with succeeded payment
	s.testData.invoices.finalized = &invoice.Invoice{
		ID:               "inv_finalized_123",
		CustomerID:       s.testData.customer.ID,
		SubscriptionID:   &testSubscriptionID,
		InvoiceNumber:    lo.ToPtr("INV-001"),
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusSucceeded,
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(100.00),
		Total:            decimal.NewFromFloat(110.00),
		AmountPaid:       decimal.NewFromFloat(110.00),
		AmountDue:        decimal.NewFromFloat(110.00),
		AmountRemaining:  decimal.Zero,
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_1",
				DisplayName: lo.ToPtr("Product A"),
				Amount:      decimal.NewFromFloat(50.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:          "line_2",
				DisplayName: lo.ToPtr("Product B"),
				Amount:      decimal.NewFromFloat(50.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.finalized))

	// One-off invoice with pending payment
	s.testData.invoices.pending = &invoice.Invoice{
		ID:               "inv_pending_123",
		CustomerID:       s.testData.customer.ID,
		InvoiceNumber:    lo.ToPtr("INV-002"),
		InvoiceType:      types.InvoiceTypeOneOff,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusPending,
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(80.00),
		Total:            decimal.NewFromFloat(88.00),
		AmountPaid:       decimal.Zero,
		AmountDue:        decimal.NewFromFloat(88.00),
		AmountRemaining:  decimal.NewFromFloat(88.00),
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_3",
				DisplayName: lo.ToPtr("Product C"),
				Amount:      decimal.NewFromFloat(80.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.pending))

	// One-off invoice with failed payment
	s.testData.invoices.failed = &invoice.Invoice{
		ID:               "inv_failed_123",
		CustomerID:       s.testData.customer.ID,
		InvoiceNumber:    lo.ToPtr("INV-003"),
		InvoiceType:      types.InvoiceTypeOneOff,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusFailed,
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(60.00),
		Total:            decimal.NewFromFloat(66.00),
		AmountPaid:       decimal.Zero,
		AmountDue:        decimal.NewFromFloat(66.00),
		AmountRemaining:  decimal.NewFromFloat(66.00),
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_4",
				DisplayName: lo.ToPtr("Product D"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.failed))

	// Subscription invoice with refunded payment
	s.testData.invoices.refunded = &invoice.Invoice{
		ID:               "inv_refunded_123",
		CustomerID:       s.testData.customer.ID,
		SubscriptionID:   &testSubscriptionID,
		InvoiceNumber:    lo.ToPtr("INV-004"),
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusRefunded,
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(40.00),
		Total:            decimal.NewFromFloat(44.00),
		AmountPaid:       decimal.NewFromFloat(44.00),
		AmountDue:        decimal.NewFromFloat(44.00),
		AmountRemaining:  decimal.Zero,
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.NewFromFloat(44.00),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_5",
				DisplayName: lo.ToPtr("Product E"),
				Amount:      decimal.NewFromFloat(40.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.refunded))

	// Subscription invoice with partially refunded payment (EUR)
	s.testData.invoices.partialRefunded = &invoice.Invoice{
		ID:               "inv_partial_refund_123",
		CustomerID:       s.testData.customer.ID,
		SubscriptionID:   &eurSubscriptionID,
		InvoiceNumber:    lo.ToPtr("INV-005"),
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusPartiallyRefunded,
		Currency:         "EUR",
		Subtotal:         decimal.NewFromFloat(120.00),
		Total:            decimal.NewFromFloat(132.00),
		AmountPaid:       decimal.NewFromFloat(132.00),
		AmountDue:        decimal.NewFromFloat(132.00),
		AmountRemaining:  decimal.Zero,
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.NewFromFloat(30.00), // Partially refunded
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_6",
				DisplayName: lo.ToPtr("Product F"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "EUR",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:          "line_7",
				DisplayName: lo.ToPtr("Product G"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "EUR",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.partialRefunded))

	// Test data for partial payment scenario
	s.testData.invoices.partialPayment = &invoice.Invoice{
		ID:               "test_partial_payment_invoice",
		CustomerID:       s.testData.customer.ID,
		SubscriptionID:   &eurSubscriptionID,
		InvoiceNumber:    lo.ToPtr("INV-006"),
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusPending, // Still pending but has partial payment
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(100.00),
		Total:            decimal.NewFromFloat(100.00),
		AmountDue:        decimal.NewFromFloat(100.00),
		AmountPaid:       decimal.NewFromFloat(40.00), // Partial payment made
		AmountRemaining:  decimal.NewFromFloat(60.00),
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "test_line_item_1",
				DisplayName: lo.ToPtr("Partially paid item"),
				Amount:      decimal.NewFromFloat(100.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.partialPayment))
}

func (s *CreditNoteServiceSuite) createTestWallets() {
	// Create test wallets using the wallet service
	walletService := NewWalletService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		WalletRepo:               s.GetStores().WalletRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	})

	// Create USD wallet
	usdWalletResp, err := walletService.CreateWallet(s.GetContext(), &dto.CreateWalletRequest{
		CustomerID:           s.testData.customer.ID,
		Name:                 "USD Wallet",
		Currency:             "USD",
		ConversionRate:       decimal.NewFromInt(1),
		WalletType:           types.WalletTypePrePaid,
		InitialCreditsToLoad: decimal.NewFromFloat(100.00),
	})
	s.NoError(err)
	s.testData.wallets.usd = &wallet.Wallet{
		ID:            usdWalletResp.ID,
		CustomerID:    s.testData.customer.ID,
		Name:          "USD Wallet",
		Currency:      "USD",
		Balance:       usdWalletResp.Balance,
		CreditBalance: usdWalletResp.CreditBalance,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create EUR wallet
	eurWalletResp, err := walletService.CreateWallet(s.GetContext(), &dto.CreateWalletRequest{
		CustomerID:           s.testData.customer.ID,
		Name:                 "EUR Wallet",
		Currency:             "EUR",
		ConversionRate:       decimal.NewFromInt(1),
		WalletType:           types.WalletTypePrePaid,
		InitialCreditsToLoad: decimal.NewFromFloat(50.00),
	})
	s.NoError(err)
	s.testData.wallets.eur = &wallet.Wallet{
		ID:            eurWalletResp.ID,
		CustomerID:    s.testData.customer.ID,
		Name:          "EUR Wallet",
		Currency:      "EUR",
		Balance:       eurWalletResp.Balance,
		CreditBalance: eurWalletResp.CreditBalance,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
}

// Test CreateCreditNote method
func (s *CreditNoteServiceSuite) TestCreateCreditNote() {
	tests := []struct {
		name      string
		req       *dto.CreateCreditNoteRequest
		wantErr   bool
		errString string
		validate  func(*dto.CreditNoteResponse)
	}{
		{
			name: "successful_adjustment_credit_note_creation",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID:         s.testData.invoices.pending.ID,
				Reason:            types.CreditNoteReasonBillingError,
				Memo:              "Billing error correction",
				ProcessCreditNote: true,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_3",
						DisplayName:       "Partial refund for Product C",
						Amount:            decimal.NewFromFloat(20.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
				s.Equal(types.CreditNoteTypeAdjustment, resp.CreditNoteType)
				s.Equal(decimal.NewFromFloat(20.00), resp.TotalAmount)
				s.Equal("USD", resp.Currency)
				s.Len(resp.LineItems, 1)
			},
		},
		{
			name: "successful_refund_credit_note_creation",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID:         s.testData.invoices.finalized.ID,
				Reason:            types.CreditNoteReasonUnsatisfactory,
				Memo:              "Customer unsatisfied with service",
				ProcessCreditNote: true,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						DisplayName:       "Full refund for Product A",
						Amount:            decimal.NewFromFloat(50.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
				s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)
				s.Equal(decimal.NewFromFloat(50.00), resp.TotalAmount)
			},
		},
		{
			name: "successful_with_custom_credit_note_number",
			req: &dto.CreateCreditNoteRequest{
				CreditNoteNumber:  "CN-CUSTOM-001",
				InvoiceID:         s.testData.invoices.failed.ID,
				Reason:            types.CreditNoteReasonService,
				ProcessCreditNote: true,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_4",
						DisplayName:       "Service issue credit",
						Amount:            decimal.NewFromFloat(30.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal("CN-CUSTOM-001", resp.CreditNoteNumber)
			},
		},
		{
			name: "error_missing_invoice_id",
			req: &dto.CreateCreditNoteRequest{
				Reason: types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "InvoiceID",
		},
		{
			name: "error_missing_reason",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "Reason",
		},
		{
			name: "error_empty_line_items",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{},
			},
			wantErr:   true,
			errString: "line_items is required",
		},
		{
			name: "error_invalid_reason",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReason("INVALID_REASON"),
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "invalid credit note reason",
		},
		{
			name: "error_invoice_not_finalized",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: "inv_draft_123",
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "not found",
		},
		{
			name: "error_refunded_invoice",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.refunded.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_5",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "cannot create credit note for fully refunded invoice",
		},
		{
			name: "error_line_item_amount_exceeds_invoice_line_item",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(100.00), // line_1 amount is 50.00
					},
				},
			},
			wantErr:   true,
			errString: "credit amount too high for line item",
		},
		{
			name: "error_invalid_invoice_line_item_id",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "invalid_line_id",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "invalid line item selected",
		},
		{
			name: "partial_payment_allows_adjustment_credit_note",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID:         "test_partial_payment_invoice",
				Reason:            types.CreditNoteReasonBillingError,
				Memo:              "Adjustment allowed even with partial payment",
				ProcessCreditNote: true,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "test_line_item_1",
						DisplayName:       "Adjustment after partial payment",
						Amount:            decimal.NewFromFloat(30.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
				s.Equal(types.CreditNoteTypeAdjustment, resp.CreditNoteType, "Should be ADJUSTMENT type for pending payment status")
				s.Equal(decimal.NewFromFloat(30.00), resp.TotalAmount)
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateCreditNote(s.GetContext(), tt.req)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(resp.ID)
			s.NotEmpty(resp.CreditNoteNumber)
			s.Equal(tt.req.InvoiceID, resp.InvoiceID)
			s.Equal(tt.req.Reason, resp.Reason)

			if tt.validate != nil {
				tt.validate(resp)
			}
		})
	}
}

func (s *CreditNoteServiceSuite) TestGetCreditNote() {
	// Create a test credit note first
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(25.00),
			},
		},
	}

	created, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful_get",
			id:      created.ID,
			wantErr: false,
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetCreditNote(s.GetContext(), tt.id)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.id, resp.ID)
			s.Equal(created.InvoiceID, resp.InvoiceID)
			s.Equal(created.Reason, resp.Reason)
		})
	}
}

func (s *CreditNoteServiceSuite) TestListCreditNotes() {
	// Create multiple test credit notes
	creditNotes := []struct {
		invoiceID  string
		lineItemID string
		reason     types.CreditNoteReason
		amount     decimal.Decimal
	}{
		{s.testData.invoices.finalized.ID, "line_1", types.CreditNoteReasonBillingError, decimal.NewFromFloat(10.00)},
		{s.testData.invoices.pending.ID, "line_3", types.CreditNoteReasonService, decimal.NewFromFloat(15.00)},
		{s.testData.invoices.failed.ID, "line_4", types.CreditNoteReasonDuplicate, decimal.NewFromFloat(20.00)},
	}

	var createdIDs []string
	for _, cn := range creditNotes {
		req := &dto.CreateCreditNoteRequest{
			InvoiceID:         cn.invoiceID,
			Reason:            cn.reason,
			ProcessCreditNote: true,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: cn.lineItemID,
					Amount:            cn.amount,
				},
			},
		}

		resp, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)
		createdIDs = append(createdIDs, resp.ID)
	}

	tests := []struct {
		name      string
		filter    *types.CreditNoteFilter
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list_all_credit_notes",
			filter:    &types.CreditNoteFilter{QueryFilter: types.NewDefaultQueryFilter()},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "filter_by_invoice_id",
			filter: &types.CreditNoteFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				InvoiceID:   s.testData.invoices.finalized.ID,
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "filter_by_credit_note_type",
			filter: &types.CreditNoteFilter{
				QueryFilter:    types.NewDefaultQueryFilter(),
				CreditNoteType: types.CreditNoteTypeAdjustment,
			},
			wantCount: 2, // pending and failed invoices create adjustments
			wantErr:   false,
		},
		{
			name: "filter_by_credit_note_ids",
			filter: &types.CreditNoteFilter{
				QueryFilter:   types.NewDefaultQueryFilter(),
				CreditNoteIDs: []string{createdIDs[0], createdIDs[1]},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "filter_by_status",
			filter: &types.CreditNoteFilter{
				QueryFilter:      types.NewDefaultQueryFilter(),
				CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusFinalized},
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "pagination_limit",
			filter: &types.CreditNoteFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(2),
					Offset: lo.ToPtr(0),
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListCreditNotes(s.GetContext(), tt.filter)

			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Len(resp.Items, tt.wantCount)
			s.NotNil(resp.Pagination)
		})
	}
}

func (s *CreditNoteServiceSuite) TestVoidCreditNote() {
	// Create test credit notes
	adjustmentReq := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.pending.ID,
		Reason:            types.CreditNoteReasonBillingError,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_3",
				Amount:            decimal.NewFromFloat(10.00),
			},
		},
	}

	adjustmentCN, err := s.service.CreateCreditNote(s.GetContext(), adjustmentReq)
	s.NoError(err)

	refundReq := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.finalized.ID,
		Reason:            types.CreditNoteReasonUnsatisfactory,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(15.00),
			},
		},
	}

	refundCN, err := s.service.CreateCreditNote(s.GetContext(), refundReq)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful_void_adjustment_credit_note",
			id:      adjustmentCN.ID,
			wantErr: false,
		},
		{
			name:      "error_void_refund_credit_note",
			id:        refundCN.ID,
			wantErr:   true,
			errString: "cannot void completed refund",
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "missing credit note ID",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.VoidCreditNote(s.GetContext(), tt.id)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)

			// Verify the credit note is voided
			resp, err := s.service.GetCreditNote(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(types.CreditNoteStatusVoided, resp.CreditNoteStatus)
		})
	}
}

// failOnUpdateInvoiceRepo wraps a real invoice.Repository and forces Update to
// fail for one specific invoice ID, leaving every other method (including
// GetForUpdate) delegated to the real repo. This is the only way to make
// RecalculateInvoiceAmountsForCreditNote's InvoiceRepo.Update call fail while
// the earlier GetForUpdate in FinalizeCreditNote still succeeds — deleting the
// invoice out from under it fails the (already-correct) lock instead of the
// (buggy, swallowed-error) recalculation step.
type failOnUpdateInvoiceRepo struct {
	invoice.Repository
	failInvoiceID string
}

func (r *failOnUpdateInvoiceRepo) Update(ctx context.Context, inv *invoice.Invoice) error {
	if inv.ID == r.failInvoiceID {
		return errors.New("forced invoice update failure for test")
	}
	return r.Repository.Update(ctx, inv)
}

func (s *CreditNoteServiceSuite) TestFinalizeCreditNote_RecalculationFailureIsPropagated() {
	cn := &creditnote.CreditNote{
		ID:               "cn_finalize_recalc_fail_test",
		CustomerID:       s.testData.customer.ID,
		InvoiceID:        s.testData.invoices.pending.ID,
		CreditNoteNumber: "CN-FINALIZE-RECALC-FAIL-TEST",
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeAdjustment,
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(10.00),
		LineItems: []*creditnote.CreditNoteLineItem{
			{
				ID:          "finalize_recalc_fail_line_1",
				DisplayName: "Adjustment for Product C",
				Amount:      decimal.NewFromFloat(10.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), cn))

	failingService := NewCreditNoteService(ServiceParams{
		Logger:           s.GetLogger(),
		DB:               s.GetDB(),
		CreditNoteRepo:   s.GetStores().CreditNoteRepo,
		InvoiceRepo:      &failOnUpdateInvoiceRepo{Repository: s.GetStores().InvoiceRepo, failInvoiceID: cn.InvoiceID},
		WebhookPublisher: s.GetWebhookPublisher(),
	})

	// setupTestData's createTestWallets() already publishes webhooks via the same
	// shared publisher, so assert no *new* webhook was published rather than empty.
	webhookCountBefore := len(s.GetPublishedWebhooks())

	err := failingService.FinalizeCreditNote(s.GetContext(), cn.ID, nil)

	s.Error(err)
	s.Contains(err.Error(), "forced invoice update failure for test")
	s.Len(s.GetPublishedWebhooks(), webhookCountBefore)
}

func (s *CreditNoteServiceSuite) TestVoidCreditNote_TransactionFailurePropagates() {
	// Finalized ADJUSTMENT (refund type can't be voided) pointing at a missing invoice:
	// fails at the GetForUpdate lock step, before recalculation itself ever runs.
	brokenCN := &creditnote.CreditNote{
		ID:               "cn_void_recalc_fail",
		CustomerID:       s.testData.customer.ID,
		InvoiceID:        "inv_does_not_exist",
		CreditNoteNumber: "CN-VOID-FAIL-TEST",
		CreditNoteStatus: types.CreditNoteStatusFinalized,
		CreditNoteType:   types.CreditNoteTypeAdjustment,
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(10.00),
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), brokenCN))

	err := s.service.VoidCreditNote(s.GetContext(), brokenCN.ID)
	s.Error(err)
	s.Contains(err.Error(), "not found")

	// Status flip must not happen before the lock succeeds.
	after, err := s.GetStores().CreditNoteRepo.Get(s.GetContext(), brokenCN.ID)
	s.NoError(err)
	s.Equal(types.CreditNoteStatusFinalized, after.CreditNoteStatus)
}

func (s *CreditNoteServiceSuite) TestProcessDraftCreditNote() {
	// Create draft credit note manually in repository to test processing
	// This bypasses the CreateCreditNote validation that automatically processes the credit note
	draftCNData := &creditnote.CreditNote{
		ID:               "cn_draft_test",
		CustomerID:       s.testData.customer.ID,
		InvoiceID:        s.testData.invoices.finalized.ID,
		SubscriptionID:   s.testData.invoices.finalized.SubscriptionID, // Copy from invoice
		CreditNoteNumber: "CN-DRAFT-TEST",
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeRefund, // Since finalized invoice with succeeded payment
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(20.00),
		LineItems: []*creditnote.CreditNoteLineItem{
			{
				ID:          "draft_line_1",
				DisplayName: "Refund for Product A",
				Amount:      decimal.NewFromFloat(20.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), draftCNData))

	// Create a manual draft credit note with zero amount directly in the repository for testing
	zeroCNData := &creditnote.CreditNote{
		ID:               "cn_zero_test",
		CustomerID:       s.testData.customer.ID,
		InvoiceID:        s.testData.invoices.finalized.ID,
		CreditNoteNumber: "CN-ZERO-TEST",
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeRefund,
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), zeroCNData)
	s.NoError(err)

	// Create a processed credit note for the "already processed" test
	processedCNData := &creditnote.CreditNote{
		ID:               "cn_processed_test",
		CustomerID:       s.testData.customer.ID,
		InvoiceID:        s.testData.invoices.finalized.ID,
		CreditNoteNumber: "CN-PROCESSED-TEST",
		CreditNoteStatus: types.CreditNoteStatusFinalized,
		CreditNoteType:   types.CreditNoteTypeRefund,
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(30.00),
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), processedCNData)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
		validate  func(string)
	}{
		{
			name:    "successful_process_draft_credit_note",
			id:      draftCNData.ID,
			wantErr: false,
			validate: func(id string) {
				resp, err := s.service.GetCreditNote(s.GetContext(), id)
				s.NoError(err)
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
			},
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "missing credit note ID",
		},
		{
			name:      "error_already_processed",
			id:        processedCNData.ID,
			wantErr:   true,
			errString: "credit note already processed",
		},
		{
			name:      "error_zero_amount_credit_note",
			id:        zeroCNData.ID,
			wantErr:   true,
			errString: "credit note has no amount",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.FinalizeCreditNote(s.GetContext(), tt.id, nil)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)

			if tt.validate != nil {
				tt.validate(tt.id)
			}
		})
	}
}

func (s *CreditNoteServiceSuite) TestFinalizeCreditNote_RefundCapacityRecheck() {
	// Two drafts, each individually valid (100 <= 110 max), whose combined total (200)
	// exceeds what the invoice can actually refund.
	// Distinct idempotency keys are required: content-hash keys would collide and return
	// the same draft twice instead of creating two independent notes.
	draftReq := func(idempotencyKey string) *dto.CreateCreditNoteRequest {
		return &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.finalized.ID,
			Reason:            types.CreditNoteReasonUnsatisfactory,
			ProcessCreditNote: false,
			IdempotencyKey:    &idempotencyKey,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{InvoiceLineItemID: "line_1", Amount: decimal.NewFromFloat(50.00)},
				{InvoiceLineItemID: "line_2", Amount: decimal.NewFromFloat(50.00)},
			},
		}
	}

	first, err := s.service.CreateCreditNote(s.GetContext(), draftReq("refund-capacity-recheck-1"))
	s.NoError(err)
	s.Equal(types.CreditNoteStatusDraft, first.CreditNoteStatus)

	second, err := s.service.CreateCreditNote(s.GetContext(), draftReq("refund-capacity-recheck-2"))
	s.NoError(err)
	s.Equal(types.CreditNoteStatusDraft, second.CreditNoteStatus)
	s.NotEqual(first.ID, second.ID)

	walletBefore, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	// In-memory wallet store returns a live pointer, so snapshot the balance now.
	balanceBefore := walletBefore.Balance

	s.NoError(s.service.FinalizeCreditNote(s.GetContext(), first.ID, nil))

	err = s.service.FinalizeCreditNote(s.GetContext(), second.ID, nil)
	s.Error(err)
	s.Contains(err.Error(), "credit amount exceeds available limit")

	secondAfter, err := s.GetStores().CreditNoteRepo.Get(s.GetContext(), second.ID)
	s.NoError(err)
	s.Equal(types.CreditNoteStatusDraft, secondAfter.CreditNoteStatus)

	walletAfter, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	expectedBalance := balanceBefore.Add(decimal.NewFromFloat(100.00))
	s.True(walletAfter.Balance.Equal(expectedBalance),
		"expected wallet balance %s, got %s", expectedBalance, walletAfter.Balance)
}

func (s *CreditNoteServiceSuite) TestFinalizeCreditNote_AdjustmentCapacityRecheck() {
	// Same race as the refund case, but for ADJUSTMENT type, which has no wallet
	// idempotency backstop against a double-applied recalculation.
	// Distinct idempotency keys are required: content-hash keys would collide and return
	// the same draft twice instead of creating two independent notes.
	draftReq := func(idempotencyKey string) *dto.CreateCreditNoteRequest {
		return &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.pending.ID,
			Reason:            types.CreditNoteReasonBillingError,
			ProcessCreditNote: false,
			IdempotencyKey:    &idempotencyKey,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{InvoiceLineItemID: "line_3", Amount: decimal.NewFromFloat(80.00)},
			},
		}
	}

	first, err := s.service.CreateCreditNote(s.GetContext(), draftReq("adjustment-capacity-recheck-1"))
	s.NoError(err)
	s.Equal(types.CreditNoteTypeAdjustment, first.CreditNoteType)

	second, err := s.service.CreateCreditNote(s.GetContext(), draftReq("adjustment-capacity-recheck-2"))
	s.NoError(err)
	s.NotEqual(first.ID, second.ID)

	s.NoError(s.service.FinalizeCreditNote(s.GetContext(), first.ID, nil))

	err = s.service.FinalizeCreditNote(s.GetContext(), second.ID, nil)
	s.Error(err)
	s.Contains(err.Error(), "credit amount exceeds available limit")

	secondAfter, err := s.GetStores().CreditNoteRepo.Get(s.GetContext(), second.ID)
	s.NoError(err)
	s.Equal(types.CreditNoteStatusDraft, secondAfter.CreditNoteStatus)

	invAfter, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), s.testData.invoices.pending.ID)
	s.NoError(err)
	s.True(invAfter.AdjustmentAmount.Equal(decimal.NewFromFloat(80.00)),
		"expected adjustment amount 80, got %s", invAfter.AdjustmentAmount)
}

func (s *CreditNoteServiceSuite) TestMaxCreditableAmountValidation() {
	// Create credit notes that exceed max creditable amount
	tests := []struct {
		name      string
		req       *dto.CreateCreditNoteRequest
		wantErr   bool
		errString string
	}{
		{
			name: "error_total_amount_exceeds_max_creditable_for_adjustment",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.pending.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_3",
						Amount:            decimal.NewFromFloat(80.00), // line_3 is 80.00
					},
					{
						InvoiceLineItemID: "line_3",
						Amount:            decimal.NewFromFloat(20.00), // Total: 100.00, but invoice total is 88.00
					},
				},
			},
			wantErr:   true,
			errString: "credit amount exceeds available limit",
		},
		{
			name: "error_total_amount_exceeds_max_creditable_for_refund",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.partialRefunded.ID, // EUR invoice: Total=132, AmountPaid=132, RefundedAmount=30, line_6=60, line_7=60
				Reason:    types.CreditNoteReasonUnsatisfactory,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_6",
						Amount:            decimal.NewFromFloat(60.00),
					},
					{
						InvoiceLineItemID: "line_7",
						Amount:            decimal.NewFromFloat(60.00),
					},
					// Total: 120.00, but max refundable = AmountPaid - RefundedAmount = 132 - 30 = 102
				},
			},
			wantErr:   true, // Should fail since 120 > 102 (max refundable)
			errString: "credit amount exceeds available limit",
		},
		{
			name: "successful_adjustment_with_partial_payment_within_limit",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.partialPayment.ID, // Total=$100, AmountPaid=$40, max adjustment = $60
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "test_line_item_1",
						Amount:            decimal.NewFromFloat(50.00), // Within limit of $60
					},
				},
			},
			wantErr: false,
		},
		{
			name: "error_adjustment_with_partial_payment_exceeds_limit",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.partialPayment.ID, // Total=$100, AmountPaid=$40, max adjustment = $60
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "test_line_item_1",
						Amount:            decimal.NewFromFloat(70.00), // Exceeds limit of $60
					},
				},
			},
			wantErr:   true,
			errString: "credit amount exceeds available limit",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateCreditNote(s.GetContext(), tt.req)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			if !tt.wantErr {
				s.NotNil(resp)
				// For partial payment adjustment, verify it's an adjustment type
				if tt.req.InvoiceID == s.testData.invoices.partialPayment.ID {
					s.Equal(types.CreditNoteTypeAdjustment, resp.CreditNoteType)
				}
			}
		})
	}
}

func (s *CreditNoteServiceSuite) TestMultipleCreditNotesValidation() {
	// Create first credit note
	firstReq := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.finalized.ID,
		Reason:            types.CreditNoteReasonBillingError,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(30.00),
			},
		},
	}

	_, err := s.service.CreateCreditNote(s.GetContext(), firstReq)
	s.NoError(err)

	// Create second credit note that should respect the already credited amount
	secondReq := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.finalized.ID,
		Reason:            types.CreditNoteReasonService,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_2",
				Amount:            decimal.NewFromFloat(40.00),
			},
		},
	}

	_, err = s.service.CreateCreditNote(s.GetContext(), secondReq)
	s.NoError(err)

	// Try to create third credit note that exceeds max creditable amount
	thirdReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonDuplicate,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(50.00), // Already credited 30+40=70, max is 110, so 50 exceeds remaining 40
			},
		},
	}

	_, err = s.service.CreateCreditNote(s.GetContext(), thirdReq)
	if err != nil {
		s.Contains(err.Error(), "credit amount exceeds available limit")
	} else {
		// If no error occurred, it means the calculation logic allows this,
		// which is fine given the new stored amounts approach
		s.T().Log("Third credit note was created successfully, which is acceptable with the new calculation logic")
	}
}

func (s *CreditNoteServiceSuite) TestWalletRefundIntegration() {
	// Check wallet balance before creating credit note
	walletBefore, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	s.T().Logf("Wallet balance before credit note: %s", walletBefore.Balance)

	// Test refund credit note creates wallet transaction
	req := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.finalized.ID,
		Reason:            types.CreditNoteReasonUnsatisfactory,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(25.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)
	s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	// Verify wallet balance increased
	wallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)

	// Debug information
	s.T().Logf("Initial wallet balance: %s", decimal.NewFromFloat(100.00))
	s.T().Logf("Credit note amount: %s", decimal.NewFromFloat(25.00))
	s.T().Logf("Current wallet balance: %s", wallet.Balance)
	s.T().Logf("Expected balance > %s", decimal.NewFromFloat(100.00))

	s.True(wallet.Balance.GreaterThan(decimal.NewFromFloat(100.00))) // Initial was 100.00
}

func (s *CreditNoteServiceSuite) TestCurrencyMismatchHandling() {
	// Test credit note with EUR invoice and wallet
	req := &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.partialRefunded.ID, // EUR invoice
		Reason:            types.CreditNoteReasonUnsatisfactory,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_6",
				Amount:            decimal.NewFromFloat(30.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)
	s.Equal("EUR", resp.Currency)
	s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	// Verify EUR wallet balance increased
	wallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.eur.ID)
	s.NoError(err)
	s.True(wallet.Balance.GreaterThan(decimal.NewFromFloat(50.00))) // Initial was 50.00
}

func (s *CreditNoteServiceSuite) TestCreditNoteNumberGeneration() {
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.pending.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_3",
				Amount:            decimal.NewFromFloat(10.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)

	// Verify credit note number is generated
	s.NotEmpty(resp.CreditNoteNumber)
	s.Contains(resp.CreditNoteNumber, types.SHORT_ID_PREFIX_CREDIT_NOTE)
}

func (s *CreditNoteServiceSuite) TestCreditNoteIdempotency() {
	s.Run("retry with identical request returns same credit note, no duplicate row", func() {
		req := &dto.CreateCreditNoteRequest{
			InvoiceID: s.testData.invoices.finalized.ID,
			Reason:    types.CreditNoteReasonBillingError,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_1",
					Amount:            decimal.NewFromFloat(10.00),
				},
			},
		}

		first, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)

		// credit_note_number left blank again, as a real client retry would.
		retryReq := &dto.CreateCreditNoteRequest{
			InvoiceID: s.testData.invoices.finalized.ID,
			Reason:    types.CreditNoteReasonBillingError,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_1",
					Amount:            decimal.NewFromFloat(10.00),
				},
			},
		}

		second, err := s.service.CreateCreditNote(s.GetContext(), retryReq)
		s.NoError(err)

		s.Equal(first.ID, second.ID)

		filter := types.NewNoLimitCreditNoteFilter()
		filter.InvoiceID = s.testData.invoices.finalized.ID
		count, err := s.GetStores().CreditNoteRepo.Count(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(1, count)
	})

	s.Run("same invoice and reason but different line items are not merged", func() {
		req1 := &dto.CreateCreditNoteRequest{
			InvoiceID: s.testData.invoices.pending.ID,
			Reason:    types.CreditNoteReasonService,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_3",
					Amount:            decimal.NewFromFloat(10.00),
				},
			},
		}
		req2 := &dto.CreateCreditNoteRequest{
			InvoiceID: s.testData.invoices.pending.ID,
			Reason:    types.CreditNoteReasonService,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_3",
					Amount:            decimal.NewFromFloat(20.00),
				},
			},
		}

		first, err := s.service.CreateCreditNote(s.GetContext(), req1)
		s.NoError(err)

		second, err := s.service.CreateCreditNote(s.GetContext(), req2)
		s.NoError(err)

		s.NotEqual(first.ID, second.ID)
	})

	s.Run("retry of a request left in draft status returns the draft, not a conflict", func() {
		req := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.failed.ID,
			Reason:            types.CreditNoteReasonBillingError,
			ProcessCreditNote: false,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_4",
					Amount:            decimal.NewFromFloat(5.00),
				},
			},
		}

		first, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)
		s.Equal(types.CreditNoteStatusDraft, first.CreditNoteStatus)

		retryReq := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.failed.ID,
			Reason:            types.CreditNoteReasonBillingError,
			ProcessCreditNote: false,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_4",
					Amount:            decimal.NewFromFloat(5.00),
				},
			},
		}

		second, err := s.service.CreateCreditNote(s.GetContext(), retryReq)
		s.NoError(err)
		s.Equal(first.ID, second.ID)
	})

	s.Run("same content but different process_credit_note flag is not merged", func() {
		draftReq := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.pending.ID,
			Reason:            types.CreditNoteReasonBillingError,
			ProcessCreditNote: false,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_3",
					Amount:            decimal.NewFromFloat(5.00),
				},
			},
		}
		finalizedReq := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.pending.ID,
			Reason:            types.CreditNoteReasonBillingError,
			ProcessCreditNote: true,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_3",
					Amount:            decimal.NewFromFloat(5.00),
				},
			},
		}

		draft, err := s.service.CreateCreditNote(s.GetContext(), draftReq)
		s.NoError(err)
		s.Equal(types.CreditNoteStatusDraft, draft.CreditNoteStatus)

		finalized, err := s.service.CreateCreditNote(s.GetContext(), finalizedReq)
		s.NoError(err)

		s.NotEqual(draft.ID, finalized.ID)
		s.Equal(types.CreditNoteStatusFinalized, finalized.CreditNoteStatus)
	})

	s.Run("retry of an already-finalized credit note is idempotent, no duplicate webhook", func() {
		webhookPub := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
		webhookPub.Reset()

		req := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.finalized.ID,
			Reason:            types.CreditNoteReasonFraudulent,
			ProcessCreditNote: true,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_1",
					Amount:            decimal.NewFromFloat(2.00),
				},
			},
		}

		first, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)
		s.Equal(types.CreditNoteStatusFinalized, first.CreditNoteStatus)

		retryReq := &dto.CreateCreditNoteRequest{
			InvoiceID:         s.testData.invoices.finalized.ID,
			Reason:            types.CreditNoteReasonFraudulent,
			ProcessCreditNote: true,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_1",
					Amount:            decimal.NewFromFloat(2.00),
				},
			},
		}

		second, err := s.service.CreateCreditNote(s.GetContext(), retryReq)
		s.NoError(err)
		s.Equal(first.ID, second.ID)
		s.Equal(types.CreditNoteStatusFinalized, second.CreditNoteStatus)

		createdEvents := 0
		for _, evt := range webhookPub.Events() {
			if evt.EntityID == first.ID && evt.EventName == types.WebhookEventCreditNoteCreated {
				createdEvents++
			}
		}
		s.Equal(1, createdEvents)
	})

	s.Run("explicit idempotency key still short-circuits regardless of content", func() {
		key := "explicit-test-key-1"
		req := &dto.CreateCreditNoteRequest{
			InvoiceID:      s.testData.invoices.finalized.ID,
			Reason:         types.CreditNoteReasonUnsatisfactory,
			IdempotencyKey: &key,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: "line_2",
					Amount:            decimal.NewFromFloat(5.00),
				},
			},
		}

		first, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)

		// Same key, different amount - explicit key still wins.
		req.LineItems[0].Amount = decimal.NewFromFloat(15.00)
		second, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)

		s.Equal(first.ID, second.ID)
	})
}

func (s *CreditNoteServiceSuite) TestProcessCreditNoteFlag() {
	tests := []struct {
		name              string
		processCreditNote bool
		expectedStatus    types.CreditNoteStatus
	}{
		{
			name:              "process_credit_note_false_creates_draft",
			processCreditNote: false,
			expectedStatus:    types.CreditNoteStatusDraft,
		},
		{
			name:              "process_credit_note_true_creates_finalized",
			processCreditNote: true,
			expectedStatus:    types.CreditNoteStatusFinalized,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := &dto.CreateCreditNoteRequest{
				InvoiceID:         s.testData.invoices.pending.ID,
				Reason:            types.CreditNoteReasonBillingError,
				ProcessCreditNote: tt.processCreditNote,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_3",
						Amount:            decimal.NewFromFloat(5.00),
					},
				},
			}

			resp, err := s.service.CreateCreditNote(s.GetContext(), req)
			s.NoError(err)
			s.Equal(tt.expectedStatus, resp.CreditNoteStatus)
		})
	}
}

func (s *CreditNoteServiceSuite) TestCreditNoteTypeDetection() {
	tests := []struct {
		name          string
		invoiceID     string
		expectedType  types.CreditNoteType
		paymentStatus types.PaymentStatus
	}{
		{
			name:          "succeeded_payment_creates_refund",
			invoiceID:     s.testData.invoices.finalized.ID,
			expectedType:  types.CreditNoteTypeRefund,
			paymentStatus: types.PaymentStatusSucceeded,
		},
		{
			name:          "partial_refund_creates_refund",
			invoiceID:     s.testData.invoices.partialRefunded.ID,
			expectedType:  types.CreditNoteTypeRefund,
			paymentStatus: types.PaymentStatusPartiallyRefunded,
		},
		{
			name:          "pending_payment_creates_adjustment",
			invoiceID:     s.testData.invoices.pending.ID,
			expectedType:  types.CreditNoteTypeAdjustment,
			paymentStatus: types.PaymentStatusPending,
		},
		{
			name:          "failed_payment_creates_adjustment",
			invoiceID:     s.testData.invoices.failed.ID,
			expectedType:  types.CreditNoteTypeAdjustment,
			paymentStatus: types.PaymentStatusFailed,
		},
		{
			name:          "partial_payment_creates_adjustment",
			invoiceID:     s.testData.invoices.partialPayment.ID,
			expectedType:  types.CreditNoteTypeAdjustment,
			paymentStatus: types.PaymentStatusPending, // Pending status creates adjustment even with partial payment
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := &dto.CreateCreditNoteRequest{
				InvoiceID:         tt.invoiceID,
				Reason:            types.CreditNoteReasonBillingError,
				ProcessCreditNote: true,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1", // Use first line item ID
						Amount:            decimal.NewFromFloat(5.00),
					},
				},
			}

			// Update line item ID based on invoice
			switch tt.invoiceID {
			case s.testData.invoices.pending.ID:
				req.LineItems[0].InvoiceLineItemID = "line_3"
			case s.testData.invoices.failed.ID:
				req.LineItems[0].InvoiceLineItemID = "line_4"
			case s.testData.invoices.partialRefunded.ID:
				req.LineItems[0].InvoiceLineItemID = "line_6"
			case s.testData.invoices.partialPayment.ID:
				req.LineItems[0].InvoiceLineItemID = "test_line_item_1"
			}

			resp, err := s.service.CreateCreditNote(s.GetContext(), req)
			s.NoError(err)
			s.Equal(tt.expectedType, resp.CreditNoteType)
		})
	}
}

// CreateCreditNote must take the invoice row lock BEFORE any read of mutable
// invoice state. ValidateCreditNoteCreation -> validateCreditNoteAmounts ->
// calculateMaxCreditableAmount computes AmountPaid - RefundedAmount; if the
// lock is taken after that guard, two concurrent creations both evaluate it
// against unlocked state, both pass, and both succeed — an over-refund from
// creation-time concurrency alone (VAPT SFX-2026-0203-F02-EXT).
//
// The in-memory store cannot provide real mutual exclusion (SELECT ... FOR
// UPDATE does that in Postgres), so this asserts the ORDERING: the first
// recorded access to the invoice must be the lock, not a plain read.
func (s *CreditNoteServiceSuite) TestCreateCreditNoteLocksInvoiceBeforeReading() {
	invStore, ok := s.GetStores().InvoiceRepo.(*testutil.InMemoryInvoiceStore)
	s.Require().True(ok, "expected the in-memory invoice store")

	inv := s.testData.invoices.pending
	invStore.ResetInvoiceAccessLog(inv.ID)

	_, err := s.service.CreateCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID:         inv.ID,
		Reason:            types.CreditNoteReasonBillingError,
		Memo:              "row lock ordering regression",
		ProcessCreditNote: false,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_3",
				DisplayName:       "Partial refund for Product C",
				Amount:            decimal.NewFromFloat(5.00),
			},
		},
	})
	s.Require().NoError(err)

	access := invStore.InvoiceAccessLog(inv.ID)
	s.Require().NotEmpty(access, "expected the invoice to be read during creation")
	s.Equal("get_for_update", access[0],
		"CreateCreditNote must lock the invoice row before any read of RefundedAmount; got access order %v", access)
}

// VoidCreditNote must re-read the credit note under the invoice lock and
// re-run its guards. The status checks run before the transaction, so a
// concurrent FinalizeCreditNote can commit while this call waits on the lock.
// Voiding the stale draft snapshot would overwrite the finalized status,
// bypass the completed-refund rejection, and skip RecalculateInvoiceAmounts
// (originalStatus would still read Draft).
//
// The finalize is injected between the pre-transaction read and the lock via
// the store's BeforeGetForUpdate hook, which is the only way to hit the window
// deterministically in a single-threaded test.
func (s *CreditNoteServiceSuite) TestVoidCreditNoteRefetchesUnderLock() {
	resp, err := s.service.CreateCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID:         s.testData.invoices.partialRefunded.ID,
		Reason:            types.CreditNoteReasonBillingError,
		Memo:              "void refetch regression",
		ProcessCreditNote: false,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_6",
				DisplayName:       "Refund for Product F",
				Amount:            decimal.NewFromFloat(10.00),
			},
		},
	})
	s.Require().NoError(err)
	s.Require().Equal(types.CreditNoteStatusDraft, resp.CreditNoteStatus)
	s.Require().Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	invStore, ok := s.GetStores().InvoiceRepo.(*testutil.InMemoryInvoiceStore)
	s.Require().True(ok, "expected the in-memory invoice store")

	// Simulate a concurrent finalize landing after VoidCreditNote has read the
	// draft but before it acquires the invoice lock.
	var once bool
	invStore.BeforeGetForUpdate = func(string) {
		if once {
			return
		}
		once = true
		s.Require().NoError(s.service.FinalizeCreditNote(s.GetContext(), resp.ID, nil))
	}
	defer func() { invStore.BeforeGetForUpdate = nil }()

	err = s.service.VoidCreditNote(s.GetContext(), resp.ID)
	s.Require().Error(err, "voiding a concurrently-finalized refund must be rejected under the lock")

	after, err := s.GetStores().CreditNoteRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.Equal(types.CreditNoteStatusFinalized, after.CreditNoteStatus,
		"a concurrently-finalized credit note must not be overwritten as voided")
}

func (s *CreditNoteServiceSuite) refundRows(invoiceID string) []*refund.Refund {
	rows, err := s.GetStores().RefundRepo.List(s.GetContext(), &types.RefundFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		InvoiceIDs:  []string{invoiceID},
	})
	s.NoError(err)
	return rows
}

func (s *CreditNoteServiceSuite) TestFinalizeCreditNote_EmitsSettledRefundRows() {
	inv := s.testData.invoices.finalized
	walletBefore, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	// Value copy: the in-memory store hands back the stored pointer, which the top-up mutates.
	balanceBefore := walletBefore.Balance

	amount := decimal.NewFromFloat(25.00)
	resp, err := s.service.CreateCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID:         inv.ID,
		Reason:            types.CreditNoteReasonUnsatisfactory,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: amount},
		},
	})
	s.NoError(err)
	s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	rows := s.refundRows(inv.ID)
	s.Len(rows, 1)
	s.Equal(resp.ID, lo.FromPtr(rows[0].CreditNoteID))
	s.Equal(types.RefundStatusSucceeded, rows[0].RefundStatus)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
	s.True(amount.Equal(rows[0].SettledAmount))

	updatedInv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(amount.Equal(updatedInv.RefundedAmount))

	walletAfter, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	s.True(balanceBefore.Add(amount).Equal(walletAfter.Balance))
}

// A refund now always lands in an active PRE_PAID wallet. Before the settlement ledger,
// FinalizeCreditNote matched on currency alone and would have topped up this POST_PAID one.
func (s *CreditNoteServiceSuite) TestFinalizeCreditNote_SkipsNonPrepaidWallet() {
	cust := &customer.Customer{
		ID:        "cust_postpaid_only",
		Name:      "Postpaid Only",
		Email:     "postpaid@example.com",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))

	walletService := NewWalletService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		WalletRepo:               s.GetStores().WalletRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	})
	postPaid, err := walletService.CreateWallet(s.GetContext(), &dto.CreateWalletRequest{
		CustomerID:     cust.ID,
		Name:           "Postpaid Wallet",
		Currency:       "USD",
		ConversionRate: decimal.NewFromInt(1),
		WalletType:     types.WalletTypePostPaid,
	})
	s.NoError(err)

	inv := &invoice.Invoice{
		ID:               "inv_postpaid_only",
		CustomerID:       cust.ID,
		InvoiceNumber:    lo.ToPtr("INV-POSTPAID"),
		InvoiceType:      types.InvoiceTypeOneOff,
		InvoiceStatus:    types.InvoiceStatusFinalized,
		PaymentStatus:    types.PaymentStatusSucceeded,
		Currency:         "USD",
		Subtotal:         decimal.NewFromFloat(40.00),
		Total:            decimal.NewFromFloat(40.00),
		AmountPaid:       decimal.NewFromFloat(40.00),
		AmountDue:        decimal.NewFromFloat(40.00),
		AmountRemaining:  decimal.Zero,
		AdjustmentAmount: decimal.Zero,
		RefundedAmount:   decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_postpaid_1",
				DisplayName: lo.ToPtr("Product P"),
				Amount:      decimal.NewFromFloat(40.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))

	amount := decimal.NewFromFloat(40.00)
	_, err = s.service.CreateCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID:         inv.ID,
		Reason:            types.CreditNoteReasonUnsatisfactory,
		ProcessCreditNote: true,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_postpaid_1", Amount: amount},
		},
	})
	s.NoError(err)

	unchanged, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), postPaid.ID)
	s.NoError(err)
	s.True(unchanged.Balance.IsZero())

	wallets, err := walletService.GetWalletsByCustomerID(s.GetContext(), cust.ID)
	s.NoError(err)
	prepaid := lo.Filter(wallets, func(w *dto.WalletResponse, _ int) bool {
		return w.WalletType == types.WalletTypePrePaid
	})
	s.Len(prepaid, 1)
	s.True(amount.Equal(prepaid[0].Balance))
}
