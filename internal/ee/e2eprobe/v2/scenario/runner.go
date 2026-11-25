package scenario

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Options tune the runner; zero values take the defaults below.
type Options struct {
	Concurrency int
	Jitter      time.Duration
	Heartbeat   time.Duration
	DigestEvery time.Duration
	TrackerTick time.Duration
	// RunTimeout bounds one run so a hung call cannot pin a slot and silence a binding.
	RunTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.TrackerTick <= 0 {
		o.TrackerTick = time.Minute
	}
	if o.RunTimeout <= 0 {
		o.RunTimeout = 30 * time.Minute
	}
	return o
}

type item struct {
	binding  suite.Binding
	scenario Scenario
	running  atomic.Bool
}

// Runner schedules every binding and records what each run reported.
type Runner struct {
	cat      *Catalog
	deps     Deps
	tracker  *incident.Tracker
	notifier incident.Notifier
	opts     Options
	tracer   trace.Tracer
	items    map[string]*item
	order    []string
	sem      chan struct{}

	mu      sync.Mutex
	status  map[string]*BindingStatus
	started time.Time
}

func bindingKey(b suite.Binding) incident.Key {
	return incident.Key{Scenario: b.Scenario, Persona: b.Persona}
}

// NewRunner builds one scenario instance per binding so per-binding state
// (cursors, legs) never leaks across personas.
func NewRunner(cat *Catalog, d Deps, bindings []suite.Binding, tracker *incident.Tracker, notifier incident.Notifier, opts Options) (*Runner, error) {
	opts = opts.withDefaults()
	tracer := d.Tracer
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("e2eprobe")
	}
	r := &Runner{cat: cat, deps: d, tracker: tracker, notifier: notifier, opts: opts, tracer: tracer, items: map[string]*item{}, sem: make(chan struct{}, opts.Concurrency), status: map[string]*BindingStatus{}}
	for _, b := range bindings {
		s, err := cat.Build(b.Scenario, d)
		if err != nil {
			return nil, err
		}
		key := bindingKey(b).String()
		r.items[key] = &item{binding: b, scenario: s}
		r.order = append(r.order, key)
		r.status[key] = &BindingStatus{Scenario: b.Scenario, Persona: b.Persona, Every: r.every(b)}
	}
	return r, nil
}

func (r *Runner) every(b suite.Binding) time.Duration {
	if b.Every > 0 {
		return b.Every
	}
	return r.deps.Tick
}

// Start runs every binding on its interval until ctx ends. Tracker state for
// bindings the suite no longer schedules is dropped first, closing their incidents.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	r.started = r.deps.now()
	r.mu.Unlock()
	keys := make([]incident.Key, 0, len(r.items))
	for _, key := range r.order {
		keys = append(keys, bindingKey(r.items[key].binding))
	}
	if dropped := r.tracker.Prune(ctx, keys); len(dropped) > 0 && r.deps.Logger != nil {
		r.deps.Logger.Info(ctx, "e2eprobe forgot bindings the suite no longer schedules", "bindings", strings.Join(dropped, ","))
	}
	var wg sync.WaitGroup
	for _, key := range r.order {
		it := r.items[key]
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.loop(ctx, it)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.housekeeping(ctx)
	}()
	wg.Wait()
}

// startDelay spreads first runs across the jitter window deterministically,
// so a binding starts at the same offset on every boot.
func startDelay(key string, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return time.Duration(h.Sum64() % uint64(jitter))
}

func (r *Runner) loop(ctx context.Context, it *item) {
	if delay := startDelay(bindingKey(it.binding).String(), r.opts.Jitter); delay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	every := r.every(it.binding)
	if every <= 0 {
		every = time.Minute
	}
	r.scheduleNext(it, every)
	r.execute(ctx, it)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.scheduleNext(it, every)
			r.execute(ctx, it)
		}
	}
}

// scheduleNext records when the ticker fires again, independent of how long the run takes.
func (r *Runner) scheduleNext(it *item, every time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[bindingKey(it.binding).String()].NextRun = r.deps.now().Add(every)
}

// housekeeping drives the tracker clock, the heartbeat log and the digest.
func (r *Runner) housekeeping(ctx context.Context) {
	tick := time.NewTicker(r.opts.TrackerTick)
	defer tick.Stop()
	var heartbeat, digest <-chan time.Time
	if r.opts.Heartbeat > 0 {
		h := time.NewTicker(r.opts.Heartbeat)
		defer h.Stop()
		heartbeat = h.C
	}
	if r.opts.DigestEvery > 0 && r.notifier != nil {
		d := time.NewTicker(r.opts.DigestEvery)
		defer d.Stop()
		digest = d.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.tracker.Tick(ctx)
		case <-heartbeat:
			r.logHeartbeat(ctx)
		case <-digest:
			if err := r.notifier.Digest(ctx, r.tracker.Digest(r.digestExtra())); err != nil && r.deps.Logger != nil {
				r.deps.Logger.Error(ctx, "e2eprobe digest delivery failed", "error", err)
			}
		}
	}
}

