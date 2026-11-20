package scenarios

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	itypes "github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func personaOf(t *testing.T, d *testDeps, name string) *world.Persona {
	t.Helper()
	p, ok := d.World.Persona(name)
	if !ok {
		t.Fatalf("no persona %s", name)
	}
	return p
}

// subInvoice is an invoice of one of p's subscriptions whose period ended at end.
func subInvoice(p *world.Persona, subID, id string, status types.InvoiceStatus, end time.Time) types.InvoiceResponse {
	return types.InvoiceResponse{
		ID:             probetest.Ptr(id),
		CustomerID:     probetest.Ptr(p.CustomerID()),
		SubscriptionID: probetest.Ptr(subID),
		InvoiceStatus:  &status,
		PeriodStart:    probetest.Ptr(end.AddDate(0, -1, 0)),
		PeriodEnd:      probetest.Ptr(end),
		CreatedAt:      probetest.Ptr(end),
	}
}

func invoiceLine(priceType types.PriceType, name, amount string) types.InvoiceLineItemResponse {
	return types.InvoiceLineItemResponse{PriceType: probetest.Ptr(string(priceType)), DisplayName: probetest.Ptr(name), Amount: probetest.Ptr(amount)}
}

// trueUpLine is the server's commitment true-up line: a FIXED charge flagged in metadata.
func trueUpLine(amount string) types.InvoiceLineItemResponse {
	li := invoiceLine(types.PriceTypeFixed, "E2EProbe Plan True Up", amount)
	li.Metadata = map[string]string{itypes.MetadataKeyIsCommitmentTrueup: itypes.MetadataValueTrue}
	return li
}

func sourcedLine(sourceID string) types.InvoiceLineItemResponse {
	li := invoiceLine(types.PriceTypeFixed, "E2EProbe Base Fee", "19.99")
	li.SubscriptionLineItemID = probetest.Ptr(sourceID)
	return li
}

func enableTrueUp(d *testDeps, subID string) {
	sub := d.fc.Subscription.Items[subID]
	sub.EnableTrueUp = probetest.Ptr(true)
	d.fc.Subscription.Items[subID] = sub
}

// ── invoice-tax ───────────────────────────────────────────────────────

