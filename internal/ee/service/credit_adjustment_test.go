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

type CreditAdjustmentServiceSuite struct {
	testutil.BaseServiceTestSuite
	service       CreditAdjustmentService
	walletService WalletService
	testData      struct {
		customer *customer.Customer
		wallets  []*wallet.Wallet
		invoice  *invoice.Invoice
	}
}

// usageLine creates a usage-type line item for testing.
func usageLine(amount, lineDisc, invDisc float64) *invoice.InvoiceLineItem {
	pt := string(types.PRICE_TYPE_USAGE)
	return &invoice.InvoiceLineItem{
		PriceType:            &pt,
		Amount:               decimal.NewFromFloat(amount),
		LineItemDiscount:     decimal.NewFromFloat(lineDisc),
		InvoiceLevelDiscount: decimal.NewFromFloat(invDisc),
	}
}

// fixedLine creates a fixed-price line item for testing.
func fixedLine(amount float64) *invoice.InvoiceLineItem {
	pt := string(types.PRICE_TYPE_FIXED)
	return &invoice.InvoiceLineItem{PriceType: &pt, Amount: decimal.NewFromFloat(amount)}
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
	params := ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		CustomerRepo:             stores.CustomerRepo,
		WalletRepo:               stores.WalletRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		InvoiceLineItemRepo:      stores.InvoiceLineItemRepo,
		SettingsRepo:             stores.SettingsRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		MeterRepo:                stores.MeterRepo,
		PriceRepo:                stores.PriceRepo,
		FeatureRepo:              stores.FeatureRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
	}
	s.service = NewCreditAdjustmentService(params)
	s.walletService = NewWalletService(params)
}

