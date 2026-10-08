package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *WalletServiceSuite) seedCoupon(code string, mutate func(c *coupon.Coupon)) *coupon.Coupon {
	c := &coupon.Coupon{
		ID:            "coupon_" + code,
		Name:          code,
		Type:          types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(10)),
		Cadence:       types.CouponCadenceOnce,
		Currency:      "usd",
		CouponCode:    lo.ToPtr(code),
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(c)
	}
	s.Require().NoError(s.GetStores().CouponRepo.Create(s.GetContext(), c))
	return c
}

func fixedOff(amount int64, currency string) func(c *coupon.Coupon) {
	return func(c *coupon.Coupon) {
		c.Type = types.CouponTypeFixed
		c.PercentageOff = nil
		c.AmountOff = lo.ToPtr(decimal.NewFromInt(amount))
		c.Currency = currency
	}
}

func percentOff(pct int64) func(c *coupon.Coupon) {
	return func(c *coupon.Coupon) { c.PercentageOff = lo.ToPtr(decimal.NewFromInt(pct)) }
}

func (s *WalletServiceSuite) topUpWithCoupons(key string, credits int64, codes ...string) (*dto.TopUpWalletResponse, error) {
	coupons := lo.Map(codes, func(code string, _ int) dto.TopUpCoupon { return dto.TopUpCoupon{CouponCode: code} })
	return s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      decimal.NewFromInt(credits),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    lo.ToPtr(key),
		Coupons:           coupons,
	})
}

func (s *WalletServiceSuite) topUpInvoice(resp *dto.TopUpWalletResponse) *invoice.Invoice {
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), lo.FromPtr(resp.InvoiceID))
	s.Require().NoError(err)
	return inv
}

func (s *WalletServiceSuite) couponApplications(invoiceID string) int {
	n, err := s.GetStores().CouponApplicationRepo.Count(s.GetContext(), &types.CouponApplicationFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		InvoiceIDs:  []string{invoiceID},
	})
	s.Require().NoError(err)
	return n
}

func (s *WalletServiceSuite) redemptions(couponID string) int {
	c, err := s.GetStores().CouponRepo.Get(s.GetContext(), couponID)
	s.Require().NoError(err)
	return c.TotalRedemptions
}

func (s *WalletServiceSuite) invoiceCount() int {
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	return len(invoices)
}

func (s *WalletServiceSuite) walletTxCount() int {
	filter := types.NewNoLimitWalletTransactionFilter()
	filter.WalletID = &s.testData.wallet.ID
	txs, err := s.GetStores().WalletRepo.ListAllWalletTransactions(s.GetContext(), filter)
	s.Require().NoError(err)
	return len(txs)
}

func (s *WalletServiceSuite) TestTopUpCoupons_PercentageDiscountsTheInvoiceNotTheCredits() {
	s.seedAutoComplete(false)
	c := s.seedCoupon("TOPUP10", nil)

	resp, err := s.topUpWithCoupons("coupon_pct", 500, "TOPUP10")
	s.Require().NoError(err)

	inv := s.topUpInvoice(resp)
	s.True(decimal.NewFromInt(500).Equal(inv.Subtotal), "subtotal %s", inv.Subtotal)
	s.True(decimal.NewFromInt(50).Equal(inv.TotalDiscount), "discount %s", inv.TotalDiscount)
	s.True(decimal.NewFromInt(450).Equal(inv.Total), "total %s", inv.Total)
	s.Equal(1, s.couponApplications(inv.ID))
	s.Equal(1, s.redemptions(c.ID))

	s.Equal(types.TransactionStatusPending, resp.WalletTransaction.TxStatus, "credits wait for payment")
	s.True(decimal.NewFromInt(500).Equal(resp.WalletTransaction.CreditAmount), "credits are never reduced")

	s.Require().NoError(s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), resp.WalletTransaction.ID, inv.ID))
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(1500).Equal(w.CreditBalance), "1000 + 500 full credits, got %s", w.CreditBalance)
}

func (s *WalletServiceSuite) TestTopUpCoupons_MultipleApplyInOrderOnTheRunningSubtotal() {
	s.seedAutoComplete(false)
	first := s.seedCoupon("TOPUP10", nil)
	second := s.seedCoupon("LESS50", fixedOff(50, "usd"))

	resp, err := s.topUpWithCoupons("coupon_multi", 500, "TOPUP10", "LESS50")
	s.Require().NoError(err)

	inv := s.topUpInvoice(resp)
	s.True(decimal.NewFromInt(100).Equal(inv.TotalDiscount), "10%% of 500, then 50 off: %s", inv.TotalDiscount)
	s.True(decimal.NewFromInt(400).Equal(inv.Total), "total %s", inv.Total)
	s.Equal(2, s.couponApplications(inv.ID))
	s.Equal(1, s.redemptions(first.ID))
	s.Equal(1, s.redemptions(second.ID))
}

