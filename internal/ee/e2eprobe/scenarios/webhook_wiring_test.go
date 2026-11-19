package scenarios

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
)

func TestWebhookWiring_SkipsDuringWarmup(t *testing.T) {
	d := newTestDeps(t)
	d.advance(time.Hour)
	if r := run(t, d, newWebhookWiring(d.Deps), ""); r.Status != e2eprobe.StatusSkip || r.Step != "warmup" {
		t.Fatalf("one hour into a six-hour silence window must skip: %+v", r)
	}
	d.advance(6 * time.Hour)
	if r := run(t, d, newWebhookWiring(d.Deps), ""); r.Status != e2eprobe.StatusSkip {
		t.Fatalf("no clone was created, so no webhook was due: %+v", r)
	}
}

func TestWebhookWiring_FailsOnSilence(t *testing.T) {
	d := newTestDeps(t)
	baseline, _ := d.World.Persona("baseline-a")
	if _, err := d.World.Clone(context.Background(), baseline, "lifecycle"); err != nil {
		t.Fatal(err)
	}
	d.advance(7 * time.Hour)
	r := run(t, d, newWebhookWiring(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "webhook_silence" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestWebhookWiring_PassesWhenRecent(t *testing.T) {
	d := newTestDeps(t)
	baseline, _ := d.World.Persona("baseline-a")
	if _, err := d.World.Clone(context.Background(), baseline, "lifecycle"); err != nil {
		t.Fatal(err)
	}
	d.advance(7 * time.Hour)
	d.Inbox.Record(inbox.Event{ReceivedAt: d.now.Add(-time.Minute), EventType: "customer.created", CustomerID: "cust_1"})
	r := run(t, d, newWebhookWiring(d.Deps), "")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
}
