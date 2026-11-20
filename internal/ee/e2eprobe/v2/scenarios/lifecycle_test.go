package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// fastPoll keeps the flows' bounded waits to milliseconds.
var fastPoll = pollWindow{every: time.Millisecond, within: 25 * time.Millisecond}

func cloneOf(t *testing.T, d *testDeps, persona, scenarioName string) *world.Ephemeral {
	t.Helper()
	p, ok := d.World.Persona(persona)
	if !ok {
		t.Fatalf("no persona %s", persona)
	}
	eph, err := d.World.Clone(context.Background(), p, scenarioName)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	return eph
}

// nextCustomerID predicts the id the fake gives the customer created next.
func nextCustomerID(d *testDeps) string {
	return "cust_" + strconv.Itoa(len(d.fc.Customer.Created)+1)
}

func TestRegisterFlows_RegistersEveryFlow(t *testing.T) {
	cat := scenario.NewCatalog()
	registerFlows(cat)
	want := map[string]struct {
		every time.Duration
		trait e2eprobe.Trait
	}{
		"lifecycle":          {30 * time.Minute, e2eprobe.TraitSubscription},
		"cancel":             {time.Hour, e2eprobe.TraitSubscription},
		"line-item-change":   {time.Hour, e2eprobe.TraitSubscription},
		"tax-preview":        {time.Hour, e2eprobe.TraitTax},
		"coupon-preview":     {time.Hour, e2eprobe.TraitCoupon},
		"commitment-preview": {time.Hour, e2eprobe.TraitCommitment},
	}
	for name, w := range want {
		info, ok := cat.Scenario(name)
		if !ok {
			t.Errorf("%s not registered", name)
			continue
		}
		if info.Kind != e2eprobe.KindFlow || info.Every != w.every || len(info.Traits) != 1 || info.Traits[0] != w.trait {
			t.Errorf("%s = %+v", name, info)
		}
		s, err := cat.Build(name, scenario.Deps{})
		if err != nil || s.Name() != name || s.Kind() != e2eprobe.KindFlow {
			t.Errorf("build %s = %v, %v", name, s, err)
		}
	}
}

func TestLifecycle_ClonesIngestsAndPasses(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	ext := r.Attrs["ephemeral_external_id"]
	if !strings.HasPrefix(ext, world.EphemeralPrefix+"lifecycle-") || r.Attrs["ephemeral_customer_id"] == "" || r.Attrs["ephemeral_subscription_id"] == "" {
		t.Fatalf("attrs = %v", r.Attrs)
	}
	if r.Attrs["customer_created_webhook"] != "no" {
		t.Errorf("no webhook was recorded, attrs = %v", r.Attrs)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 3 {
		t.Fatalf("batches = %v", d.fc.Event.Batches)
	}
	stamp := d.now.UTC().Format(time.RFC3339Nano)
	for i, ev := range d.fc.Event.Batches[0] {
		wantID := fmt.Sprintf("%s-lifecycle-%d-%d", ext, d.now.Unix(), i)
		if ev.EventName != "e2eprobe_count" || ev.ExternalCustomerID != ext || ev.EventID == nil || *ev.EventID != wantID || ev.Timestamp == nil || *ev.Timestamp != stamp {
			t.Errorf("event %d = %+v", i, ev)
		}
		if ev.Properties["e2eprobe_run_id"] != "run-test" {
			t.Errorf("event %d properties = %v", i, ev.Properties)
		}
	}
	if d.World.Ephemerals().Count("customer") != 1 || d.World.Ephemerals().Count("subscription") != 1 {
		t.Errorf("the clone must stay registered for the janitor")
	}
}

func TestLifecycle_RecordsWebhookAttr(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	d.Inbox.Record(inbox.Event{ReceivedAt: d.now, EventType: "customer.created", CustomerID: nextCustomerID(d)})
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusPass || r.Attrs["customer_created_webhook"] != "yes" {
		t.Fatalf("result = %+v", r)
	}
}

func TestLifecycle_UsageNeverAnswersIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsError(503)
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "poll_usage" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if r.Attrs["ephemeral_external_id"] == "" {
		t.Errorf("a failure must still name the clone: %v", r.Attrs)
	}
}

func TestLifecycle_IngestFailureIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.IngestErr = probetest.ServerError(500)
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "baseline-a"); r.Status != e2eprobe.StatusFail || r.Step != "ingest_bulk" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestPollWindow_Until(t *testing.T) {
	calls := 0
	p := pollWindow{every: time.Millisecond, within: time.Second}
	err := p.until(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err = %v after %d calls", err, calls)
	}

	err = fastPoll.until(context.Background(), func() error { return probetest.ServerError(503) })
	if err == nil || !e2eprobe.IsInfra(err) || !strings.Contains(err.Error(), "25ms") {
		t.Fatalf("a timeout must wrap the last error: %v", err)
	}

	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := fastPoll.until(ctx, func() error { return errors.New("x") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled parent must surface as such: %v", err)
	}
}

func TestLifecycle_BacklogCountsOnlyItsOwnClones(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.Analytics = analyticsFromIngested(d)
	for i := 0; i < 5; i++ {
		d.World.Ephemerals().Register("customer", fmt.Sprintf("%stax-preview-%d", world.EphemeralPrefix, i), d.now)
	}
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "baseline-a"); r.Status == e2eprobe.StatusSkip {
		t.Fatalf("other scenarios' clones must not block lifecycle: %+v", r)
	}
	for i := 0; i < 5; i++ {
		d.World.Ephemerals().Register("customer", fmt.Sprintf("%slifecycle-%d", world.EphemeralPrefix, i), d.now)
	}
	created, batches := len(d.fc.Customer.Created), len(d.fc.Event.Batches)
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusSkip || r.Step != "backlog" || !strings.Contains(r.Reason, "6 lifecycle clones") {
		t.Fatalf("six live lifecycle clones must skip: %+v", r)
	}
	if len(d.fc.Customer.Created) != created || len(d.fc.Event.Batches) != batches {
		t.Error("a skipped run must not clone or ingest")
	}
}

func TestLifecycle_ComparesUsageWithTheBatch(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	d.fc.Event.Analytics = func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: []types.UsageAnalyticItem{{FeatureID: probetest.Ptr(featureID), TotalUsage: probetest.Ptr("2")}}}}, nil
	}
	s := newLifecycle(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Infra || r.Step != "poll_usage" {
		t.Fatalf("two of three events landing is a stalled pipeline: %+v", r)
	}
}