// getServiceImpl returns the concrete service implementation for accessing testing-only methods
func (s *CreditAdjustmentServiceSuite) getServiceImpl() *creditAdjustmentService {
	return s.service.(*creditAdjustmentService)
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

// TestCalculateCreditAdjustments_DustBalanceNoHang ensures that when a wallet has a positive balance
// that rounds to zero (e.g. 0.001 USD), the loop skips it and advances instead of hanging.
func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_DustBalanceNoHang() {
	svc := s.getServiceImpl()

	// One usage line item for 1.00 USD
	li := s.createLineItemForCalculation(decimal.NewFromFloat(1.00), lo.ToPtr(string(types.PRICE_TYPE_USAGE)), decimal.Zero)
	li.InvoiceLevelDiscount = decimal.Zero
	inv := s.createInvoiceForCalculation("inv_dust", "USD", []*invoice.InvoiceLineItem{li})

	// Single wallet with dust balance: 0.001 USD rounds to 0.00 for USD (2 decimals)
	wallets := []*wallet.Wallet{
		s.createWalletForCalculation("wallet_dust", "USD", decimal.RequireFromString("0.001")),
	}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	// Dust is skipped (not debited); no amount applied to line item
	s.Empty(debits, "dust wallet should not be debited")
	s.True(inv.LineItems[0].PrepaidCreditsApplied.IsZero(), "no amount should be applied from dust")
}

func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_UsageOnlyAppliesAfterDiscounts() {
	svc := s.getServiceImpl()

	li := s.createLineItemForCalculation(decimal.NewFromInt(100), lo.ToPtr(string(types.PRICE_TYPE_USAGE)), decimal.NewFromInt(20))
	li.InvoiceLevelDiscount = decimal.NewFromInt(10)
	inv := s.createInvoiceForCalculation("inv_usage_after_discounts", "USD", []*invoice.InvoiceLineItem{li})

	wallets := []*wallet.Wallet{
		s.createWalletForCalculation("wallet_1", "USD", decimal.NewFromInt(50)),
	}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	// Net line amount = 100 - 20 - 10 = 70; wallet balance 50 => apply 50.
	s.True(decimal.NewFromInt(50).Equal(inv.LineItems[0].PrepaidCreditsApplied))
	s.Len(debits, 1)
	s.True(decimal.NewFromInt(50).Equal(debits["wallet_1"]))
}

func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_SkipsNonUsageLineItems() {
	svc := s.getServiceImpl()

	fixed := s.createLineItemForCalculation(decimal.NewFromInt(100), lo.ToPtr(string(types.PRICE_TYPE_FIXED)), decimal.Zero)
	fixed.InvoiceLevelDiscount = decimal.Zero
	inv := s.createInvoiceForCalculation("inv_fixed_skip", "USD", []*invoice.InvoiceLineItem{fixed})

	wallets := []*wallet.Wallet{
		s.createWalletForCalculation("wallet_1", "USD", decimal.NewFromInt(100)),
	}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	s.True(inv.LineItems[0].PrepaidCreditsApplied.IsZero(), "fixed line item should not get prepaid credits applied")
	s.Empty(debits, "no wallets should be debited when invoice has no usage items")
}

func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_MultipleWalletsConsumedInOrder() {
	svc := s.getServiceImpl()

	li := s.createLineItemForCalculation(decimal.NewFromInt(50), lo.ToPtr(string(types.PRICE_TYPE_USAGE)), decimal.Zero)
	li.InvoiceLevelDiscount = decimal.Zero
	inv := s.createInvoiceForCalculation("inv_multi_wallet", "USD", []*invoice.InvoiceLineItem{li})

	wallets := []*wallet.Wallet{
		s.createWalletForCalculation("wallet_a", "USD", decimal.NewFromInt(30)),
		s.createWalletForCalculation("wallet_b", "USD", decimal.NewFromInt(40)),
	}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	// Need 50. Consume wallet_a(30) then wallet_b(20).
	s.True(decimal.NewFromInt(50).Equal(inv.LineItems[0].PrepaidCreditsApplied))
	s.Len(debits, 2)
	s.True(decimal.NewFromInt(30).Equal(debits["wallet_a"]))
	s.True(decimal.NewFromInt(20).Equal(debits["wallet_b"]))
}

// createWalletWithCredit creates a DB-backed prepaid wallet and credits it via WalletService.
// Mirrors InvoiceDiscountCreditWorkflowSuite.createWalletWithCredit.
func (s *CreditAdjustmentServiceSuite) createWalletWithCredit(id string, currency string, balance decimal.Decimal) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:             id,
		CustomerID:     s.testData.customer.ID,
		Currency:       currency,
		Balance:        decimal.Zero,
		CreditBalance:  decimal.Zero,
		WalletStatus:   types.WalletStatusActive,
		Name:           "Test Wallet " + id,
		Description:    "Test wallet",
		ConversionRate: decimal.NewFromInt(1),
		EnvironmentID:  "env_test",
		WalletType:     types.WalletTypePrePaid,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))

	if balance.GreaterThan(decimal.Zero) {
		creditOp := &wallet.WalletOperation{
			WalletID:          w.ID,
			Type:              types.TransactionTypeCredit,
			CreditAmount:      balance,
			Description:       "Initial credit for test wallet",
			TransactionReason: types.TransactionReasonFreeCredit,
		}
		s.NoError(s.walletService.CreditWallet(s.GetContext(), creditOp))

		updatedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		w = updatedWallet
	}

	return w
}

// createWalletCredit directly creates a wallet credit transaction via the repository, bypassing
// WalletOperation.Validate() (which unconditionally rejects a past ExpiryDate). Use this whenever a
// test needs a credit with a past ExpiryDate - the exact population applyExpiringCreditToInvoice
// targets - and/or an explicit Priority. Updates the wallet's Balance/CreditBalance to match, mirroring
// what processWalletOperation does for an ordinary credit (all test wallets use conversion rate 1).
func (s *CreditAdjustmentServiceSuite) createWalletCredit(walletID string, amount decimal.Decimal, expiryDate *time.Time, priority *int) *wallet.Transaction {
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
	s.Require().NoError(err)

	tx := &wallet.Transaction{
		ID:                  s.GetUUID(),
		WalletID:            walletID,
		CustomerID:          w.CustomerID,
		Type:                types.TransactionTypeCredit,
		Amount:              amount,
		CreditAmount:        amount,
		TxStatus:            types.TransactionStatusCompleted,
		TransactionReason:   types.TransactionReasonFreeCredit,
		ExpiryDate:          expiryDate,
		Priority:            priority,
		CreditBalanceBefore: w.CreditBalance,
		CreditBalanceAfter:  w.CreditBalance.Add(amount),
		CreditsAvailable:    amount,
		Currency:            w.Currency,
		EnvironmentID:       "env_test",
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), tx))

	newCreditBalance := w.CreditBalance.Add(amount)
	newBalance := w.Balance.Add(amount) // conversion rate is 1 for all test wallets
	s.Require().NoError(s.GetStores().WalletRepo.UpdateWalletBalance(s.GetContext(), walletID, newBalance, newCreditBalance))

	return tx
}

