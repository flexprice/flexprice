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
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
	})
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
