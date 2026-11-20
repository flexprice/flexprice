package inbox

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrTimeout = errors.New("inbox: no matching webhook before timeout")

// Options bound the store and optionally require a shared token on every post.
type Options struct {
	Capacity    int
	Retention   time.Duration
	Token       string
	TokenHeader string
}

func (o Options) withDefaults() Options {
	if o.Capacity <= 0 {
		o.Capacity = 10000
	}
	if o.Retention <= 0 {
		o.Retention = 48 * time.Hour
	}
	if o.TokenHeader == "" {
		o.TokenHeader = "X-E2EProbe-Token"
	}
	return o
}

// Stats summarises what the inbox holds.
type Stats struct {
	Total          int            `json:"total"`
	ByType         map[string]int `json:"by_type"`
	LastReceivedAt time.Time      `json:"last_received_at,omitzero"`
	OldestAt       time.Time      `json:"oldest_at,omitzero"`
}

type waiter struct {
	match Match
	ch    chan Event
}

// Inbox is a bounded, time-limited store of delivered webhooks.
type Inbox struct {
	opts    Options
	mu      sync.Mutex
	events  []Event
	waiters map[*waiter]struct{}
	last    time.Time
}

func New(o Options) *Inbox {
	return &Inbox{opts: o.withDefaults(), waiters: map[*waiter]struct{}{}}
}

// Record stores an event, evicts what is too old or beyond capacity, and wakes
// every waiter it satisfies.
func (i *Inbox) Record(e Event) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, e)
	if e.ReceivedAt.After(i.last) {
		i.last = e.ReceivedAt
	}
	cutoff := i.last.Add(-i.opts.Retention)
	kept := i.events[:0]
	for _, old := range i.events {
		if !old.ReceivedAt.Before(cutoff) {
			kept = append(kept, old)
		}
	}
	i.events = kept
	if excess := len(i.events) - i.opts.Capacity; excess > 0 {
		i.events = append([]Event(nil), i.events[excess:]...)
	}
	for w := range i.waiters {
		if w.match.Matches(e) {
			w.ch <- e
			delete(i.waiters, w)
		}
	}
}

func (i *Inbox) Find(m Match) []Event {
	i.mu.Lock()
	defer i.mu.Unlock()
	var out []Event
	for _, e := range i.events {
		if m.Matches(e) {
			out = append(out, e)
		}
	}
	return out
}

// Await returns the first stored or future event matching m within timeout.
func (i *Inbox) Await(ctx context.Context, m Match, timeout time.Duration) (Event, error) {
	i.mu.Lock()
	for _, e := range i.events {
		if m.Matches(e) {
			i.mu.Unlock()
			return e, nil
		}
	}
	w := &waiter{match: m, ch: make(chan Event, 1)}
	i.waiters[w] = struct{}{}
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		delete(i.waiters, w)
		i.mu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case e := <-w.ch:
		return e, nil
	case <-timer.C:
		return Event{}, ErrTimeout
	case <-ctx.Done():
		return Event{}, ctx.Err()
	}
}

func (i *Inbox) Stats() Stats {
	i.mu.Lock()
	defer i.mu.Unlock()
	st := Stats{Total: len(i.events), ByType: map[string]int{}, LastReceivedAt: i.last}
	for _, e := range i.events {
		st.ByType[e.EventType]++
		if st.OldestAt.IsZero() || e.ReceivedAt.Before(st.OldestAt) {
			st.OldestAt = e.ReceivedAt
		}
	}
	return st
}

// LastReceived is when the newest event arrived; zero when nothing has.
func (i *Inbox) LastReceived() time.Time {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.last
}
