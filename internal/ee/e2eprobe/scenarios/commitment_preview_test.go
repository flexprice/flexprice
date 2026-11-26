package scenarios

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func committedPreview(total string, lines ...types.InvoiceLineItemResponse) *dtos.GetInvoicePreviewResponse {
	return &dtos.GetInvoicePreviewResponse{InvoiceResponse: &types.InvoiceResponse{Total: probetest.Ptr(total), LineItems: lines}}
}

func fixedLine(amount string) types.InvoiceLineItemResponse {
	return types.InvoiceLineItemResponse{PriceType: probetest.Ptr(string(types.PriceTypeFixed)), Amount: probetest.Ptr(amount), DisplayName: probetest.Ptr("E2EProbe Base Fee")}
}

func usageLine(amount string) types.InvoiceLineItemResponse {
	return types.InvoiceLineItemResponse{PriceType: probetest.Ptr(string(types.PriceTypeUsage)), Amount: probetest.Ptr(amount), DisplayName: probetest.Ptr("E2EProbe Sum (commitment) Usage")}
}

// analyticsFromIngested answers analytics from the events the fake has taken
// for the requested customer: total_usage per feature is the sum of each
// event's amount, or one unit per event without one.
func analyticsFromIngested(d *testDeps) func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return func(req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		fx := d.World.Fixtures()
		var items []types.UsageAnalyticItem
		for _, featureID := range req.FeatureIds {
			total := 0.0
			for _, m := range e2eprobe.Meters() {
				if fx.FeatureID(m.Key) != featureID {
					continue
				}
				for _, ev := range d.fc.Event.Ingested {
					if ev.EventName != m.EventName || (req.ExternalCustomerID != nil && ev.ExternalCustomerID != *req.ExternalCustomerID) {
						continue
					}
					v := 1.0 // a count event carries no amount
					if raw, ok := ev.Properties["amount"]; ok {
						v, _ = strconv.ParseFloat(raw, 64)
					}
					total += v
				}
			}
			items = append(items, types.UsageAnalyticItem{FeatureID: probetest.Ptr(featureID), TotalUsage: probetest.Ptr(strconv.FormatFloat(total, 'f', -1, 64))})
		}
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: items}}, nil
	}
}

func analyticsError(status int) func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		return nil, probetest.ServerError(status)
	}
}

func TestCommitmentPreview_AlternatesLegs(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	previews := []*dtos.GetInvoicePreviewResponse{
		committedPreview("24.99", fixedLine("19.99"), usageLine("1.00"), trueUpLine("4.00")),
		committedPreview("27.99", fixedLine("19.99"), usageLine("8.00")),
	}
	d.fc.Invoice.Preview = func(types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
		return previews[len(d.fc.Invoice.PreviewCalls)-1], nil
	}
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll

	r := run(t, d, s, "committed")
	if r.Status != e2eprobe.StatusPass || r.Attrs["leg"] != "under" {
		t.Fatalf("first run = %+v", r)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 100 {
		t.Fatalf("under leg batches = %d", len(d.fc.Event.Batches))
	}
	ev := d.fc.Event.Batches[0][0]
	if ev.EventName != "e2eprobe_sum_commit" || ev.Properties["amount"] != "1" || ev.ExternalCustomerID != r.Attrs["ephemeral_external_id"] {
		t.Errorf("event = %+v", ev)
	}

	d.advance(time.Hour)
	r = run(t, d, s, "committed")
	if r.Status != e2eprobe.StatusPass || r.Attrs["leg"] != "over" {
		t.Fatalf("second run = %+v", r)
	}
	if len(d.fc.Event.Batches) != 2 || len(d.fc.Event.Batches[1]) != 100 || d.fc.Event.Batches[1][0].Properties["amount"] != "7" {
		t.Fatalf("the over leg sends 100 events of 7 units, got %d batches", len(d.fc.Event.Batches))
	}
	if d.World.Ephemerals().Count("customer") != 2 || len(d.fc.Invoice.PreviewCalls) != 2 {
		t.Errorf("each leg clones and previews once: %d clones, %d previews", d.World.Ephemerals().Count("customer"), len(d.fc.Invoice.PreviewCalls))
	}
}

func TestCommitmentPreview_UnderLegMismatches(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	servePreview(d, committedPreview("20.99", fixedLine("19.99"), usageLine("1.00")))
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "committed")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_commitment_preview" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(r.Mismatches) != 2 || r.Mismatches[0] != "usage billed to commitment: want 5.00, got 1.00" || !strings.Contains(r.Mismatches[1], "true-up") {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestCommitmentPreview_OverLegMismatch(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	servePreview(d, committedPreview("26.99", fixedLine("19.99"), usageLine("7.00")))
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	s.runs.Store(1)
	r := run(t, d, s, "committed")
	if r.Status != e2eprobe.StatusFail || r.Attrs["leg"] != "over" || len(r.Mismatches) != 1 || r.Mismatches[0] != "usage billed with overage: want 8.00, got 7.00" {
		t.Fatalf("result = %+v", r)
	}
}

func TestCommitmentPreview_WaitsForUsageToLand(t *testing.T) {
	d := newTestDeps(t)
	calls := 0
	featureID := d.World.Fixtures().FeatureID("sum_commit")
	d.fc.Event.Analytics = func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		calls++
		units := "0"
		if calls >= 3 {
			units = "100"
		}
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: []types.UsageAnalyticItem{{FeatureID: probetest.Ptr(featureID), TotalUsage: probetest.Ptr(units)}}}}, nil
	}
	servePreview(d, committedPreview("24.99", fixedLine("19.99"), usageLine("1.00"), trueUpLine("4.00")))
	s := newCommitmentPreview(d.Deps)
	s.poll = pollWindow{every: 0, within: fastPoll.within}
	if r := run(t, d, s, "committed"); r.Status != e2eprobe.StatusPass || calls != 3 {
		t.Fatalf("result = %+v after %d usage reads", r, calls)
	}
}

func TestCommitmentPreview_FailsWhenUsageNeverLands(t *testing.T) {
	d := newTestDeps(t)
	servePreview(d, committedPreview("24.99", fixedLine("19.99"), usageLine("1.00"), trueUpLine("4.00")))
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "committed")
	if r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || r.Infra || r.Attrs["usage_units"] != "0" {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Invoice.PreviewCalls) != 0 {
		t.Error("no preview must be read before usage lands")
	}
}

func TestCommitmentPreview_UsageErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsError(503)
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "committed"); r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestCommitmentPreview_RunsDespitePersonaGap(t *testing.T) {
	d := newTestDeps(t)
	committed, _ := d.World.Persona("committed")
	sub := d.fc.Subscription.Items[committed.SubscriptionID()]
	sub.CommitmentAmount = nil
	d.fc.Subscription.Items[committed.SubscriptionID()] = sub
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	created := len(d.fc.Customer.Created)
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "committed")
	// The clone carries its own commitment; the persona's adopted subscription is irrelevant here.
	if r.Status == e2eprobe.StatusSkip {
		t.Fatalf("a clone flow must not skip on the persona's gap: %+v", r)
	}
	if len(d.fc.Customer.Created) != created+1 || len(d.fc.Event.Batches) == 0 {
		t.Error("the flow must clone and ingest")
	}
}
