package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CreditAdjustmentServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CreditAdjustmentService
	testData struct {
		customer *customer.Customer
		wallets  []*wallet.Wallet
		invoice  *invoice.Invoice
	}
}

func TestCreditAdjustmentService(t *testing.T) {
	suite.Run(t, new(CreditAdjustmentServiceSuite))
}

func (s *CreditAdjustmentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *CreditAdjustmentServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// GetContext returns context with environment ID set for settings lookup
func (s *CreditAdjustmentServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *CreditAdjustmentServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewCreditAdjustmentService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		WalletRepo:               stores.WalletRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		SettingsRepo:             stores.SettingsRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		MeterRepo:                stores.MeterRepo,
		PriceRepo:                stores.PriceRepo,
		FeatureRepo:              stores.FeatureRepo,
		FeatureUsageRepo:         stores.FeatureUsageRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
	})
}

// getServiceImpl returns the concrete service implementation for accessing testing-only methods
func (s *CreditAdjustmentServiceSuite) getServiceImpl() *creditAdjustmentService {
	return s.service.(*creditAdjustmentService)
}

// getWalletService returns a wallet service instance for creating credit transactions
func (s *CreditAdjustmentServiceSuite) getWalletService() WalletService {
	stores := s.GetStores()
	pubsub := testutil.NewInMemoryPubSub()
	return NewWalletService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		WalletRepo:               stores.WalletRepo,
		SettingsRepo:             stores.SettingsRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: pubsub},
	})
}

func (s *CreditAdjustmentServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_credit_test",
		ExternalID: "ext_cust_credit_test",
		Name:       "Credit Test Customer",
		Email:      "credit@test.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Initialize wallets slice
	s.testData.wallets = []*wallet.Wallet{}
}

// Helper method to create a wallet with specified properties
func (s *CreditAdjustmentServiceSuite) createWallet(id string, currency string, balance decimal.Decimal, creditBalance decimal.Decimal, status types.WalletStatus) *wallet.Wallet {
	// If a credit transaction will be created, set initial CreditBalance to 0
	// The credit transaction will set both Balance and CreditBalance correctly
	initialCreditBalance := creditBalance
	if creditBalance.GreaterThan(decimal.Zero) && status == types.WalletStatusActive {
		initialCreditBalance = decimal.Zero
	}

	w := &wallet.Wallet{
		ID:             id,
		CustomerID:     s.testData.customer.ID,
		Currency:       currency,
		Balance:        balance,
		CreditBalance:  initialCreditBalance,
		WalletStatus:   status,
		Name:           "Test Wallet " + id,
		Description:    "Test wallet for credit adjustment",
		ConversionRate: decimal.NewFromInt(1),
		EnvironmentID:  "env_test",              // Required for wallet balance alert events
		WalletType:     types.WalletTypePrePaid, // Credit adjustments use PrePaid wallets
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))

	// If creditBalance is greater than zero and wallet is active, create a credit transaction
	// This is required because DebitWallet uses FindEligibleCredits which looks for credit transactions
	// Only create credits for active wallets since inactive wallets won't be used anyway
	if creditBalance.GreaterThan(decimal.Zero) && status == types.WalletStatusActive {
		walletService := s.getWalletService()
		creditOp := &wallet.WalletOperation{
			WalletID:          w.ID,
			Type:              types.TransactionTypeCredit,
			CreditAmount:      creditBalance,
			Description:       "Initial credit for test wallet",
			TransactionReason: types.TransactionReasonFreeCredit,
		}
		s.NoError(walletService.CreditWallet(s.GetContext(), creditOp))

		// Reload wallet to get updated balances
		updatedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		w = updatedWallet
	}

	return w
}

// Helper method to create an invoice with line items
func (s *CreditAdjustmentServiceSuite) createInvoice(id string, currency string, lineItemAmounts []decimal.Decimal) *invoice.Invoice {
	lineItems := make([]*invoice.InvoiceLineItem, len(lineItemAmounts))
	for i, amount := range lineItemAmounts {
		lineItems[i] = &invoice.InvoiceLineItem{
			ID:         s.GetUUID(),
			InvoiceID:  id,
			CustomerID: s.testData.customer.ID,
			Amount:     amount,
			Currency:   currency,
			Quantity:   decimal.NewFromInt(1),
			PriceType:  lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		}
	}

	// Calculate subtotal
	subtotal := decimal.Zero
	for _, amount := range lineItemAmounts {
		subtotal = subtotal.Add(amount)
	}

	inv := &invoice.Invoice{
		ID:            id,
		CustomerID:    s.testData.customer.ID,
		Currency:      currency,
		Subtotal:      subtotal,
		Total:         subtotal,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
		LineItems:     lineItems,
	}

	// Create invoice with line items
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

// Helper method to create a wallet for calculation tests (in-memory, no database)
func (s *CreditAdjustmentServiceSuite) createWalletForCalculation(id string, currency string, balance decimal.Decimal) *wallet.Wallet {
	return &wallet.Wallet{
		ID:             id,
		CustomerID:     s.testData.customer.ID,
		Currency:       currency,
		Balance:        balance,
		CreditBalance:  decimal.Zero,
		WalletStatus:   types.WalletStatusActive,
		Name:           "Test Wallet " + id,
		Description:    "Test wallet for calculation",
		ConversionRate: decimal.NewFromInt(1),
		WalletType:     types.WalletTypePrePaid, // Credit adjustments only process PrePaid wallets
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
}

// Helper method to create an invoice line item for calculation tests (in-memory, no database)
func (s *CreditAdjustmentServiceSuite) createLineItemForCalculation(amount decimal.Decimal, priceType *string, lineItemDiscount decimal.Decimal) *invoice.InvoiceLineItem {
	if priceType == nil {
		priceType = lo.ToPtr(string(types.PRICE_TYPE_USAGE))
	}
	return &invoice.InvoiceLineItem{
		ID:                    s.GetUUID(),
		Amount:                amount,
		Currency:              "USD",
		Quantity:              decimal.NewFromInt(1),
		PriceType:             priceType,
		LineItemDiscount:      lineItemDiscount,
		PrepaidCreditsApplied: decimal.Zero,
		BaseModel:             types.GetDefaultBaseModel(s.GetContext()),
	}
}

// Helper method to create an invoice for calculation tests (in-memory, no database)
func (s *CreditAdjustmentServiceSuite) createInvoiceForCalculation(id string, currency string, lineItems []*invoice.InvoiceLineItem) *invoice.Invoice {
	return &invoice.Invoice{
		ID:            id,
		CustomerID:    s.testData.customer.ID,
		Currency:      currency,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		LineItems:     lineItems,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
}

// All test methods have been removed - see invoice_discount_credit_workflow_test.go for comprehensive tests
