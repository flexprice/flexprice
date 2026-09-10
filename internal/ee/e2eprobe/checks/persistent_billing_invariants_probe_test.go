package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

// pbiSeeds returns Seeds populated with 2 persistent customers plus the
// shared tax + coupon IDs — the minimum for the probe to run its assertions.
func pbiSeeds() e2eprobe.Seeds {
	return e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"e2eprobe-cust-persistent-0", "e2eprobe-cust-persistent-1"},
		SharedTaxRateID:       "taxrate_1",
		SharedCouponID:        "coupon_1",
	}
}

func TestPersistentBillingInvariantsProbe_ListOmitsTaxesUsesGet(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Production shape: POST /invoices/search does not expand tax_applied, so
	// the list row has an ID and empty Taxes. GET /invoices/{id} loads them.
	invID := "inv_tax_1"
	trID := "taxrate_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{{ID: &invID}}
	fc.invoices.getByID = map[string]sdktypes.InvoiceResponse{
		invID: {
			ID:                 &invID,
			Taxes:              []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}},
			CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}},
		},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("list-without-taxes + get-with-taxes must pass; got %v", err)
	}
	if fc.invoices.lastFilter.InvoiceType == nil || *fc.invoices.lastFilter.InvoiceType != sdktypes.InvoiceTypeSubscription {
		t.Fatalf("Query must filter InvoiceType=SUBSCRIPTION so wallet ONE_OFF invoices are ignored")
	}
}

func TestPersistentBillingInvariantsProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Both custs have an invoice: cust #0 has the tax, cust #1 has the coupon.
	invID := "inv_1"
	trID := "taxrate_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &invID, Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}, CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

func TestPersistentBillingInvariantsProbe_NoInvoiceSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	// No invoices set — fake returns empty response.

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty invoice list must soft-skip; got %v", err)
	}
}

func TestPersistentBillingInvariantsProbe_MissingTaxFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Invoice has coupon but NOT the tax — expected step=assert_tax_present_cust0.
	invID := "inv_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &invID, CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when invoice missing tax rate, got nil")
	}
}

func TestPersistentBillingInvariantsProbe_MissingCouponFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Invoice has tax but NOT the coupon — expected step=assert_coupon_present_cust1.
	invID := "inv_1"
	trID := "taxrate_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &invID, Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when invoice missing coupon, got nil")
	}
}

func TestPersistentBillingInvariantsProbe_MissingSeedsSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	// PersistentCustomerIDs < 2 → soft skip.
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty seeds must soft-skip; got %v", err)
	}
}
