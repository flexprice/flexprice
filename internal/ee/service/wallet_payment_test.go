package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type WalletPaymentServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  WalletPaymentService
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
		wallets  struct {
			promotional       *wallet.Wallet
			prepaid           *wallet.Wallet
			smallBalance      *wallet.Wallet
			differentCurrency *wallet.Wallet
			inactive          *wallet.Wallet
		}
		now time.Time
	}
}

func TestWalletPaymentService(t *testing.T) {
	suite.Run(t, new(WalletPaymentServiceSuite))
}

func (s *WalletPaymentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *WalletPaymentServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// GetContext returns context with environment ID set for settings lookup
func (s *WalletPaymentServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *WalletPaymentServiceSuite) setupService() {
	// Create the WalletPaymentService
	s.service = NewWalletPaymentService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		SettingsRepo:     s.GetStores().SettingsRepo,
		AlertLogsRepo:    s.GetStores().AlertLogsRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *WalletPaymentServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_test_wallet_payment",
		ExternalID: "ext_cust_test_wallet_payment",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test invoice
	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_test_wallet_payment",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(150),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(150),
		Description:     "Test Invoice for Wallet Payments",
		PeriodStart:     &s.testData.now,
		PeriodEnd:       lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), s.testData.invoice))

	s.setupWallet()
}

func (s *WalletPaymentServiceSuite) setupWallet() {
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()
	// Create test wallets
	// 1. Postpaid wallet (can be used for payments) - single postpaid wallet with combined balance
	s.testData.wallets.promotional = &wallet.Wallet{
		ID:             "wallet_promotional",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(60), // Combined balance: 50 + 10 from previous setup
		CreditBalance:  decimal.NewFromFloat(60),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePostPaid,
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.promotional))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         s.testData.wallets.promotional.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           s.testData.wallets.promotional.Balance,
		CreditAmount:     s.testData.wallets.promotional.CreditBalance,
		CreditsAvailable: s.testData.wallets.promotional.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// 2. Prepaid wallet (cannot be used for payments - used for credit adjustments)
	s.testData.wallets.prepaid = &wallet.Wallet{
		ID:             "wallet_prepaid",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(200),
		CreditBalance:  decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid, // Fixed: Actually PrePaid type
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.prepaid))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         s.testData.wallets.prepaid.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           s.testData.wallets.prepaid.Balance,
		CreditAmount:     s.testData.wallets.prepaid.CreditBalance,
		CreditsAvailable: s.testData.wallets.prepaid.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Note: smallBalance wallet removed - consolidated into promotional wallet
	s.testData.wallets.smallBalance = nil

	// 4. Different currency wallet
	s.testData.wallets.differentCurrency = &wallet.Wallet{
		ID:             "wallet_different_currency",
		CustomerID:     s.testData.customer.ID,
		Currency:       "eur",
		Balance:        decimal.NewFromFloat(300),
		CreditBalance:  decimal.NewFromFloat(300),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePostPaid,
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.differentCurrency))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         s.testData.wallets.differentCurrency.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           s.testData.wallets.differentCurrency.Balance,
		CreditAmount:     s.testData.wallets.differentCurrency.CreditBalance,
		CreditsAvailable: s.testData.wallets.differentCurrency.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// 5. Inactive wallet
	s.testData.wallets.inactive = &wallet.Wallet{
		ID:             "wallet_inactive",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusClosed,
		WalletType:     types.WalletTypePostPaid,
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.inactive))
}

func (s *WalletPaymentServiceSuite) TestGetWalletsForPayment() {
	// Test that wallets are returned in the correct order based on price type restrictions and balance
	options := WalletPaymentOptions{
		Strategy: BalanceOptimizedStrategy, // Only postpaid wallets can be used, sorted by balance
	}

	wallets, err := s.service.GetWalletsForPayment(s.GetContext(), s.testData.customer.ID, "usd", options)
	s.NoError(err)

	// Only postpaid wallets can be used for payments; prepaid wallets are excluded
	// Only one postpaid wallet exists: wallet_promotional(60, postpaid)
	// Note: wallet_prepaid is excluded because it's prepaid type
	expectedWallets := []string{"wallet_promotional"}
	s.Equal(len(expectedWallets), len(wallets), "Wrong number of wallets returned")

	// Verify wallets are in expected order
	for i, expectedID := range expectedWallets {
		s.Equal(expectedID, wallets[i].ID, "Wallet at position %d incorrect", i)
	}

	// Verify that inactive, different currency, and prepaid wallets are excluded
	for _, w := range wallets {
		s.NotEqual("wallet_inactive", w.ID, "Inactive wallet should not be included")
		s.NotEqual("wallet_different_currency", w.ID, "Different currency wallet should not be included")
		s.NotEqual("wallet_prepaid", w.ID, "Prepaid wallet should not be included (only postpaid can be used for payments)")
	}
}

