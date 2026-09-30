package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// TestRejectPrepaidCrossCurrencyOneOff covers the §8.4 create-time guard.
func (s *InvoiceConversionFinalizeSuite) TestRejectPrepaidCrossCurrencyOneOff() {
	s.seedCustomer("cust_pp_inr", lo.ToPtr("inr"))
	s.seedCustomer("cust_pp_usd", lo.ToPtr("usd"))
	s.seedCustomer("cust_pp_none", nil)

	paid := lo.ToPtr(types.PaymentStatusSucceeded)
	amt := lo.ToPtr(decimal.RequireFromString("100"))

	cases := []struct {
		name      string
		req       dto.CreateInvoiceRequest
		expectErr bool
	}{
		{
			name:      "prepaid + cross-currency rejected",
			req:       dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd", PaymentStatus: paid},
			expectErr: true,
		},
		{
			name:      "amount_paid + cross-currency rejected",
			req:       dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd", AmountPaid: amt},
			expectErr: true,
		},
		{
			name: "prepaid + same-currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_usd", Currency: "usd", PaymentStatus: paid},
		},
		{
			name: "prepaid + no billing currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_none", Currency: "usd", PaymentStatus: paid},
		},
		{
			name: "no payment + cross-currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd"},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			err := s.svc.rejectPrepaidCrossCurrencyOneOff(s.ctx(), tc.req)
			if tc.expectErr {
				s.Error(err)
				s.True(ierr.IsValidation(err))
			} else {
				s.NoError(err)
			}
		})
	}
}

// TestPaymentBeforeConversionRejected covers the §8.4 payment-eligibility guard.
func (s *InvoiceConversionFinalizeSuite) TestPaymentBeforeConversionRejected() {
	paySvc := NewPaymentService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		SubRepo:             s.GetStores().SubscriptionRepo,
		CheckoutSessionRepo: s.GetStores().CheckoutSessionRepo,
		PaymentRepo:         s.GetStores().PaymentRepo,
		SettingsRepo:        s.GetStores().SettingsRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}).(*paymentService)

	s.seedCustomer("cust_pay_inr", lo.ToPtr("inr"))

	// An unconverted DRAFT for a cross-currency customer cannot be paid.
	draft := s.seedDraftInvoice("inv_pay_draft", "cust_pay_inr", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_pay1", "100")})
	err := paySvc.validateInvoicePaymentEligibility(s.ctx(), draft, &dto.CreatePaymentRequest{
		DestinationID: draft.ID,
		Currency:      "usd",
		Amount:        decimal.RequireFromString("100"),
	})
	s.Error(err, "paying an unconverted cross-currency draft must be rejected")
	s.True(ierr.IsValidation(err))

	// Once converted (fx_conversion set, currency now inr), a matching payment is allowed by the guard.
	converted := s.seedDraftInvoice("inv_pay_converted", "cust_pay_inr", "inr", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_pay2", "8300")})
	converted.FxConversion = &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "inr", Rate: decimal.RequireFromString("83")}
	err = paySvc.validateInvoicePaymentEligibility(s.ctx(), converted, &dto.CreatePaymentRequest{
		DestinationID: converted.ID,
		Currency:      "inr",
		Amount:        decimal.RequireFromString("8300"),
	})
	s.NoError(err, "a converted invoice can be paid in the billing currency")
}
