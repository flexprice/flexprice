package ledger

import (
	"reflect"
	"testing"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
)

var tick = 10 * time.Minute

func TestTickEnd_Floors(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 17, 42, 0, time.UTC)
	if got := TickEnd(now, tick); !got.Equal(time.Date(2026, 11, 19, 10, 10, 0, 0, time.UTC)) {
		t.Errorf("TickEnd = %s", got)
	}
	exact := time.Date(2026, 11, 19, 10, 20, 0, 0, time.UTC)
	if got := TickEnd(exact, tick); !got.Equal(exact) {
		t.Errorf("an aligned instant is its own tick end, got %s", got)
	}
}

func TestBatch_IsDeterministic(t *testing.T) {
	p, _ := Lookup("standard")
	end := time.Date(2026, 11, 19, 10, 20, 0, 0, time.UTC)
	a := Batch(p, "e2eprobe-taxed", end, tick, "run-1")
	b := Batch(p, "e2eprobe-taxed", end, tick, "run-2")
	if len(a) != 27 || !reflect.DeepEqual(ids(a), ids(b)) {
		t.Fatalf("ids must not depend on the run: %v vs %v", ids(a), ids(b))
	}
	if *a[0].EventID != "e2eprobe-taxed-count-1795083600-0" {
		t.Errorf("id pattern = %q", *a[0].EventID)
	}
	for _, ev := range a {
		ts, err := time.Parse(time.RFC3339Nano, *ev.Timestamp)
		if err != nil || !ts.After(end.Add(-tick)) || ts.After(end) {
			t.Errorf("timestamp %q outside (%s, %s]", *ev.Timestamp, end.Add(-tick), end)
		}
		if ev.ExternalCustomerID != "e2eprobe-taxed" || ev.Properties["e2eprobe"] != "true" || ev.Properties["e2eprobe_run_id"] != "run-1" {
			t.Errorf("event = %+v", ev)
		}
	}
	var filtered []string
	for _, ev := range a {
		if ev.EventName == "e2eprobe_sum_filtered" {
			filtered = append(filtered, *ev.Source+"/"+ev.Properties["source"])
		}
	}
	if !reflect.DeepEqual(filtered, []string{"api/api", "api/api", "web/web"}) {
		t.Errorf("filtered sources = %v", filtered)
	}
}

func TestHourWindow_RespectsGrace(t *testing.T) {
	now := time.Date(2026, 11, 19, 11, 2, 0, 0, time.UTC)
	w := HourWindow(now, 3*time.Minute)
	if !w.Start.Equal(time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)) || !w.End.Equal(time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("two minutes past the hour is inside the grace; window = %s..%s", w.Start, w.End)
	}
	w = HourWindow(now.Add(2*time.Minute), 3*time.Minute)
	if !w.End.Equal(time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("after the grace the last hour closes; window end = %s", w.End)
	}
}

func ids(evs []types.IngestEventRequest) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, *ev.EventID)
	}
	return out
}
