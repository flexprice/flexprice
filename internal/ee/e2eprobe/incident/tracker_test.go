package incident

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// recorder keeps one line per notification so tests assert order and count;
// failNext rejects that many deliveries first, as a Slack outage would.
type recorder struct {
	events   []string
	opened   []Incident
	failNext int
}

func (r *recorder) deliver(line string) error {
	if r.failNext > 0 {
		r.failNext--
		r.events = append(r.events, "rejected:"+line)
		return errors.New("slack: 429")
	}
	r.events = append(r.events, line)
	return nil
}

func (r *recorder) Opened(_ context.Context, inc Incident, _ BindingState) error {
	r.opened = append(r.opened, inc)
	return r.deliver(fmt.Sprintf("opened:%s:%s", inc.Key, inc.Kind))
}
func (r *recorder) Reminded(_ context.Context, inc Incident, _ BindingState) error {
	return r.deliver(fmt.Sprintf("reminded:%s", inc.Key))
}
func (r *recorder) Resolved(_ context.Context, inc Incident, _ BindingState, _ time.Time) error {
	return r.deliver(fmt.Sprintf("resolved:%s:%s", inc.Key, inc.Kind))
}
func (r *recorder) Digest(_ context.Context, _ Digest) error { return r.deliver("digest") }

var testPolicy = Policy{AfterFailures: 3, AfterDuration: 20 * time.Minute, RenotifyEvery: 6 * time.Hour, SkipAfter: 24 * time.Hour}

func newTestTracker(t *testing.T, store Store) (*Tracker, *recorder, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	tr, err := NewTracker(func(Key) Policy { return testPolicy }, DefaultRollup(), rec, store, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return tr, rec, clock
}

var usageTaxed = Key{Scenario: "usage", Persona: "taxed"}

func fail(step string) e2eprobe.Result {
	return e2eprobe.Mismatch(step, []string{"sum: want 30, got 20"}, map[string]string{"persona": "taxed"})
}

func TestKey_String(t *testing.T) {
	if got := usageTaxed.String(); got != "usage · taxed" {
		t.Errorf("got %q", got)
	}
	if got := (Key{Scenario: "preflight"}).String(); got != "preflight" {
		t.Errorf("got %q", got)
	}
}

func TestTracker_OpensAfterFailuresAndDuration(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	tr.Observe(ctx, usageTaxed, fail("assert"))
	tr.Observe(ctx, usageTaxed, fail("assert"))
	if len(rec.events) != 0 {
		t.Fatalf("two failures must not open: %v", rec.events)
	}
	clock.Advance(5 * time.Minute)
	tr.Observe(ctx, usageTaxed, fail("assert"))
	if len(rec.events) != 0 {
		t.Fatalf("three failures inside 5m must not open: %v", rec.events)
	}
	clock.Advance(15 * time.Minute)
	tr.Observe(ctx, usageTaxed, fail("assert"))
	if len(rec.events) != 1 || rec.events[0] != "opened:usage · taxed:failure" {
		t.Fatalf("expected one opened, got %v", rec.events)
	}
	open := tr.Open()
	if len(open) != 1 || open[0].Runs != 4 || open[0].Summary != "assert: sum: want 30, got 20" {
		t.Errorf("open incident = %+v", open)
	}
}

func TestTracker_NoSecondOpened(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 1 {
		t.Fatalf("expected exactly one opened, got %v", rec.events)
	}
	st := tr.Snapshot().Bindings[usageTaxed.String()]
	if st.Consecutive != 6 || st.Incident == nil || st.Incident.Runs != 6 {
		t.Errorf("state = %+v", st)
	}
}

func TestTracker_RemindsOnTick(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	tr.Tick(ctx)
	if len(rec.events) != 1 {
		t.Fatalf("no reminder before renotify_every: %v", rec.events)
	}
	clock.Advance(6 * time.Hour)
	tr.Tick(ctx)
	tr.Tick(ctx)
	if len(rec.events) != 2 || rec.events[1] != "reminded:usage · taxed" {
		t.Fatalf("expected one reminder after 6h, got %v", rec.events)
	}
}

func TestTracker_ResolvesOnPass(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	tr.Observe(ctx, usageTaxed, e2eprobe.Pass())
	tr.Observe(ctx, usageTaxed, e2eprobe.Pass())
	if len(rec.events) != 2 || rec.events[1] != "resolved:usage · taxed:failure" {
		t.Fatalf("expected opened then one resolved, got %v", rec.events)
	}
	st := tr.Snapshot().Bindings[usageTaxed.String()]
	if st.Incident != nil || st.Consecutive != 0 || !st.FailingSince.IsZero() || st.Passes != 2 || st.Runs != 5 {
		t.Errorf("state after pass = %+v", st)
	}
	if len(tr.Open()) != 0 {
		t.Error("no incident should stay open")
	}
}

func TestTracker_InfraFailsPageAfterThresholds(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, infra)
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 0 {
		t.Fatalf("infra pages wait for the rollup evaluation on Tick, got %v", rec.events)
	}
	tr.Tick(ctx)
	if len(rec.events) != 1 || rec.events[0] != "opened:usage · taxed:failure" {
		t.Fatalf("a single endpoint failing for 20 minutes must page, got %v", rec.events)
	}
	if st := tr.Snapshot().Bindings[usageTaxed.String()]; st.Consecutive != 3 || st.Last.Infra != true {
		t.Errorf("infra runs must count: %+v", st)
	}
}