func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithWallets() {
	tests := []struct {
		name                string
		maxWalletsToUse     int
		additionalMetadata  types.Metadata
		expectedAmountPaid  decimal.Decimal
		expectedWalletsUsed int
	}{
		{
			name:                "Full payment",
			maxWalletsToUse:     0, // No limit
			additionalMetadata:  types.Metadata{"test_key": "test_value"},
			expectedAmountPaid:  decimal.NewFromFloat(60), // Single postpaid wallet with 60 balance
			expectedWalletsUsed: 1,                        // 1 wallet used (single postpaid wallet)
		},
		{
			name:                "Limited number of wallets",
			maxWalletsToUse:     1,                        // Only use one wallet
			expectedAmountPaid:  decimal.NewFromFloat(60), // Full balance from single promotional wallet
			expectedWalletsUsed: 1,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset payment store for this test
			s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
			s.setupWallet()

			// Reset the invoice for each test case by creating a fresh copy
			// This ensures we're not trying to pay an already paid invoice
			freshInvoice := &invoice.Invoice{
				ID:              s.testData.invoice.ID,
				CustomerID:      s.testData.invoice.CustomerID,
				AmountDue:       s.testData.invoice.AmountDue,
				AmountPaid:      decimal.Zero,
				AmountRemaining: s.testData.invoice.AmountDue,
				Currency:        s.testData.invoice.Currency,
				DueDate:         s.testData.invoice.DueDate,
				PeriodStart:     s.testData.invoice.PeriodStart,
				PeriodEnd:       s.testData.invoice.PeriodEnd,
				LineItems:       s.testData.invoice.LineItems,
				Metadata:        s.testData.invoice.Metadata,
				BaseModel:       s.testData.invoice.BaseModel,
			}

			// Update the invoice in the store
			err := s.GetStores().InvoiceRepo.Update(s.GetContext(), freshInvoice)
			s.NoError(err)

			// Create options for this test
			options := WalletPaymentOptions{
				Strategy:           BalanceOptimizedStrategy, // Only postpaid wallets can be used
				MaxWalletsToUse:    tc.maxWalletsToUse,
				AdditionalMetadata: tc.additionalMetadata,
			}

			// Process payment with the fresh invoice
			amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
				s.GetContext(),
				freshInvoice,
				options,
			)

			// Verify results
			s.NoError(err)
			s.True(tc.expectedAmountPaid.Equal(amountPaid),
				"Amount paid mismatch: expected %s, got %s, invoice remaining %s",
				tc.expectedAmountPaid, amountPaid, freshInvoice.AmountRemaining)

			// Verify payment requests to the store
			payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
				DestinationID:   &freshInvoice.ID,
				DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
			})
			s.NoError(err)
			s.Equal(tc.expectedWalletsUsed, len(payments),
				"Expected %d payment requests, got %d", tc.expectedWalletsUsed, len(payments))
		})
	}
}

