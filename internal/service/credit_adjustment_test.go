package service

import (
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
	service  *CreditAdjustmentService
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

func (s *CreditAdjustmentServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewCreditAdjustmentService(ServiceParams{
		Logger:      s.GetLogger(),
		Config:      s.GetConfig(),
		DB:          s.GetDB(),
		WalletRepo:  stores.WalletRepo,
		InvoiceRepo: stores.InvoiceRepo,
	})
}

// getWalletService returns a wallet service instance for creating credit transactions
func (s *CreditAdjustmentServiceSuite) getWalletService() WalletService {
	stores := s.GetStores()
	return NewWalletService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		WalletRepo:       stores.WalletRepo,
		AlertLogsRepo:    stores.AlertLogsRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
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
	w := &wallet.Wallet{
		ID:             id,
		CustomerID:     s.testData.customer.ID,
		Currency:       currency,
		Balance:        balance,
		CreditBalance:  creditBalance,
		WalletStatus:   status,
		Name:           "Test Wallet " + id,
		Description:    "Test wallet for credit adjustment",
		ConversionRate: decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))

	// If creditBalance is greater than zero, create a credit transaction
	// This is required because DebitWallet uses FindEligibleCredits which looks for credit transactions
	if creditBalance.GreaterThan(decimal.Zero) {
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

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_NoEligibleWallets() {
	// Create invoice with no wallets
	inv := s.createInvoice("inv_no_wallets", "USD", []decimal.Decimal{
		decimal.NewFromFloat(50.00),
		decimal.NewFromFloat(50.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.Zero, inv.TotalCreditsApplied)
	s.Equal(decimal.Zero, inv.LineItems[0].CreditsApplied)
	s.Equal(decimal.Zero, inv.LineItems[1].CreditsApplied)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_WithEligibleWallets() {
	// Create wallets with balances
	wallet1 := s.createWallet("wallet_1", "USD", decimal.NewFromFloat(30.00), decimal.NewFromFloat(30.00), types.WalletStatusActive)
	wallet2 := s.createWallet("wallet_2", "USD", decimal.NewFromFloat(40.00), decimal.NewFromFloat(40.00), types.WalletStatusActive)

	// Create invoice with 2 line items ($50 each = $100 total)
	inv := s.createInvoice("inv_with_wallets", "USD", []decimal.Decimal{
		decimal.NewFromFloat(50.00),
		decimal.NewFromFloat(50.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.NewFromFloat(70.00), inv.TotalCreditsApplied)         // 30 + 40 = 70
	s.Equal(decimal.NewFromFloat(50.00), inv.LineItems[0].CreditsApplied) // Full line item covered
	s.Equal(decimal.NewFromFloat(20.00), inv.LineItems[1].CreditsApplied) // Partial coverage

	// Verify wallet transactions were created
	wallet1Transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet1.ID),
	})
	s.NoError(err)
	wallet2Transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet2.ID),
	})
	s.NoError(err)
	s.GreaterOrEqual(len(wallet1Transactions)+len(wallet2Transactions), 2) // At least 2 transactions should be created

	// Verify line items were updated in repository
	updatedInv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.Equal(decimal.NewFromFloat(70.00), updatedInv.TotalCreditsApplied)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_InsufficientCredits() {
	// Create wallet with insufficient balance
	wallet1 := s.createWallet("wallet_insufficient", "USD", decimal.NewFromFloat(25.00), decimal.NewFromFloat(25.00), types.WalletStatusActive)

	// Create invoice with $100 line item
	inv := s.createInvoice("inv_insufficient", "USD", []decimal.Decimal{
		decimal.NewFromFloat(100.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.NewFromFloat(25.00), inv.TotalCreditsApplied)
	s.Equal(decimal.NewFromFloat(25.00), inv.LineItems[0].CreditsApplied) // Partial coverage

	// Verify wallet transaction was created
	transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet1.ID),
	})
	s.NoError(err)
	s.GreaterOrEqual(len(transactions), 1)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_MultiWalletSequential() {
	// Create wallets with different balances
	wallet1 := s.createWallet("wallet_seq_1", "USD", decimal.NewFromFloat(20.00), decimal.NewFromFloat(20.00), types.WalletStatusActive)
	wallet2 := s.createWallet("wallet_seq_2", "USD", decimal.NewFromFloat(50.00), decimal.NewFromFloat(50.00), types.WalletStatusActive)

	// Create invoice with 3 line items ($30, $40, $30 = $100 total)
	inv := s.createInvoice("inv_sequential", "USD", []decimal.Decimal{
		decimal.NewFromFloat(30.00),
		decimal.NewFromFloat(40.00),
		decimal.NewFromFloat(30.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.NewFromFloat(70.00), inv.TotalCreditsApplied) // 20 + 50 = 70

	// Verify credits applied across line items
	// First line item: $20 from wallet1, $10 from wallet2 = $30
	// Second line item: $30 from wallet2 = $30 (partial)
	// Third line item: $10 from wallet2 = $10 (partial)
	s.True(inv.LineItems[0].CreditsApplied.GreaterThan(decimal.Zero))
	s.True(inv.LineItems[1].CreditsApplied.GreaterThan(decimal.Zero))
	s.True(inv.LineItems[2].CreditsApplied.GreaterThan(decimal.Zero))

	// Verify wallet transactions were created
	wallet1Transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet1.ID),
	})
	s.NoError(err)
	wallet2Transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet2.ID),
	})
	s.NoError(err)
	s.GreaterOrEqual(len(wallet1Transactions)+len(wallet2Transactions), 2)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_CurrencyMismatch() {
	// Create wallet with EUR currency
	wallet1 := s.createWallet("wallet_eur", "EUR", decimal.NewFromFloat(100.00), decimal.NewFromFloat(100.00), types.WalletStatusActive)

	// Create invoice with USD currency
	inv := s.createInvoice("inv_currency_mismatch", "USD", []decimal.Decimal{
		decimal.NewFromFloat(50.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.Zero, inv.TotalCreditsApplied)
	s.Equal(decimal.Zero, inv.LineItems[0].CreditsApplied)

	// Verify no wallet transactions were created
	transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(wallet1.ID),
	})
	s.NoError(err)
	// Should have no credit adjustment transactions
	creditAdjustmentCount := 0
	for _, tx := range transactions {
		if tx.TransactionReason == types.TransactionReasonCreditAdjustment {
			creditAdjustmentCount++
		}
	}
	s.Equal(0, creditAdjustmentCount)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_InactiveWalletFiltered() {
	// Create active wallet with balance
	activeWallet := s.createWallet("wallet_active", "USD", decimal.NewFromFloat(50.00), decimal.NewFromFloat(50.00), types.WalletStatusActive)

	// Create closed wallet with balance
	inactiveWallet := s.createWallet("wallet_inactive", "USD", decimal.NewFromFloat(100.00), decimal.NewFromFloat(100.00), types.WalletStatusClosed)

	// Create invoice
	inv := s.createInvoice("inv_inactive_filter", "USD", []decimal.Decimal{
		decimal.NewFromFloat(30.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.NewFromFloat(30.00), inv.TotalCreditsApplied) // Only from active wallet

	// Verify only active wallet transaction was created
	activeTransactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(activeWallet.ID),
	})
	s.NoError(err)
	activeCreditAdjustmentCount := 0
	for _, tx := range activeTransactions {
		if tx.TransactionReason == types.TransactionReasonCreditAdjustment {
			activeCreditAdjustmentCount++
		}
	}
	s.GreaterOrEqual(activeCreditAdjustmentCount, 1)

	// Verify inactive wallet has no credit adjustment transactions
	inactiveTransactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(inactiveWallet.ID),
	})
	s.NoError(err)
	inactiveCreditAdjustmentCount := 0
	for _, tx := range inactiveTransactions {
		if tx.TransactionReason == types.TransactionReasonCreditAdjustment {
			inactiveCreditAdjustmentCount++
		}
	}
	s.Equal(0, inactiveCreditAdjustmentCount)
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_ZeroBalanceWalletFiltered() {
	// Create wallet with zero balance
	zeroWallet := s.createWallet("wallet_zero", "USD", decimal.Zero, decimal.Zero, types.WalletStatusActive)

	// Create invoice
	inv := s.createInvoice("inv_zero_balance", "USD", []decimal.Decimal{
		decimal.NewFromFloat(50.00),
	})

	// Execute
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)

	// Assert
	s.NoError(err)
	s.Equal(decimal.Zero, inv.TotalCreditsApplied)
	s.Equal(decimal.Zero, inv.LineItems[0].CreditsApplied)

	// Verify no wallet transactions were created
	transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
		WalletID: lo.ToPtr(zeroWallet.ID),
	})
	s.NoError(err)
	creditAdjustmentCount := 0
	for _, tx := range transactions {
		if tx.TransactionReason == types.TransactionReasonCreditAdjustment {
			creditAdjustmentCount++
		}
	}
	s.Equal(0, creditAdjustmentCount)
}
