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

// Tracker owns the incident state machine for every binding. Transitions are
// decided under the lock and notifications are delivered after it is released,
// so a slow Slack never stalls the runner.
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
	now := t.now()
	st := t.binding(k)
	st.Runs++
	st.LastRun = now
	st.Last = r
	st.remember(now, r)
	var out []delivery
	switch {
	case r.Status == e2eprobe.StatusPass:
		st.Passes++
		st.Consecutive = 0
		st.FailingSince = time.Time{}
		st.SkippingSince = time.Time{}
		delete(t.state.InfraSeen, k.String())
		out = t.resolve(st, now, out)

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
			out = t.resolve(st, now, out)
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
			out = t.open(st, KindFailure, r.Summary(), now, out)
		}
	}
	t.save()
	t.mu.Unlock()
	t.deliver(ctx, out, now)
}

// Tick evaluates the infra rollup first, then pages infrastructure failures
// that are not covered by it, opens coverage incidents, retries undelivered
// messages and sends reminders.
func (t *Tracker) Tick(ctx context.Context) {
	t.mu.Lock()
	now := t.now()
	var out []delivery
	out = t.evaluateInfra(now, out)
	for _, key := range t.sortedKeys() {
		st := t.state.Bindings[key]
		policy := t.policy(st.Key)
		switch {
		case st.Incident == nil && !st.SkippingSince.IsZero() && now.Sub(st.SkippingSince) >= policy.SkipAfter:
			out = t.open(st, KindCoverage, st.Last.Summary(), now, out)
		case st.Incident == nil && infraFailing(st) && t.state.Infra == nil && meetsThresholds(st, policy, now):
			out = t.open(st, KindFailure, st.Last.Summary(), now, out)
		case st.Incident != nil && infraFailing(st) && t.state.Infra != nil:
			// Covered by the region-wide incident; its own reminders would only repeat it.
		default:
			out = t.remind(st.Incident, *st, policy, now, out)
		}
	}
	for _, p := range t.state.PendingResolved {
		out = append(out, delivery{kind: deliverResolved, inc: p.Incident, st: p.Binding, closedAt: p.ClosedAt, retry: true})
	}
	t.save()
	t.mu.Unlock()
	t.deliver(ctx, out, now)
}

// Prune forgets bindings the suite no longer schedules and closes their open
// incidents, so a retired binding cannot page or remind forever.
func (t *Tracker) Prune(ctx context.Context, active []Key) []string {
	t.mu.Lock()
	now := t.now()
	keep := make(map[string]bool, len(active))
	for _, k := range active {
		keep[k.String()] = true
	}
	var dropped []string
	var out []delivery
	for _, key := range t.sortedKeys() {
		if keep[key] {
			continue
		}
		out = t.resolve(t.state.Bindings[key], now, out)
		delete(t.state.Bindings, key)
		delete(t.state.InfraSeen, key)
		dropped = append(dropped, key)
	}
	if len(dropped) > 0 {
		t.save()
	}
	t.mu.Unlock()
	t.deliver(ctx, out, now)
	return dropped
}

// meetsThresholds is the paging gate: enough consecutive failures for long enough.
func meetsThresholds(st *BindingState, p Policy, now time.Time) bool {
	return st.Consecutive >= p.AfterFailures && now.Sub(st.FailingSince) >= p.AfterDuration
}

func infraFailing(st *BindingState) bool {
	return st.Consecutive > 0 && st.Last.Status == e2eprobe.StatusFail && st.Last.Infra
}

// evaluateInfra opens the region-wide incident when enough bindings fail on
// infrastructure inside the window and closes it as soon as fewer do, so a
// lone broken endpoint pages as itself instead of hiding under the rollup.
func (t *Tracker) evaluateInfra(now time.Time, out []delivery) []delivery {
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
	summary := fmt.Sprintf("%d bindings saw infrastructure errors in the last %s: %s", len(affected), t.rollup.Window, strings.Join(affected, ", "))
	switch {
	case t.state.Infra == nil && len(affected) >= t.rollup.MinBindings:
		inc := Incident{ID: incidentID(infraKey, now), Key: infraKey, Kind: KindInfra, OpenedAt: now, Summary: summary, Runs: len(affected)}
		t.state.Infra = &inc
		return append(out, delivery{kind: deliverOpened, inc: inc, st: synthetic})
	case t.state.Infra != nil && len(affected) < t.rollup.MinBindings:
		inc := *t.state.Infra
		t.state.Infra = nil
		return append(out, delivery{kind: deliverResolved, inc: inc, st: synthetic, closedAt: now})
	case t.state.Infra != nil:
		t.state.Infra.Runs = len(affected)
		t.state.Infra.Summary = summary
		return t.remind(t.state.Infra, synthetic, t.policy(infraKey), now, out)
	}
	return out
}