// walletBalance sums Balance across all of a customer's wallets in the given currency.
func (s *CreditAdjustmentServiceSuite) walletBalance(customerID, currency string) decimal.Decimal {
	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), customerID)
	s.Require().NoError(err)
	total := decimal.Zero
	for _, w := range wallets {
		if w.Currency == currency {
			total = total.Add(w.Balance)
		}
	}
	return total
}

// createDraftInvoiceWithUsageLineItem creates a DB-backed draft invoice with a single usage line
// item of the given amount. Mirrors InvoiceDiscountCreditWorkflowSuite.createInvoiceWithLineItems.
func (s *CreditAdjustmentServiceSuite) createDraftInvoiceWithUsageLineItem(id string, currency string, amount decimal.Decimal) *invoice.Invoice {
	pt := string(types.PRICE_TYPE_USAGE)
	li := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        id,
		CustomerID:       s.testData.customer.ID,
		Amount:           amount,
		Currency:         currency,
		Quantity:         decimal.NewFromInt(1),
		PriceType:        &pt,
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	inv := &invoice.Invoice{
		ID:            id,
		CustomerID:    s.testData.customer.ID,
		Currency:      currency,
		Subtotal:      amount,
		Total:         amount,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
		LineItems:     []*invoice.InvoiceLineItem{li},
	}

	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

// reloadInvoiceWithLineItems reloads an invoice and its line items from the repos, mirroring the
// reload pattern used throughout InvoiceDiscountCreditWorkflowSuite (Get() no longer eager-loads
// line items; they must be fetched separately).
func (s *CreditAdjustmentServiceSuite) reloadInvoiceWithLineItems(id string) *invoice.Invoice {
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), id)
	s.NoError(err)
	lineItems, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), id)
	s.NoError(err)
	inv.LineItems = lineItems
	return inv
}

// Applying twice must be additive and must not double-debit: the second call honors the first
// via the invoice-level TotalPrepaidCreditsApplied authority and only applies the remaining delta.
func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_HonorsPriorAndAddsDelta() {
	// 1. Draft invoice with one usage line item, amount 100 USD.
	inv := s.createDraftInvoiceWithUsageLineItem("inv_honor_prior", "USD", decimal.NewFromInt(100))

	// 2. Credit a prepaid wallet for the invoice's customer with 60 USD.
	w := s.createWalletWithCredit("wallet_honor_prior", "USD", decimal.NewFromInt(60))

	// 3. First call: applies the full 60 available.
	result1, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(result1.TotalPrepaidCreditsApplied),
		"first call: expected 60 applied, got %s", result1.TotalPrepaidCreditsApplied.String())

	// Persist the invoice-level authority, as the real caller is responsible for doing.
	inv.TotalPrepaidCreditsApplied = result1.TotalPrepaidCreditsApplied
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	// 4. Credit the SAME wallet with another 100 USD. The first call already really debited the
	// wallet's 60 (balance 60 -> 0), so the wallet's balance right after this credit is 100 (total
	// ever credited across both credits is 160, but 60 of that was already spent by the first call).
	creditOp := &wallet.WalletOperation{
		WalletID:          w.ID,
		Type:              types.TransactionTypeCredit,
		CreditAmount:      decimal.NewFromInt(100),
		Description:       "Additional credit for test wallet",
		TransactionReason: types.TransactionReasonFreeCredit,
	}
	s.NoError(s.walletService.CreditWallet(s.GetContext(), creditOp))

	walletBeforeSecondCall, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(100).Equal(walletBeforeSecondCall.Balance),
		"wallet balance before second call: expected 100 (0 after first debit + 100 new credit), got %s", walletBeforeSecondCall.Balance.String())

	// 5. Reload the invoice WITH ITS LINE ITEMS from the repo (line item carries the 60 already
	// applied from step 3; invoice-level authority carries the persisted 60 from step 3's update).
	reloadedInv := s.reloadInvoiceWithLineItems(inv.ID)
	reloadedInv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied

	// 6. Second call.
	result2, err := s.service.ApplyCreditsToInvoice(s.GetContext(), reloadedInv)
	s.Require().NoError(err)

	// 7. Cumulative total is capped at the line's ceiling (100), not naively summed (60+160=220,
	// nor is it just the wallet's new credit of 100).
	s.True(decimal.NewFromInt(100).Equal(result2.TotalPrepaidCreditsApplied),
		"second call: expected cumulative 100 applied, got %s", result2.TotalPrepaidCreditsApplied.String())

	// 8. Wallet was debited only the NEW delta (40): balance goes from 100 -> 60, not drained to 0.
	reloadedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(reloadedWallet.Balance),
		"wallet balance: expected 60 (100 - 40 delta debited), got %s", reloadedWallet.Balance.String())
}

