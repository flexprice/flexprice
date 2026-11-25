package incident

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

func TestFileStore_RoundTrip(t *testing.T) {
	path := t.TempDir() + "/nested/state.json"
	store := FileStore(path)
	empty, err := store.Load()
	if err != nil || empty == nil || len(empty.Bindings) != 0 {
		t.Fatalf("missing file must load an empty state, got %+v %v", empty, err)
	}
	opened := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	want := &State{Bindings: map[string]*BindingState{
		"usage · taxed": {
			Key:          Key{Scenario: "usage", Persona: "taxed"},
			Consecutive:  3,
			FailingSince: opened,
			Last:         e2eprobe.Mismatch("assert", []string{"x"}, map[string]string{"a": "b"}),
			Incident:     &Incident{ID: "usage-taxed-1", Key: Key{Scenario: "usage", Persona: "taxed"}, Kind: KindFailure, OpenedAt: opened, LastNotified: opened, Summary: "assert: x", Runs: 3},
		},
	}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	st := got.Bindings["usage · taxed"]
	if st == nil || st.Consecutive != 3 || !st.FailingSince.Equal(opened) || st.Incident == nil || st.Incident.ID != "usage-taxed-1" || st.Last.Attrs["a"] != "b" {
		t.Errorf("round trip lost data: %+v", st)
	}
	if got.SavedAt.IsZero() {
		t.Error("SavedAt must be stamped on save")
	}
}

func TestFileStore_EmptyPathKeepsStateInMemory(t *testing.T) {
	store := FileStore("")
	if err := store.Save(&State{Bindings: map[string]*BindingState{"k": {Key: Key{Scenario: "k"}}}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.Bindings["k"] == nil {
		t.Fatalf("memory store must hand back what it was given: %+v %v", got, err)
	}
}

func TestState_CloneIsolatesPendingResolutions(t *testing.T) {
	s := newState()
	s.PendingResolved = []Resolution{{Binding: BindingState{Last: e2eprobe.Result{Attrs: map[string]string{"k": "v"}, Mismatches: []string{"m"}}}}}
	cp := s.clone()
	cp.PendingResolved[0].Binding.Last.Attrs["k"] = "changed"
	cp.PendingResolved[0].Binding.Last.Mismatches[0] = "changed"
	if got := s.PendingResolved[0].Binding.Last; got.Attrs["k"] != "v" || got.Mismatches[0] != "m" {
		t.Errorf("a snapshot must not share a pending resolution's attrs or mismatches with the tracker, got %+v", got)
	}
}
