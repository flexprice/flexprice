package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
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

// seedCustomCurrencyConfig configures "mac" at 1 mac = 0.10 usd.
func (s *CreditAdjustmentServiceSuite) seedCustomCurrencyConfig() {
	cfg := types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			"mac": {
				Name:                  "MoEngage AI Credits",
				Symbol:                "MAC",
				FiatConversionFactors: map[string]decimal.Decimal{"usd": decimal.NewFromFloat(0.1)},
			},
		},
		DefaultFiatCurrency: "usd",
	}
	s.NoError(cfg.Validate())
	value, err := utils.ToMap(cfg)
	s.NoError(err)
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyCustomCurrencyConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// customCurrencyInvoice returns a fiat invoice whose denomination holds macAmount, split
// across a single usage line item.
func (s *CreditAdjustmentServiceSuite) customCurrencyInvoice(id string, macAmount decimal.Decimal) *invoice.Invoice {
	li := s.createLineItemForCalculation(decimal.Zero, lo.ToPtr(string(types.PRICE_TYPE_USAGE)), decimal.Zero)
	li.InvoiceID = id
	li.CustomerID = s.testData.customer.ID
	li.CustomCurrency = &types.CustomCurrencyLineItem{Amount: macAmount}

	inv := s.createInvoiceForCalculation(id, "usd", []*invoice.InvoiceLineItem{li})
	inv.CustomCurrency = &types.CustomCurrency{
		Code:     "mac",
		Rate:     decimal.NewFromFloat(0.1),
		Subtotal: macAmount,
	}
	inv.ProjectCustomCurrency()
	return inv
}

// Credits are drawn in the denomination currency, so a mac wallet pays down a mac charge
// one-for-one rather than being converted.
func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_CustomCurrencyDrawsFromLedger() {
	svc := s.getServiceImpl()

	inv := s.customCurrencyInvoice("inv_cc_ledger", decimal.NewFromInt(50))
	wallets := []*wallet.Wallet{s.createWalletForCalculation("wallet_mac", "mac", decimal.NewFromInt(30))}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	s.True(decimal.NewFromInt(30).Equal(debits["wallet_mac"]), "30 mac debited against a 50 mac charge, got %s", debits["wallet_mac"])
	s.True(decimal.NewFromInt(30).Equal(inv.LineItems[0].CustomCurrency.PrepaidCreditsApplied),
		"credits recorded on the denomination, got %s", inv.LineItems[0].CustomCurrency.PrepaidCreditsApplied)
}

// The fiat columns are left alone by the calculation itself; projection is what moves them.
func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_CustomCurrencyProjectsToFiat() {
	svc := s.getServiceImpl()

	inv := s.customCurrencyInvoice("inv_cc_project", decimal.NewFromInt(50))
	wallets := []*wallet.Wallet{s.createWalletForCalculation("wallet_mac", "mac", decimal.NewFromInt(30))}

	_, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)
	inv.LineItems[0].ProjectCustomCurrency(inv.CustomCurrency, inv.Currency)

	s.True(decimal.NewFromInt(3).Equal(inv.LineItems[0].PrepaidCreditsApplied),
		"30 mac * 0.1 = $3.00, got %s", inv.LineItems[0].PrepaidCreditsApplied)
}

// A fiat wallet is not a candidate for a custom-currency invoice: ApplyCreditsToInvoice
// selects wallets by denomination currency, so nothing is applied.
func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_FiatWalletSkippedForCustomCurrency() {
	s.seedCustomCurrencyConfig()

	inv := s.customCurrencyInvoice("inv_cc_fiat_wallet", decimal.NewFromInt(50))
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(),
		s.createWalletForCalculation("wallet_usd_only", "usd", decimal.NewFromInt(100))))

	result, err := s.service.ApplyCreditsToInvoice(s.GetContext(), inv)
	s.Require().NoError(err)
	s.Equal("mac", result.Currency, "result is denominated in the denomination currency")
	s.True(result.TotalPrepaidCreditsApplied.IsZero(),
		"a usd wallet must not pay down mac charges, got %s", result.TotalPrepaidCreditsApplied)
}

// Wallet selection is what makes the denomination work: a custom-currency invoice draws
// only from wallets in that currency.
func (s *CreditAdjustmentServiceSuite) TestGetWalletsForCreditAdjustment_MatchesDenominationCurrency() {
	inv := s.customCurrencyInvoice("inv_cc_selection", decimal.NewFromInt(50))

	macWallet := s.createWalletForCalculation("wallet_sel_mac", "mac", decimal.NewFromInt(20))
	usdWallet := s.createWalletForCalculation("wallet_sel_usd", "usd", decimal.NewFromInt(100))
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), macWallet))
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), usdWallet))

	walletPaymentService := NewWalletPaymentService(s.getServiceImpl().ServiceParams)
	selected, err := walletPaymentService.GetWalletsForCreditAdjustment(s.GetContext(), inv.CustomerID, inv.DenominationCurrency())
	s.NoError(err)
	s.Require().Len(selected, 1, "only the mac wallet is eligible")
	s.Equal("wallet_sel_mac", selected[0].ID)

	fiatSelected, err := walletPaymentService.GetWalletsForCreditAdjustment(s.GetContext(), inv.CustomerID, inv.Currency)
	s.NoError(err)
	s.Require().Len(fiatSelected, 1, "the fiat currency selects the usd wallet instead")
	s.Equal("wallet_sel_usd", fiatSelected[0].ID)
}

// A fiat invoice keeps the pre-existing behaviour: no denomination, fiat fields used directly.
func (s *CreditAdjustmentServiceSuite) TestCalculateCreditAdjustments_FiatInvoiceUnaffected() {
	svc := s.getServiceImpl()

	li := s.createLineItemForCalculation(decimal.NewFromInt(50), lo.ToPtr(string(types.PRICE_TYPE_USAGE)), decimal.Zero)
	inv := s.createInvoiceForCalculation("inv_fiat_regression", "usd", []*invoice.InvoiceLineItem{li})
	wallets := []*wallet.Wallet{s.createWalletForCalculation("wallet_usd", "usd", decimal.NewFromInt(30))}

	debits, err := svc.CalculateCreditAdjustments(inv, wallets)
	s.Require().NoError(err)

	s.True(decimal.NewFromInt(30).Equal(debits["wallet_usd"]))
	s.True(decimal.NewFromInt(30).Equal(inv.LineItems[0].PrepaidCreditsApplied))
	s.Nil(inv.LineItems[0].CustomCurrency, "no denomination is created for a fiat invoice")
}
