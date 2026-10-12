package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// convertedInvoice builds a FINALIZED invoice converted from usd to inr at the given frozen rate.
func (s *RefundServiceSuite) convertedInvoice(rate decimal.Decimal) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:            "inv_conv_cn",
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded,
		Currency:      "inr",
		AmountDue:     decimal.NewFromInt(8300),
		AmountPaid:    decimal.NewFromInt(8300),
		Total:         decimal.NewFromInt(8300),
		InvoiceNumber: lo.ToPtr("INV-CONV-CN"),
		FxConversion: &types.FxConversion{
			ChargeCurrency:  "usd",
			BillingCurrency: "inr",
			Rate:            rate,
			Scope:           "tenant",
			ConvertedAt:     s.testData.now,
			Source:          types.FxConversionSource{Net: decimal.NewFromInt(100)},
		},
		EnvironmentID: "env_test",
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

func (s *RefundServiceSuite) creditNoteFor(inv *invoice.Invoice, amount decimal.Decimal) *creditnote.CreditNote {
	cn := s.creditNote(amount)
	cn.InvoiceID = inv.ID
	cn.Currency = inv.Currency
	return cn
}

// inrPayment records a succeeded inr payment on inv; a non-nil gateway makes it gateway-refundable.
func (s *RefundServiceSuite) inrPayment(inv *invoice.Invoice, id string, amount decimal.Decimal, method types.PaymentMethodType, gateway *string, createdAt time.Time) *payment.Payment {
	p := s.createPayment(id, amount, method, gateway, createdAt)
	p.DestinationID = inv.ID
	p.Currency = inv.Currency
	s.NoError(s.GetStores().PaymentRepo.Update(s.GetContext(), p))
	return p
}

// cardAndOffline pays the ₹8,300 invoice as P1 card ₹4,980 (older) and P2 offline ₹3,320.
func (s *RefundServiceSuite) cardAndOffline(inv *invoice.Invoice) (*payment.Payment, *payment.Payment) {
	card := s.inrPayment(inv, "pay_card", decimal.NewFromInt(4980), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now.Add(-2*time.Hour))
	offline := s.inrPayment(inv, "pay_offline", decimal.NewFromInt(3320), types.PaymentMethodTypeOffline, nil, s.testData.now.Add(-time.Hour))
	return card, offline
}

func (s *RefundServiceSuite) usdWallet() *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  "wallet_usd_cn",
		CustomerID:          s.testData.customer.ID,
		Currency:            "usd",
		Balance:             decimal.Zero,
		CreditBalance:       decimal.Zero,
		WalletStatus:        types.WalletStatusActive,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletType:          types.WalletTypePrePaid,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))
	return w
}

func (s *RefundServiceSuite) walletBalance(id string) decimal.Decimal {
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), id)
	s.NoError(err)
	return w.Balance
}

func (s *RefundServiceSuite) dispatchAll(rows []*refund.Refund) {
	for _, row := range rows {
		s.Require().NoError(s.service.Dispatch(s.GetContext(), row.ID))
	}
}

// A PREPAID_WALLET refund stays in inr against the payment it reverses and credits the usd wallet.
func (s *RefundServiceSuite) TestPrepareRefundsForCreditNote_ConvertedPrepaidWallet_InrRowWithPayment() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	card, _ := s.cardAndOffline(inv)
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(2000)), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.Require().Len(rows, 1)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
	s.Equal("inr", rows[0].Currency)
	s.True(decimal.NewFromInt(2000).Equal(rows[0].Amount), "amount got %s", rows[0].Amount)
	s.Equal(card.ID, lo.FromPtr(rows[0].PaymentID))
	s.Empty(rows[0].Metadata, "planning a converted refund adds nothing beyond an unconverted one")

	s.dispatchAll(rows)
	s.True(decimal.RequireFromString("24.10").Equal(s.walletBalance(usd.ID)), "₹2,000 ÷ 83 = $24.10, got %s", s.walletBalance(usd.ID))
}

// BACK_TO_SOURCE: the card slice goes to the gateway in inr; the offline slice is credited to the usd wallet.
func (s *RefundServiceSuite) TestPrepareRefundsForCreditNote_ConvertedBackToSource_OfflineSliceToChargeWallet() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	card, offline := s.cardAndOffline(inv)
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(6000)), inv, backToSource())
	s.Require().NoError(err)
	s.Require().Len(rows, 2)

	s.Equal(card.ID, lo.FromPtr(rows[0].PaymentID))
	s.Equal(types.RefundDestinationGateway, rows[0].RefundDestination)
	s.Equal("inr", rows[0].Currency)
	s.True(decimal.NewFromInt(4980).Equal(rows[0].Amount))

	s.Equal(offline.ID, lo.FromPtr(rows[1].PaymentID))
	s.Equal(types.RefundDestinationWallet, rows[1].RefundDestination)
	s.Equal("inr", rows[1].Currency)
	s.True(decimal.NewFromInt(1020).Equal(rows[1].Amount))

	s.NoError(s.service.Dispatch(s.GetContext(), rows[1].ID))
	s.True(decimal.RequireFromString("12.29").Equal(s.walletBalance(usd.ID)), "₹1,020 ÷ 83 = $12.29, got %s", s.walletBalance(usd.ID))
}

