package incident

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// Tracker owns the incident state machine for every binding.
type Tracker struct {
	mu        sync.Mutex
	policy    func(Key) Policy
	rollup    Rollup
	notifier  Notifier
	store     Store
	now       func() time.Time
	state     *State
	infraSeen map[string]time.Time
}

// NewTracker restores state from the store; restored incidents are not re-announced.
func NewTracker(policy func(Key) Policy, rollup Rollup, n Notifier, store Store, now func() time.Time) (*Tracker, error) {
	state, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("incident: load state: %w", err)
	}
	if rollup.MinBindings <= 0 || rollup.Window <= 0 {
		rollup = DefaultRollup()
	}
	return &Tracker{
		policy:    policy,
		rollup:    rollup,
		notifier:  n,
		store:     store,
		now:       now,
		state:     state,
		infraSeen: map[string]time.Time{},
	}, nil
}

// Observe records one run and opens or resolves incidents for its binding.
func (t *Tracker) Observe(ctx context.Context, k Key, r e2eprobe.Result) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	st := t.binding(k)
	st.Runs++
	st.LastRun = now
	st.Last = r

	switch {
	case r.Status == e2eprobe.StatusPass:
		st.Passes++
		st.Consecutive = 0
		st.FailingSince = time.Time{}
		st.SkippingSince = time.Time{}
		delete(t.infraSeen, k.String())
		t.resolve(ctx, st, now)

	case r.Status == e2eprobe.StatusSkip:
		st.Consecutive = 0
		st.FailingSince = time.Time{}
		if st.SkippingSince.IsZero() {
			st.SkippingSince = now
		}

	default:
		if r.Infra {
			t.infraSeen[k.String()] = now
		}
		if st.Incident != nil && st.Incident.Kind == KindCoverage {
			t.resolve(ctx, st, now)
		}
		st.SkippingSince = time.Time{}
		st.Consecutive++
		if st.FailingSince.IsZero() {
			st.FailingSince = now
		}
		policy := t.policy(k)
		switch {
		case st.Incident != nil:
			st.Incident.Runs = st.Consecutive
			st.Incident.Summary = r.Summary()
		case r.Infra && t.state.Infra != nil:
			// A region-wide outage is one incident; its bindings do not page on their own.
		case st.Consecutive >= policy.AfterFailures && now.Sub(st.FailingSince) >= policy.AfterDuration:
			t.open(ctx, st, KindFailure, r.Summary(), now)
		}
	}
	t.save()
}

// Tick sends reminders, opens coverage incidents and evaluates the infra rollup.
func (t *Tracker) Tick(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for _, key := range t.sortedKeys() {
		st := t.state.Bindings[key]
		policy := t.policy(st.Key)
		if st.Incident == nil && !st.SkippingSince.IsZero() && now.Sub(st.SkippingSince) >= policy.SkipAfter {
			t.open(ctx, st, KindCoverage, st.Last.Summary(), now)
			continue
		}
		t.remind(ctx, st.Incident, *st, policy, now)
	}
	t.evaluateInfra(ctx, now)
	t.save()
}

func (t *Tracker) evaluateInfra(ctx context.Context, now time.Time) {
	affected := 0
	for key, seen := range t.infraSeen {
		if now.Sub(seen) > t.rollup.Window {
			delete(t.infraSeen, key)
			continue
		}
		affected++
	}
	synthetic := BindingState{Key: infraKey}
	switch {
	case t.state.Infra == nil && affected >= t.rollup.MinBindings:
		inc := Incident{
			ID:           incidentID(infraKey, now),
			Key:          infraKey,
			Kind:         KindInfra,
			OpenedAt:     now,
			LastNotified: now,
			Summary:      fmt.Sprintf("%d bindings saw infrastructure errors in the last %s", affected, t.rollup.Window),
			Runs:         affected,
		}
		t.state.Infra = &inc
		t.notifier.Opened(ctx, inc, synthetic)
	case t.state.Infra != nil && affected == 0:
		inc := *t.state.Infra
		t.state.Infra = nil
		t.notifier.Resolved(ctx, inc, synthetic, now)
	case t.state.Infra != nil:
		t.state.Infra.Runs = affected
		t.remind(ctx, t.state.Infra, synthetic, t.policy(infraKey), now)
	}
}

func (t *Tracker) remind(ctx context.Context, inc *Incident, st BindingState, policy Policy, now time.Time) {
	if inc == nil || policy.RenotifyEvery <= 0 || now.Sub(inc.LastNotified) < policy.RenotifyEvery {
		return
	}
	inc.LastNotified = now
	t.notifier.Reminded(ctx, *inc, st)
}

func (t *Tracker) open(ctx context.Context, st *BindingState, kind IncidentKind, summary string, now time.Time) {
	inc := Incident{
		ID:           incidentID(st.Key, now),
		Key:          st.Key,
		Kind:         kind,
		OpenedAt:     now,
		LastNotified: now,
		Summary:      summary,
		Runs:         st.Consecutive,
	}
	st.Incident = &inc
	t.notifier.Opened(ctx, inc, *st)
}

func (t *Tracker) resolve(ctx context.Context, st *BindingState, now time.Time) {
	if st.Incident == nil {
		return
	}
	inc := *st.Incident
	st.Incident = nil
	t.notifier.Resolved(ctx, inc, *st, now)
}

func (t *Tracker) binding(k Key) *BindingState {
	st, ok := t.state.Bindings[k.String()]
	if !ok {
		st = &BindingState{Key: k}
		t.state.Bindings[k.String()] = st
	}
	return st
}

func (t *Tracker) sortedKeys() []string {
	keys := make([]string, 0, len(t.state.Bindings))
	for k := range t.state.Bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (t *Tracker) save() {
	// A failed save loses only the restart snapshot; the in-memory state stays authoritative.
	_ = t.store.Save(t.state)
}

func (t *Tracker) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state.clone()
}

// Open lists every open incident, oldest first.
func (t *Tracker) Open() []Incident {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.openLocked()
}

func (t *Tracker) openLocked() []Incident {
	var out []Incident
	for _, st := range t.state.Bindings {
		if st.Incident != nil {
			out = append(out, *st.Incident)
		}
	}
	if t.state.Infra != nil {
		out = append(out, *t.state.Infra)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt.Before(out[j].OpenedAt) })
	return out
}

// Digest is the periodic summary: open incidents, every binding's state and
// whatever extra facts the runner adds (inbox counts, event budget).
type Digest struct {
	GeneratedAt time.Time
	Open        []Incident
	Bindings    []BindingState
	Extra       map[string]string
}

func (t *Tracker) Digest(extra map[string]string) Digest {
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := t.state.clone()
	bindings := make([]BindingState, 0, len(snap.Bindings))
	for _, key := range t.sortedKeys() {
		bindings = append(bindings, *snap.Bindings[key])
	}
	return Digest{GeneratedAt: t.now(), Open: t.openLocked(), Bindings: bindings, Extra: extra}
}

func incidentID(k Key, at time.Time) string {
	return fmt.Sprintf("%s-%d", strings.ReplaceAll(k.String(), " · ", "-"), at.Unix())
}