func (s *WalletServiceSuite) TestTopUpCoupons_FullDiscountGrantsCreditsImmediately() {
	s.seedAutoComplete(false)
	s.seedCoupon("FREE100", percentOff(100))

	resp, err := s.topUpWithCoupons("coupon_full", 500, "FREE100")
	s.Require().NoError(err)

	inv := s.topUpInvoice(resp)
	s.True(inv.Total.IsZero(), "total %s", inv.Total)
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)

	purchase, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), resp.WalletTransaction.ID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, purchase.TxStatus, "a $0 invoice has no payment to wait for")
	s.Equal(types.WalletTxSourceTypeInvoice, purchase.SourceType)
	s.Equal(inv.ID, purchase.SourceID)

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(1500).Equal(w.CreditBalance), "got %s", w.CreditBalance)
}

func (s *WalletServiceSuite) TestTopUpCoupons_PercentageCouponWorksInAnyCurrency() {
	s.seedAutoComplete(false)
	s.seedCoupon("EURPCT", func(c *coupon.Coupon) { c.Currency = "eur" })

	resp, err := s.topUpWithCoupons("coupon_pct_eur", 500, "EURPCT")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(450).Equal(s.topUpInvoice(resp).Total))
}

func (s *WalletServiceSuite) TestTopUpCoupons_InvalidCouponRejectsTheWholeTopUp() {
	past := time.Now().UTC().Add(-48 * time.Hour)
	future := time.Now().UTC().Add(48 * time.Hour)

	tests := []struct {
		name         string
		autoComplete bool
		seed         func()
		req          func(req *dto.TopUpWalletRequest)
		wantNotFound bool
	}{
		{
			name: "reason other than invoiced purchase",
			seed: func() { s.seedCoupon("TOPUP10", nil) },
			req: func(req *dto.TopUpWalletRequest) {
				req.TransactionReason = types.TransactionReasonPurchasedCreditDirect
			},
		},
		{
			name: "together with checkout",
			seed: func() { s.seedCoupon("TOPUP10", nil) },
			req:  func(req *dto.TopUpWalletRequest) { req.Checkout = s.checkoutParamsRazorpay() },
		},
		{
			name: "same code twice",
			seed: func() { s.seedCoupon("TOPUP10", nil) },
			req: func(req *dto.TopUpWalletRequest) {
				req.Coupons = append(req.Coupons, dto.TopUpCoupon{CouponCode: "TOPUP10"})
			},
		},
		{
			name:         "unknown code",
			seed:         func() {},
			wantNotFound: true,
		},
		{
			name:         "unpublished coupon",
			seed:         func() { s.seedCoupon("TOPUP10", func(c *coupon.Coupon) { c.Status = types.StatusArchived }) },
			wantNotFound: true,
		},
		{
			name: "before redeem_after",
			seed: func() { s.seedCoupon("TOPUP10", func(c *coupon.Coupon) { c.RedeemAfter = &future }) },
		},
		{
			name: "after redeem_before",
			seed: func() { s.seedCoupon("TOPUP10", func(c *coupon.Coupon) { c.RedeemBefore = &past }) },
		},
		{
			name: "max redemptions reached",
			seed: func() {
				s.seedCoupon("TOPUP10", func(c *coupon.Coupon) {
					c.MaxRedemptions = lo.ToPtr(1)
					c.TotalRedemptions = 1
				})
			},
		},
		{
			name: "fixed coupon in another currency",
			seed: func() { s.seedCoupon("TOPUP10", fixedOff(50, "eur")) },
		},
		{
			name:         "auto-complete on",
			autoComplete: true,
			seed:         func() { s.seedCoupon("TOPUP10", nil) },
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.seedAutoComplete(tt.autoComplete)
			tt.seed()
			txsBefore := s.walletTxCount()
			invoicesBefore := s.invoiceCount()

			req := &dto.TopUpWalletRequest{
				CreditsToAdd:      decimal.NewFromInt(500),
				TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
				IdempotencyKey:    lo.ToPtr("coupon_reject"),
				Coupons:           []dto.TopUpCoupon{{CouponCode: "TOPUP10"}},
			}
			if tt.req != nil {
				tt.req(req)
			}

			_, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, req)
			s.Require().Error(err)
			if tt.wantNotFound {
				s.True(ierr.IsNotFound(err), "want not found, got %v", err)
			} else {
				s.True(ierr.IsValidation(err), "want validation error, got %v", err)
			}
			s.Equal(txsBefore, s.walletTxCount(), "nothing is created")
			s.Equal(invoicesBefore, s.invoiceCount(), "no invoice is created")
		})
	}
}
