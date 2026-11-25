package incident

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

type IncidentKind string

const (
	KindFailure  IncidentKind = "failure"
	KindCoverage IncidentKind = "coverage"
	KindInfra    IncidentKind = "infra"
)

type Incident struct {
	ID           string       `json:"id"`
	Key          Key          `json:"key"`
	Kind         IncidentKind `json:"kind"`
	OpenedAt     time.Time    `json:"opened_at"`
	LastNotified time.Time    `json:"last_notified"`
	Summary      string       `json:"summary"`
	Runs         int          `json:"runs"`
}

// recentRuns bounds the per-binding history: two days of a 10-minute cadence.
const recentRuns = 288

// Run is one recorded run, the unit of the history behind /history and the
// dashboard; reasons are clipped so the state file stays small.
type Run struct {
	At         time.Time       `json:"at"`
	Status     e2eprobe.Status `json:"status"`
	Step       string          `json:"step,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Infra      bool            `json:"infra,omitempty"`
	Duration   time.Duration   `json:"duration,omitempty"`
	Mismatches int             `json:"mismatches,omitempty"`
}

// BindingState is everything the tracker remembers about one binding.
type BindingState struct {
	Key           Key             `json:"key"`
	Consecutive   int             `json:"consecutive"`
	FailingSince  time.Time       `json:"failing_since,omitzero"`
	SkippingSince time.Time       `json:"skipping_since,omitzero"`
	LastRun       time.Time       `json:"last_run,omitzero"`
	Last          e2eprobe.Result `json:"last"`
	Runs          int             `json:"runs"`
	Passes        int             `json:"passes"`
	Incident      *Incident       `json:"incident,omitempty"`
	Recent        []Run           `json:"recent,omitempty"`
}

// remember appends a run and drops the oldest beyond the bound.
func (st *BindingState) remember(at time.Time, r e2eprobe.Result) {
	reason := r.Reason
	if len(reason) > 200 {
		reason = reason[:200]
	}
	st.Recent = append(st.Recent, Run{At: at, Status: r.Status, Step: r.Step, Reason: reason, Infra: r.Infra, Duration: r.Duration, Mismatches: len(r.Mismatches)})
	if len(st.Recent) > recentRuns {
		st.Recent = append([]Run(nil), st.Recent[len(st.Recent)-recentRuns:]...)
	}
}

// Resolution is a recovery message that was not delivered yet.
type Resolution struct {
	Incident Incident     `json:"incident"`
	Binding  BindingState `json:"binding"`
	ClosedAt time.Time    `json:"closed_at"`
}

// State is the snapshot persisted across restarts.
type State struct {
	Bindings map[string]*BindingState `json:"bindings"`
	Infra    *Incident                `json:"infra,omitempty"`
	// InfraSeen is when each binding last failed on infrastructure; it feeds the
	// rollup and survives restarts so an outage is not declared over on boot.
	InfraSeen       map[string]time.Time `json:"infra_seen,omitempty"`
	PendingResolved []Resolution         `json:"pending_resolved,omitempty"`
	SavedAt         time.Time            `json:"saved_at"`
}

func newState() *State {
	return &State{Bindings: map[string]*BindingState{}, InfraSeen: map[string]time.Time{}}
}

func (s *State) clone() State {
	out := State{Bindings: make(map[string]*BindingState, len(s.Bindings)), InfraSeen: make(map[string]time.Time, len(s.InfraSeen)), SavedAt: s.SavedAt}
	for k, v := range s.InfraSeen {
		out.InfraSeen[k] = v
	}
	for k, st := range s.Bindings {
		cp := st.clone()
		out.Bindings[k] = &cp
	}
	if s.Infra != nil {
		inc := *s.Infra
		out.Infra = &inc
	}
	out.PendingResolved = make([]Resolution, len(s.PendingResolved))
	for i, r := range s.PendingResolved {
		r.Binding = r.Binding.clone()
		out.PendingResolved[i] = r
	}
	return out
}

// clone copies the binding so a snapshot shares no map or slice with the tracker.
func (st *BindingState) clone() BindingState {
	cp := *st
	cp.Last.Mismatches = append([]string(nil), st.Last.Mismatches...)
	if st.Last.Attrs != nil {
		cp.Last.Attrs = make(map[string]string, len(st.Last.Attrs))
		for k, v := range st.Last.Attrs {
			cp.Last.Attrs[k] = v
		}
	}
	if st.Incident != nil {
		inc := *st.Incident
		cp.Incident = &inc
	}
	cp.Recent = append([]Run(nil), st.Recent...)
	return cp
}

// Store persists tracker state so a restart keeps open incidents.
type Store interface {
	Load() (*State, error)
	Save(*State) error
}

// FileStore writes state as JSON at path; an empty path keeps it in memory.
func FileStore(path string) Store {
	if path == "" {
		return &memoryStore{}
	}
	return &fileStore{path: path}
}

type memoryStore struct {
	mu    sync.Mutex
	state *State
}

func (m *memoryStore) Load() (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return newState(), nil
	}
	cp := m.state.clone()
	return &cp, nil
}

func (m *memoryStore) Save(s *State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := s.clone()
	cp.SavedAt = time.Now()
	m.state = &cp
	return nil
}

type fileStore struct {
	path string
}

func (f *fileStore) Load() (*State, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return newState(), nil
	}
	if err != nil {
		return nil, err
	}
	s := newState()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if s.Bindings == nil {
		s.Bindings = map[string]*BindingState{}
	}
	if s.InfraSeen == nil {
		s.InfraSeen = map[string]time.Time{}
	}
	return s, nil
}

// Save writes through a temp file and rename so a crash never leaves a torn file.
func (f *fileStore) Save(s *State) error {
	s.SavedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
