package service

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
		EntitlementRepo:          stores.EntitlementRepo,
		PlanRepo:                 stores.PlanRepo,
		AddonRepo:                stores.AddonRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		CreditGrantRepo:          stores.CreditGrantRepo,
		CouponAssociationRepo:    stores.CouponAssociationRepo,
		TaxRateRepo:              stores.TaxRateRepo,
		TaxAssociationRepo:       stores.TaxAssociationRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		Locker:                   s.GetLocker(),
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

// createActiveStandaloneSubscription creates and persists a minimal active standalone subscription for
// the suite's test customer. ConsumeExpiringCreditIntoInvoices filters draft invoices by active
// standalone/parent subscriptions, so its tests need a real, persisted subscription (no plan/prices
// needed - we bypass usage-pricing machinery for these tests, per the fallback described in the plan).
func (s *CreditAdjustmentServiceSuite) createActiveStandaloneSubscription(id, currency string) *subscription.Subscription {
	now := s.GetNow()
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         s.testData.customer.ID,
		Currency:           currency,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// createDraftSubInvoiceWithUsageLineItem creates a DB-backed draft invoice tied to a real subscription,
// with a single usage line item of the given amount. Uses InvoiceType OneOff (not Subscription) with
// req=nil on the ComputeInvoice call this exercises: for a OneOff invoice, ComputeInvoice's nil-request
// path does not touch line items or recompute totals from usage (that recompute path is
// Subscription-type-only and requires real ClickHouse-backed usage data), so the manually-seeded line
// item and Subtotal survive the orchestrator's recompute call untouched - letting this test exercise the
// ORCHESTRATION (finding the subscription/invoice, locking, calling ComputeInvoice, applying credit)
// without needing a full usage-pricing fixture.
func (s *CreditAdjustmentServiceSuite) createDraftSubInvoiceWithUsageLineItem(id, currency string, amount decimal.Decimal, subID string, periodStart time.Time) *invoice.Invoice {
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

	periodEnd := periodStart.Add(30 * 24 * time.Hour)
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(subID),
		Currency:        currency,
		Subtotal:        amount,
		Total:           amount,
		AmountDue:       amount,
		AmountRemaining: amount,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PeriodStart:     lo.ToPtr(periodStart),
		PeriodEnd:       lo.ToPtr(periodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{li},
	}

	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
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

// Gap 1 (non-zero starting authority): all the TestApplyExpiringCreditToInvoice_* tests above start
// from inv.TotalPrepaidCreditsApplied == 0. Here the invoice already carries prior authority from a
// real ApplyCreditsToInvoice call (a pooled/general credit, unrelated to the targeted expiring grant),
// and the targeted expiring credit must add ON TOP of that, capped at the remaining room - not
// overwrite it, and not ignore it when computing the ceiling.
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_AddsOnTopOfNonZeroAuthority() {
	// 1. Draft invoice with one usage line item, ceiling 100 USD.
	inv := s.createDraftInvoiceWithUsageLineItem("inv_nonzero_authority", "USD", decimal.NewFromInt(100))

	// 2. A general prepaid wallet credited with 50 USD, applied via the normal pooled path.
	s.createWalletWithCredit("wallet_general_pool", "USD", decimal.NewFromInt(50))
	result1, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(50).Equal(result1.TotalPrepaidCreditsApplied),
		"pooled call: expected 50 applied, got %s", result1.TotalPrepaidCreditsApplied.String())

	// Persist the invoice-level authority, as the real caller is responsible for doing.
	inv.TotalPrepaidCreditsApplied = result1.TotalPrepaidCreditsApplied
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))

	// 3. Reload the invoice WITH its persisted line items (the 50 already applied is on the line
	// item too, since ApplyCreditsToInvoice persists line items unconditionally).
	reloadedInv := s.reloadInvoiceWithLineItems(inv.ID)
	reloadedInv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied

	// 4. A targeted, about-to-expire credit of 80 sitting on a DIFFERENT wallet.
	targetWallet := s.createWalletWithCredit("wallet_targeted_expiry", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(targetWallet.ID, decimal.NewFromInt(80), &pastExpiry, nil)

	// 5. Applying the expiring credit must only add the remaining room (100 - 50 = 50), not the full
	// 80 available, and must not clobber the 50 already applied.
	result2, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), reloadedInv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(100).Equal(result2.TotalPrepaidCreditsApplied),
		"expected cumulative 100 applied (50 prior + 50 new, capped at ceiling), got %s", result2.TotalPrepaidCreditsApplied.String())

	// 6. Only 50 of the 80 available was drawn from the source grant; 30 remains for the next invoice
	// (or the eventual expiry sweep).
	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(reloadedSource.CreditsAvailable),
		"expected 30 remaining on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