func TestTracker_InfraFailsDoNotOpenWhileRollupOpen(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	keys := []Key{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "wallet-debit", Persona: "wallet-a"}}
	for i := 0; i < 3; i++ {
		for _, k := range keys {
			tr.Observe(ctx, k, infra)
		}
		tr.Tick(ctx)
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 1 || rec.events[0] != "opened:api:infra" {
		t.Fatalf("a region-wide outage pages once, as the rollup: %v", rec.events)
	}
}

func TestTracker_InfraRollupOpensAtThreeBindings(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	tr.Observe(ctx, Key{Scenario: "usage", Persona: "a"}, infra)
	tr.Observe(ctx, Key{Scenario: "usage", Persona: "b"}, infra)
	tr.Tick(ctx)
	if len(rec.events) != 0 {
		t.Fatalf("two bindings are below the rollup threshold: %v", rec.events)
	}
	tr.Observe(ctx, Key{Scenario: "wallet-debit", Persona: "wallet-a"}, infra)
	tr.Tick(ctx)
	if len(rec.events) != 1 || rec.events[0] != "opened:api:infra" {
		t.Fatalf("expected the infra rollup to open, got %v", rec.events)
	}
	tr.Tick(ctx)
	if len(rec.events) != 1 {
		t.Fatalf("rollup must open once: %v", rec.events)
	}
	clock.Advance(20 * time.Minute)
	tr.Tick(ctx)
	if len(rec.events) != 2 || rec.events[1] != "resolved:api:infra" {
		t.Fatalf("rollup must resolve once the window is quiet, got %v", rec.events)
	}
}

func TestTracker_CoverageIncidentAfterSkipAfter(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	skip := e2eprobe.Skip("window", "partial hour")
	tr.Observe(ctx, usageTaxed, skip)
	clock.Advance(23 * time.Hour)
	tr.Observe(ctx, usageTaxed, skip)
	tr.Tick(ctx)
	if len(rec.events) != 0 {
		t.Fatalf("skipping under skip_after must stay quiet: %v", rec.events)
	}
	clock.Advance(2 * time.Hour)
	tr.Tick(ctx)
	if len(rec.events) != 1 || rec.events[0] != "opened:usage · taxed:coverage" {
		t.Fatalf("expected a coverage incident, got %v", rec.events)
	}
	tr.Observe(ctx, usageTaxed, fail("assert"))
	if len(rec.events) != 2 || rec.events[1] != "resolved:usage · taxed:coverage" {
		t.Fatalf("a run that evaluates resolves the coverage incident, got %v", rec.events)
	}
}

func TestTracker_SkipDoesNotResolveFailureIncident(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	tr.Observe(ctx, usageTaxed, e2eprobe.Skip("persona", "not provisioned"))
	if len(rec.events) != 1 || len(tr.Open()) != 1 {
		t.Fatalf("a skip must leave the failure incident open: %v", rec.events)
	}
}

func TestTracker_RestoresOpenIncidentWithoutRenotify(t *testing.T) {
	path := t.TempDir() + "/state.json"
	tr, rec, clock := newTestTracker(t, FileStore(path))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 1 {
		t.Fatalf("setup: %v", rec.events)
	}

	rec2 := &recorder{}
	restarted, err := NewTracker(func(Key) Policy { return testPolicy }, DefaultRollup(), rec2, FileStore(path), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec2.events) != 0 {
		t.Fatalf("loading state must not notify: %v", rec2.events)
	}
	if open := restarted.Open(); len(open) != 1 || !open[0].OpenedAt.Equal(tr.Open()[0].OpenedAt) {
		t.Fatalf("open incident not restored: %+v", open)
	}
	restarted.Observe(ctx, usageTaxed, e2eprobe.Pass())
	if len(rec2.events) != 1 || rec2.events[0] != "resolved:usage · taxed:failure" {
		t.Fatalf("expected one resolved after restart, got %v", rec2.events)
	}
}

