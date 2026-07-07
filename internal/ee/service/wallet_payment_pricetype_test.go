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

// WalletPaymentPriceTypeSuite covers price-type restricted wallet payments:
// GetWalletsForPayment categorization, calculateAllowedPaymentAmount and
// deductFromPriceTypes via ProcessInvoicePaymentWithWallets.
type WalletPaymentPriceTypeSuite struct {
	testutil.BaseServiceTestSuite
	service  WalletPaymentService
	testData struct {
		customer *customer.Customer
		now      time.Time
	}
}

func TestWalletPaymentPriceTypeService(t *testing.T) {
	suite.Run(t, new(WalletPaymentPriceTypeSuite))
}

func (s *WalletPaymentPriceTypeSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewWalletPaymentService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_pricetype",
		ExternalID: "ext_cust_pricetype",
		Name:       "Price Type Customer",
		Email:      "pricetype@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))
}

// newPostpaidWallet creates an active postpaid wallet with a consumable credit
// balance and the given allowed price types (nil means unrestricted).
func (s *WalletPaymentPriceTypeSuite) newPostpaidWallet(id string, balance decimal.Decimal, priceTypes ...types.WalletConfigPriceType) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  id,
		CustomerID:          s.testData.customer.ID,
		Currency:            "usd",
		Balance:             balance,
		CreditBalance:       balance,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          types.WalletTypePostPaid,
		Config:              types.WalletConfig{AllowedPriceTypes: priceTypes},
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))
	if balance.GreaterThan(decimal.Zero) {
		s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            w.ID,
			CustomerID:          w.CustomerID,
			Type:                types.TransactionTypeCredit,
			Amount:              balance,
			CreditAmount:        balance,
			CreditsAvailable:    balance,
			CreditBalanceBefore: decimal.Zero,
			CreditBalanceAfter:  balance,
			TxStatus:            types.TransactionStatusCompleted,
			ReferenceType:       types.WalletTxReferenceTypeExternal,
			ReferenceID:         "seed_" + w.ID,
			IdempotencyKey:      "seed_" + w.ID,
			Currency:            "usd",
			BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
		}))
	}
	return w
}

type priceTypeLineItem struct {
	priceType *string
	amount    decimal.Decimal
}

// newInvoice creates a finalized, unpaid invoice with the given line items.
func (s *WalletPaymentPriceTypeSuite) newInvoice(id string, lineItems []priceTypeLineItem) *invoice.Invoice {
	total := decimal.Zero
	items := make([]*invoice.InvoiceLineItem, 0, len(lineItems))
	for i, li := range lineItems {
		total = total.Add(li.amount)
		items = append(items, &invoice.InvoiceLineItem{
			ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
			InvoiceID:  id,
			CustomerID: s.testData.customer.ID,
			PriceType:  li.priceType,
			Amount:     li.amount,
			Quantity:   decimal.NewFromInt(1),
			Currency:   "usd",
			DisplayName: lo.ToPtr(
				id + "_line_" + string(rune('a'+i)),
			),
			BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		})
	}

	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       total,
		AmountPaid:      decimal.Zero,
		AmountRemaining: total,
		LineItems:       items,
		PeriodStart:     lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
		PeriodEnd:       lo.ToPtr(s.testData.now),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

// listPayments returns the payments recorded against an invoice.
func (s *WalletPaymentPriceTypeSuite) listPayments(invoiceID string) []decimal.Decimal {
	payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
		DestinationID:   lo.ToPtr(invoiceID),
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
	})
	s.NoError(err)
	amounts := make([]decimal.Decimal, 0, len(payments))
	for _, p := range payments {
		amounts = append(amounts, p.Amount)
	}
	return amounts
}

var (
	usagePriceType = lo.ToPtr(string(types.PRICE_TYPE_USAGE))
	fixedPriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
)