func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithNoWallets() {
	// Create a customer with no wallets
	customer := &customer.Customer{
		ID:         "cust_no_wallets",
		ExternalID: "ext_cust_no_wallets",
		Name:       "Customer With No Wallets",
		Email:      "no-wallets@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), customer))

	// Create an invoice for this customer
	inv := &invoice.Invoice{
		ID:              "inv_no_wallets",
		CustomerID:      customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		Description:     "Test Invoice for Customer With No Wallets",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	// Attempt payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		inv,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	s.True(decimal.Zero.Equal(amountPaid), "Amount paid should be zero")

	// Verify no payment requests were made
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Empty(payments, "No payment requests should have been made")
}

func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithInsufficientBalance() {
	// Create an invoice with a very large amount
	inv := &invoice.Invoice{
		ID:              "inv_large_amount",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PeriodStart:     lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
		PeriodEnd:       lo.ToPtr(s.testData.now.Add(6 * 24 * time.Hour)),
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(1000),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(1000),
		Description:     "Test Invoice with Large Amount",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	// Reset payment store
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()

	// Attempt payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		inv,
		DefaultWalletPaymentOptions(),
	)

	// Verify results - should pay partial amount
	// Only postpaid wallet can be used: 60 (prepaid wallet excluded)
	s.NoError(err)
	expectedAmount := decimal.NewFromFloat(60) // Single postpaid wallet with 60 balance
	s.True(expectedAmount.Equal(amountPaid),
		"Amount paid mismatch: expected %s, got %s", expectedAmount, amountPaid)

	// Verify payment requests
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(1, len(payments), "Expected 1 payment request (single postpaid wallet)")
}

func (s *WalletPaymentServiceSuite) TestNilInvoice() {
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		nil,
		DefaultWalletPaymentOptions(),
	)

	s.Error(err)
	s.True(decimal.Zero.Equal(amountPaid), "Amount paid should be zero")
	s.Contains(err.Error(), "nil", "Error should mention nil invoice")
}

func (s *WalletPaymentServiceSuite) TestInvoiceWithNoRemainingAmount() {
	// Create a paid invoice
	inv := &invoice.Invoice{
		ID:              "inv_already_paid",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusSucceeded,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.NewFromFloat(100),
		AmountRemaining: decimal.Zero,
		Description:     "Test Invoice Already Paid",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	// Attempt payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		inv,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	s.True(decimal.Zero.Equal(amountPaid), "Amount paid should be zero")

	// Verify no payment requests were made
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Empty(payments, "No payment requests should have been made")
}

// TestProcessInvoicePaymentWithInvoicingCustomerID tests payment processing using invoicing customer's wallets
func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithInvoicingCustomerID() {
	// Create subscription customer
	subscriptionCustomer := &customer.Customer{
		ID:         "cust_subscription",
		ExternalID: "ext_cust_subscription",
		Name:       "Subscription Customer",
		Email:      "subscription@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), subscriptionCustomer))

	// Create invoicing customer
	invoicingCustomer := &customer.Customer{
		ID:         "cust_invoicing",
		ExternalID: "ext_cust_invoicing",
		Name:       "Invoicing Customer",
		Email:      "invoicing@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), invoicingCustomer))

	// Create wallet for invoicing customer (not subscription customer)
	invoicingWallet := &wallet.Wallet{
		ID:             "wallet_invoicing",
		CustomerID:     invoicingCustomer.ID, // Wallet belongs to invoicing customer
		Name:           "Invoicing Customer Wallet",
		Currency:       "usd",
		WalletType:     types.WalletTypePostPaid,
		Balance:        decimal.NewFromFloat(200),
		CreditBalance:  decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), invoicingWallet))
	// Create transaction to make credits available
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         invoicingWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           invoicingWallet.Balance,
		CreditAmount:     invoicingWallet.CreditBalance,
		CreditsAvailable: invoicingWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice for invoicing customer (subscription invoice)
	invoiceForInvoicingCustomer := &invoice.Invoice{
		ID:              "inv_invoicing_cust",
		CustomerID:      invoicingCustomer.ID, // Invoice is for invoicing customer
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(150),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(150),
		Description:     "Invoice for Invoicing Customer",
		DueDate:         lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
		PeriodStart:     lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
		PeriodEnd:       lo.ToPtr(s.testData.now.Add(6 * 24 * time.Hour)),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), invoiceForInvoicingCustomer))

	// Process payment - should use invoicing customer's wallet
	options := WalletPaymentOptions{
		Strategy:        BalanceOptimizedStrategy,
		MaxWalletsToUse: 0, // No limit
	}
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		invoiceForInvoicingCustomer,
		options,
	)

	// Verify payment was successful using invoicing customer's wallet
	s.NoError(err)
	s.True(decimal.NewFromFloat(150).Equal(amountPaid), "Should pay full amount from invoicing customer's wallet")

	// Verify payment was created
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &invoiceForInvoicingCustomer.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Len(payments, 1, "Should have one payment from invoicing customer's wallet")
	if len(payments) > 0 {
		s.Equal(invoicingCustomer.ID, invoiceForInvoicingCustomer.CustomerID, "Invoice should be for invoicing customer")
	}

	// Verify invoice was updated
	updatedInvoice, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), invoiceForInvoicingCustomer.ID)
	s.NoError(err)
	s.True(decimal.NewFromFloat(150).Equal(updatedInvoice.AmountPaid), "Invoice should show amount paid")
	s.True(decimal.Zero.Equal(updatedInvoice.AmountRemaining), "Invoice should have no remaining amount")
}

