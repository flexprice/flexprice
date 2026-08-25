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

func TestPersistentBillingInvariantsProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Both custs have an invoice: cust #0 has the tax, cust #1 has the coupon.
	trID := "taxrate_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}, CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
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
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
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
	trID := "taxrate_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}},
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