// TestApplyCreditsToInvoice_PersistsSpreadEvenWithoutNewWalletDebit is a regression test for a bug
// where ApplyCreditsToInvoice's early-return branches ("no wallets available" / "no amounts to
// debit") returned right after spreadPrepaidCreditsAcrossLineItems mutated inv.LineItems in memory,
// WITHOUT persisting those (possibly now-smaller, re-derived) values via InvoiceLineItemRepo.Update.
// This left invoice_line_items.PrepaidCreditsApplied stale in the DB even though the returned
// TotalPrepaidCreditsApplied was correct, desyncing invoice PDF display and
// GetUnpaidInvoicesToBePaid's unpaidUsageCharges (which feeds WalletBalanceResponse.RealTimeBalance).
func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_PersistsSpreadEvenWithoutNewWalletDebit() {
	// 1. Draft invoice with one usage line item, amount 100 USD.
	inv := s.createDraftInvoiceWithUsageLineItem("inv_persist_spread", "USD", decimal.NewFromInt(100))
	lineItemID := inv.LineItems[0].ID

	// 2. Credit a prepaid wallet with 60 USD.
	w := s.createWalletWithCredit("wallet_persist_spread", "USD", decimal.NewFromInt(60))

	// 3. First call: applies the full 60 available and drains the wallet to 0.
	result1, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(result1.TotalPrepaidCreditsApplied),
		"first call: expected 60 applied, got %s", result1.TotalPrepaidCreditsApplied.String())

	walletAfterFirstCall, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.Require().NoError(err)
	s.True(walletAfterFirstCall.Balance.IsZero(),
		"wallet should be fully drained after first call, got %s", walletAfterFirstCall.Balance.String())

	// Persist the invoice-level authority, as the real caller is responsible for doing.
	inv.TotalPrepaidCreditsApplied = result1.TotalPrepaidCreditsApplied
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	// 4. Simulate a recompute (mirroring reconcileLineItems in production): the line's ceiling
	// shrinks below the previously-applied amount (Amount drops from 100 to 40, below the 60 already
	// applied), and the line item's in-memory PrepaidCreditsApplied is reset to 0. The invoice-level
	// TotalPrepaidCreditsApplied is the preserved authority and recompute never touches it, so it
	// stays at the old (now too-high) value of 60.
	reloadedInv := s.reloadInvoiceWithLineItems(inv.ID)
	reloadedInv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied // still 60, preserved authority
	reloadedInv.LineItems[0].Amount = decimal.NewFromInt(40)
	reloadedInv.LineItems[0].PrepaidCreditsApplied = decimal.Zero

	// 5. Second call. The wallet is fully drained (balance 0), so GetWalletsForCreditAdjustment
	// returns no wallets and there is no NEW credit to debit - this is exactly the path that used to
	// return early without persisting.
	result2, err := s.service.ApplyCreditsToInvoice(s.GetContext(), reloadedInv)
	s.Require().NoError(err)

	// The re-derived, correctly-capped value is min(authority=60, new ceiling=40) = 40.
	s.True(decimal.NewFromInt(40).Equal(result2.TotalPrepaidCreditsApplied),
		"second call: expected re-derived 40 applied, got %s", result2.TotalPrepaidCreditsApplied.String())

	// 6. Reload the line item FRESH from the repo (not the in-memory struct) to prove the DB was
	// actually written. It must reflect the correctly-capped, spread-derived value (40) - not the
	// stale 60 from before this call, and not an unpersisted 0.
	persistedLineItem, err := s.GetStores().InvoiceLineItemRepo.Get(s.GetContext(), lineItemID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(persistedLineItem.PrepaidCreditsApplied),
		"persisted line item PrepaidCreditsApplied: expected 40, got %s", persistedLineItem.PrepaidCreditsApplied.String())
}

