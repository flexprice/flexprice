package incident

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

// Tracker owns the incident state machine for every binding.
type Tracker struct {
	mu       sync.Mutex
	policy   func(Key) Policy
	rollup   Rollup
	notifier Notifier
	store    Store
	now      func() time.Time
	state    *State
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
	if state.InfraSeen == nil {
		state.InfraSeen = map[string]time.Time{}
	}
	return &Tracker{
		policy:   policy,
		rollup:   rollup,
		notifier: n,
		store:    store,
		now:      now,
		state:    state,
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
		delete(t.state.InfraSeen, k.String())
		t.resolve(ctx, st, now)

	case r.Status == e2eprobe.StatusSkip:
		st.Consecutive = 0
		st.FailingSince = time.Time{}
		if st.SkippingSince.IsZero() {
			st.SkippingSince = now
		}

	default:
		if r.Infra {
			t.state.InfraSeen[k.String()] = now
		}
		if st.Incident != nil && st.Incident.Kind == KindCoverage {
			t.resolve(ctx, st, now)
		}
		st.SkippingSince = time.Time{}
		st.Consecutive++
		if st.FailingSince.IsZero() {
			st.FailingSince = now
		}
		switch {
		case st.Incident != nil:
			st.Incident.Runs = st.Consecutive
			st.Incident.Summary = r.Summary()
		case r.Infra:
			// Infrastructure failures page from Tick, after the rollup has had its say.
		case meetsThresholds(st, t.policy(k), now):
			t.open(ctx, st, KindFailure, r.Summary(), now)
		}
	}
	t.save()
}

// Tick evaluates the infra rollup first, then pages infrastructure failures
// that are not covered by it, opens coverage incidents, retries undelivered
// messages and sends reminders.
func (t *Tracker) Tick(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.evaluateInfra(ctx, now)
	for _, key := range t.sortedKeys() {
		st := t.state.Bindings[key]
		policy := t.policy(st.Key)
		switch {
		case st.Incident == nil && !st.SkippingSince.IsZero() && now.Sub(st.SkippingSince) >= policy.SkipAfter:
			t.open(ctx, st, KindCoverage, st.Last.Summary(), now)
		case st.Incident == nil && infraFailing(st) && t.state.Infra == nil && meetsThresholds(st, policy, now):
			t.open(ctx, st, KindFailure, st.Last.Summary(), now)
		case st.Incident != nil && infraFailing(st) && t.state.Infra != nil:
			// Covered by the region-wide incident; its own reminders would only repeat it.
		default:
			t.remind(ctx, st.Incident, *st, policy, now)
		}
	}
	t.save()
}

// meetsThresholds is the paging gate: enough consecutive failures for long enough.
func meetsThresholds(st *BindingState, p Policy, now time.Time) bool {
	return st.Consecutive >= p.AfterFailures && now.Sub(st.FailingSince) >= p.AfterDuration
}

func infraFailing(st *BindingState) bool {
	return st.Consecutive > 0 && st.Last.Status == e2eprobe.StatusFail && st.Last.Infra
}

func (t *Tracker) evaluateInfra(ctx context.Context, now time.Time) {
	var affected []string
	for key, seen := range t.state.InfraSeen {
		if now.Sub(seen) > t.rollup.Window {
			delete(t.state.InfraSeen, key)
			continue
		}
		affected = append(affected, key)
	}
	sort.Strings(affected)
	synthetic := BindingState{Key: infraKey}
	switch {
	case t.state.Infra == nil && len(affected) >= t.rollup.MinBindings:
		inc := Incident{
			ID:       incidentID(infraKey, now),
			Key:      infraKey,
			Kind:     KindInfra,
			OpenedAt: now,
			Summary:  fmt.Sprintf("%d bindings saw infrastructure errors in the last %s: %s", len(affected), t.rollup.Window, strings.Join(affected, ", ")),
			Runs:     len(affected),
		}
		t.state.Infra = &inc
		t.notifyOpened(ctx, t.state.Infra, synthetic, now)
	case t.state.Infra != nil && len(affected) == 0:
		inc := *t.state.Infra
		t.state.Infra = nil
		_ = t.notifier.Resolved(ctx, inc, synthetic, now)
	case t.state.Infra != nil:
		t.state.Infra.Runs = len(affected)
		t.remind(ctx, t.state.Infra, synthetic, t.policy(infraKey), now)
	}
}

// remind retries an undelivered open first; otherwise it reminds once per
// renotify window. A rejected delivery leaves the stamp alone so the next
// tick tries again.
func (t *Tracker) remind(ctx context.Context, inc *Incident, st BindingState, policy Policy, now time.Time) {
	switch {
	case inc == nil:
	case inc.LastNotified.IsZero():
		t.notifyOpened(ctx, inc, st, now)
	case policy.RenotifyEvery <= 0 || now.Sub(inc.LastNotified) < policy.RenotifyEvery:
	default:
		if err := t.notifier.Reminded(ctx, *inc, st); err == nil {
			inc.LastNotified = now
		}
	}
}

func (t *Tracker) notifyOpened(ctx context.Context, inc *Incident, st BindingState, now time.Time) {
	if err := t.notifier.Opened(ctx, *inc, st); err == nil {
		inc.LastNotified = now
	}
}

func (t *Tracker) open(ctx context.Context, st *BindingState, kind IncidentKind, summary string, now time.Time) {
	inc := Incident{
		ID:       incidentID(st.Key, now),
		Key:      st.Key,
		Kind:     kind,
		OpenedAt: now,
		Summary:  summary,
		Runs:     st.Consecutive,
	}
	st.Incident = &inc
	t.notifyOpened(ctx, st.Incident, *st, now)
}

func (t *Tracker) resolve(ctx context.Context, st *BindingState, now time.Time) {
	if st.Incident == nil {
		return
	}
	inc := *st.Incident
	st.Incident = nil
	_ = t.notifier.Resolved(ctx, inc, *st, now)
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
