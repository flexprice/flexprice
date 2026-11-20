package scenarios

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
)

func TestIngest_SendsOneBulkBatchAndRecords(t *testing.T) {
	d := newTestDeps(t)
	d.now = testNow.Add(3 * time.Minute)
	r := run(t, d, newIngest(d.Deps), "taxed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 27 {
		t.Fatalf("batches = %d", len(d.fc.Event.Batches))
	}
	tickEnd := ledger.TickEnd(d.now, d.Tick)
	if !d.Ledger.Recorded("taxed", tickEnd) {
		t.Error("the tick must be recorded under the persona name")
	}
	if d.fc.Event.Batches[0][0].ExternalCustomerID != "e2eprobe-taxed" {
		t.Errorf("events must target the persona's customer: %+v", d.fc.Event.Batches[0][0])
	}
	if r.Attrs["tick"] != tickEnd.Format(time.RFC3339) {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestIngest_SecondRunInSameTickIsNoop(t *testing.T) {
	d := newTestDeps(t)
	d.now = testNow.Add(3 * time.Minute)
	run(t, d, newIngest(d.Deps), "taxed")
	d.advance(2 * time.Minute)
	if r := run(t, d, newIngest(d.Deps), "taxed"); r.Status != e2eprobe.StatusPass {
		t.Fatalf("second run = %+v", r)
	}
	if len(d.fc.Event.Batches) != 1 {
		t.Fatalf("one tick must send one batch, got %d", len(d.fc.Event.Batches))
	}
	d.advance(10 * time.Minute)
	run(t, d, newIngest(d.Deps), "taxed")
	if len(d.fc.Event.Batches) != 2 {
		t.Fatalf("the next tick sends again, got %d batches", len(d.fc.Event.Batches))
	}
}

func TestIngest_FailureIsInfraOn503(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.IngestErr = probetest.ServerError(503)
	r := run(t, d, newIngest(d.Deps), "taxed")
	if r.Status != e2eprobe.StatusFail || r.Step != "ingest_bulk" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if d.Ledger.Recorded("taxed", ledger.TickEnd(d.now, d.Tick)) {
		t.Error("a failed batch must not be recorded as sent")
	}
}

func TestIngest_SkipsPersonaWithoutProfile(t *testing.T) {
	d := newTestDeps(t)
	if r := run(t, d, newIngest(d.Deps), "wallet-a"); r.Status != e2eprobe.StatusSkip {
		t.Fatalf("result = %+v", r)
	}
}

func TestIngest_BackfillsMissedTicks(t *testing.T) {
	d := newTestDeps(t)
	d.now = testNow.Add(3 * time.Minute)
	run(t, d, newIngest(d.Deps), "taxed")
	d.now = testNow.Add(33 * time.Minute)
	if r := run(t, d, newIngest(d.Deps), "taxed"); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Batches) != 4 {
		t.Fatalf("missed ticks must be backfilled: %d batches, want 4", len(d.fc.Event.Batches))
	}
	for _, offset := range []time.Duration{10 * time.Minute, 20 * time.Minute, 30 * time.Minute} {
		if !d.Ledger.Recorded("taxed", testNow.Add(offset)) {
			t.Errorf("tick %s not recorded", testNow.Add(offset))
		}
	}
	d.now = testNow.Add(5 * time.Hour)
	run(t, d, newIngest(d.Deps), "taxed")
	if len(d.fc.Event.Batches) != 10 {
		t.Fatalf("backfill is bounded to six ticks: %d batches, want 10", len(d.fc.Event.Batches))
	}
}
