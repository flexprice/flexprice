package service

import (
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
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
	// 1. Promotional wallet
	s.testData.wallets.promotional = &wallet.Wallet{
		ID:             "wallet_promotional",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(50),
		CreditBalance:  decimal.NewFromFloat(50),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
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

	// 2. Prepaid wallet
	s.testData.wallets.prepaid = &wallet.Wallet{
		ID:             "wallet_prepaid",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(200),
		CreditBalance:  decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
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

	// 3. Small balance wallet
	s.testData.wallets.smallBalance = &wallet.Wallet{
		ID:             "wallet_small_balance",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(10),
		CreditBalance:  decimal.NewFromFloat(10),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.smallBalance))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         s.testData.wallets.smallBalance.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           s.testData.wallets.smallBalance.Balance,
		CreditAmount:     s.testData.wallets.smallBalance.CreditBalance,
		CreditsAvailable: s.testData.wallets.smallBalance.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// 4. Different currency wallet
	s.testData.wallets.differentCurrency = &wallet.Wallet{
		ID:             "wallet_different_currency",
		CustomerID:     s.testData.customer.ID,
		Currency:       "eur",
		Balance:        decimal.NewFromFloat(300),
		CreditBalance:  decimal.NewFromFloat(300),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
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
		WalletType:     types.WalletTypePromotional,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallets.inactive))
}

func (s *WalletPaymentServiceSuite) TestGetWalletsForPaymentWithDifferentStrategies() {
	tests := []struct {
		name            string
		strategy        WalletPaymentStrategy
		expectedWallets []string
	}{
		{
			name:     "PromotionalFirstStrategy",
			strategy: PromotionalFirstStrategy,
			// Expect promotional wallets first (in order of largest balance first), then prepaid
			expectedWallets: []string{"wallet_promotional", "wallet_small_balance", "wallet_prepaid"},
		},
		{
			name:     "PrepaidFirstStrategy",
			strategy: PrepaidFirstStrategy,
			// Expect prepaid wallets first, then promotional
			expectedWallets: []string{"wallet_prepaid", "wallet_small_balance", "wallet_promotional"},
		},
		{
			name:     "BalanceOptimizedStrategy",
			strategy: BalanceOptimizedStrategy,
			// Expect wallets ordered by balance (smallest first)
			expectedWallets: []string{"wallet_small_balance", "wallet_promotional", "wallet_prepaid"},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			options := WalletPaymentOptions{
				Strategy: tc.strategy,
			}

			wallets, err := s.service.GetWalletsForPayment(s.GetContext(), s.testData.customer.ID, "usd", options)
			s.NoError(err)
			s.Equal(len(tc.expectedWallets), len(wallets), "Wrong number of wallets returned")

			// Verify wallets are in expected order
			for i, expectedID := range tc.expectedWallets {
				s.Equal(expectedID, wallets[i].ID, "Wallet at position %d incorrect", i)
			}

			// Verify that inactive and different currency wallets are excluded
			for _, w := range wallets {
				s.NotEqual("wallet_inactive", w.ID, "Inactive wallet should not be included")
				s.NotEqual("wallet_different_currency", w.ID, "Different currency wallet should not be included")
			}
		})
	}
}