// RunOnce executes a binding immediately; unknown bindings skip.
func (r *Runner) RunOnce(ctx context.Context, b suite.Binding) e2eprobe.Result {
	it, ok := r.items[bindingKey(b).String()]
	if !ok {
		return e2eprobe.Skip("binding", "unknown binding")
	}
	return r.execute(ctx, it)
}

func (r *Runner) execute(ctx context.Context, it *item) e2eprobe.Result {
	// One run per binding at a time: the scenario instance is shared and a slow
	// run must not be lapped by its own schedule.
	if !it.running.CompareAndSwap(false, true) {
		return e2eprobe.Skip("in_flight", "previous run of this binding is still in progress")
	}
	defer it.running.Store(false)
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return e2eprobe.Skip("shutdown", "runner stopping")
	}
	defer func() { <-r.sem }()

	b := it.binding
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, r.opts.RunTimeout)
	defer cancel()
	ctx, span := r.tracer.Start(ctx, "e2eprobe.scenario", trace.WithAttributes(
		attribute.String("e2eprobe.scenario", b.Scenario),
		attribute.String("e2eprobe.persona", b.Persona),
		attribute.String("e2eprobe.run_id", r.deps.RunID),
	))
	defer span.End()

	start := time.Now()
	res := r.run(ctx, it)
	res.Duration = time.Since(start)
	if parent.Err() != nil && res.Status != e2eprobe.StatusPass {
		// A run cut short by shutdown is not evidence either way.
		return res
	}
	span.SetAttributes(attribute.String("e2eprobe.status", string(res.Status)), attribute.String("e2eprobe.step", res.Step))
	if res.Failed() {
		span.SetStatus(codes.Error, res.Summary())
	}
	r.record(it, res)
	// The run context may already be past its deadline; pages go out on the parent.
	r.tracker.Observe(parent, bindingKey(b), res)
	if res.Status != e2eprobe.StatusPass && r.deps.Logger != nil {
		r.deps.Logger.Info(parent, "e2eprobe scenario did not pass",
			"scenario", b.Scenario, "persona", b.Persona, "status", string(res.Status), "step", res.Step, "reason", res.Reason, "infra", strconv.FormatBool(res.Infra))
	}
	return res
}

// run resolves the persona and executes with panic recovery.
func (r *Runner) run(ctx context.Context, it *item) (res e2eprobe.Result) {
	b := it.binding
	var persona = (*world.Persona)(nil)
	if b.Persona != "" {
		p, ok := r.deps.World.Persona(b.Persona)
		if !ok || !p.Ready() {
			sc := NewContext(r.deps, p, b.Scenario)
			if p == nil {
				sc = NewContext(r.deps, nil, b.Scenario)
				sc.Attr("persona", b.Persona)
			}
			return sc.Skip("persona", "persona not provisioned")
		}
		persona = p
	}
	sc := NewContext(r.deps, persona, b.Scenario)
	defer func() {
		if rec := recover(); rec != nil {
			res = sc.Fail("panic", fmt.Errorf("panic: %v", rec))
		}
	}()
	return it.scenario.Run(ctx, sc)
}

func (r *Runner) record(it *item, res e2eprobe.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.status[bindingKey(it.binding).String()]
	now := r.deps.now()
	st.Runs++
	if res.Status == e2eprobe.StatusPass {
		st.Passes++
	}
	st.LastRun = now
	st.Last = res
}

func (r *Runner) logHeartbeat(ctx context.Context) {
	if r.deps.Logger == nil {
		return
	}
	st := r.Status()
	runs, passes := 0, 0
	for _, b := range st.Bindings {
		runs += b.Runs
		passes += b.Passes
	}
	r.deps.Logger.Info(ctx, "e2eprobe heartbeat",
		"event", "e2eprobe.heartbeat", "run_id", r.deps.RunID, "uptime", r.deps.now().Sub(st.StartedAt).Round(time.Second).String(),
		"bindings", len(st.Bindings), "runs", runs, "passes", passes, "open_incidents", len(st.Open),
		"webhooks_received", st.Inbox.Total)
}

func (r *Runner) digestExtra() map[string]string {
	st := r.Status()
	extra := map[string]string{
		"webhooks received": strconv.Itoa(st.Inbox.Total),
		"events/day":        strconv.Itoa(r.deps.Suite.EventsPerDay(r.cat)),
	}
	if !st.Inbox.LastReceivedAt.IsZero() {
		extra["last webhook"] = st.Inbox.LastReceivedAt.UTC().Format(time.RFC3339)
	}
	batches := 0
	for _, ps := range st.Ledger.Personas {
		batches += ps.Batches
	}
	extra["event batches sent"] = strconv.Itoa(batches)
	return extra
}