// applyExpiringCreditToInvoice must draw ONLY from the named source transaction, never from another,
// unrelated (non-expiring) credit grant sitting in the same wallet - regression test for the
// FindEligibleCredits expiry-window exclusion bug (a generic debit could never reach an already-past-
// expiry transaction, and could drain an unrelated higher-priority credit instead).
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_TargetsOnlySourceGrant() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_targets_only", "USD", decimal.NewFromInt(100)) // ceiling 100

	w := s.createWalletWithCredit("wallet_targets_only", "USD", decimal.Zero)

	// A large, non-expiring, high-priority credit that would normally win generic FIFO selection...
	s.createWalletCredit(w.ID, decimal.NewFromInt(500), nil, lo.ToPtr(1))

	// ...and the actual expiring grant we're targeting (ExpiryDate in the past).
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(40), &pastExpiry, lo.ToPtr(2))

	result, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(result.TotalPrepaidCreditsApplied),
		"expected 40 applied, got %s", result.TotalPrepaidCreditsApplied.String())

	// The 40 source grant must be fully drawn down...
	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(reloadedSource.CreditsAvailable.IsZero(),
		"source grant should be fully consumed, got %s available", reloadedSource.CreditsAvailable.String())

	// ...and the 500 non-expiring credit must be untouched.
	s.True(decimal.NewFromInt(500).Equal(s.walletBalance(inv.CustomerID, "USD")),
		"non-expiring credit must be untouched, wallet balance = %s", s.walletBalance(inv.CustomerID, "USD").String())
}

// A source grant larger than the invoice's settleable ceiling is capped at the ceiling - the excess
// stays available on the source grant (for the next invoice, or the eventual expiry debit).
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_CapsAtCeiling() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_caps_at_ceiling", "USD", decimal.NewFromInt(30)) // ceiling 30

	w := s.createWalletWithCredit("wallet_caps_at_ceiling", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(100), &pastExpiry, nil)

	result, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(result.TotalPrepaidCreditsApplied),
		"expected 30 applied (capped at ceiling), got %s", result.TotalPrepaidCreditsApplied.String())

	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(70).Equal(reloadedSource.CreditsAvailable),
		"70 should remain on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

// Even when no NEW debit is needed (source.CreditsAvailable is fully spent, or the ceiling leaves no
// room), spreadPrepaidCreditsAcrossLineItems's re-derived per-line values must still be persisted to the
// DB - this is the exact bug class Task 4 fixed in ApplyCreditsToInvoice; this function must not repeat
// it.
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_PersistsSpreadEvenWithoutNewDebit() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_persist_no_debit", "USD", decimal.NewFromInt(100)) // ceiling 100
	lineItemID := inv.LineItems[0].ID

	w := s.createWalletWithCredit("wallet_persist_no_debit", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(60), &pastExpiry, nil)

	first, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(first.TotalPrepaidCreditsApplied),
		"first call: expected 60 applied, got %s", first.TotalPrepaidCreditsApplied.String())

	// Simulate a recompute that shrinks the line's ceiling to 40 (e.g. a discount added) and resets the
	// in-memory line item's PrepaidCreditsApplied to 0 (mirroring what reconcileLineItems does in
	// production), while inv.TotalPrepaidCreditsApplied (the preserved authority) stays at 60.
	inv.LineItems[0].LineItemDiscount = decimal.NewFromInt(60) // ceiling now 100-60=40
	inv.LineItems[0].PrepaidCreditsApplied = decimal.Zero      // simulated reset

	// Re-fetch the source transaction: it now has 0 CreditsAvailable (fully spent by the first call), so
	// this second call needs no new debit - exactly the path that used to skip persistence.
	exhaustedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(exhaustedSource.CreditsAvailable.IsZero(),
		"source should be exhausted before second call, got %s", exhaustedSource.CreditsAvailable.String())

	second, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, exhaustedSource)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(second.TotalPrepaidCreditsApplied),
		"second call: expected 40 applied (capped at the shrunk ceiling), got %s", second.TotalPrepaidCreditsApplied.String())

	// Reload the line item FRESH from the repo (not the in-memory struct) to prove the DB was written.
	reloadedLineItem, err := s.GetStores().InvoiceLineItemRepo.Get(s.GetContext(), lineItemID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(40).Equal(reloadedLineItem.PrepaidCreditsApplied),
		"persisted value = %s, want 40 (spread must persist even with no new debit)", reloadedLineItem.PrepaidCreditsApplied.String())
}