// Settling converted wallet rows credits the usd wallet; each refund row stays settled in inr.
func (s *RefundServiceSuite) TestDispatch_ConvertedWalletRow_SettlesToChargeWallet() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	s.cardAndOffline(inv)
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(8300)), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.Require().Len(rows, 2)
	s.dispatchAll(rows)

	s.True(decimal.NewFromInt(100).Equal(s.walletBalance(usd.ID)), "usd wallet got %s", s.walletBalance(usd.ID))
	for _, row := range rows {
		settled, err := s.GetStores().RefundRepo.Get(s.GetContext(), row.ID)
		s.NoError(err)
		s.Equal(types.RefundStatusSucceeded, settled.RefundStatus)
		s.True(row.Amount.Equal(settled.SettledAmount), "settled amount stays inr, got %s", settled.SettledAmount)
	}

	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(wallets, 1, "no inr wallet is created")
}

// A failed gateway refund on a converted invoice falls back to the usd wallet at the frozen rate.
func (s *RefundServiceSuite) TestFail_ConvertedGatewayRow_FallsBackToChargeWallet() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	card := s.inrPayment(inv, "pay_card", decimal.NewFromInt(8300), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(4980)), inv, backToSource())
	s.Require().NoError(err)
	s.Require().Len(rows, 1)

	s.NoError(s.service.Fail(s.GetContext(), rows[0].ID, "gateway declined"))

	failed, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	fallback := s.fallbackOf(failed)
	s.Equal(types.RefundStatusSucceeded, fallback.RefundStatus)
	s.Equal("inr", fallback.Currency)
	s.Equal(card.ID, lo.FromPtr(fallback.PaymentID))
	s.True(decimal.NewFromInt(60).Equal(s.walletBalance(usd.ID)), "₹4,980 ÷ 83 = $60, got %s", s.walletBalance(usd.ID))

	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(wallets, 1, "no inr wallet is created")
}

// Rows of one refund add up to a single rounding of its inr total: 36.14 + 36.15 + 45.71 = $118.00.
func (s *RefundServiceSuite) TestDispatch_ConvertedRowsAddUpToOneRounding() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	s.inrPayment(inv, "pay_1", decimal.NewFromInt(3000), types.PaymentMethodTypeOffline, nil, s.testData.now.Add(-3*time.Hour))
	s.inrPayment(inv, "pay_2", decimal.NewFromInt(3000), types.PaymentMethodTypeOffline, nil, s.testData.now.Add(-2*time.Hour))
	s.inrPayment(inv, "pay_3", decimal.NewFromInt(3794), types.PaymentMethodTypeOffline, nil, s.testData.now.Add(-time.Hour))
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(9794)), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.Require().Len(rows, 3)
	s.dispatchAll(rows)

	s.True(decimal.NewFromInt(118).Equal(s.walletBalance(usd.ID)), "₹9,794 ÷ 83 = $118.00 once, got %s", s.walletBalance(usd.ID))
}

// The running total spans credit notes: ₹1,004 twice is $24.19 in all (one rounding of ₹2,008), not 2 × $12.10.
func (s *RefundServiceSuite) TestDispatch_ConvertedRunningTotalSpansCreditNotes() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	s.inrPayment(inv, "pay_offline", decimal.NewFromInt(8300), types.PaymentMethodTypeOffline, nil, s.testData.now)
	usd := s.usdWallet()

	first, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(1004)), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.dispatchAll(first)
	s.True(decimal.RequireFromString("12.10").Equal(s.walletBalance(usd.ID)), "first note got %s", s.walletBalance(usd.ID))

	cn := s.creditNoteFor(inv, decimal.NewFromInt(1004))
	cn.ID = "cn_refund_2"
	second, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), cn, inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.dispatchAll(second)
	s.True(decimal.RequireFromString("24.19").Equal(s.walletBalance(usd.ID)), "₹2,008 ÷ 83 = $24.19, got %s", s.walletBalance(usd.ID))
}

// A row worth less than half a cent settles with no wallet credit instead of failing the top-up.
func (s *RefundServiceSuite) TestDispatch_ConvertedRowRoundingToZero_SettlesWithoutWalletCredit() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	usd := s.usdWallet()

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.RequireFromString("0.40")), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)
	s.Require().Len(rows, 1)

	s.NoError(s.service.Dispatch(s.GetContext(), rows[0].ID))

	settled, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(types.RefundStatusSucceeded, settled.RefundStatus)
	s.True(s.walletBalance(usd.ID).IsZero())
}

// A wallet refund uses up its payment, so a later BACK_TO_SOURCE note draws only from what is left.
func (s *RefundServiceSuite) TestPrepareRefundsForCreditNote_ConvertedWalletRefundUsesPaymentCapacity() {
	inv := s.convertedInvoice(decimal.NewFromInt(83))
	_, offline := s.cardAndOffline(inv)

	_, err := s.service.PrepareRefundsForCreditNote(s.GetContext(),
		s.creditNoteFor(inv, decimal.NewFromInt(4980)), inv, lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.Require().NoError(err)

	second := s.creditNoteFor(inv, decimal.NewFromInt(3320))
	second.ID = "cn_refund_2"
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), second, inv, backToSource())
	s.Require().NoError(err)
	s.Require().Len(rows, 1)
	s.Equal(offline.ID, lo.FromPtr(rows[0].PaymentID), "the card payment is already used up")
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
}
