package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestRegisterInvoices_RegistersEveryFamily(t *testing.T) {
	cat := scenario.NewCatalog()
	registerInvoices(cat)
	want := map[string]e2eprobe.Trait{
		"invoice-freshness":     e2eprobe.TraitSubscription,
		"invoice-tax":           e2eprobe.TraitTax,
		"invoice-coupon":        e2eprobe.TraitCoupon,
		"invoice-commitment":    e2eprobe.TraitCommitment,
		"invoice-multi-cadence": e2eprobe.TraitMultiCadence,
	}
	for name, trait := range want {
		info, ok := cat.Scenario(name)
		if !ok {
			t.Errorf("%s not registered", name)
			continue
		}
		if info.Kind != e2eprobe.KindProbe || info.Every != time.Hour || len(info.Traits) != 1 || info.Traits[0] != trait {
			t.Errorf("%s = %+v", name, info)
		}
	}
}

func TestInvoiceFreshness_PassFreshInvoice(t *testing.T) {
	d := newTestDeps(t)
	d.advance(40 * 24 * time.Hour)
	p := personaOf(t, d, "baseline-a")
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_fresh", types.InvoiceStatusFinalized, d.now.Add(-5*24*time.Hour)),
	}
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if r.Attrs["subscription_id"] != p.SubscriptionID() || r.Attrs["latest_period_end"] == "" || r.Attrs["lag"] == "" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	if q := d.fc.Invoice.Queries[0]; q.CustomerID == nil || *q.CustomerID != p.CustomerID() || q.SubscriptionID != nil {
		t.Errorf("invoices must be queried by customer and filtered here: %+v", q)
	}
}

func TestInvoiceFreshness_FailStale(t *testing.T) {
	d := newTestDeps(t)
	d.advance(120 * 24 * time.Hour)
	p := personaOf(t, d, "baseline-a")
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_stale", types.InvoiceStatusFinalized, d.now.Add(-75*24*time.Hour)),
	}
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "freshness" || len(r.Mismatches) != 1 || !strings.Contains(r.Mismatches[0], "invoice freshness") {
		t.Fatalf("result = %+v", r)
	}
	if r.Attrs["lag"] == "" || r.Attrs["latest_period_end"] == "" {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestInvoiceFreshness_FailOldSubNoInvoice(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "baseline-a")
	sub := d.fc.Subscription.Items[p.SubscriptionID()]
	sub.CreatedAt = probetest.Ptr(d.now.Add(-90 * 24 * time.Hour))
	d.fc.Subscription.Items[p.SubscriptionID()] = sub
	// Another subscription's fresh invoice must not count for this one.
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, "sub_other", "inv_other", types.InvoiceStatusFinalized, d.now.Add(-time.Hour)),
	}
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "freshness" || !strings.Contains(r.Reason, "no invoice after one cycle") {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceFreshness_SkipYoungSubNoInvoice(t *testing.T) {
	d := newTestDeps(t)
	d.advance(time.Hour)
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusSkip || r.Step != "young" {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceFreshness_SkipsWhenSubscriptionMissing(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.GetErr = probetest.NotFound()
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusSkip || r.Step != "subscription" {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Invoice.Queries) != 0 {
		t.Error("a missing subscription must not query invoices")
	}
}

func TestInvoiceFreshness_InfraErrorIsFail(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Invoice.QueryErr = probetest.ServerError(503)
	r := run(t, d, newInvoiceFreshness(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "invoices" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
