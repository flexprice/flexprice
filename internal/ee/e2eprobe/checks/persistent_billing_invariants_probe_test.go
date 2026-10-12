package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

// pbiSeeds returns Seeds populated with 2 persistent customers plus the
// shared tax + coupon IDs — the minimum for the probe to run its assertions.
// TaxedSubID and TaxAssociationActiveFrom mirror what the seed records once it
// has attached the shared tax association; activation is backdated so every
// fixture invoice falls after it.
func pbiSeeds() e2eprobe.Seeds {
	return e2eprobe.Seeds{
		PersistentCustomerIDs:    []string{"e2eprobe-cust-persistent-0", "e2eprobe-cust-persistent-1"},
		SharedTaxRateID:          "taxrate_1",
		SharedCouponID:           "coupon_1",
		TaxedSubID:               taxedSubID,
		TaxAssociationActiveFrom: &taxActiveFrom,
	}
}

var (
	taxedSubID      = "subs_taxed"
	taxActiveFrom   = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	taxFinalizedAt  = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	finalizedStatus = sdktypes.InvoiceStatusFinalized
)

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
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &invID, SubscriptionID: &taxedSubID, InvoiceStatus: &finalizedStatus, FinalizedAt: &taxFinalizedAt},
	}
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
		{ID: &invID, SubscriptionID: &taxedSubID, InvoiceStatus: &finalizedStatus, FinalizedAt: &taxFinalizedAt,
			Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}, CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
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
		{ID: &invID, SubscriptionID: &taxedSubID, InvoiceStatus: &finalizedStatus, FinalizedAt: &taxFinalizedAt,
			CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when invoice missing tax rate, got nil")
	}
}

func TestPersistentBillingInvariantsProbe_MissingCouponFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	seeds := pbiSeeds()
	seeds.PersistentSubIDs = []string{"sub_0", "sub_1"}
	reg.LoadSeeds(seeds)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	couponID := "coupon_1"
	subID := "sub_1"
	fc.couponAssociations.resp = &sdkdtos.ListCouponAssociationsResponse{
		ListCouponAssociationsResponse: &sdktypes.ListCouponAssociationsResponse{
			Items: []sdktypes.CouponAssociationResponse{{CouponID: &couponID, SubscriptionID: &subID}},
		},
	}

	// Invoice has tax but NOT the coupon — expected step=assert_coupon_present_cust1.
	invID := "inv_1"
	trID := "taxrate_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &invID, Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error when invoice missing coupon, got nil")
	}
	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil || attrs["step"] != "assert_coupon_present_cust1" {
		t.Fatalf("expected step=assert_coupon_present_cust1, got %v", attrs)
	}
}

func TestPersistentBillingInvariantsProbe_OnceCadenceOlderInvoiceHasCoupon(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	trID := "taxrate_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}},
		{CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("ONCE coupon on an older invoice must pass; got %v", err)
	}
	if fc.invoices.lastFilter.Order == nil || *fc.invoices.lastFilter.Order != sdktypes.InvoiceFilterOrderAsc {
		t.Fatalf("coupon query must use order=asc, got %v", fc.invoices.lastFilter.Order)
	}
	if fc.invoices.lastFilter.Limit == nil || *fc.invoices.lastFilter.Limit != 1 {
		t.Fatalf("coupon query must use limit=1, got %v", fc.invoices.lastFilter.Limit)
	}
}

func TestPersistentBillingInvariantsProbe_OnceCadenceOldestBeyondNewest50(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	seeds := pbiSeeds()
	seeds.PersistentSubIDs = []string{"sub_0", "sub_1"}
	reg.LoadSeeds(seeds)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	couponID := "coupon_1"
	subID := "sub_1"
	fc.couponAssociations.resp = &sdkdtos.ListCouponAssociationsResponse{
		ListCouponAssociationsResponse: &sdktypes.ListCouponAssociationsResponse{
			Items: []sdktypes.CouponAssociationResponse{{CouponID: &couponID, SubscriptionID: &subID}},
		},
	}

	trID := "taxrate_1"
	const n = 51
	invoices := make([]sdktypes.InvoiceResponse, n)
	invoices[0] = sdktypes.InvoiceResponse{Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}}
	invoices[n-1] = sdktypes.InvoiceResponse{CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}}
	fc.invoices.invoices = invoices

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("ONCE coupon on the oldest invoice (beyond newest 50) must pass; got %v", err)
	}
}