// TestProcessInvoicePaymentWithInvoicingCustomerIDNoWallet tests that payment fails if invoicing customer has no wallet
func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithInvoicingCustomerIDNoWallet() {
	// Create subscription customer
	subscriptionCustomer := &customer.Customer{
		ID:         "cust_sub_no_wallet",
		ExternalID: "ext_cust_sub_no_wallet",
		Name:       "Subscription Customer",
		Email:      "subscription@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), subscriptionCustomer))

	// Create invoicing customer (no wallet)
	invoicingCustomer := &customer.Customer{
		ID:         "cust_invoicing_no_wallet",
		ExternalID: "ext_cust_invoicing_no_wallet",
		Name:       "Invoicing Customer",
		Email:      "invoicing@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), invoicingCustomer))

	// Create wallet for subscription customer (NOT invoicing customer)
	subscriptionWallet := &wallet.Wallet{
		ID:             "wallet_subscription",
		CustomerID:     subscriptionCustomer.ID, // Wallet belongs to subscription customer
		Name:           "Subscription Customer Wallet",
		Currency:       "usd",
		WalletType:     types.WalletTypePostPaid,
		Balance:        decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromInt(1), // Required for wallet operations
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), subscriptionWallet))

	// Create invoice for invoicing customer
	invoiceForInvoicingCustomer := &invoice.Invoice{
		ID:              "inv_invoicing_no_wallet",
		CustomerID:      invoicingCustomer.ID, // Invoice is for invoicing customer
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(150),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(150),
		Description:     "Invoice for Invoicing Customer",
		DueDate:         lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
		PeriodStart:     lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
		PeriodEnd:       lo.ToPtr(s.testData.now.Add(6 * 24 * time.Hour)),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), invoiceForInvoicingCustomer))

	// Process payment - should NOT use subscription customer's wallet
	options := WalletPaymentOptions{
		Strategy:        BalanceOptimizedStrategy,
		MaxWalletsToUse: 0,
	}
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		invoiceForInvoicingCustomer,
		options,
	)

	// Verify payment failed (no wallet for invoicing customer)
	s.NoError(err) // ProcessInvoicePaymentWithWallets doesn't return error, just returns zero
	s.True(decimal.Zero.Equal(amountPaid), "Should not pay anything - invoicing customer has no wallet")

	// Verify no payment was created
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &invoiceForInvoicingCustomer.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Empty(payments, "No payment should be created - invoicing customer has no wallet")
}

// TestGetWalletsForPayment_PostpaidOnly tests that only postpaid wallets are returned for payments
func (s *WalletPaymentServiceSuite) TestGetWalletsForPayment_PostpaidOnly() {
	options := WalletPaymentOptions{
		Strategy: BalanceOptimizedStrategy,
	}

	wallets, err := s.service.GetWalletsForPayment(s.GetContext(), s.testData.customer.ID, "usd", options)
	s.NoError(err)

	// Verify only postpaid wallets are returned
	for _, w := range wallets {
		s.Equal(types.WalletTypePostPaid, w.WalletType, "Wallet %s should be postpaid", w.ID)
		s.NotEqual("wallet_prepaid", w.ID, "Prepaid wallet should not be included")
	}

	// Verify expected postpaid wallet is included
	walletIDs := make(map[string]bool)
	for _, w := range wallets {
		walletIDs[w.ID] = true
	}
	s.True(walletIDs["wallet_promotional"], "Postpaid promotional wallet should be included")
	s.Equal(1, len(wallets), "Should have only 1 postpaid wallet")
}

// TestGetWalletsForCreditAdjustment_PrepaidOnly tests that only prepaid wallets are returned for credit adjustments
func (s *WalletPaymentServiceSuite) TestGetWalletsForCreditAdjustment_PrepaidOnly() {
	wallets, err := s.service.GetWalletsForCreditAdjustment(s.GetContext(), s.testData.customer.ID, "usd")
	s.NoError(err)

	// Verify only prepaid wallets are returned
	for _, w := range wallets {
		s.Equal(types.WalletTypePrePaid, w.WalletType, "Wallet %s should be prepaid", w.ID)
		s.NotEqual("wallet_promotional", w.ID, "Postpaid promotional wallet should not be included")
	}

	// Verify expected prepaid wallet is included
	walletIDs := make(map[string]bool)
	for _, w := range wallets {
		walletIDs[w.ID] = true
	}
	s.True(walletIDs["wallet_prepaid"], "Prepaid wallet should be included")
}

