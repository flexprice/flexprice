package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

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
	svc := NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		SubRepo:             s.GetStores().SubscriptionRepo,
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		FXRateRepo:          spy,
		TaxRateRepo:         s.GetStores().TaxRateRepo,
		TaxAppliedRepo:      s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:  s.GetStores().TaxAssociationRepo,
		SettingsRepo:        s.GetStores().SettingsRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}).(*invoiceService)
	return svc, spy
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
			s.NoError(svc.convertAndRetaxAtFinalize(s.ctx(), inv))

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