// Gap 2 (retry/idempotency-collision safety): calling applyExpiringCreditToInvoice twice with the
// SAME (inv, source) pair - as a Temporal activity retry might, before anything about the invoice or
// source is reloaded/mutated externally - must not double-debit the wallet.
//
// NOTE on which angle this covers: the function's own defense here is the additive ceiling check
// (remainingCeiling := totalCeiling.Sub(inv.TotalPrepaidCreditsApplied)) - the first call mutates
// inv.TotalPrepaidCreditsApplied in place, so the second call naturally computes remainingCeiling as
// already-exhausted and amountToApply=0, and DebitWallet is never even invoked a second time. This is
// the angle exercised below.
//
// The OTHER layer of protection - the deterministic per-(invoice,source) idempotency key colliding on
// wallet_transactions' unique (tenant_id, environment_id, idempotency_key) index - is enforced only by
// the real Postgres index. This suite runs against testutil's InMemoryWalletStore, whose CreateTransaction
// does not replicate that uniqueness constraint (InMemoryWalletStore.GetTransactionByIdempotencyKey exists
// as a lookup but nothing calls it to reject a duplicate insert), so a true collision on that index cannot
// be reproduced here without a Postgres-backed test. The additive-ceiling angle below is what's actually
// exercisable in this suite, and it is also the first line of defense in production (the idempotency key
// is the backstop for the case where the ceiling math doesn't already prevent it, e.g. a concurrent/
// interleaved retry racing on a stale in-memory inv copy).
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_RepeatedCallDoesNotDoubleDebit() {
	// Ceiling equals the source grant exactly, so the first call fully exhausts both.
	inv := s.createDraftInvoiceWithUsageLineItem("inv_retry_safety", "USD", decimal.NewFromInt(60))

	w := s.createWalletWithCredit("wallet_retry_safety", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(60), &pastExpiry, nil)

	// First call: applies the full 60, draining both the ceiling and the source grant.
	first, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(first.TotalPrepaidCreditsApplied),
		"first call: expected 60 applied, got %s", first.TotalPrepaidCreditsApplied.String())

	walletAfterFirstCall, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.Require().NoError(err)
	s.True(walletAfterFirstCall.Balance.IsZero(),
		"wallet should be fully drained after first call, got %s", walletAfterFirstCall.Balance.String())

	// Second call: SAME inv pointer (already mutated in place to TotalPrepaidCreditsApplied=60) and
	// SAME source struct (its in-memory CreditsAvailable field is untouched by the function itself -
	// nothing is reloaded from the DB between calls). A naive re-run that recomputed
	// remainingCeiling from scratch without honoring the prior authority would apply another 60.
	second, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(second.TotalPrepaidCreditsApplied),
		"second call: expected total to remain 60 (not 120), got %s", second.TotalPrepaidCreditsApplied.String())

	// The wallet must NOT have been debited a second time.
	walletAfterSecondCall, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.Require().NoError(err)
	s.True(walletAfterSecondCall.Balance.IsZero(),
		"wallet should still be zero, not over-debited, got %s", walletAfterSecondCall.Balance.String())

	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(reloadedSource.CreditsAvailable.IsZero(),
		"source grant should still show zero available, not negative, got %s", reloadedSource.CreditsAvailable.String())
}

// Gap 3 (currency-rounding edge case): a fractional CreditsAvailable that matters at 2-decimal USD
// precision must round sensibly and never result in an applied amount greater than what's actually
// available - mirroring the rounding-safety pattern covered for CalculateCreditAdjustments by
// TestCalculateCreditAdjustments_DustBalanceNoHang.
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_RoundsFractionalSourceWithoutExceedingAvailable() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_fractional_source", "USD", decimal.NewFromInt(1000)) // ceiling far exceeds the source

	w := s.createWalletWithCredit("wallet_fractional_source", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.RequireFromString("33.334"), &pastExpiry, nil)

	result, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)

	// 33.334 rounds down to 33.33 at USD's 2-decimal precision.
	s.True(decimal.RequireFromString("33.33").Equal(result.TotalPrepaidCreditsApplied),
		"expected 33.33 applied (rounded), got %s", result.TotalPrepaidCreditsApplied.String())

	// Never exceeds what was actually available on the source grant.
	s.True(result.TotalPrepaidCreditsApplied.LessThanOrEqual(decimal.RequireFromString("33.334")),
		"applied (%s) must not exceed available (33.334)", result.TotalPrepaidCreditsApplied.String())

	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.RequireFromString("0.004").Equal(reloadedSource.CreditsAvailable),
		"expected 0.004 dust remaining on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