func (s *WalletPaymentServiceSuite) TestProcessInvoicePaymentWithWallets() {
	tests := []struct {
		name                string
		strategy            WalletPaymentStrategy
		maxWalletsToUse     int
		additionalMetadata  types.Metadata
		expectedAmountPaid  decimal.Decimal
		expectedWalletsUsed int
	}{
		{
			name:                "Full payment with promotional first strategy",
			strategy:            PromotionalFirstStrategy,
			maxWalletsToUse:     0, // No limit
			additionalMetadata:  types.Metadata{"test_key": "test_value"},
			expectedAmountPaid:  decimal.NewFromFloat(150), // 50 + 10 + 90
			expectedWalletsUsed: 3,                         // 3 wallets used (promotional, small balance, prepaid)
		},
		{
			name:                "Limited number of wallets",
			strategy:            PromotionalFirstStrategy,
			maxWalletsToUse:     1,                        // Only use one wallet
			expectedAmountPaid:  decimal.NewFromFloat(50), // Only 50 from the first wallet
			expectedWalletsUsed: 1,
		},
		{
			name:                "PrepaidFirst strategy",
			strategy:            PrepaidFirstStrategy,
			maxWalletsToUse:     0,                         // No limit
			expectedAmountPaid:  decimal.NewFromFloat(150), // 150 from prepaid wallet (of 200 total)
			expectedWalletsUsed: 1,
		},
		{
			name:                "BalanceOptimized strategy",
			strategy:            BalanceOptimizedStrategy,
			maxWalletsToUse:     0,                         // No limit
			expectedAmountPaid:  decimal.NewFromFloat(150), // 10 + 50 + 90 (from small, promotional, prepaid)
			expectedWalletsUsed: 3,
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
				Strategy:        tc.strategy,
				MaxWalletsToUse: tc.maxWalletsToUse,
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
	s.NoError(err)
	expectedAmount := decimal.NewFromFloat(260) // 50 + 10 + 200 (all wallets combined)
	s.True(expectedAmount.Equal(amountPaid),
		"Amount paid mismatch: expected %s, got %s", expectedAmount, amountPaid)

	// Verify payment requests
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &inv.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(3, len(payments), "Expected 3 payment requests (all wallets)")
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

// TestUsageRestrictedWalletsWithMixedLineItems tests wallets with usage restrictions on invoices with both usage and fixed line items
func (s *WalletPaymentServiceSuite) TestUsageRestrictedWalletsWithMixedLineItems() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create usage-restricted wallet
	usageRestrictedWallet := &wallet.Wallet{
		ID:             "wallet_usage_restricted_mixed",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), usageRestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         usageRestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           usageRestrictedWallet.Balance,
		CreditAmount:     usageRestrictedWallet.CreditBalance,
		CreditsAvailable: usageRestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create unrestricted wallet
	unrestrictedWallet := &wallet.Wallet{
		ID:             "wallet_unrestricted_mixed",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(150),
		CreditBalance:  decimal.NewFromFloat(150),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), unrestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         unrestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           unrestrictedWallet.Balance,
		CreditAmount:     unrestrictedWallet.CreditBalance,
		CreditsAvailable: unrestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with mixed line items (usage + fixed)
	mixedInvoice := &invoice.Invoice{
		ID:              "inv_mixed_line_items_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(200), // $80 usage + $120 fixed
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(200),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_usage_1_mixed",
				InvoiceID: "inv_mixed_line_items_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(50),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_usage_2_mixed",
				InvoiceID: "inv_mixed_line_items_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(30),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_fixed_1_mixed",
				InvoiceID: "inv_mixed_line_items_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(120),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), mixedInvoice))

	// Process payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		mixedInvoice,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	// Expected: $80 from restricted wallet (all usage) + $120 from unrestricted wallet (remaining amount)
	s.True(decimal.NewFromFloat(200).Equal(amountPaid), "Should pay full amount: expected 200, got %s", amountPaid)

	// Verify payment breakdown
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &mixedInvoice.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(2, len(payments), "Should have 2 payments (one from each wallet)")

	// Find payments by wallet
	var restrictedPayment, unrestrictedPayment *payment.Payment
	for _, payment := range payments {
		if payment.PaymentMethodID == usageRestrictedWallet.ID {
			restrictedPayment = payment
		} else if payment.PaymentMethodID == unrestrictedWallet.ID {
			unrestrictedPayment = payment
		}
	}

	s.NotNil(restrictedPayment, "Should have payment from restricted wallet")
	s.NotNil(unrestrictedPayment, "Should have payment from unrestricted wallet")
	s.True(decimal.NewFromFloat(80).Equal(restrictedPayment.Amount), "Restricted wallet should pay exactly the usage amount: expected 80, got %s", restrictedPayment.Amount)
	s.True(decimal.NewFromFloat(120).Equal(unrestrictedPayment.Amount), "Unrestricted wallet should pay remaining amount: expected 120, got %s", unrestrictedPayment.Amount)
}

// TestUsageRestrictedWalletsWithNoUsageLineItems tests that restricted wallets cannot pay when there are no usage line items
func (s *WalletPaymentServiceSuite) TestUsageRestrictedWalletsWithNoUsageLineItems() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create usage-restricted wallet
	usageRestrictedWallet := &wallet.Wallet{
		ID:             "wallet_usage_restricted_no_usage_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), usageRestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         usageRestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           usageRestrictedWallet.Balance,
		CreditAmount:     usageRestrictedWallet.CreditBalance,
		CreditsAvailable: usageRestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with only fixed line items
	fixedOnlyInvoice := &invoice.Invoice{
		ID:              "inv_fixed_only_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_fixed_only_test",
				InvoiceID: "inv_fixed_only_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), fixedOnlyInvoice))

	// Process payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		fixedOnlyInvoice,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	s.True(decimal.Zero.Equal(amountPaid), "Should pay nothing since restricted wallet cannot pay for fixed line items")

	// Verify no payment requests were made
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &fixedOnlyInvoice.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Empty(payments, "No payment requests should have been made")
}

// TestMultipleUsageRestrictedWallets tests multiple restricted wallets competing for limited usage amount
func (s *WalletPaymentServiceSuite) TestMultipleUsageRestrictedWallets() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create multiple usage-restricted wallets
	restrictedWallet1 := &wallet.Wallet{
		ID:             "wallet_restricted_1_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(60),
		CreditBalance:  decimal.NewFromFloat(60),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet1))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         restrictedWallet1.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           restrictedWallet1.Balance,
		CreditAmount:     restrictedWallet1.CreditBalance,
		CreditsAvailable: restrictedWallet1.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	restrictedWallet2 := &wallet.Wallet{
		ID:             "wallet_restricted_2_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(40),
		CreditBalance:  decimal.NewFromFloat(40),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet2))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         restrictedWallet2.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           restrictedWallet2.Balance,
		CreditAmount:     restrictedWallet2.CreditBalance,
		CreditsAvailable: restrictedWallet2.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with limited usage amount
	limitedUsageInvoice := &invoice.Invoice{
		ID:              "inv_limited_usage_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(150), // $50 usage + $100 fixed
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(150),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_usage_limited_test",
				InvoiceID: "inv_limited_usage_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(50),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_fixed_limited_test",
				InvoiceID: "inv_limited_usage_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), limitedUsageInvoice))

	// Process payment with balance optimized strategy to use smallest balance wallet first
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		limitedUsageInvoice,
		WalletPaymentOptions{
			Strategy:        BalanceOptimizedStrategy,
			MaxWalletsToUse: 0,
		},
	)

	// Verify results
	s.NoError(err)
	// Expected: Only $50 should be paid (the usage amount) since only restricted wallets are available
	s.True(decimal.NewFromFloat(50).Equal(amountPaid), "Should pay only the usage amount: expected 50, got %s", amountPaid)

	// Verify payment breakdown
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &limitedUsageInvoice.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(2, len(payments), "Should have 2 payments: first wallet pays $40, second pays remaining $10")

	// Sort payments by amount to make assertions predictable
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Amount.GreaterThan(payments[j].Amount)
	})

	// The first payment should be from wallet2 (smaller balance wallet) paying its full balance
	s.Equal(restrictedWallet2.ID, payments[0].PaymentMethodID, "First payment should be from wallet with smaller balance")
	s.True(decimal.NewFromFloat(40).Equal(payments[0].Amount), "First payment should be wallet's full balance: expected 40, got %s", payments[0].Amount)

	// The second payment should be from wallet1 paying the remaining usage amount
	s.Equal(restrictedWallet1.ID, payments[1].PaymentMethodID, "Second payment should be from wallet with larger balance")
	s.True(decimal.NewFromFloat(10).Equal(payments[1].Amount), "Second payment should be remaining usage amount: expected 10, got %s", payments[1].Amount)
}

