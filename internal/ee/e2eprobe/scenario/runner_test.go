package scenario

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

type recordingNotifier struct{ opened, resolved int32 }

func (r *recordingNotifier) Opened(context.Context, incident.Incident, incident.BindingState) {
	atomic.AddInt32(&r.opened, 1)
}
func (r *recordingNotifier) Reminded(context.Context, incident.Incident, incident.BindingState) {}
func (r *recordingNotifier) Resolved(context.Context, incident.Incident, incident.BindingState, time.Time) {
	atomic.AddInt32(&r.resolved, 1)
}
func (r *recordingNotifier) Digest(context.Context, incident.Digest) {}

func newTracker(t *testing.T, deps Deps) (*incident.Tracker, *recordingNotifier) {
	t.Helper()
	n := &recordingNotifier{}
	tr, err := incident.NewTracker(func(incident.Key) incident.Policy {
		return incident.Policy{AfterFailures: 1, AfterDuration: 0, SkipAfter: time.Hour}
	}, incident.DefaultRollup(), n, incident.FileStore(""), deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	return tr, n
}

func TestRunOnce_SkipsUnprovisionedPersona(t *testing.T) {
	deps, _ := newDeps(t, false)
	cat := NewCatalog()
	called := false
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Hour, func(context.Context, *Context) e2eprobe.Result {
		called = true
		return e2eprobe.Pass()
	})
	tracker, _ := newTracker(t, deps)
	b := suite.Binding{Scenario: "usage", Persona: "taxed", Every: time.Hour, Kind: e2eprobe.KindProbe}
	r, err := NewRunner(cat, deps, []suite.Binding{b}, tracker, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	res := r.RunOnce(context.Background(), b)
	if res.Status != e2eprobe.StatusSkip || res.Step != "persona" || called {
		t.Fatalf("result = %+v called=%v", res, called)
	}
	ghost := suite.Binding{Scenario: "usage", Persona: "ghost"}
	if res := r.RunOnce(context.Background(), ghost); res.Status != e2eprobe.StatusSkip {
		t.Errorf("unknown binding must skip, got %+v", res)
	}
}

func TestRunOnce_RecoversPanic(t *testing.T) {
	deps, _ := newDeps(t, true)
	cat := NewCatalog()
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Hour, func(context.Context, *Context) e2eprobe.Result {
		panic("nil deref")
	})
	tracker, _ := newTracker(t, deps)
	b := suite.Binding{Scenario: "usage", Persona: "taxed", Every: time.Hour}
	r, _ := NewRunner(cat, deps, []suite.Binding{b}, tracker, nil, Options{})
	res := r.RunOnce(context.Background(), b)
	if res.Status != e2eprobe.StatusFail || res.Step != "panic" || res.Attrs["persona"] != "taxed" {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunOnce_ObservesIntoTrackerAndStatus(t *testing.T) {
	deps, _ := newDeps(t, true)
	cat := NewCatalog()
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Hour, func(_ context.Context, sc *Context) e2eprobe.Result {
		return sc.Fail("get", errors.New("boom"))
	})
	tracker, n := newTracker(t, deps)
	b := suite.Binding{Scenario: "usage", Persona: "taxed", Every: time.Hour}
	r, _ := NewRunner(cat, deps, []suite.Binding{b}, tracker, nil, Options{})
	r.RunOnce(context.Background(), b)
	st := tracker.Snapshot().Bindings["usage · taxed"]
	if st == nil || st.Runs != 1 || st.Consecutive != 1 || atomic.LoadInt32(&n.opened) != 1 {
		t.Fatalf("tracker state = %+v opened=%d", st, n.opened)
	}
	status := r.Status()
	if len(status.Bindings) != 1 || status.Bindings[0].Runs != 1 || status.Bindings[0].Last.Status != e2eprobe.StatusFail || len(status.Open) != 1 {
		t.Errorf("status = %+v", status)
	}
}

func TestStart_RespectsConcurrency(t *testing.T) {
	deps, _ := newDeps(t, true)
	cat := NewCatalog()
	var running, maxSeen int32
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Hour, func(context.Context, *Context) e2eprobe.Result {
		cur := atomic.AddInt32(&running, 1)
		for {
			prev := atomic.LoadInt32(&maxSeen)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxSeen, prev, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return e2eprobe.Pass()
	})
	var bindings []suite.Binding
	for _, p := range deps.Suite.Personas {
		if p.HasTrait(e2eprobe.TraitEvents) {
			bindings = append(bindings, suite.Binding{Scenario: "usage", Persona: p.Name, Every: time.Hour})
		}
	}
	tracker, _ := newTracker(t, deps)
	r, err := NewRunner(cat, deps, bindings, tracker, nil, Options{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	r.Start(ctx)
	if got := atomic.LoadInt32(&maxSeen); got == 0 || got > 2 {
		t.Errorf("max concurrent runs = %d, want 1..2", got)
	}
	st := r.Status()
	if len(st.Bindings) != 8 {
		t.Fatalf("bindings = %d", len(st.Bindings))
	}
	for _, b := range st.Bindings {
		if b.Runs != 1 {
			t.Errorf("%s ran %d times, want 1", b.Persona, b.Runs)
		}
	}
}

func TestStart_UsesTickWhenEveryZero(t *testing.T) {
	deps, _ := newDeps(t, true)
	deps.Tick = 40 * time.Millisecond
	cat := NewCatalog()
	var runs int32
	registerStub(cat, "ingest", []e2eprobe.Trait{e2eprobe.TraitEvents}, 0, func(context.Context, *Context) e2eprobe.Result {
		atomic.AddInt32(&runs, 1)
		return e2eprobe.Pass()
	})
	tracker, _ := newTracker(t, deps)
	b := suite.Binding{Scenario: "ingest", Persona: "taxed", Every: 0}
	r, _ := NewRunner(cat, deps, []suite.Binding{b}, tracker, nil, Options{Concurrency: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	r.Start(ctx)
	if got := atomic.LoadInt32(&runs); got < 3 {
		t.Errorf("a zero interval must fall back to the tick; got %d runs", got)
	}
}
