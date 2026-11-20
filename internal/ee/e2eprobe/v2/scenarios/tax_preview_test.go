package scenarios

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// taxedPreview is a preview carrying one applied tax; an empty rateID or code leaves that field unset.
func taxedPreview(rateID, code, taxable, tax string) *dtos.GetInvoicePreviewResponse {
	applied := types.TaxAppliedResponse{TaxableAmount: probetest.Ptr(taxable), TaxAmount: probetest.Ptr(tax)}
	if rateID != "" {
		applied.TaxRateID = probetest.Ptr(rateID)
	}
	if code != "" {
		applied.TaxRate = &types.TaxRateResponse{Code: probetest.Ptr(code)}
	}
	return &dtos.GetInvoicePreviewResponse{InvoiceResponse: &types.InvoiceResponse{Taxes: []types.TaxAppliedResponse{applied}}}
}

func servePreview(d *testDeps, p *dtos.GetInvoicePreviewResponse) {
	d.fc.Invoice.Preview = func(types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) { return p, nil }
}

func TestTaxPreview_Math(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	fx := d.World.Fixtures()
	servePreview(d, taxedPreview(fx.TaxRateID(), "", "20.99", "2.10"))
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "taxed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 100 {
		t.Fatalf("batches = %d", len(d.fc.Event.Batches))
	}
	ev := d.fc.Event.Batches[0][0]
	if ev.EventName != "e2eprobe_sum_commit" || ev.Properties["amount"] != "1" || ev.ExternalCustomerID != r.Attrs["ephemeral_external_id"] {
		t.Errorf("event = %+v", ev)
	}
	subID := r.Attrs["ephemeral_subscription_id"]
	if len(d.fc.Invoice.PreviewCalls) != 1 || d.fc.Invoice.PreviewCalls[0].SubscriptionID != subID {
		t.Errorf("preview calls = %+v", d.fc.Invoice.PreviewCalls)
	}
	attached := false
	for _, ta := range d.fc.TaxAssoc.Items {
		attached = attached || (ta.EntityID != nil && *ta.EntityID == subID)
	}
	if !attached {
		t.Error("the clone must carry the shared tax association")
	}
}

func TestTaxPreview_MatchesByCodeWhenIDMissing(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	servePreview(d, taxedPreview("", d.World.Fixtures().TaxRateCode(), "20.99", "2.10"))
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "taxed"); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
}

func TestTaxPreview_MismatchOnWrongTaxAmount(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	servePreview(d, taxedPreview(d.World.Fixtures().TaxRateID(), "", "20.99", "1.50"))
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "taxed")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_tax_preview" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(r.Mismatches) != 1 || r.Mismatches[0] != "tax amount: want 2.10, got 1.50" {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestTaxPreview_MismatchWhenRateMissing(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	servePreview(d, taxedPreview("taxrate_other", "OTHER", "20.99", "2.10"))
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "taxed")
	if r.Status != e2eprobe.StatusFail || len(r.Mismatches) != 1 || r.Mismatches[0] != "preview carries the shared tax rate" {
		t.Fatalf("foreign rate: %+v", r)
	}
	servePreview(d, &dtos.GetInvoicePreviewResponse{InvoiceResponse: &types.InvoiceResponse{}})
	d.advance(time.Hour)
	if r := run(t, d, s, "taxed"); r.Status != e2eprobe.StatusFail || len(r.Mismatches) != 1 {
		t.Fatalf("no taxes: %+v", r)
	}
}

func TestTaxPreview_UsageNeverAnswersIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	d.fc.Event.Analytics = analyticsError(503)
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "taxed")
	if r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Invoice.PreviewCalls) != 0 {
		t.Error("no preview must be read before usage answers")
	}
}

func TestTaxPreview_PreviewErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	d.fc.Invoice.Preview = func(types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
		return nil, probetest.ServerError(500)
	}
	s := newTaxPreview(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "taxed"); r.Status != e2eprobe.StatusFail || r.Step != "preview" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