// TestUsageRestrictedWalletsWithExistingPayments tests restricted wallets when usage has been partially paid
func (s *WalletPaymentServiceSuite) TestUsageRestrictedWalletsWithExistingPayments() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create usage-restricted wallet
	usageRestrictedWallet := &wallet.Wallet{
		ID:             "wallet_restricted_existing_payments_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(100),
		CreditBalance:  decimal.NewFromFloat(100),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), usageRestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         usageRestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           usageRestrictedWallet.Balance,
		CreditAmount:     usageRestrictedWallet.CreditBalance,
		CreditsAvailable: usageRestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with usage + fixed line items
	existingPaymentInvoice := &invoice.Invoice{
		ID:              "inv_existing_payments_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(200), // $100 usage + $100 fixed
		AmountPaid:      decimal.NewFromFloat(30),  // $30 already paid via credits
		AmountRemaining: decimal.NewFromFloat(170),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_usage_existing_test",
				InvoiceID: "inv_existing_payments_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_fixed_existing_test",
				InvoiceID: "inv_existing_payments_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), existingPaymentInvoice))

	// Create existing credit payment (simulating previous payment)
	existingPayment := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		Amount:            decimal.NewFromFloat(30),
		Currency:          "usd",
		PaymentMethodType: types.PaymentMethodTypeCredits,
		PaymentMethodID:   "some_previous_wallet_test",
		PaymentStatus:     types.PaymentStatusSucceeded,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     existingPaymentInvoice.ID,
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), existingPayment))

	// Process payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		existingPaymentInvoice,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	// Expected: $70 from restricted wallet (remaining usage after $30 already paid)
	// This tests the critical issue: should the restricted wallet be able to pay $70 or only $70 (100-30)?
	s.True(decimal.NewFromFloat(70).Equal(amountPaid), "Should pay remaining usage amount: expected 70, got %s", amountPaid)
}