// Companion to the above: this time the INVOICE ceiling (not the source grant) is the fractional,
// binding constraint, and rounding it UP (12.347 -> 12.35) would exceed the ceiling. The
// decimal.Min(rounded, raw) guard in applyExpiringCreditToInvoice must fall back to the raw,
// unrounded amount rather than over-apply past what the line item can actually hold.
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_RoundsFractionalCeilingWithoutExceeding() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_fractional_ceiling", "USD", decimal.RequireFromString("12.347")) // ceiling 12.347

	w := s.createWalletWithCredit("wallet_fractional_ceiling", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(1000), &pastExpiry, nil) // plenty available

	result, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)

	// Rounding 12.347 up to 12.35 would exceed the 12.347 ceiling, so the raw (unrounded) amount is
	// used instead - it never exceeds the ceiling.
	s.True(decimal.RequireFromString("12.347").Equal(result.TotalPrepaidCreditsApplied),
		"expected 12.347 applied (raw, ceiling-bound), got %s", result.TotalPrepaidCreditsApplied.String())
	s.True(result.TotalPrepaidCreditsApplied.LessThanOrEqual(decimal.RequireFromString("12.347")),
		"applied (%s) must not exceed the ceiling (12.347)", result.TotalPrepaidCreditsApplied.String())

	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.RequireFromString("987.653").Equal(reloadedSource.CreditsAvailable),
		"expected 987.653 remaining on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