func TestTracker_DigestCarriesOpenSkippingAndExtra(t *testing.T) {
	tr, _, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	tr.Observe(ctx, Key{Scenario: "lifecycle", Persona: "baseline-a"}, e2eprobe.Skip("persona", "not provisioned"))
	d := tr.Digest(map[string]string{"inbox.total": "12"})
	if len(d.Open) != 1 || len(d.Bindings) != 2 || d.Extra["inbox.total"] != "12" || !d.GeneratedAt.Equal(clock.Now()) {
		t.Errorf("digest = %+v", d)
	}
	if d.Bindings[0].Key.Scenario != "lifecycle" {
		t.Errorf("bindings must be sorted by key, got %v", d.Bindings[0].Key)
	}
}

func TestTracker_PolicyPerKey(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	tr, err := NewTracker(func(k Key) Policy {
		if k.Scenario == "preflight" {
			return Policy{AfterFailures: 1, AfterDuration: 0, RenotifyEvery: 0, SkipAfter: time.Hour}
		}
		return testPolicy
	}, DefaultRollup(), rec, FileStore(""), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	tr.Observe(context.Background(), Key{Scenario: "preflight"}, e2eprobe.Fail("connections", errors.New("stripe missing"), nil))
	if len(rec.events) != 1 || rec.events[0] != "opened:preflight:failure" {
		t.Fatalf("after_failures 1 with no duration must open immediately: %v", rec.events)
	}
	clock.Advance(48 * time.Hour)
	tr.Tick(context.Background())
	if len(rec.events) != 1 {
		t.Fatalf("renotify_every 0 must never remind: %v", rec.events)
	}
}

func TestTracker_InfraRollupSuppressesBindingPages(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	eager := Policy{AfterFailures: 1, RenotifyEvery: 6 * time.Hour, SkipAfter: 24 * time.Hour}
	tr, err := NewTracker(func(Key) Policy { return eager }, DefaultRollup(), rec, FileStore(""), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	for _, k := range []Key{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "wallet-debit", Persona: "wallet-a"}} {
		tr.Observe(ctx, k, infra)
	}
	tr.Tick(ctx)
	if len(rec.events) != 1 || rec.events[0] != "opened:api:infra" {
		t.Fatalf("three infra failures page once, as the rollup, got %v", rec.events)
	}
	if s := rec.opened[0].Summary; !strings.Contains(s, "usage · a") || !strings.Contains(s, "wallet-debit · wallet-a") {
		t.Errorf("the rollup must name the bindings it covers: %q", s)
	}
	// Six hours of an ongoing outage: the rollup reminds once and no binding pages on its own.
	for elapsed := time.Duration(0); elapsed < 6*time.Hour; elapsed += 10 * time.Minute {
		clock.Advance(10 * time.Minute)
		for _, k := range []Key{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "wallet-debit", Persona: "wallet-a"}} {
			tr.Observe(ctx, k, infra)
		}
		tr.Tick(ctx)
	}
	if len(rec.events) != 2 || rec.events[1] != "reminded:api" {
		t.Errorf("only the rollup reminds while it is open, got %v", rec.events)
	}
}

func TestTracker_RestoredInfraRollupDoesNotRecoverOnRestart(t *testing.T) {
	path := t.TempDir() + "/state.json"
	tr, rec, clock := newTestTracker(t, FileStore(path))
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	for _, k := range []Key{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "wallet-debit", Persona: "wallet-a"}} {
		tr.Observe(ctx, k, infra)
	}
	tr.Tick(ctx)
	if len(rec.events) != 1 {
		t.Fatalf("setup: %v", rec.events)
	}
	rec2 := &recorder{}
	restarted, err := NewTracker(func(Key) Policy { return testPolicy }, DefaultRollup(), rec2, FileStore(path), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	restarted.Tick(ctx)
	if len(rec2.events) != 0 {
		t.Fatalf("a restart inside the window must not resolve the outage: %v", rec2.events)
	}
	clock.Advance(20 * time.Minute)
	restarted.Tick(ctx)
	if len(rec2.events) != 1 || rec2.events[0] != "resolved:api:infra" {
		t.Fatalf("a quiet window resolves the restored rollup, got %v", rec2.events)
	}
}

func TestTracker_RetriesOpenedUntilDelivered(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	rec.failNext = 1
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 1 || rec.events[0] != "rejected:opened:usage · taxed:failure" {
		t.Fatalf("setup: %v", rec.events)
	}
	if open := tr.Open(); len(open) != 1 || !open[0].LastNotified.IsZero() {
		t.Fatalf("a rejected delivery leaves the incident undelivered: %+v", open)
	}
	tr.Tick(ctx)
	tr.Tick(ctx)
	if len(rec.events) != 2 || rec.events[1] != "opened:usage · taxed:failure" {
		t.Fatalf("the next tick retries the delivery once, got %v", rec.events)
	}
	if open := tr.Open(); open[0].LastNotified.IsZero() {
		t.Error("a delivered open must stamp last_notified")
	}
}