// TestCombinedRestrictedAndUnrestrictedWallets tests the complex scenario with both types of wallets
func (s *WalletPaymentServiceSuite) TestCombinedRestrictedAndUnrestrictedWallets() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create two restricted wallets with different balances
	restrictedWallet1 := &wallet.Wallet{
		ID:             "wallet_restricted_small_combo",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(30),
		CreditBalance:  decimal.NewFromFloat(30),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet1))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         restrictedWallet1.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           restrictedWallet1.Balance,
		CreditAmount:     restrictedWallet1.CreditBalance,
		CreditsAvailable: restrictedWallet1.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	restrictedWallet2 := &wallet.Wallet{
		ID:             "wallet_restricted_large_combo",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(80),
		CreditBalance:  decimal.NewFromFloat(80),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet2))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         restrictedWallet2.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           restrictedWallet2.Balance,
		CreditAmount:     restrictedWallet2.CreditBalance,
		CreditsAvailable: restrictedWallet2.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create unrestricted wallet
	unrestrictedWallet := &wallet.Wallet{
		ID:             "wallet_unrestricted_combo_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(200),
		CreditBalance:  decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), unrestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         unrestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           unrestrictedWallet.Balance,
		CreditAmount:     unrestrictedWallet.CreditBalance,
		CreditsAvailable: unrestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with usage + fixed line items
	comboInvoice := &invoice.Invoice{
		ID:              "inv_combo_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(250), // $60 usage + $190 fixed
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(250),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_usage_combo_test",
				InvoiceID: "inv_combo_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(60),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_fixed_combo_test",
				InvoiceID: "inv_combo_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(190),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), comboInvoice))

	// Process payment with balance optimized strategy to ensure smaller restricted wallet goes first
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		comboInvoice,
		WalletPaymentOptions{
			Strategy:        BalanceOptimizedStrategy,
			MaxWalletsToUse: 0,
		},
	)

	// Verify results
	s.NoError(err)
	// Expected: $30 (from small restricted) + $30 (from large restricted) + $190 (from unrestricted) = $250
	s.True(decimal.NewFromFloat(250).Equal(amountPaid), "Should pay full amount: expected 250, got %s", amountPaid)

	// Verify payment breakdown
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &comboInvoice.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(3, len(payments), "Should have 3 payments (2 restricted + 1 unrestricted)")

	// Verify that usage amount is fully covered by restricted wallets
	restrictedPaymentsTotal := decimal.Zero
	for _, payment := range payments {
		if payment.PaymentMethodID == restrictedWallet1.ID || payment.PaymentMethodID == restrictedWallet2.ID {
			restrictedPaymentsTotal = restrictedPaymentsTotal.Add(payment.Amount)
		}
	}
	s.True(decimal.NewFromFloat(60).Equal(restrictedPaymentsTotal), "Restricted wallets should pay exactly the usage amount: expected 60, got %s", restrictedPaymentsTotal)
}