// Nice-to-have: a 0-decimal currency (JPY) rounds down sensibly too, using the same helpers with no
// extra plumbing.
func (s *CreditAdjustmentServiceSuite) TestApplyExpiringCreditToInvoice_ZeroDecimalCurrencyRounding() {
	inv := s.createDraftInvoiceWithUsageLineItem("inv_jpy_rounding", "JPY", decimal.NewFromInt(1000)) // ceiling far exceeds the source

	w := s.createWalletWithCredit("wallet_jpy_rounding", "JPY", decimal.Zero)
	pastExpiry := s.GetNow().Add(-24 * time.Hour)
	source := s.createWalletCredit(w.ID, decimal.RequireFromString("333.4"), &pastExpiry, nil)

	result, err := s.getServiceImpl().applyExpiringCreditToInvoice(s.GetContext(), inv, source)
	s.Require().NoError(err)

	// 333.4 rounds down to 333 at JPY's 0-decimal precision.
	s.True(decimal.NewFromInt(333).Equal(result.TotalPrepaidCreditsApplied),
		"expected 333 applied (rounded), got %s", result.TotalPrepaidCreditsApplied.String())
	s.True(result.TotalPrepaidCreditsApplied.LessThanOrEqual(decimal.RequireFromString("333.4")),
		"applied (%s) must not exceed available (333.4)", result.TotalPrepaidCreditsApplied.String())

	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.RequireFromString("0.4").Equal(reloadedSource.CreditsAvailable),
		"expected 0.4 dust remaining on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

func TestPrepaidCreditApplyLockKey(t *testing.T) {
	got := cache.GenerateKey(nil, cache.PrefixPrepaidCreditApplyLock, "inv_123")
	want := "prepaid_credit_apply:invoice::inv_123"
	if got != want {
		t.Fatalf("GenerateKey prepaid credit apply lock = %q, want %q", got, want)
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
		got := spreadPrepaidCreditsAcrossLineItems(inv)
		if !a.PrepaidCreditsApplied.Equal(decimal.NewFromInt(60)) {
			t.Fatalf("applied = %s, want 60 (capped)", a.PrepaidCreditsApplied)
		}
		if !got.Equal(decimal.NewFromInt(60)) {
			t.Fatalf("total applied = %s, want 60", got)
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

// TestConsumeExpiringCreditIntoInvoices_EndToEnd proves the full orchestration: given an about-to-expire
// wallet transaction, the service finds the customer's active subscription, finds its draft invoice,
// takes the per-invoice lock, recomputes the invoice via ComputeInvoice, and applies the credit -
// persisting both the line item and the invoice's Total/AmountDue/AmountRemaining.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_EndToEnd() {
	sub := s.createActiveStandaloneSubscription("sub_e2e", "USD")
	inv := s.createDraftSubInvoiceWithUsageLineItem("inv_e2e", "USD", decimal.NewFromInt(100), sub.ID, s.GetNow().Add(-24*time.Hour))

	w := s.createWalletWithCredit("wallet_e2e", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(60), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(consumed),
		"expected 60 consumed, got %s", consumed.String())

	// The source grant must be drawn down by exactly what was consumed.
	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(reloadedSource.CreditsAvailable.IsZero(),
		"source grant should be fully consumed, got %s available", reloadedSource.CreditsAvailable.String())

	// The invoice's Total/AmountDue/AmountRemaining must reflect the applied credit, and the line item's
	// PrepaidCreditsApplied must be persisted (not just held in memory).
	reloadedInv := s.reloadInvoiceWithLineItems(inv.ID)
	s.True(decimal.NewFromInt(60).Equal(reloadedInv.TotalPrepaidCreditsApplied),
		"invoice TotalPrepaidCreditsApplied = %s, want 60", reloadedInv.TotalPrepaidCreditsApplied.String())
	s.True(decimal.NewFromInt(40).Equal(reloadedInv.Total),
		"invoice Total = %s, want 40 (100 - 60)", reloadedInv.Total.String())
	s.True(decimal.NewFromInt(40).Equal(reloadedInv.AmountDue),
		"invoice AmountDue = %s, want 40", reloadedInv.AmountDue.String())
	s.True(decimal.NewFromInt(40).Equal(reloadedInv.AmountRemaining),
		"invoice AmountRemaining = %s, want 40", reloadedInv.AmountRemaining.String())
	s.Require().Len(reloadedInv.LineItems, 1)
	s.True(decimal.NewFromInt(60).Equal(reloadedInv.LineItems[0].PrepaidCreditsApplied),
		"line item PrepaidCreditsApplied = %s, want 60", reloadedInv.LineItems[0].PrepaidCreditsApplied.String())
}

// TestConsumeExpiringCreditIntoInvoices_BestEffortSkipsFailingInvoice proves per-invoice failures are
// isolated: one invoice whose recompute errors (a Subscription-type invoice missing PeriodStart/PeriodEnd,
// which ComputeInvoice rejects) must not block the credit from being applied to the other, healthy draft
// invoice on the same subscription, and the orchestrator itself must return no error (best-effort).
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_BestEffortSkipsFailingInvoice() {
	sub := s.createActiveStandaloneSubscription("sub_best_effort", "USD")

	// Healthy invoice: OneOff, ceiling 50.
	healthyInv := s.createDraftSubInvoiceWithUsageLineItem("inv_healthy", "USD", decimal.NewFromInt(50), sub.ID, s.GetNow())

	// Broken invoice: InvoiceType Subscription with SubscriptionID set but PeriodStart/PeriodEnd nil -
	// ComputeInvoice rejects this combination with a validation error before ever touching line items.
	pt := string(types.PRICE_TYPE_USAGE)
	brokenLine := &invoice.InvoiceLineItem{
		ID:         s.GetUUID(),
		InvoiceID:  "inv_broken",
		CustomerID: s.testData.customer.ID,
		Amount:     decimal.NewFromInt(999),
		Currency:   "USD",
		Quantity:   decimal.NewFromInt(1),
		PriceType:  &pt,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	brokenInv := &invoice.Invoice{
		ID:              "inv_broken",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		Currency:        "USD",
		Subtotal:        decimal.NewFromInt(999),
		Total:           decimal.NewFromInt(999),
		AmountDue:       decimal.NewFromInt(999),
		AmountRemaining: decimal.NewFromInt(999),
		InvoiceType:     types.InvoiceTypeSubscription, // PeriodStart/PeriodEnd required, but left nil below
		InvoiceStatus:   types.InvoiceStatusDraft,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{brokenLine},
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), brokenInv))

	w := s.createWalletWithCredit("wallet_best_effort", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(200), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err, "orchestrator must be best-effort and not fail the whole call")
	s.True(decimal.NewFromInt(50).Equal(consumed),
		"expected only the healthy invoice's ceiling (50) consumed, got %s", consumed.String())

	// The healthy invoice got its credit.
	reloadedHealthy := s.reloadInvoiceWithLineItems(healthyInv.ID)
	s.True(decimal.NewFromInt(50).Equal(reloadedHealthy.TotalPrepaidCreditsApplied),
		"healthy invoice TotalPrepaidCreditsApplied = %s, want 50", reloadedHealthy.TotalPrepaidCreditsApplied.String())

	// The broken invoice was left untouched - no credit applied, no line item mutated.
	reloadedBroken := s.reloadInvoiceWithLineItems(brokenInv.ID)
	s.True(reloadedBroken.TotalPrepaidCreditsApplied.IsZero(),
		"broken invoice TotalPrepaidCreditsApplied should remain 0, got %s", reloadedBroken.TotalPrepaidCreditsApplied.String())
	s.Require().Len(reloadedBroken.LineItems, 1)
	s.True(reloadedBroken.LineItems[0].PrepaidCreditsApplied.IsZero(),
		"broken invoice line item PrepaidCreditsApplied should remain 0, got %s", reloadedBroken.LineItems[0].PrepaidCreditsApplied.String())

	// Only 50 of the 200 available was drawn from the source grant; 150 remains.
	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(150).Equal(reloadedSource.CreditsAvailable),
		"expected 150 remaining on the source grant, got %s", reloadedSource.CreditsAvailable.String())
}

// TestConsumeExpiringCreditIntoInvoices_StopsEarlyAcrossMultipleInvoices proves the remaining-balance
// tracking in ConsumeExpiringCreditIntoInvoices actually stops consuming once the credit is exhausted,
// across MULTIPLE invoices on the same subscription - not just within a single invoice's own ceiling
// capping. Two draft invoices each with a 40-ceiling, and an expiring credit of 60: whichever invoice is
// processed first consumes its full 40-ceiling, leaving only 20 for the other - regardless of which
// specific invoice is processed first (no ordering guarantee), the orchestrator's total `consumed` must
// be exactly 60, and the two invoices' applied amounts must be exactly {40, 20} as a set.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_StopsEarlyAcrossMultipleInvoices() {
	sub := s.createActiveStandaloneSubscription("sub_stop_early", "USD")

	invA := s.createDraftSubInvoiceWithUsageLineItem(
		"inv_stop_early_a", "USD", decimal.NewFromInt(40), sub.ID, s.GetNow())
	invB := s.createDraftSubInvoiceWithUsageLineItem(
		"inv_stop_early_b", "USD", decimal.NewFromInt(40), sub.ID, s.GetNow().Add(-48*time.Hour))

	w := s.createWalletWithCredit("wallet_stop_early", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(60), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(60).Equal(consumed),
		"expected total 60 consumed across both invoices, got %s", consumed.String())

	// Whichever invoice was processed first got fully capped at 40; the other got only the remaining
	// 20 (not another 40) - order is not guaranteed, so assert on the set of applied amounts.
	reloadedA := s.reloadInvoiceWithLineItems(invA.ID)
	reloadedB := s.reloadInvoiceWithLineItems(invB.ID)
	applied := []decimal.Decimal{reloadedA.TotalPrepaidCreditsApplied, reloadedB.TotalPrepaidCreditsApplied}
	sort.Slice(applied, func(i, j int) bool { return applied[i].LessThan(applied[j]) })
	s.True(decimal.NewFromInt(20).Equal(applied[0]),
		"smaller applied amount = %s, want 20 (remaining only)", applied[0].String())
	s.True(decimal.NewFromInt(40).Equal(applied[1]),
		"larger applied amount = %s, want 40 (fully capped)", applied[1].String())

	// The source grant is fully drawn down: 40 + 20 = 60.
	reloadedSource, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.True(reloadedSource.CreditsAvailable.IsZero(),
		"source grant should be fully consumed, got %s available", reloadedSource.CreditsAvailable.String())
}