func TestTracker_RetriesResolvedUntilDelivered(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, usageTaxed, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	rec.failNext = 1
	tr.Observe(ctx, usageTaxed, e2eprobe.Pass())
	if len(rec.events) != 2 || rec.events[1] != "rejected:resolved:usage · taxed:failure" {
		t.Fatalf("setup: %v", rec.events)
	}
	tr.Tick(ctx)
	if len(rec.events) != 3 || rec.events[2] != "resolved:usage · taxed:failure" {
		t.Fatalf("the next tick retries the recovery message, got %v", rec.events)
	}
	tr.Tick(ctx)
	if len(rec.events) != 3 {
		t.Fatalf("a delivered recovery is not repeated: %v", rec.events)
	}
}

func TestTracker_PruneDropsBindingsNoLongerScheduled(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(t.TempDir()+"/state.json"))
	ctx := context.Background()
	stale := Key{Scenario: "usage", Persona: "gone"}
	for i := 0; i < 3; i++ {
		tr.Observe(ctx, stale, fail("assert"))
		clock.Advance(10 * time.Minute)
	}
	tr.Observe(ctx, usageTaxed, e2eprobe.Skip("window", "partial"))
	if len(rec.events) != 1 {
		t.Fatalf("setup: %v", rec.events)
	}
	if dropped := tr.Prune(ctx, []Key{usageTaxed}); !reflect.DeepEqual(dropped, []string{stale.String()}) {
		t.Fatalf("dropped = %v, want the retired binding", dropped)
	}
	if len(rec.events) != 2 || rec.events[1] != "resolved:usage · gone:failure" {
		t.Fatalf("an open incident on a retired binding is closed, got %v", rec.events)
	}
	clock.Advance(25 * time.Hour)
	tr.Tick(ctx)
	if len(rec.events) != 3 || rec.events[2] != "opened:usage · taxed:coverage" {
		t.Fatalf("only live bindings keep producing incidents, got %v", rec.events)
	}
	if _, still := tr.Snapshot().Bindings[stale.String()]; still {
		t.Error("pruned state must not come back")
	}
}

func TestTracker_RollupResolvesWhenOutageNarrows(t *testing.T) {
	tr, rec, clock := newTestTracker(t, FileStore(""))
	ctx := context.Background()
	infra := e2eprobe.Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	keys := []Key{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "low-balance-alert", Persona: "alert-canary"}}
	for i := 0; i < 3; i++ {
		for _, k := range keys {
			tr.Observe(ctx, k, infra)
		}
		tr.Tick(ctx)
		clock.Advance(10 * time.Minute)
	}
	if len(rec.events) != 1 || rec.events[0] != "opened:api:infra" {
		t.Fatalf("setup: %v", rec.events)
	}
	tr.Observe(ctx, keys[0], e2eprobe.Pass())
	tr.Observe(ctx, keys[1], e2eprobe.Pass())
	tr.Observe(ctx, keys[2], infra)
	tr.Tick(ctx)
	want := []string{"opened:api:infra", "resolved:api:infra", "opened:low-balance-alert · alert-canary:failure"}
	if !reflect.DeepEqual(rec.events, want) {
		t.Fatalf("one endpoint left failing becomes its own incident, got %v", rec.events)
	}
}

func TestTracker_KeepsRecentRunsBounded(t *testing.T) {
	tr, _, clock := newTestTracker(t, FileStore(t.TempDir()+"/state.json"))
	ctx := context.Background()
	for i := 0; i < recentRuns+12; i++ {
		clock.Advance(10 * time.Minute)
		res := e2eprobe.Pass()
		res.Duration = time.Duration(i) * time.Millisecond
		tr.Observe(ctx, usageTaxed, res)
	}
	st := tr.Snapshot().Bindings[usageTaxed.String()]
	if len(st.Recent) != recentRuns {
		t.Fatalf("recent runs = %d, want the bound %d", len(st.Recent), recentRuns)
	}
	newest := st.Recent[len(st.Recent)-1]
	if !newest.At.Equal(clock.Now()) || newest.Status != e2eprobe.StatusPass || newest.Duration != time.Duration(recentRuns+11)*time.Millisecond {
		t.Fatalf("newest run = %+v", newest)
	}
	if st.Recent[0].Duration != 12*time.Millisecond {
		t.Fatalf("the oldest surviving run must be the 13th observed, got %+v", st.Recent[0])
	}
	tr.Observe(ctx, usageTaxed, e2eprobe.Mismatch("assert", []string{"a", "b"}, nil))
	last := tr.Snapshot().Bindings[usageTaxed.String()].Recent
	if got := last[len(last)-1]; got.Status != e2eprobe.StatusFail || got.Step != "assert" || got.Mismatches != 2 {
		t.Fatalf("a failure is recorded with its step and mismatch count: %+v", got)
	}
}