func TestPrepaidCreditApplyLockKey(t *testing.T) {
	got := prepaidCreditApplyLockKey("inv_123")
	want := "prepaid_credit_apply:invoice:inv_123"
	if got != want {
		t.Fatalf("prepaidCreditApplyLockKey = %q, want %q", got, want)
	}
}

func TestSpreadPrepaidCreditsAcrossLineItems(t *testing.T) {
	t.Run("spreads across usage lines capped at ceiling, skips fixed", func(t *testing.T) {
		a := usageLine(100, 20, 0) // ceiling 80
		b := fixedLine(50)         // not creditable
		inv := &invoice.Invoice{
			TotalPrepaidCreditsApplied: decimal.NewFromInt(80),
			LineItems:                  []*invoice.InvoiceLineItem{a, b},
		}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.Equal(decimal.NewFromInt(80)) {
			t.Fatalf("usage line applied = %s, want 80", a.PrepaidCreditsApplied)
		}
		if !b.PrepaidCreditsApplied.IsZero() {
			t.Fatalf("fixed line applied = %s, want 0", b.PrepaidCreditsApplied)
		}
	})

	t.Run("over-application caps at ceiling, excess dropped from per-line sum", func(t *testing.T) {
		a := usageLine(100, 40, 0) // ceiling 60
		inv := &invoice.Invoice{
			TotalPrepaidCreditsApplied: decimal.NewFromInt(80), // authority higher than ceiling
			LineItems:                  []*invoice.InvoiceLineItem{a},
		}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.Equal(decimal.NewFromInt(60)) {
			t.Fatalf("applied = %s, want 60 (capped)", a.PrepaidCreditsApplied)
		}
	})

	t.Run("multiple usage lines consumed in order", func(t *testing.T) {
		a := usageLine(30, 0, 0) // ceiling 30
		b := usageLine(50, 0, 0) // ceiling 50
		inv := &invoice.Invoice{
			TotalPrepaidCreditsApplied: decimal.NewFromInt(60),
			LineItems:                  []*invoice.InvoiceLineItem{a, b},
		}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.Equal(decimal.NewFromInt(30)) {
			t.Fatalf("line a = %s, want 30", a.PrepaidCreditsApplied)
		}
		if !b.PrepaidCreditsApplied.Equal(decimal.NewFromInt(30)) {
			t.Fatalf("line b = %s, want 30", b.PrepaidCreditsApplied)
		}
	})

	t.Run("zero authority zeroes all lines", func(t *testing.T) {
		a := usageLine(100, 0, 0)
		a.PrepaidCreditsApplied = decimal.NewFromInt(40) // stale
		inv := &invoice.Invoice{TotalPrepaidCreditsApplied: decimal.Zero, LineItems: []*invoice.InvoiceLineItem{a}}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.IsZero() {
			t.Fatalf("applied = %s, want 0", a.PrepaidCreditsApplied)
		}
	})

	t.Run("total higher than sum of all ceilings: excess left unplaced, no panic", func(t *testing.T) {
		a := usageLine(30, 0, 0) // ceiling 30
		b := usageLine(20, 0, 0) // ceiling 20
		inv := &invoice.Invoice{
			TotalPrepaidCreditsApplied: decimal.NewFromInt(100), // way more than 30+20
			LineItems:                  []*invoice.InvoiceLineItem{a, b},
		}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.Equal(decimal.NewFromInt(30)) {
			t.Fatalf("line a = %s, want 30", a.PrepaidCreditsApplied)
		}
		if !b.PrepaidCreditsApplied.Equal(decimal.NewFromInt(20)) {
			t.Fatalf("line b = %s, want 20", b.PrepaidCreditsApplied)
		}
	})

	t.Run("negative authority zeroes all usage lines (defensive clamp)", func(t *testing.T) {
		a := usageLine(100, 0, 0)
		inv := &invoice.Invoice{
			TotalPrepaidCreditsApplied: decimal.NewFromInt(-50), // corrupt/invalid input
			LineItems:                  []*invoice.InvoiceLineItem{a},
		}
		spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.IsZero() {
			t.Fatalf("applied = %s, want 0 (negative authority must not go negative or apply)", a.PrepaidCreditsApplied)
		}
	})
}