// TestConsumeExpiringCreditIntoInvoices_Phase1SufficientSkipsPhase2 proves Phase 2 never runs when
// Phase 1 alone fully consumes the credit: a second active subscription with NO draft invoice at all
// must be left completely untouched (no invoice created for it).
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase1SufficientSkipsPhase2() {
	subWithInvoice := s.createActiveStandaloneSubscription("sub_phase1_sufficient", "USD")
	s.createDraftSubInvoiceWithUsageLineItem("inv_phase1_sufficient", "USD", decimal.NewFromInt(50), subWithInvoice.ID, s.GetNow())

	// Second subscription has no draft invoice at all - if Phase 2 ran, it would try to create one.
	subWithoutInvoice := s.createActiveStandaloneSubscription("sub_phase1_sufficient_untouched", "USD")

	w := s.createWalletWithCredit("wallet_phase1_sufficient", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(50).Equal(consumed), "expected 50 consumed, got %s", consumed.String())

	untouchedFilter := types.NewNoLimitInvoiceFilter()
	untouchedFilter.SubscriptionID = subWithoutInvoice.ID
	untouchedInvoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), untouchedFilter)
	s.Require().NoError(err)
	s.Empty(untouchedInvoices, "Phase 2 must not have created a draft invoice for the untouched subscription")
}

// TestConsumeExpiringCreditIntoInvoices_Phase2CreatesMissingDraft proves Phase 2 creates a draft
// invoice for a subscription's current period when none exists, instead of skipping it. The
// subscription has no plan/priced line items, so the freshly-created draft's ComputeInvoice call finds
// zero usage and marks it SKIPPED - this test asserts on CREATION, not on credit being applied.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase2CreatesMissingDraft() {
	sub := s.createActiveStandaloneSubscription("sub_phase2_creates", "USD")

	w := s.createWalletWithCredit("wallet_phase2_creates", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(consumed.IsZero(), "expected zero consumed (subscription has no usage), got %s", consumed.String())

	f := types.NewNoLimitInvoiceFilter()
	f.SubscriptionID = sub.ID
	// The in-memory store's default List() excludes SKIPPED invoices (mirroring the real repository
	// default), and this zero-usage subscription's freshly-created draft lands on SKIPPED once
	// ComputeInvoice runs - so both statuses must be requested explicitly to observe the creation.
	f.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusSkipped}
	created, err := s.GetStores().InvoiceRepo.List(s.GetContext(), f)
	s.Require().NoError(err)
	s.Require().Len(created, 1, "Phase 2 must have created exactly one draft invoice for the subscription's current period")
	s.Equal(sub.CurrentPeriodStart.Unix(), created[0].PeriodStart.Unix())
	s.Equal(sub.CurrentPeriodEnd.Unix(), created[0].PeriodEnd.Unix())
	s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), created[0].BillingReason) // BillingReason is a plain string field
}