// TestUsageRestrictedWalletsWithPartialUsagePayment tests when usage amount exceeds what restricted wallets can pay
func (s *WalletPaymentServiceSuite) TestUsageRestrictedWalletsWithPartialUsagePayment() {
	// Clear existing data
	s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
	s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

	// Create restricted wallet with insufficient balance for all usage
	restrictedWallet := &wallet.Wallet{
		ID:             "wallet_restricted_insufficient",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(20), // Less than usage amount
		CreditBalance:  decimal.NewFromFloat(20),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePromotional,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         restrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           restrictedWallet.Balance,
		CreditAmount:     restrictedWallet.CreditBalance,
		CreditsAvailable: restrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create unrestricted wallet
	unrestrictedWallet := &wallet.Wallet{
		ID:             "wallet_unrestricted_partial",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(200),
		CreditBalance:  decimal.NewFromFloat(200),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), unrestrictedWallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         unrestrictedWallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           unrestrictedWallet.Balance,
		CreditAmount:     unrestrictedWallet.CreditBalance,
		CreditsAvailable: unrestrictedWallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))

	// Create invoice with high usage amount
	partialUsageInvoice := &invoice.Invoice{
		ID:              "inv_partial_usage_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(200), // $100 usage + $100 fixed
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(200),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:        "line_usage_partial_test",
				InvoiceID: "inv_partial_usage_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:        "line_fixed_partial_test",
				InvoiceID: "inv_partial_usage_test",
				PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
				Amount:    decimal.NewFromFloat(100),
				Currency:  "usd",
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), partialUsageInvoice))

	// Process payment
	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
		s.GetContext(),
		partialUsageInvoice,
		DefaultWalletPaymentOptions(),
	)

	// Verify results
	s.NoError(err)
	// Expected: $20 (restricted, partial usage) + $180 (unrestricted, remaining amount)
	s.True(decimal.NewFromFloat(200).Equal(amountPaid), "Should pay full amount: expected 200, got %s", amountPaid)

	// Verify payment breakdown
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   &partialUsageInvoice.ID,
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	s.Equal(2, len(payments), "Should have 2 payments")

	// Find payments by wallet
	var restrictedPayment, unrestrictedPayment *payment.Payment
	for _, payment := range payments {
		if payment.PaymentMethodID == restrictedWallet.ID {
			restrictedPayment = payment
		} else if payment.PaymentMethodID == unrestrictedWallet.ID {
			unrestrictedPayment = payment
		}
	}

	s.NotNil(restrictedPayment, "Should have payment from restricted wallet")
	s.NotNil(unrestrictedPayment, "Should have payment from unrestricted wallet")
	s.True(decimal.NewFromFloat(20).Equal(restrictedPayment.Amount), "Restricted wallet should pay its full balance: expected 20, got %s", restrictedPayment.Amount)
	s.True(decimal.NewFromFloat(180).Equal(unrestrictedPayment.Amount), "Unrestricted wallet should pay remaining amount: expected 180, got %s", unrestrictedPayment.Amount)
}

