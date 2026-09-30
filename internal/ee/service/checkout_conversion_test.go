package service

import (
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// checkoutSvcForConversion builds a checkout service sharing the finalize suite's wired repos,
// plus the PaymentRepo that createCheckoutPayment needs.
func (s *InvoiceConversionFinalizeSuite) checkoutSvcForConversion() *checkoutSessionService {
	sp := s.svc.ServiceParams
	sp.PaymentRepo = s.GetStores().PaymentRepo
	return &checkoutSessionService{ServiceParams: sp}
}

// TestCheckoutPaymentConvertsBeforeMinting proves §5.6: a checkout payment for a cross-currency
// customer converts the draft to the billing currency before the payment record is minted, so the
// payment (and the link it drives) inherits the billing currency.
func (s *InvoiceConversionFinalizeSuite) TestCheckoutPaymentConvertsBeforeMinting() {
	s.seedCustomer("cust_co", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_co", "cust_co", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_co", "100")})

	payResp, err := s.checkoutSvcForConversion().createCheckoutPayment(s.ctx(), inv, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	// Payment inherits the billing currency + converted amount.
	s.Equal("inr", payResp.Currency, "payment must be minted in the billing currency")
	s.True(decimal.RequireFromString("8300").Equal(payResp.Amount), "payment amount got %s", payResp.Amount)

	// Invoice is converted and persisted before the payment.
	got, gerr := s.GetStores().InvoiceRepo.Get(s.ctx(), "inv_co")
	s.Require().NoError(gerr)
	s.Equal("inr", got.Currency)
	s.Require().NotNil(got.FxConversion)
	s.Equal("usd", got.FxConversion.ChargeCurrency)
}

// TestCheckoutPaymentNoBillingCurrencyUnaffected: a normal customer's checkout payment is minted in
// the charge currency, unconverted.
func (s *InvoiceConversionFinalizeSuite) TestCheckoutPaymentNoBillingCurrencyUnaffected() {
	s.seedCustomer("cust_co_bau", nil)
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_co_bau", "cust_co_bau", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_co_bau", "100")})

	payResp, err := s.checkoutSvcForConversion().createCheckoutPayment(s.ctx(), inv, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	s.Equal("usd", payResp.Currency)
	s.True(decimal.RequireFromString("100").Equal(payResp.Amount))
	got, gerr := s.GetStores().InvoiceRepo.Get(s.ctx(), "inv_co_bau")
	s.Require().NoError(gerr)
	s.Equal("usd", got.Currency)
	s.Nil(got.FxConversion)
}

// TestCheckoutPaymentMissingRateFails: no rate ⇒ session/payment cannot proceed, nothing minted.
func (s *InvoiceConversionFinalizeSuite) TestCheckoutPaymentMissingRateFails() {
	s.seedCustomer("cust_co_norate", lo.ToPtr("inr"))
	// no tenant rate seeded
	inv := s.seedDraftInvoice("inv_co_norate", "cust_co_norate", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_co_nr", "100")})

	_, err := s.checkoutSvcForConversion().createCheckoutPayment(s.ctx(), inv, types.CheckoutPaymentProviderRazorpay)
	s.Require().Error(err, "a checkout payment with no rate must fail before minting")
	s.Equal("usd", inv.Currency, "invoice must stay in the charge currency")
}