// TestConsumeExpiringCreditIntoInvoices_Phase2PicksUpStaleAmountRemaining proves Phase 2 finds and
// applies credit to a current-period invoice that Phase 1's AmountRemainingGt(0) filter would have
// excluded because its STORED AmountRemaining is zero, even though its line item has real room. The
// invoice's period is set to exactly match the subscription's current period (required for Phase 2's
// period-scoped lookup to find it).
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase2PicksUpStaleAmountRemaining() {
	sub := s.createActiveStandaloneSubscription("sub_phase2_stale", "USD")

	pt := string(types.PRICE_TYPE_USAGE)
	li := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        "inv_phase2_stale",
		CustomerID:       s.testData.customer.ID,
		Amount:           decimal.NewFromInt(50),
		Currency:         "USD",
		Quantity:         decimal.NewFromInt(1),
		PriceType:        &pt,
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	inv := &invoice.Invoice{
		ID:              "inv_phase2_stale",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		Currency:        "USD",
		Subtotal:        decimal.NewFromInt(50),
		Total:           decimal.NewFromInt(50),
		AmountDue:       decimal.NewFromInt(50),
		AmountRemaining: decimal.Zero, // stale - Phase 1's AmountRemainingGt(0) filter must skip this
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PeriodStart:     lo.ToPtr(sub.CurrentPeriodStart),
		PeriodEnd:       lo.ToPtr(sub.CurrentPeriodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{li},
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))

	w := s.createWalletWithCredit("wallet_phase2_stale", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(50).Equal(consumed), "expected 50 consumed via Phase 2, got %s", consumed.String())

	reloaded := s.reloadInvoiceWithLineItems(inv.ID)
	s.True(decimal.NewFromInt(50).Equal(reloaded.TotalPrepaidCreditsApplied),
		"TotalPrepaidCreditsApplied = %s, want 50", reloaded.TotalPrepaidCreditsApplied.String())
}

// TestConsumeExpiringCreditIntoInvoices_Phase2SkipsAlreadyProcessedInvoice proves Phase 2 does not
// re-process an invoice Phase 1 already found and applied credit to. Invoice A is found and fully
// applied by Phase 1, but credit remains afterward, so Phase 2 actually runs - and its own
// SubscriptionID+Draft+period lookup for subA's current period RE-FINDS invoice A (its period is
// aligned with subA's current period), which must be skipped via the processedIDs dedup rather than
// re-applied. Invoice B (stale AmountRemaining, invisible to Phase 1) is genuinely found and applied by
// Phase 2, proving Phase 2 did run and dedup isn't just a no-op from Phase 2 never firing.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase2SkipsAlreadyProcessedInvoice() {
	pt := string(types.PRICE_TYPE_USAGE)

	subA := s.createActiveStandaloneSubscription("sub_dedup_a", "USD")
	liA := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        "inv_dedup_a",
		CustomerID:       s.testData.customer.ID,
		Amount:           decimal.NewFromInt(20),
		Currency:         "USD",
		Quantity:         decimal.NewFromInt(1),
		PriceType:        &pt,
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	invA := &invoice.Invoice{
		ID:              "inv_dedup_a",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(subA.ID),
		Currency:        "USD",
		Subtotal:        decimal.NewFromInt(20),
		Total:           decimal.NewFromInt(20),
		AmountDue:       decimal.NewFromInt(20),
		AmountRemaining: decimal.NewFromInt(20), // found by Phase 1 directly
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PeriodStart:     lo.ToPtr(subA.CurrentPeriodStart), // aligned so Phase 2's lookup re-finds it
		PeriodEnd:       lo.ToPtr(subA.CurrentPeriodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{liA},
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), invA))

	subB := s.createActiveStandaloneSubscription("sub_dedup_b", "USD")
	liB := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        "inv_dedup_b",
		CustomerID:       s.testData.customer.ID,
		Amount:           decimal.NewFromInt(30),
		Currency:         "USD",
		Quantity:         decimal.NewFromInt(1),
		PriceType:        &pt,
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	invB := &invoice.Invoice{
		ID:              "inv_dedup_b",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(subB.ID),
		Currency:        "USD",
		Subtotal:        decimal.NewFromInt(30),
		Total:           decimal.NewFromInt(30),
		AmountDue:       decimal.NewFromInt(30),
		AmountRemaining: decimal.Zero, // stale - invisible to Phase 1, forcing Phase 2 to find it
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PeriodStart:     lo.ToPtr(subB.CurrentPeriodStart),
		PeriodEnd:       lo.ToPtr(subB.CurrentPeriodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{liB},
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), invB))

	w := s.createWalletWithCredit("wallet_dedup", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(50).Equal(consumed), "expected 50 consumed across both invoices, got %s", consumed.String())

	reloadedA := s.reloadInvoiceWithLineItems(invA.ID)
	s.True(decimal.NewFromInt(20).Equal(reloadedA.TotalPrepaidCreditsApplied),
		"invoice A TotalPrepaidCreditsApplied = %s, want 20 (applied once by Phase 1, not re-applied by Phase 2)", reloadedA.TotalPrepaidCreditsApplied.String())

	reloadedB := s.reloadInvoiceWithLineItems(invB.ID)
	s.True(decimal.NewFromInt(30).Equal(reloadedB.TotalPrepaidCreditsApplied),
		"invoice B TotalPrepaidCreditsApplied = %s, want 30 (found and applied by Phase 2)", reloadedB.TotalPrepaidCreditsApplied.String())
}

