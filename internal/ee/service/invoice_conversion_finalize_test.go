package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
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

	s.NoError(s.svc.convertAndRetaxInvoice(s.ctx(), inv))

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

func (s *InvoiceConversionFinalizeSuite) TestMissingRateStaysDraft() {
	s.seedCustomer("cust_5", lo.ToPtr("inr"))
	// no tenant rate
	inv := s.seedDraftInvoice("inv_norate", "cust_5", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_r1", "100")})

	err := s.svc.convertAndRetaxInvoice(s.ctx(), inv)
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "missing rate must be an invalid-operation error, got %v", err)
	// The resolver's not-found must not leak through: a double-marked error makes the HTTP status
	// non-deterministic (ResolveError picks whichever sentinel it meets first).
	s.False(ierr.IsNotFound(err), "missing rate must not also match not-found, got %v", err)
	s.Equal("usd", inv.Currency, "invoice must stay in the charge currency")
	s.Nil(inv.FxConversion)
}

// A one-off invoice is taxed at compute in the charge currency. Re-tax after conversion must
// rewrite the existing tax_applied row in the billing currency, not leave INR amounts labelled usd.
func (s *InvoiceConversionFinalizeSuite) TestOneOffRetaxRewritesTaxAppliedInBillingCurrency() {
	s.seedCustomer("cust_tax", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_tax", "cust_tax", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_t1", "100")})

	pct := decimal.RequireFromString("18")
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "GST",
		Code:            "gst_" + types.GenerateUUIDWithPrefix("code"),
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pct,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.ctx(), tr))

	// The row compute wrote, keyed exactly as processTaxApplication will look it up.
	key := idempotency.NewGenerator().GenerateKey(idempotency.ScopeTaxApplication, map[string]interface{}{
		"tax_rate_id": tr.ID,
		"entity_id":   inv.ID,
		"entity_type": string(types.TaxRateEntityTypeInvoice),
	})
	s.Require().NoError(s.GetStores().TaxAppliedRepo.Create(s.ctx(), &taxapplied.TaxApplied{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
		TaxRateID:      tr.ID,
		EntityType:     types.TaxRateEntityTypeInvoice,
		EntityID:       inv.ID,
		TaxableAmount:  decimal.RequireFromString("100"),
		TaxAmount:      decimal.RequireFromString("18"),
		TaxBehavior:    types.TaxBehaviorExclusive,
		Currency:       "usd",
		AppliedAt:      time.Now().UTC(),
		IdempotencyKey: lo.ToPtr(key),
		EnvironmentID:  types.GetEnvironmentID(s.ctx()),
		BaseModel:      types.GetDefaultBaseModel(s.ctx()),
	}))

	s.Require().NoError(s.svc.convertAndRetaxInvoice(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("1494").Equal(inv.TotalTax), "18%% of 8300, got %s", inv.TotalTax)

	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	rows, err := s.GetStores().TaxAppliedRepo.List(s.ctx(), filter)
	s.Require().NoError(err)
	s.Require().Len(rows, 1, "re-tax must update the existing row, not add a second one")
	s.Equal("inr", rows[0].Currency, "tax_applied must be rewritten in the billing currency")
	s.True(decimal.RequireFromString("1494").Equal(rows[0].TaxAmount), "tax_amount got %s", rows[0].TaxAmount)
	s.True(decimal.RequireFromString("8300").Equal(rows[0].TaxableAmount), "taxable_amount got %s", rows[0].TaxableAmount)
}

func (s *InvoiceConversionFinalizeSuite) TestConvertOnceRetrySkips() {
	s.seedCustomer("cust_6", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_once", "cust_6", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_o1", "100")})

	s.NoError(s.svc.convertAndRetaxInvoice(s.ctx(), inv))
	s.Require().NotNil(inv.FxConversion)
	firstRate := inv.FxConversion.Rate

	// A second pass must be a no-op even if a different rate now exists.
	s.seedTenantRate("inr", "usd", "0.5") // unrelated pair; the invoice is already inr
	s.NoError(s.svc.convertAndRetaxInvoice(s.ctx(), inv))
	s.Equal("inr", inv.Currency)
	s.True(firstRate.Equal(inv.FxConversion.Rate), "fx_conversion must not change on a second pass")
}

func (s *InvoiceConversionFinalizeSuite) TestAmountPaidNonZeroNoOp() {
	s.seedCustomer("cust_7", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_paid", "cust_7", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_p1", "100")})
	inv.AmountPaid = decimal.RequireFromString("100")

	s.NoError(s.svc.convertAndRetaxInvoice(s.ctx(), inv))
	s.Equal("usd", inv.Currency, "a paid invoice is not converted here (pay-first is a later PR)")
	s.Nil(inv.FxConversion)
}

// taxAppliedSpy records the currency of every tax_applied write so a test can tell one tax pass from two.
type taxAppliedSpy struct {
	taxapplied.Repository
	created []string
	updated []string
}

func (r *taxAppliedSpy) Create(ctx context.Context, ta *taxapplied.TaxApplied) error {
	r.created = append(r.created, ta.Currency)
	return r.Repository.Create(ctx, ta)
}

func (r *taxAppliedSpy) Update(ctx context.Context, ta *taxapplied.TaxApplied) error {
	r.updated = append(r.updated, ta.Currency)
	return r.Repository.Update(ctx, ta)
}