func TestPersistentBillingInvariantsProbe_NoCouponAssociationSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	seeds := pbiSeeds()
	seeds.PersistentSubIDs = []string{"sub_0", "sub_1"}
	reg.LoadSeeds(seeds)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	trID := "taxrate_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("no coupon association on the persistent sub must soft-skip; got %v", err)
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

func TestPersistentBillingInvariantsProbe_TaxSkipsUnfinalizedDraft(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(pbiSeeds())
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	draft, finalized := sdktypes.InvoiceStatusDraft, sdktypes.InvoiceStatusFinalized
	draftID, finalID := "inv_draft", "inv_final"
	trID := "taxrate_1"
	couponID := "coupon_1"
	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		{ID: &draftID, InvoiceStatus: &draft},
		{ID: &finalID, InvoiceStatus: &finalized, Taxes: []sdktypes.TaxAppliedResponse{{TaxRateID: &trID}}, CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &couponID}}},
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("a newer untaxed draft must not fail the tax check; got %v", err)
	}
}

// taxSeeds pins the taxed sub and an activation time, the shape the seed now
// records after attaching the shared tax association.
func taxSeeds(activeFrom time.Time) e2eprobe.Seeds {
	s := pbiSeeds()
	s.TaxAssociationActiveFrom = &activeFrom
	return s
}

func finalizedInvoice(id, subID string, finalizedAt time.Time, taxRateID string) sdktypes.InvoiceResponse {
	status := sdktypes.InvoiceStatusFinalized
	inv := sdktypes.InvoiceResponse{
		ID:             &id,
		SubscriptionID: &subID,
		InvoiceStatus:  &status,
		FinalizedAt:    &finalizedAt,
	}
	if taxRateID != "" {
		inv.Taxes = []sdktypes.TaxAppliedResponse{{TaxRateID: &taxRateID}}
	}
	return inv
}

// Cust #0 owns the taxed monthly sub AND the untaxed multi-cadence quarterly
// sub. The quarterly sub's invoice is newest, but it carries no association and
// never will — asserting on it alarms forever.
func TestPersistentBillingInvariantsProbe_IgnoresOtherSubsInvoice(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	activeFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	reg.LoadSeeds(taxSeeds(activeFrom))
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		finalizedInvoice("inv_quarterly", "subs_other", activeFrom.AddDate(0, 0, 5), ""),
		finalizedInvoice("inv_monthly", "subs_taxed", activeFrom.AddDate(0, 0, 2), "taxrate_1"),
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("another sub's untaxed invoice must not fail the taxed sub's invariant; got %v", err)
	}
}

// Tax is stamped at finalize and never backfilled, so an invoice finalized
// before the association existed is untaxed forever.
func TestPersistentBillingInvariantsProbe_IgnoresInvoicePredatingAssociation(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	activeFrom := time.Date(2026, 10, 8, 5, 31, 0, 0, time.UTC)
	reg.LoadSeeds(taxSeeds(activeFrom))
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		finalizedInvoice("inv_opening", "subs_taxed", activeFrom.Add(-6*time.Hour), ""),
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("invoice finalized before the association must soft-skip; got %v", err)
	}
}

// The regression this all protects: an invoice of the taxed sub, finalized
// after the association went active, with no tax on it.
func TestPersistentBillingInvariantsProbe_MissingTaxAfterAssociationFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	activeFrom := time.Date(2026, 10, 8, 5, 31, 0, 0, time.UTC)
	reg.LoadSeeds(taxSeeds(activeFrom))
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		finalizedInvoice("inv_cycle", "subs_taxed", activeFrom.AddDate(0, 1, 0), ""),
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected failure when the taxed sub's post-association invoice has no tax")
	}
}

// Until the seed pins TaxedSubID there is nothing to assert against.
func TestPersistentBillingInvariantsProbe_NoTaxedSubSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	seeds := pbiSeeds()
	seeds.TaxedSubID = ""
	reg.LoadSeeds(seeds)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	fc.invoices.invoices = []sdktypes.InvoiceResponse{
		finalizedInvoice("inv_1", taxedSubID, time.Now().UTC(), ""),
	}

	p := NewPersistentBillingInvariantsProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("unpinned taxed sub must soft-skip; got %v", err)
	}
}