// TestConsumeExpiringCreditIntoInvoices_NonEligibleSubscriptionTypesUntouched proves delegated_invoicing,
// grouped_invoicing, and inherited subscriptions are never processed by either phase - Phase 2 must not
// attempt to create a draft for them.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_NonEligibleSubscriptionTypesUntouched() {
	now := s.GetNow()
	makeSub := func(id string, subType types.SubscriptionType) *subscription.Subscription {
		sub := &subscription.Subscription{
			ID:                 id,
			CustomerID:         s.testData.customer.ID,
			Currency:           "USD",
			SubscriptionType:   subType,
			SubscriptionStatus: types.SubscriptionStatusActive,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			StartDate:          now.Add(-30 * 24 * time.Hour),
			CurrentPeriodStart: now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
		s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
		return sub
	}

	delegated := makeSub("sub_non_eligible_delegated", types.SubscriptionTypeDelegatedInvoicing)
	grouped := makeSub("sub_non_eligible_grouped", types.SubscriptionTypeGroupedInvoicing)
	inherited := makeSub("sub_non_eligible_inherited", types.SubscriptionTypeInherited)

	w := s.createWalletWithCredit("wallet_non_eligible", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(consumed.IsZero(), "expected zero consumed (no eligible subscriptions), got %s", consumed.String())

	for _, sub := range []*subscription.Subscription{delegated, grouped, inherited} {
		f := types.NewNoLimitInvoiceFilter()
		f.SubscriptionID = sub.ID
		invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), f)
		s.Require().NoError(err)
		s.Empty(invoices, "no invoice should have been created for subscription %s (type %s)", sub.ID, sub.SubscriptionType)
	}
}

// TestConsumeExpiringCreditIntoInvoices_CumulativeAcrossCallPaths proves TotalPrepaidCreditsApplied
// accumulates across independent call paths: a general/pooled ApplyCreditsToInvoice application first,
// then a separate pre-expiry ConsumeExpiringCreditIntoInvoices application, must sum together.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_CumulativeAcrossCallPaths() {
	sub := s.createActiveStandaloneSubscription("sub_cumulative", "USD")
	inv := s.createDraftSubInvoiceWithUsageLineItem("inv_cumulative", "USD", decimal.NewFromInt(100), sub.ID, s.GetNow())

	// First: general/pooled path applies 20 from an ordinary (non-expiring) wallet.
	poolWallet := s.createWalletWithCredit("wallet_cumulative_pool", "USD", decimal.NewFromInt(20))
	_, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.Require().NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), inv))
	_ = poolWallet

	afterPool := s.reloadInvoiceWithLineItems(inv.ID)
	s.True(decimal.NewFromInt(20).Equal(afterPool.TotalPrepaidCreditsApplied),
		"after pooled application, TotalPrepaidCreditsApplied = %s, want 20", afterPool.TotalPrepaidCreditsApplied.String())

	// Second: a SEPARATE expiring credit, applied via ConsumeExpiringCreditIntoInvoices.
	expiringWallet := s.createWalletWithCredit("wallet_cumulative_expiring", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(expiringWallet.ID, decimal.NewFromInt(30), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(30).Equal(consumed), "expected 30 consumed, got %s", consumed.String())

	final := s.reloadInvoiceWithLineItems(inv.ID)
	s.True(decimal.NewFromInt(50).Equal(final.TotalPrepaidCreditsApplied),
		"final TotalPrepaidCreditsApplied = %s, want 50 (20 pooled + 30 expiring)", final.TotalPrepaidCreditsApplied.String())
}