func (s *InvoiceConversionFinalizeSuite) TestFinalizeTaxesSubscriptionInvoiceOnce() {
	cases := []struct {
		name          string
		billing       *string
		wantCurrency  string
		wantTax       string
		wantTotal     string
		wantConverted bool
	}{
		{name: "converted: taxed once, in the billing currency", billing: lo.ToPtr("inr"), wantCurrency: "inr", wantTax: "1494", wantTotal: "9794", wantConverted: true},
		{name: "not converted: taxed once, in the charge currency", billing: nil, wantCurrency: "usd", wantTax: "18", wantTotal: "118"},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			spy := &taxAppliedSpy{Repository: s.GetStores().TaxAppliedRepo}
			params := s.svc.ServiceParams
			params.TaxAppliedRepo = spy
			svc := NewInvoiceService(params).(*invoiceService)

			s.seedCustomer("cust_once", tc.billing)
			s.seedTenantRate("usd", "inr", "83")
			s.seedDraftInvoice("inv_once_tax", "cust_once", "usd", types.InvoiceTypeSubscription, lo.ToPtr("sub_once"),
				[]*invoice.InvoiceLineItem{line("il_once", "100")})

			pct := decimal.RequireFromString("18")
			tr := &taxrate.TaxRate{
				ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
				Name:            "GST",
				Code:            "gst_" + types.GenerateUUIDWithPrefix("code"),
				TaxRateStatus:   types.TaxRateStatusActive,
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: &pct,
				EnvironmentID:   types.GetEnvironmentID(s.ctx()),
				BaseModel:       types.GetDefaultBaseModel(s.ctx()),
			}
			s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.ctx(), tr))
			s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(s.ctx(), &taxassociation.TaxAssociation{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
				TaxRateID:     tr.ID,
				EntityType:    types.TaxRateEntityTypeSubscription,
				EntityID:      "sub_once",
				StartDate:     time.Now().UTC().Add(-24 * time.Hour),
				Priority:      100,
				AutoApply:     true,
				Currency:      "usd",
				EnvironmentID: types.GetEnvironmentID(s.ctx()),
				BaseModel:     types.GetDefaultBaseModel(s.ctx()),
			}))

			s.Require().NoError(svc.FinalizeInvoice(s.ctx(), "inv_once_tax", dto.FinalizeInvoiceRequest{}))

			got, err := s.GetStores().InvoiceRepo.Get(s.ctx(), "inv_once_tax")
			s.Require().NoError(err)
			s.Equal(types.InvoiceStatusFinalized, got.InvoiceStatus)
			s.Equal(tc.wantCurrency, got.Currency)
			s.Equal(tc.wantConverted, got.FxConversion != nil)
			s.True(decimal.RequireFromString(tc.wantTax).Equal(got.TotalTax), "total_tax got %s", got.TotalTax)
			s.True(decimal.RequireFromString(tc.wantTotal).Equal(got.Total), "total got %s", got.Total)

			s.Equal([]string{tc.wantCurrency}, spy.created, "exactly one tax_applied row, written in the final currency")
			s.Empty(spy.updated, "no second tax pass rewriting the row")
		})
	}
}

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

// countingFXRateRepo wraps an fxrate.Repository and counts every read so a test can prove that
// finalizing an unaffected invoice never touches fx_rates (§1.5).
type countingFXRateRepo struct {
	fxrate.Repository
	reads int
}

func (c *countingFXRateRepo) Get(ctx context.Context, id string) (*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.Get(ctx, id)
}

func (c *countingFXRateRepo) List(ctx context.Context, f *types.FXRateFilter) ([]*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.List(ctx, f)
}

func (c *countingFXRateRepo) Count(ctx context.Context, f *types.FXRateFilter) (int, error) {
	c.reads++
	return c.Repository.Count(ctx, f)
}

func (c *countingFXRateRepo) GetTenantRate(ctx context.Context, from, to string) (*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.GetTenantRate(ctx, from, to)
}

func (c *countingFXRateRepo) FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, vf, vt *time.Time, excludeID string) ([]*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.FindOverlapping(ctx, scope, scopeID, from, to, vf, vt, excludeID)
}

func (s *InvoiceConversionFinalizeSuite) newServiceWithFXSpy() (*invoiceService, *countingFXRateRepo) {
	spy := &countingFXRateRepo{Repository: s.GetStores().FXRateRepo}
	params := s.svc.ServiceParams
	params.FXRateRepo = spy
	return NewInvoiceService(params).(*invoiceService), spy
}

// TestExistingCustomersUnaffected proves §1.5: finalizing an invoice for a customer with no billing
// currency, or one equal to the charge currency, issues no fx_rates query and leaves the invoice
// byte-for-byte unchanged — even when a tenant rate exists.
func (s *InvoiceConversionFinalizeSuite) TestExistingCustomersUnaffected() {
	cases := []struct {
		name    string
		billing *string
	}{
		{name: "no billing currency", billing: nil},
		{name: "billing currency equals charge currency", billing: lo.ToPtr("usd")},
	}

	for i, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.seedCustomer("cust_un", tc.billing)
			// A tenant rate exists, so the test proves it is deliberately NOT consulted.
			s.seedTenantRate("usd", "inr", "83")

			inv := s.seedDraftInvoice("inv_un", "cust_un", "usd", types.InvoiceTypeOneOff, nil,
				[]*invoice.InvoiceLineItem{line("il_u1", "60"), line("il_u2", "40")})

			svc, spy := s.newServiceWithFXSpy()
			s.NoError(svc.convertAndRetaxInvoice(s.ctx(), inv))

			s.Equal(0, spy.reads, "case %d: finalize must not query fx_rates", i)
			s.Equal("usd", inv.Currency)
			s.Nil(inv.FxConversion)
			s.True(decimal.RequireFromString("100").Equal(inv.Subtotal))
			for _, li := range inv.LineItems {
				s.Equal("usd", li.Currency)
				s.Nil(li.OriginalCurrency, "case %d: no line original should be stamped", i)
				s.Nil(li.OriginalAmount)
			}
		})
	}
}
