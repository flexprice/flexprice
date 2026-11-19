package scenarios

import (
	"context"
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

// usageFromIngested answers subscription usage from the events the fake has
// taken for that subscription's customer, at the plan's $0.01 per unit.
func usageFromIngested(d *testDeps) func(types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return func(req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
		sub := d.fc.Subscription.Items[req.SubscriptionID]
		units := 0
		for _, ev := range d.fc.Event.Ingested {
			if sub.CustomerID != nil && d.fc.Customer.Internal(ev.ExternalCustomerID) == *sub.CustomerID {
				units++
			}
		}
		amount := float64(units) * 0.01
		return &dtos.GetSubscriptionUsageResponse{GetUsageBySubscriptionResponse: &types.GetUsageBySubscriptionResponse{Amount: &amount}}, nil
	}
}

func TestCommitmentPreview_AlternatesLegs(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.Usage = usageFromIngested(d)
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
	if len(d.fc.Event.Batches) != 2 || len(d.fc.Event.Batches[1]) != 700 {
		t.Fatalf("over leg batches = %d", len(d.fc.Event.Batches))
	}
	if d.World.Ephemerals().Count("customer") != 2 || len(d.fc.Invoice.PreviewCalls) != 2 {
		t.Errorf("each leg clones and previews once: %d clones, %d previews", d.World.Ephemerals().Count("customer"), len(d.fc.Invoice.PreviewCalls))
	}
}

func TestCommitmentPreview_UnderLegMismatches(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.Usage = usageFromIngested(d)
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
	d.fc.Subscription.Usage = usageFromIngested(d)
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
	d.fc.Subscription.Usage = func(types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
		calls++
		amount := 0.0
		if calls >= 3 {
			amount = 1.0
		}
		return &dtos.GetSubscriptionUsageResponse{GetUsageBySubscriptionResponse: &types.GetUsageBySubscriptionResponse{Amount: &amount}}, nil
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
	if r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || r.Infra || r.Attrs["usage_amount"] != "0.00" {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Invoice.PreviewCalls) != 0 {
		t.Error("no preview must be read before usage lands")
	}
}

func TestCommitmentPreview_UsageErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.Usage = func(types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
		return nil, probetest.ServerError(503)
	}
	s := newCommitmentPreview(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "committed"); r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestCommitmentPreview_SkipsOnGap(t *testing.T) {
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
	if r.Status != e2eprobe.StatusSkip || r.Step != "gap" || !strings.Contains(r.Reason, "predates") {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Customer.Created) != created || len(d.fc.Event.Batches) != 0 {
		t.Error("a skipped run must not clone or ingest")
	}
}