// TestUsageRestrictedWalletsEdgeCases tests various edge cases
func (s *WalletPaymentServiceSuite) TestUsageRestrictedWalletsEdgeCases() {
	tests := []struct {
		name                    string
		usageAmount             decimal.Decimal
		fixedAmount             decimal.Decimal
		restrictedWalletBalance decimal.Decimal
		existingCreditPayment   decimal.Decimal
		expectedRestrictedPay   decimal.Decimal
		expectedTotalPay        decimal.Decimal
	}{
		{
			name:                    "Zero usage amount",
			usageAmount:             decimal.Zero,
			fixedAmount:             decimal.NewFromFloat(100),
			restrictedWalletBalance: decimal.NewFromFloat(50),
			existingCreditPayment:   decimal.Zero,
			expectedRestrictedPay:   decimal.Zero,
			expectedTotalPay:        decimal.Zero, // No unrestricted wallet in this test
		},
		{
			name:                    "Usage fully paid by existing credits",
			usageAmount:             decimal.NewFromFloat(50),
			fixedAmount:             decimal.NewFromFloat(100),
			restrictedWalletBalance: decimal.NewFromFloat(50),
			existingCreditPayment:   decimal.NewFromFloat(50), // Usage fully paid
			expectedRestrictedPay:   decimal.Zero,
			expectedTotalPay:        decimal.Zero,
		},
		{
			name:                    "Usage partially paid by existing credits",
			usageAmount:             decimal.NewFromFloat(100),
			fixedAmount:             decimal.NewFromFloat(50),
			restrictedWalletBalance: decimal.NewFromFloat(80),
			existingCreditPayment:   decimal.NewFromFloat(30), // $70 usage remaining
			expectedRestrictedPay:   decimal.NewFromFloat(70),
			expectedTotalPay:        decimal.NewFromFloat(70),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Clear existing data
			s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore).Clear()
			s.GetStores().WalletRepo.(*testutil.InMemoryWalletStore).Clear()

			// Create restricted wallet
			restrictedWallet := &wallet.Wallet{
				ID:             "wallet_restricted_edge_" + tc.name,
				CustomerID:     s.testData.customer.ID,
				Currency:       "usd",
				Balance:        tc.restrictedWalletBalance,
				CreditBalance:  tc.restrictedWalletBalance,
				ConversionRate: decimal.NewFromFloat(1.0),
				WalletStatus:   types.WalletStatusActive,
				WalletType:     types.WalletTypePromotional,
				Config: types.WalletConfig{
					AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
				},
				BaseModel: types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), restrictedWallet))
			s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:         restrictedWallet.ID,
				Type:             types.TransactionTypeCredit,
				Amount:           restrictedWallet.Balance,
				CreditAmount:     restrictedWallet.CreditBalance,
				CreditsAvailable: restrictedWallet.CreditBalance,
				TxStatus:         types.TransactionStatusCompleted,
				BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
			}))
			s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
				WalletID:         restrictedWallet.ID,
				Type:             types.TransactionTypeCredit,
				Amount:           restrictedWallet.Balance,
				CreditAmount:     restrictedWallet.CreditBalance,
				CreditsAvailable: restrictedWallet.CreditBalance,
				TxStatus:         types.TransactionStatusCompleted,
				BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
			}))

			// Create line items
			invoiceID := "inv_edge_" + tc.name
			lineItems := []*invoice.InvoiceLineItem{}
			if tc.usageAmount.GreaterThan(decimal.Zero) {
				lineItems = append(lineItems, &invoice.InvoiceLineItem{
					ID:        "line_usage_edge_" + tc.name,
					InvoiceID: invoiceID,
					PriceType: lo.ToPtr(types.PRICE_TYPE_USAGE.String()),
					Amount:    tc.usageAmount,
					Currency:  "usd",
					BaseModel: types.GetDefaultBaseModel(s.GetContext()),
				})
			}
			if tc.fixedAmount.GreaterThan(decimal.Zero) {
				lineItems = append(lineItems, &invoice.InvoiceLineItem{
					ID:        "line_fixed_edge_" + tc.name,
					InvoiceID: invoiceID,
					PriceType: lo.ToPtr(types.PRICE_TYPE_FIXED.String()),
					Amount:    tc.fixedAmount,
					Currency:  "usd",
					BaseModel: types.GetDefaultBaseModel(s.GetContext()),
				})
			}

			totalAmount := tc.usageAmount.Add(tc.fixedAmount)
			invoiceID = "inv_edge_" + tc.name
			edgeInvoice := &invoice.Invoice{
				ID:              invoiceID,
				CustomerID:      s.testData.customer.ID,
				InvoiceType:     types.InvoiceTypeOneOff,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        "usd",
				AmountDue:       totalAmount,
				AmountPaid:      tc.existingCreditPayment,
				AmountRemaining: totalAmount.Sub(tc.existingCreditPayment),
				LineItems:       lineItems,
				BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), edgeInvoice))

			// Create existing credit payment if any
			if tc.existingCreditPayment.GreaterThan(decimal.Zero) {
				existingPayment := &payment.Payment{
					ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
					Amount:            tc.existingCreditPayment,
					Currency:          "usd",
					PaymentMethodType: types.PaymentMethodTypeCredits,
					PaymentMethodID:   "previous_wallet",
					PaymentStatus:     types.PaymentStatusSucceeded,
					DestinationType:   types.PaymentDestinationTypeInvoice,
					DestinationID:     edgeInvoice.ID,
					BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
				}
				s.NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), existingPayment))
			}

			// Process payment
			amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(
				s.GetContext(),
				edgeInvoice,
				DefaultWalletPaymentOptions(),
			)

			// Verify results
			s.NoError(err)
			s.True(tc.expectedTotalPay.Equal(amountPaid),
				"Amount paid mismatch for %s: expected %s, got %s",
				tc.name, tc.expectedTotalPay, amountPaid)

			// Verify restricted wallet payment amount
			allPayments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
				DestinationID:   &edgeInvoice.ID,
				DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
			})
			s.NoError(err)

			// Filter payments by restricted wallet
			var payments []*payment.Payment
			for _, p := range allPayments {
				if p.PaymentMethodID == restrictedWallet.ID {
					payments = append(payments, p)
				}
			}

			if tc.expectedRestrictedPay.IsZero() {
				s.Empty(payments, "Restricted wallet should not make any payment for %s", tc.name)
			} else {
				s.Equal(1, len(payments), "Should have 1 payment from restricted wallet for %s", tc.name)
				s.True(tc.expectedRestrictedPay.Equal(payments[0].Amount),
					"Restricted wallet payment mismatch for %s: expected %s, got %s",
					tc.name, tc.expectedRestrictedPay, payments[0].Amount)
			}
		})
	}
}