// remind retries an undelivered open first; otherwise it reminds once per
// renotify window.
func (t *Tracker) remind(inc *Incident, st BindingState, policy Policy, now time.Time, out []delivery) []delivery {
	switch {
	case inc == nil:
		return out
	case inc.LastNotified.IsZero():
		return append(out, delivery{kind: deliverOpened, inc: *inc, st: st})
	case policy.RenotifyEvery <= 0 || now.Sub(inc.LastNotified) < policy.RenotifyEvery:
		return out
	}
	return append(out, delivery{kind: deliverReminded, inc: *inc, st: st})
}

func (t *Tracker) open(st *BindingState, kind IncidentKind, summary string, now time.Time, out []delivery) []delivery {
	inc := Incident{ID: incidentID(st.Key, now), Key: st.Key, Kind: kind, OpenedAt: now, Summary: summary, Runs: st.Consecutive}
	st.Incident = &inc
	return append(out, delivery{kind: deliverOpened, inc: inc, st: *st})
}

func (t *Tracker) resolve(st *BindingState, now time.Time, out []delivery) []delivery {
	if st.Incident == nil {
		return out
	}
	inc := *st.Incident
	st.Incident = nil
	return append(out, delivery{kind: deliverResolved, inc: inc, st: *st, closedAt: now})
}

type deliveryKind int

const (
	deliverOpened deliveryKind = iota
	deliverReminded
	deliverResolved
)

// delivery is one notification decided under the lock and sent outside it.
type delivery struct {
	kind     deliveryKind
	inc      Incident
	st       BindingState
	closedAt time.Time
	retry    bool // a resolution already waiting on the pending list
}

// deliver sends what the locked phase decided and records the outcome: a
// delivered open or reminder is stamped, an undelivered recovery waits for
// the next tick.
func (t *Tracker) deliver(ctx context.Context, out []delivery, now time.Time) {
	if len(out) == 0 {
		return
	}
	errs := make([]error, len(out))
	for i, d := range out {
		switch d.kind {
		case deliverOpened:
			errs[i] = t.notifier.Opened(ctx, d.inc, d.st)
		case deliverReminded:
			errs[i] = t.notifier.Reminded(ctx, d.inc, d.st)
		case deliverResolved:
			errs[i] = t.notifier.Resolved(ctx, d.inc, d.st, d.closedAt)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, d := range out {
		switch {
		case d.kind == deliverResolved && errs[i] == nil:
			t.forgetResolution(d.inc.ID)
		case d.kind == deliverResolved && !d.retry:
			t.state.PendingResolved = append(t.state.PendingResolved, Resolution{Incident: d.inc, Binding: d.st, ClosedAt: d.closedAt})
		case d.kind != deliverResolved && errs[i] == nil:
			t.stamp(d.inc, now)
		}
	}
	t.save()
}

// stamp marks a delivered open or reminder on the incident, if it is still the live one.
func (t *Tracker) stamp(inc Incident, now time.Time) {
	if inc.Key == infraKey {
		if t.state.Infra != nil && t.state.Infra.ID == inc.ID {
			t.state.Infra.LastNotified = now
		}
		return
	}
	if st := t.state.Bindings[inc.Key.String()]; st != nil && st.Incident != nil && st.Incident.ID == inc.ID {
		st.Incident.LastNotified = now
	}
}

func (t *Tracker) forgetResolution(id string) {
	kept := t.state.PendingResolved[:0]
	for _, p := range t.state.PendingResolved {
		if p.Incident.ID != id {
			kept = append(kept, p)
		}
	}
	t.state.PendingResolved = kept
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