// TestConsumeExpiringCreditIntoInvoices_Phase2FindsNonCycleReasonDraftNoDuplicate proves Phase 2's
// broad SubscriptionID+Draft lookup (no billing-reason filter) finds an existing draft invoice created
// under a DIFFERENT billing reason (e.g. SUBSCRIPTION_CREATE for a subscription still in its first
// period), and does not create a duplicate SUBSCRIPTION_CYCLE draft alongside it.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase2FindsNonCycleReasonDraftNoDuplicate() {
	sub := s.createActiveStandaloneSubscription("sub_first_period", "USD")

	pt := string(types.PRICE_TYPE_USAGE)
	li := &invoice.InvoiceLineItem{
		ID:               s.GetUUID(),
		InvoiceID:        "inv_first_period_create",
		CustomerID:       s.testData.customer.ID,
		Amount:           decimal.NewFromInt(50),
		Currency:         "USD",
		Quantity:         decimal.NewFromInt(1),
		PriceType:        &pt,
		LineItemDiscount: decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	existing := &invoice.Invoice{
		ID:              "inv_first_period_create",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		Currency:        "USD",
		Subtotal:        decimal.NewFromInt(50),
		Total:           decimal.NewFromInt(50),
		AmountDue:       decimal.NewFromInt(50),
		AmountRemaining: decimal.Zero, // forces Phase 1 to skip it, so Phase 2 must be the one to find it
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
		PeriodStart:     lo.ToPtr(sub.CurrentPeriodStart),
		PeriodEnd:       lo.ToPtr(sub.CurrentPeriodEnd),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{li},
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), existing))

	w := s.createWalletWithCredit("wallet_first_period", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(50).Equal(consumed), "expected 50 consumed, got %s", consumed.String())

	f := types.NewNoLimitInvoiceFilter()
	f.SubscriptionID = sub.ID
	allInvoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), f)
	s.Require().NoError(err)
	s.Require().Len(allInvoices, 1, "Phase 2 must not have created a duplicate SUBSCRIPTION_CYCLE draft")
	s.Equal(existing.ID, allInvoices[0].ID)
}

// TestConsumeExpiringCreditIntoInvoices_Phase2CreateErrorIsolated proves a
// GetOrCreateDraftInvoiceForSubscription failure for one subscription (here: a finalized invoice
// already occupies its exact current period, so creation is rejected) is logged and skipped, and does
// not block credit from reaching a healthy subscription processed in the same run.
func (s *CreditAdjustmentServiceSuite) TestConsumeExpiringCreditIntoInvoices_Phase2CreateErrorIsolated() {
	brokenSub := s.createActiveStandaloneSubscription("sub_create_error", "USD")
	// A FINALIZED invoice already exists for this exact (subscription, period, SUBSCRIPTION_CYCLE) -
	// CreateEmptyDraftInvoice's period-conflict check rejects creating another draft for it. Not found
	// by Phase 1 (InvoiceStatus filter is Draft-only) or by Phase 2's Draft-status lookup either -
	// only surfaces when Phase 2 attempts to create.
	finalized := &invoice.Invoice{
		ID:             "inv_create_error_finalized",
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr(brokenSub.ID),
		Currency:       "USD",
		Subtotal:       decimal.NewFromInt(10),
		Total:          decimal.NewFromInt(10),
		AmountDue:      decimal.NewFromInt(10),
		AmountPaid:     decimal.NewFromInt(10),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusFinalized,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		PeriodStart:    lo.ToPtr(brokenSub.CurrentPeriodStart),
		PeriodEnd:      lo.ToPtr(brokenSub.CurrentPeriodEnd),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), finalized))

	healthySub := s.createActiveStandaloneSubscription("sub_create_error_healthy", "USD")
	healthyInv := s.createDraftSubInvoiceWithUsageLineItem("inv_create_error_healthy", "USD", decimal.NewFromInt(30), healthySub.ID, s.GetNow())

	w := s.createWalletWithCredit("wallet_create_error", "USD", decimal.Zero)
	pastExpiry := s.GetNow().Add(-time.Hour)
	// More than healthyInv's 30 ceiling, so credit remains after Phase 1 and Phase 2 actually runs.
	source := s.createWalletCredit(w.ID, decimal.NewFromInt(50), &pastExpiry, nil)

	consumed, err := s.service.ConsumeExpiringCreditIntoInvoices(s.GetContext(), source)
	s.Require().NoError(err, "orchestrator must remain best-effort despite the create failure")
	s.True(decimal.NewFromInt(30).Equal(consumed), "expected 30 consumed from the healthy subscription only, got %s", consumed.String())

	reloadedHealthy := s.reloadInvoiceWithLineItems(healthyInv.ID)
	s.True(decimal.NewFromInt(30).Equal(reloadedHealthy.TotalPrepaidCreditsApplied),
		"healthy invoice TotalPrepaidCreditsApplied = %s, want 30", reloadedHealthy.TotalPrepaidCreditsApplied.String())

	f := types.NewNoLimitInvoiceFilter()
	f.SubscriptionID = brokenSub.ID
	brokenSubInvoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), f)
	s.Require().NoError(err)
	s.Require().Len(brokenSubInvoices, 1, "no new draft should have been created for the broken subscription")
	s.Equal(types.InvoiceStatusFinalized, brokenSubInvoices[0].InvoiceStatus)
}