// TestProcessInvoicePaymentWithWallets_PostpaidOnly tests that only postpaid wallets are used for payments
func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithWallets_PostpaidOnly() {
	// Reset payment store
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.setupWallet()

	// Create invoice
	now := time.Now().UTC()
	inv := &invoice.Invoice{
		ID:              "inv_postpaid_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		PeriodEnd:       lo.ToPtr(now),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	// Process payment
	options := WalletPaymentOptions{
		Strategy:        BalanceOptimizedStrategy,
		MaxWalletsToUse: 0, // No limit
	}
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, options)
	s.NoError(err)

	// Should only use postpaid wallet: 60 (prepaid wallet with 200 excluded)
	expectedAmount := decimal.NewFromFloat(60)
	s.True(expectedAmount.Equal(amountPaid), "Should only use postpaid wallet, expected %s, got %s", expectedAmount, amountPaid)

	// Verify payments were created
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(1, len(payments), "Should have 1 payment from postpaid wallet")

	// Verify all payments are from postpaid wallets
	for _, payment := range payments {
		walletID := payment.Metadata["wallet_id"]
		walletType := payment.Metadata["wallet_type"]
		s.Equal(string(types.WalletTypePostPaid), walletType, "Payment from wallet %s should be postpaid", walletID)
		s.NotEqual("wallet_prepaid", walletID, "Prepaid wallet should not be used for payments")
	}
}

// TestProcessInvoicePaymentWithWallets_MixedWalletTypes tests payment with both prepaid and postpaid wallets
func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithWallets_MixedWalletTypes() {
	// Reset payment store
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.setupWallet()

	// Create invoice
	now := time.Now().UTC()
	inv := &invoice.Invoice{
		ID:              "inv_mixed_wallets",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		PeriodEnd:       lo.ToPtr(now),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	// Process payment - should only use postpaid wallets
	options := WalletPaymentOptions{
		Strategy:        BalanceOptimizedStrategy,
		MaxWalletsToUse: 0,
	}
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, options)
	s.NoError(err)

	// Should only use postpaid wallet (60), prepaid wallet (200) should be excluded
	expectedAmount := decimal.NewFromFloat(60)
	s.True(expectedAmount.Equal(amountPaid), "Should only use postpaid wallet, expected %s, got %s", expectedAmount, amountPaid)

	// Verify prepaid wallet was not used
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	for _, payment := range payments {
		walletID := payment.Metadata["wallet_id"]
		s.NotEqual("wallet_prepaid", walletID, "Prepaid wallet should not be used for payments")
	}
}

// TestGetWalletsForPayment_NoPostpaidWallets tests behavior when only prepaid wallets exist
func (s *WalletPaymentServiceSuite) TestGetWalletsForPayment_NoPostpaidWallets() {
	// Clear wallets and create only prepaid wallets
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	prepaidOnly := &wallet.Wallet{
		ID:             "wallet_prepaid_only",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid, // Fixed: Should be PrePaid, not PostPaid
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), prepaidOnly))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         prepaidOnly.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           prepaidOnly.Balance,
		CreditAmount:     prepaidOnly.CreditBalance,
		CreditsAvailable: prepaidOnly.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	options := WalletPaymentOptions{
		Strategy: BalanceOptimizedStrategy,
	}

	wallets, err := s.service.GetWalletsForPayment(s.GetContext(), s.testData.customer.ID, "usd", options)
	s.NoError(err)
	s.Empty(wallets, "Should return no wallets when only prepaid wallets exist")
}

// TestGetWalletsForCreditAdjustment_NoPrepaidWallets tests behavior when only postpaid wallets exist
func (s *WalletPaymentServiceSuite) TestGetWalletsForCreditAdjustment_NoPrepaidWallets() {
	// Clear wallets and create only postpaid wallets
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	postpaidOnly := &wallet.Wallet{
		ID:             "wallet_postpaid_only",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePostPaid,
		EnvironmentID:  "env_test",
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), postpaidOnly))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         postpaidOnly.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           postpaidOnly.Balance,
		CreditAmount:     postpaidOnly.CreditBalance,
		CreditsAvailable: postpaidOnly.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	wallets, err := s.service.GetWalletsForCreditAdjustment(s.GetContext(), s.testData.customer.ID, "usd")
	s.NoError(err)
	s.Empty(wallets, "Should return no wallets when only postpaid wallets exist")
}