func TestCalculateCreditAdjustments_Additive(t *testing.T) {
	svc := &creditAdjustmentService{}
	newWallet := func(id string, bal float64) *wallet.Wallet {
		return &wallet.Wallet{ID: id, Balance: decimal.NewFromFloat(bal), WalletType: types.WalletTypePrePaid}
	}

	t.Run("adds on top of already-applied, capped at remaining ceiling", func(t *testing.T) {
		line := usageLine(100, 0, 0)                        // ceiling 100
		line.PrepaidCreditsApplied = decimal.NewFromInt(30) // already applied
		inv := &invoice.Invoice{Currency: "USD", LineItems: []*invoice.InvoiceLineItem{line}}
		debits, err := svc.CalculateCreditAdjustments(inv, []*wallet.Wallet{newWallet("w1", 1000)})
		if err != nil {
			t.Fatal(err)
		}
		// remaining ceiling 70 -> applies 70 more -> line total 100
		if !line.PrepaidCreditsApplied.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("line applied = %s, want 100", line.PrepaidCreditsApplied)
		}
		if !debits["w1"].Equal(decimal.NewFromInt(70)) {
			t.Fatalf("debit = %s, want 70 (delta only)", debits["w1"])
		}
	})

	t.Run("mixed fixed+usage never credits the fixed line", func(t *testing.T) {
		u := usageLine(100, 20, 0) // ceiling 80
		f := fixedLine(50)
		f.PrepaidCreditsApplied = decimal.NewFromInt(15) // stale value; must NOT be reset by this function
		inv := &invoice.Invoice{Currency: "USD", LineItems: []*invoice.InvoiceLineItem{u, f}}
		_, err := svc.CalculateCreditAdjustments(inv, []*wallet.Wallet{newWallet("w1", 1000)})
		if err != nil {
			t.Fatal(err)
		}
		if !u.PrepaidCreditsApplied.Equal(decimal.NewFromInt(80)) {
			t.Fatalf("usage applied = %s, want 80", u.PrepaidCreditsApplied)
		}
		if !f.PrepaidCreditsApplied.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("fixed applied = %s, want unchanged 15 (not reset)", f.PrepaidCreditsApplied)
		}
	})

	t.Run("fully-applied line gets no new debit", func(t *testing.T) {
		line := usageLine(100, 0, 0)
		line.PrepaidCreditsApplied = decimal.NewFromInt(100) // maxed
		inv := &invoice.Invoice{Currency: "USD", LineItems: []*invoice.InvoiceLineItem{line}}
		debits, err := svc.CalculateCreditAdjustments(inv, []*wallet.Wallet{newWallet("w1", 1000)})
		if err != nil {
			t.Fatal(err)
		}
		if len(debits) != 0 {
			t.Fatalf("debits = %v, want empty", debits)
		}
		if !line.PrepaidCreditsApplied.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("line applied = %s, want unchanged 100", line.PrepaidCreditsApplied)
		}
	})
}