func TestInvoiceTax_SkipsBeforeActivation(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "taxed")
	activeFrom := p.TaxActiveFrom()
	if activeFrom.IsZero() {
		t.Fatal("provisioning must pin when the tax association became active")
	}
	// Tax is stamped at finalize, so an invoice from before the association never carries it.
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_before", types.InvoiceStatusFinalized, activeFrom.Add(-time.Hour)),
	}
	r := run(t, d, newInvoiceTax(d.Deps), "taxed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "no_invoice" {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceTax_PassesWithRate(t *testing.T) {
	cases := []struct {
		name  string
		taxes func(fx *world.Fixtures) []types.TaxAppliedResponse
	}{
		{"matched by rate id", func(fx *world.Fixtures) []types.TaxAppliedResponse {
			return []types.TaxAppliedResponse{{TaxRateID: probetest.Ptr(fx.TaxRateID()), TaxableAmount: probetest.Ptr("100.00"), TaxAmount: probetest.Ptr("10.00")}}
		}},
		{"matched by rate code", func(fx *world.Fixtures) []types.TaxAppliedResponse {
			return []types.TaxAppliedResponse{{TaxRate: &types.TaxRateResponse{Code: probetest.Ptr(fx.TaxRateCode())}, TaxableAmount: probetest.Ptr("20.99"), TaxAmount: probetest.Ptr("2.10")}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			d.advance(48 * time.Hour)
			p := personaOf(t, d, "taxed")
			listed := subInvoice(p, p.SubscriptionID(), "inv_taxed", types.InvoiceStatusFinalized, d.now.Add(-time.Hour))
			// Search responses omit taxes; only GET carries them. A newer draft is not yet taxed and must be ignored.
			d.fc.Invoice.Items = []types.InvoiceResponse{
				subInvoice(p, p.SubscriptionID(), "inv_draft", types.InvoiceStatusDraft, d.now),
				listed,
			}
			full := listed
			full.Taxes = tc.taxes(d.World.Fixtures())
			d.fc.Invoice.ByID["inv_taxed"] = full
			r := run(t, d, newInvoiceTax(d.Deps), "taxed")
			if r.Status != e2eprobe.StatusPass {
				t.Fatalf("result = %+v", r)
			}
			if r.Attrs["invoice_id"] != "inv_taxed" || r.Attrs["subscription_id"] != p.SubscriptionID() {
				t.Errorf("attrs = %v", r.Attrs)
			}
		})
	}
}

func TestInvoiceTax_FailsWhenRateMissing(t *testing.T) {
	cases := []struct {
		name     string
		taxes    func(fx *world.Fixtures) []types.TaxAppliedResponse
		mismatch string
	}{
		{"another rate applied", func(*world.Fixtures) []types.TaxAppliedResponse {
			return []types.TaxAppliedResponse{{TaxRateID: probetest.Ptr("taxrate_other"), TaxRate: &types.TaxRateResponse{Code: probetest.Ptr("other")}, TaxableAmount: probetest.Ptr("100.00"), TaxAmount: probetest.Ptr("10.00")}}
		}, "shared tax rate"},
		{"no tax at all", func(*world.Fixtures) []types.TaxAppliedResponse { return nil }, "shared tax rate"},
		{"wrong amount", func(fx *world.Fixtures) []types.TaxAppliedResponse {
			return []types.TaxAppliedResponse{{TaxRateID: probetest.Ptr(fx.TaxRateID()), TaxableAmount: probetest.Ptr("100.00"), TaxAmount: probetest.Ptr("5.00")}}
		}, "tax amount: want 10.00, got 5.00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			d.advance(48 * time.Hour)
			p := personaOf(t, d, "taxed")
			listed := subInvoice(p, p.SubscriptionID(), "inv_taxed", types.InvoiceStatusFinalized, d.now.Add(-time.Hour))
			d.fc.Invoice.Items = []types.InvoiceResponse{listed}
			full := listed
			full.Taxes = tc.taxes(d.World.Fixtures())
			d.fc.Invoice.ByID["inv_taxed"] = full
			r := run(t, d, newInvoiceTax(d.Deps), "taxed")
			if r.Status != e2eprobe.StatusFail || r.Step != "assert_tax" || !strings.Contains(r.Reason, tc.mismatch) {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

// ── invoice-coupon ────────────────────────────────────────────────────

func TestInvoiceContent_SkipsWhenCouponGap(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "couponed")
	// Detach the coupon as if the subscription predated it; re-provisioning records the gap.
	kept := d.fc.CouponAssoc.Items[:0]
	for _, ca := range d.fc.CouponAssoc.Items {
		if ca.SubscriptionID == nil || *ca.SubscriptionID != p.SubscriptionID() {
			kept = append(kept, ca)
		}
	}
	d.fc.CouponAssoc.Items = kept
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Gap(e2eprobe.TraitCoupon) == "" {
		t.Fatal("re-provisioning must record the coupon gap")
	}
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_1", types.InvoiceStatusFinalized, d.now),
	}
	r := run(t, d, newInvoiceCoupon(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "gap" || !strings.Contains(r.Reason, "predates") {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceCoupon_PassOnOldestInvoice(t *testing.T) {
	d := newTestDeps(t)
	d.advance(70 * 24 * time.Hour)
	p := personaOf(t, d, "couponed")
	first := subInvoice(p, p.SubscriptionID(), "inv_first", types.InvoiceStatusFinalized, d.now.Add(-40*24*time.Hour))
	first.CouponApplications = []types.CouponApplicationResponse{{CouponID: probetest.Ptr(d.World.Fixtures().CouponID())}}
	// The coupon is ONCE cadence: the second invoice never carries it.
	second := subInvoice(p, p.SubscriptionID(), "inv_second", types.InvoiceStatusFinalized, d.now.Add(-10*24*time.Hour))
	d.fc.Invoice.Items = []types.InvoiceResponse{second, first}
	r := run(t, d, newInvoiceCoupon(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if r.Attrs["invoice_id"] != "inv_first" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	q := d.fc.Invoice.Queries[0]
	if q.SubscriptionID == nil || *q.SubscriptionID != p.SubscriptionID() || q.Order == nil || *q.Order != types.InvoiceFilterOrderAsc || q.Limit == nil || *q.Limit != 1 {
		t.Errorf("the oldest invoice is asked for by subscription, ascending, limit 1: %+v", q)
	}
}

func TestInvoiceCoupon_FallsBackToCustomerQuery(t *testing.T) {
	d := newTestDeps(t)
	d.advance(70 * 24 * time.Hour)
	d.fc.Invoice.SubscriptionFilterEmpty = true
	p := personaOf(t, d, "couponed")
	first := subInvoice(p, p.SubscriptionID(), "inv_first", types.InvoiceStatusFinalized, d.now.Add(-40*24*time.Hour))
	first.CouponApplications = []types.CouponApplicationResponse{{CouponID: probetest.Ptr(d.World.Fixtures().CouponID())}}
	second := subInvoice(p, p.SubscriptionID(), "inv_second", types.InvoiceStatusFinalized, d.now.Add(-10*24*time.Hour))
	d.fc.Invoice.Items = []types.InvoiceResponse{second, first}
	r := run(t, d, newInvoiceCoupon(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusPass || r.Attrs["invoice_id"] != "inv_first" {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Invoice.Queries) != 2 || d.fc.Invoice.Queries[1].CustomerID == nil || *d.fc.Invoice.Queries[1].CustomerID != p.CustomerID() {
		t.Errorf("queries = %+v", d.fc.Invoice.Queries)
	}
}

func TestInvoiceCoupon_FailsWhenCouponMissing(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "couponed")
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_first", types.InvoiceStatusFinalized, d.now),
	}
	r := run(t, d, newInvoiceCoupon(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_coupon" || !strings.Contains(r.Reason, "shared coupon") {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceCoupon_SkipsWithoutInvoice(t *testing.T) {
	d := newTestDeps(t)
	r := run(t, d, newInvoiceCoupon(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "no_invoice" {
		t.Fatalf("result = %+v", r)
	}
}

// ── invoice-commitment ────────────────────────────────────────────────

func TestInvoiceCommitment_TrueUpWhenUnderCommitment(t *testing.T) {
	base := invoiceLine(types.PriceTypeFixed, "E2EProbe Base Fee", "19.99")
	folded := invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "5.00")
	folded.CommitmentInfo = &types.CommitmentInfo{Amount: probetest.Ptr("5.00"), ComputedCommitmentUtilizedAmount: probetest.Ptr("1.00"), ComputedTrueUpAmount: probetest.Ptr("4.00")}
	cases := []struct {
		name     string
		lines    []types.InvoiceLineItemResponse
		total    string
		want     e2eprobe.Status
		mismatch string
	}{
		{"true-up lifts usage to the commitment", []types.InvoiceLineItemResponse{base, invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "1.00"), trueUpLine("4.00")}, "24.99", e2eprobe.StatusPass, ""},
		{"true-up missing", []types.InvoiceLineItemResponse{base, invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "1.00")}, "20.99", e2eprobe.StatusFail, "true-up line"},
		{"true-up short", []types.InvoiceLineItemResponse{base, invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "1.00"), trueUpLine("3.00")}, "23.99", e2eprobe.StatusFail, "usage plus true-up: want 5.00, got 4.00"},
		{"line-level commitment folds the true-up into the usage line", []types.InvoiceLineItemResponse{base, folded}, "24.99", e2eprobe.StatusPass, ""},
		{"usage over the commitment needs no true-up", []types.InvoiceLineItemResponse{base, invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "5.75")}, "25.74", e2eprobe.StatusPass, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			p := personaOf(t, d, "committed")
			enableTrueUp(d, p.SubscriptionID())
			inv := subInvoice(p, p.SubscriptionID(), "inv_commit", types.InvoiceStatusFinalized, d.now)
			inv.LineItems = tc.lines
			inv.Total = probetest.Ptr(tc.total)
			d.fc.Invoice.Items = []types.InvoiceResponse{inv}
			r := run(t, d, newInvoiceCommitment(d.Deps), "committed")
			if r.Status != tc.want {
				t.Fatalf("result = %+v", r)
			}
			if tc.want == e2eprobe.StatusFail && (r.Step != "assert_commitment" || !strings.Contains(r.Reason, tc.mismatch)) {
				t.Fatalf("result = %+v", r)
			}
			if r.Attrs["invoice_id"] != "inv_commit" || r.Attrs["usage"] == "" {
				t.Errorf("attrs = %v", r.Attrs)
			}
		})
	}
}

func TestInvoiceCommitment_SkipsOnGap(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "committed")
	sub := d.fc.Subscription.Items[p.SubscriptionID()]
	sub.CommitmentAmount = nil
	d.fc.Subscription.Items[p.SubscriptionID()] = sub
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	inv := subInvoice(p, p.SubscriptionID(), "inv_commit", types.InvoiceStatusFinalized, d.now)
	inv.LineItems = []types.InvoiceLineItemResponse{invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "1.00")}
	d.fc.Invoice.Items = []types.InvoiceResponse{inv}
	r := run(t, d, newInvoiceCommitment(d.Deps), "committed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "gap" || !strings.Contains(r.Reason, "predates") {
		t.Fatalf("result = %+v", r)
	}
}

func TestInvoiceCommitment_SkipsWithoutTrueUp(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "committed")
	// An adopted subscription may predate enable_true_up; the probe cannot retrofit it.
	sub := d.fc.Subscription.Items[p.SubscriptionID()]
	sub.EnableTrueUp = nil
	d.fc.Subscription.Items[p.SubscriptionID()] = sub
	inv := subInvoice(p, p.SubscriptionID(), "inv_commit", types.InvoiceStatusFinalized, d.now)
	inv.LineItems = []types.InvoiceLineItemResponse{invoiceLine(types.PriceTypeUsage, "E2EProbe Sum Commit Usage", "1.00")}
	inv.Total = probetest.Ptr("20.99")
	d.fc.Invoice.Items = []types.InvoiceResponse{inv}
	r := run(t, d, newInvoiceCommitment(d.Deps), "committed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "true_up" || !strings.Contains(r.Reason, "enable_true_up") {
		t.Fatalf("result = %+v", r)
	}
}

// ── invoice-multi-cadence ─────────────────────────────────────────────

func TestInvoiceMultiCadence_FanOutCount(t *testing.T) {
	monthly := invoiceLine(types.PriceTypeFixed, "E2EProbe Base Fee", "19.99")
	cases := []struct {
		name  string
		lines []types.InvoiceLineItemResponse
		want  e2eprobe.Status
	}{
		{"three monthly line items", []types.InvoiceLineItemResponse{monthly, monthly, monthly}, e2eprobe.StatusPass},
		{"one quarter-wide line item", []types.InvoiceLineItemResponse{monthly}, e2eprobe.StatusFail},
		{"one source fans out, another does not", []types.InvoiceLineItemResponse{sourcedLine("sli_fixed"), sourcedLine("sli_fixed"), sourcedLine("sli_fixed"), sourcedLine("sli_usage")}, e2eprobe.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			p := personaOf(t, d, "taxed")
			subID := p.MultiCadenceSubID()
			if subID == "" {
				t.Fatal("taxed persona must own the quarterly subscription")
			}
			inv := subInvoice(p, subID, "inv_quarter", types.InvoiceStatusFinalized, d.now)
			inv.LineItems = tc.lines
			d.fc.Invoice.Items = []types.InvoiceResponse{inv}
			r := run(t, d, newInvoiceMultiCadence(d.Deps), "taxed")
			if r.Status != tc.want || (tc.want == e2eprobe.StatusFail && r.Step != "assert_fan_out") {
				t.Fatalf("result = %+v", r)
			}
			if r.Attrs["subscription_id"] != subID || r.Attrs["invoice_id"] != "inv_quarter" {
				t.Errorf("attrs = %v", r.Attrs)
			}
		})
	}
}

func TestInvoiceMultiCadence_SkipsWithoutInvoice(t *testing.T) {
	d := newTestDeps(t)
	p := personaOf(t, d, "taxed")
	// The monthly subscription's invoice is not the quarterly one's.
	d.fc.Invoice.Items = []types.InvoiceResponse{
		subInvoice(p, p.SubscriptionID(), "inv_monthly", types.InvoiceStatusFinalized, d.now),
	}
	r := run(t, d, newInvoiceMultiCadence(d.Deps), "taxed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "no_invoice" {
		t.Fatalf("result = %+v", r)
	}
}