// TestProcessInvoicePaymentWithWallets_PostpaidWalletTypes tests various postpaid wallet scenarios
func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithWallets_PostpaidWalletTypes() {
	tests := []struct {
		name                string
		postpaidBalance     decimal.Decimal
		prepaidBalance      decimal.Decimal
		invoiceAmount       decimal.Decimal
		expectedAmountPaid  decimal.Decimal
		expectedWalletsUsed int
	}{
		{
			name:                "Postpaid sufficient, prepaid ignored",
			postpaidBalance:     decimal.NewFromFloat(100),
			prepaidBalance:      decimal.NewFromFloat(200),
			invoiceAmount:       decimal.NewFromFloat(80),
			expectedAmountPaid:  decimal.NewFromFloat(80),
			expectedWalletsUsed: 1,
		},
		{
			name:                "Postpaid insufficient, prepaid still ignored",
			postpaidBalance:     decimal.NewFromFloat(30),
			prepaidBalance:      decimal.NewFromFloat(500),
			invoiceAmount:       decimal.NewFromFloat(100),
			expectedAmountPaid:  decimal.NewFromFloat(30), // Only postpaid wallet used (single wallet with 30)
			expectedWalletsUsed: 1,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset stores
			s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
			s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

			// Create postpaid wallet
			postpaidWallet := &wallet.Wallet{
				ID:             "wallet_postpaid_test",
				CustomerID:     s.testData.customer.ID,
				Currency:       "usd",
				Balance:        tc.postpaidBalance,
				CreditBalance:  tc.postpaidBalance,
				ConversionRate: decimal.NewFromFloat(1.0),
				WalletStatus:   types.WalletStatusActive,
				WalletType:     types.WalletTypePostPaid,
				EnvironmentID:  "env_test",
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), postpaidWallet))
			s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:         postpaidWallet.ID,
				Type:             types.TransactionTypeCredit,
				Amount:           postpaidWallet.Balance,
				CreditAmount:     postpaidWallet.CreditBalance,
				CreditsAvailable: postpaidWallet.CreditBalance,
				TxStatus:         types.TransactionStatusCompleted,
				BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
			}))

			// Create prepaid wallet (should be ignored for payments, used for credit adjustments)
			prepaidWallet := &wallet.Wallet{
				ID:             "wallet_prepaid_test",
				CustomerID:     s.testData.customer.ID,
				Currency:       "usd",
				Balance:        tc.prepaidBalance,
				CreditBalance:  tc.prepaidBalance,
				ConversionRate: decimal.NewFromFloat(1.0),
				WalletStatus:   types.WalletStatusActive,
				WalletType:     types.WalletTypePrePaid, // Fixed: Actually PrePaid type
				EnvironmentID:  "env_test",
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), prepaidWallet))
			s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:         prepaidWallet.ID,
				Type:             types.TransactionTypeCredit,
				Amount:           prepaidWallet.Balance,
				CreditAmount:     prepaidWallet.CreditBalance,
				CreditsAvailable: prepaidWallet.CreditBalance,
				TxStatus:         types.TransactionStatusCompleted,
				BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
			}))

			// Create invoice
			now := time.Now().UTC()
			inv := &invoice.Invoice{
				ID:              "inv_" + tc.name,
				CustomerID:      s.testData.customer.ID,
				InvoiceType:     types.InvoiceTypeOneOff,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        "usd",
				AmountDue:       tc.invoiceAmount,
				AmountPaid:      decimal.Zero,
				AmountRemaining: tc.invoiceAmount,
				PeriodEnd:       lo.ToPtr(now),
				BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

			// Process payment
			options := WalletPaymentOptions{
				Strategy:        BalanceOptimizedStrategy,
				MaxWalletsToUse: 0,
			}
			amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, options)
			s.NoError(err)

			// Verify results
			s.True(tc.expectedAmountPaid.Equal(amountPaid),
				"Amount paid mismatch: expected %s, got %s", tc.expectedAmountPaid, amountPaid)

			// Verify payments
			payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
				DestinationID:   &inv.ID,
				DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
			})
			s.NoError(err)
			s.Equal(tc.expectedWalletsUsed, len(payments),
				"Expected %d payments, got %d", tc.expectedWalletsUsed, len(payments))

			// Verify all payments are from postpaid wallets
			for _, payment := range payments {
				walletType := payment.Metadata["wallet_type"]
				s.Equal(string(types.WalletTypePostPaid), walletType, "All payments should be from postpaid wallets")
			}
		})
	}
}
