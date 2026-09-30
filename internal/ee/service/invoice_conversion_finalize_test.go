package service

import (
	"context"
	"testing"

	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type InvoiceConversionFinalizeSuite struct {
	testutil.BaseServiceTestSuite
	svc *invoiceService
}

func TestInvoiceConversionFinalize(t *testing.T) {
	suite.Run(t, new(InvoiceConversionFinalizeSuite))
}

func (s *InvoiceConversionFinalizeSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		SubRepo:             s.GetStores().SubscriptionRepo,
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		FXRateRepo:          s.GetStores().FXRateRepo,
		TaxRateRepo:         s.GetStores().TaxRateRepo,
		TaxAppliedRepo:      s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:  s.GetStores().TaxAssociationRepo,
		SettingsRepo:        s.GetStores().SettingsRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}).(*invoiceService)
}

func (s *InvoiceConversionFinalizeSuite) ctx() context.Context {
	return types.SetEnvironmentID(s.GetContext(), "env_test")
}

func (s *InvoiceConversionFinalizeSuite) seedCustomer(id string, billing *string) {
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx(), &domainCustomer.Customer{
		ID:              id,
		ExternalID:      id,
		Name:            "C " + id,
		BillingCurrency: billing,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}))
}

func (s *InvoiceConversionFinalizeSuite) seedTenantRate(from, to, rate string) {
	s.NoError(s.GetStores().FXRateRepo.Create(s.ctx(), &fxrate.FXRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FX_RATE),
		Scope:         types.FXRateScopeTenant,
		ScopeID:       types.FXRateScopeIDTenant,
		FromCurrency:  from,
		ToCurrency:    to,
		Rate:          decimal.RequireFromString(rate),
		EnvironmentID: types.GetEnvironmentID(s.ctx()),
		BaseModel:     types.GetDefaultBaseModel(s.ctx()),
	}))
}

func (s *InvoiceConversionFinalizeSuite) seedDraftInvoice(id, customerID, currency string, invType types.InvoiceType, subID *string, lines []*invoice.InvoiceLineItem) *invoice.Invoice {
	subtotal := decimal.Zero
	for _, li := range lines {
		li.InvoiceID = id
		li.CustomerID = customerID
		li.Currency = currency
		li.EnvironmentID = types.GetEnvironmentID(s.ctx())
		li.BaseModel = types.GetDefaultBaseModel(s.ctx())
		subtotal = subtotal.Add(li.Amount)
	}
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      customerID,
		SubscriptionID:  subID,
		InvoiceType:     invType,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        currency,
		Subtotal:        subtotal,
		Total:           subtotal,
		AmountDue:       subtotal,
		AmountRemaining: subtotal,
		LineItems:       lines,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.ctx(), inv))
	return inv
}

func line(id, amount string) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:       id,
		Amount:   decimal.RequireFromString(amount),
		Quantity: decimal.NewFromInt(1),
	}
}

func (s *InvoiceConversionFinalizeSuite) TestOneOffConverts() {
	s.seedCustomer("cust_1", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_oneoff", "cust_1", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_1", "60"), line("il_2", "40")})

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("8300").Equal(inv.Subtotal), "subtotal got %s", inv.Subtotal)
	s.True(decimal.RequireFromString("8300").Equal(inv.AmountDue), "amount_due got %s", inv.AmountDue)
	s.True(inv.TotalTax.IsZero(), "no tax configured, total_tax got %s", inv.TotalTax)
	s.Require().NotNil(inv.FxConversion)
	s.Equal("usd", inv.FxConversion.ChargeCurrency)
	s.Equal("inr", inv.FxConversion.BillingCurrency)
	s.True(decimal.RequireFromString("83").Equal(inv.FxConversion.Rate))
	s.Equal(string(types.FXRateScopeTenant), inv.FxConversion.Scope)

	// Line originals persisted.
	stored, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.ctx(), "inv_oneoff")
	s.NoError(err)
	s.Len(stored, 2)
	for _, li := range stored {
		s.Equal("inr", li.Currency)
		s.Require().NotNil(li.OriginalCurrency)
		s.Equal("usd", *li.OriginalCurrency)
		s.Require().NotNil(li.OriginalAmount)
	}
}

func (s *InvoiceConversionFinalizeSuite) TestSubscriptionConverts() {
	s.seedCustomer("cust_2", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_sub", "cust_2", "usd", types.InvoiceTypeSubscription, lo.ToPtr("sub_x"),
		[]*invoice.InvoiceLineItem{line("il_s1", "100")})

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("8300").Equal(inv.AmountDue))
	s.Require().NotNil(inv.FxConversion)
}

func (s *InvoiceConversionFinalizeSuite) TestNoBillingCurrencyNoOp() {
	s.seedCustomer("cust_3", nil)
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_nobc", "cust_3", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_n1", "100")})

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))
	s.Equal("usd", inv.Currency)
	s.Nil(inv.FxConversion)
}

func (s *InvoiceConversionFinalizeSuite) TestMatchingCurrencyNoOp() {
	s.seedCustomer("cust_4", lo.ToPtr("usd"))
	inv := s.seedDraftInvoice("inv_match", "cust_4", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_m1", "100")})

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))
	s.Equal("usd", inv.Currency)
	s.Nil(inv.FxConversion)
}

func (s *InvoiceConversionFinalizeSuite) TestMissingRateStaysDraft() {
	s.seedCustomer("cust_5", lo.ToPtr("inr"))
	// no tenant rate
	inv := s.seedDraftInvoice("inv_norate", "cust_5", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_r1", "100")})

	err := s.svc.convertAndRetaxAtFinalize(s.ctx(), inv)
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "missing rate must be an invalid-operation error, got %v", err)
	s.Equal("usd", inv.Currency, "invoice must stay in the charge currency")
	s.Nil(inv.FxConversion)
}

func (s *InvoiceConversionFinalizeSuite) TestConvertOnceRetrySkips() {
	s.seedCustomer("cust_6", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_once", "cust_6", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_o1", "100")})

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))
	s.Require().NotNil(inv.FxConversion)
	firstRate := inv.FxConversion.Rate

	// A second pass must be a no-op even if a different rate now exists.
	s.seedTenantRate("inr", "usd", "0.5") // unrelated pair; the invoice is already inr
	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))
	s.Equal("inr", inv.Currency)
	s.True(firstRate.Equal(inv.FxConversion.Rate), "fx_conversion must not change on a second pass")
}

func (s *InvoiceConversionFinalizeSuite) TestAmountPaidNonZeroNoOp() {
	s.seedCustomer("cust_7", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_paid", "cust_7", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_p1", "100")})
	inv.AmountPaid = decimal.RequireFromString("100")

	s.NoError(s.svc.convertAndRetaxAtFinalize(s.ctx(), inv))
	s.Equal("usd", inv.Currency, "a paid invoice is not converted here (pay-first is a later PR)")
	s.Nil(inv.FxConversion)
}
