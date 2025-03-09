package service

import (
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