func (s *WalletPaymentPriceTypeSuite) TestGetWalletsForPaymentCategorizesByPriceType() {
	usageOnly := s.newPostpaidWallet("wallet_usage_only", decimal.NewFromInt(30), types.WalletConfigPriceTypeUsage)
	fixedOnly := s.newPostpaidWallet("wallet_fixed_only", decimal.NewFromInt(25), types.WalletConfigPriceTypeFixed)
	allTyped := s.newPostpaidWallet("wallet_all_typed", decimal.NewFromInt(100), types.WalletConfigPriceTypeAll)
	noConfig := s.newPostpaidWallet("wallet_no_config", decimal.NewFromInt(50))
	usageAndFixed := s.newPostpaidWallet("wallet_usage_and_fixed", decimal.NewFromInt(40), types.WalletConfigPriceTypeUsage, types.WalletConfigPriceTypeFixed)

	wallets, err := s.service.GetWalletsForPayment(s.GetContext(), s.testData.customer.ID, "usd", DefaultWalletPaymentOptions())
	s.NoError(err)
	s.Len(wallets, 5)

	// Priority order: usage-restricted, then fixed-restricted, then ALL-capable
	// wallets sorted by balance descending. A wallet allowing both usage and
	// fixed is treated as ALL.
	gotIDs := lo.Map(wallets, func(w *wallet.Wallet, _ int) string { return w.ID })
	s.Equal([]string{usageOnly.ID, fixedOnly.ID, allTyped.ID, noConfig.ID, usageAndFixed.ID}, gotIDs)
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentUsageOnlyWallet() {
	s.newPostpaidWallet("wallet_usage_pay", decimal.NewFromInt(100), types.WalletConfigPriceTypeUsage)
	inv := s.newInvoice("inv_usage_pay", []priceTypeLineItem{
		{priceType: usagePriceType, amount: decimal.NewFromInt(60)},
		{priceType: fixedPriceType, amount: decimal.NewFromInt(40)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.NewFromInt(60)), "usage-only wallet pays exactly the usage portion, got %s", amountPaid)

	amounts := s.listPayments(inv.ID)
	s.Len(amounts, 1)
	s.True(amounts[0].Equal(decimal.NewFromInt(60)))

	// The invoice retains the unpaid fixed portion.
	storedInv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(storedInv.AmountRemaining.Equal(decimal.NewFromInt(40)), "expected 40 remaining, got %s", storedInv.AmountRemaining)
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentFixedOnlyWallet() {
	s.newPostpaidWallet("wallet_fixed_pay", decimal.NewFromInt(100), types.WalletConfigPriceTypeFixed)
	inv := s.newInvoice("inv_fixed_pay", []priceTypeLineItem{
		{priceType: usagePriceType, amount: decimal.NewFromInt(60)},
		{priceType: fixedPriceType, amount: decimal.NewFromInt(40)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.NewFromInt(40)), "fixed-only wallet pays exactly the fixed portion, got %s", amountPaid)

	amounts := s.listPayments(inv.ID)
	s.Len(amounts, 1)
	s.True(amounts[0].Equal(decimal.NewFromInt(40)))
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentMixedWalletsSettleFullInvoice() {
	// usage(30) pays 30 of the 60 usage charges, fixed(25) pays 25 of the 40
	// fixed charges, the unrestricted wallet covers the remaining 45.
	usageOnly := s.newPostpaidWallet("wallet_mix_usage", decimal.NewFromInt(30), types.WalletConfigPriceTypeUsage)
	fixedOnly := s.newPostpaidWallet("wallet_mix_fixed", decimal.NewFromInt(25), types.WalletConfigPriceTypeFixed)
	allWallet := s.newPostpaidWallet("wallet_mix_all", decimal.NewFromInt(100), types.WalletConfigPriceTypeAll)

	inv := s.newInvoice("inv_mixed_pricetypes", []priceTypeLineItem{
		{priceType: usagePriceType, amount: decimal.NewFromInt(60)},
		{priceType: fixedPriceType, amount: decimal.NewFromInt(40)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.NewFromInt(100)), "expected full settlement, got %s", amountPaid)

	amounts := s.listPayments(inv.ID)
	s.Len(amounts, 3)
	got := lo.Map(amounts, func(d decimal.Decimal, _ int) string { return d.String() })
	s.ElementsMatch([]string{"30", "25", "45"}, got)

	// Each wallet's balance was debited by exactly what it paid.
	for _, tc := range []struct {
		walletID string
		expected decimal.Decimal
	}{
		{usageOnly.ID, decimal.Zero},
		{fixedOnly.ID, decimal.Zero},
		{allWallet.ID, decimal.NewFromInt(55)},
	} {
		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), tc.walletID)
		s.NoError(err)
		s.True(stored.Balance.Equal(tc.expected), "wallet %s expected balance %s got %s", tc.walletID, tc.expected, stored.Balance)
	}

	storedInv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(storedInv.AmountRemaining.IsZero(), "invoice fully settled, got remaining %s", storedInv.AmountRemaining)
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentUsageWalletSkipsFixedOnlyInvoice() {
	s.newPostpaidWallet("wallet_usage_skip", decimal.NewFromInt(100), types.WalletConfigPriceTypeUsage)
	inv := s.newInvoice("inv_fixed_only", []priceTypeLineItem{
		{priceType: fixedPriceType, amount: decimal.NewFromInt(50)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.IsZero(), "usage-restricted wallet cannot pay fixed charges, got %s", amountPaid)
	s.Empty(s.listPayments(inv.ID))

	storedInv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(storedInv.AmountRemaining.Equal(decimal.NewFromInt(50)))
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentNilAndUnknownPriceTypesTreatedAsFixed() {
	s.newPostpaidWallet("wallet_fixed_default", decimal.NewFromInt(100), types.WalletConfigPriceTypeFixed)
	inv := s.newInvoice("inv_default_pricetypes", []priceTypeLineItem{
		{priceType: nil, amount: decimal.NewFromInt(30)},
		{priceType: lo.ToPtr("CUSTOM_TYPE"), amount: decimal.NewFromInt(20)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.NewFromInt(50)), "nil/unknown price types default to FIXED, got %s", amountPaid)

	amounts := s.listPayments(inv.ID)
	s.Len(amounts, 1)
	s.True(amounts[0].Equal(decimal.NewFromInt(50)))
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentUsageAndFixedWalletTreatedAsAll() {
	s.newPostpaidWallet("wallet_both_types", decimal.NewFromInt(100), types.WalletConfigPriceTypeUsage, types.WalletConfigPriceTypeFixed)
	inv := s.newInvoice("inv_both_types", []priceTypeLineItem{
		{priceType: usagePriceType, amount: decimal.NewFromInt(60)},
		{priceType: fixedPriceType, amount: decimal.NewFromInt(40)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.NewFromInt(100)), "usage+fixed wallet can settle the full invoice, got %s", amountPaid)
}

func (s *WalletPaymentPriceTypeSuite) TestProcessInvoicePaymentPartialUsageBalance() {
	// Wallet balance below the usage portion: pays up to its balance only and
	// never drives its own balance negative.
	w := s.newPostpaidWallet("wallet_partial_usage", decimal.RequireFromString("12.5"), types.WalletConfigPriceTypeUsage)
	inv := s.newInvoice("inv_partial_usage", []priceTypeLineItem{
		{priceType: usagePriceType, amount: decimal.NewFromInt(60)},
	})

	amountPaid, err := s.service.ProcessInvoicePaymentWithWallets(s.GetContext(), inv, DefaultWalletPaymentOptions())
	s.NoError(err)
	s.True(amountPaid.Equal(decimal.RequireFromString("12.5")), "payment is capped at the wallet balance, got %s", amountPaid)

	stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
	s.NoError(err)
	s.True(stored.Balance.IsZero(), "wallet is fully drained, got %s", stored.Balance)
	s.False(stored.Balance.IsNegative(), "wallet balance must never go negative")
}
