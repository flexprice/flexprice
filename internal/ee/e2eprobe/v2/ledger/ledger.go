package ledger

import (
	"sort"
	"sync"
	"time"
)

// Ledger remembers which ticks were sent for each persona and when each batch
// landed, so a window with a missing batch is reported as partial and one whose
// last batch is still aggregating is not asserted yet.
type Ledger struct {
	mu        sync.Mutex
	retention time.Duration
	now       func() time.Time
	sent      map[string]map[time.Time]time.Time // tick end → when the batch was recorded
}

type PersonaStats struct {
	Batches int       `json:"batches"`
	Last    time.Time `json:"last,omitzero"`
}

type Stats struct {
	Personas map[string]PersonaStats `json:"personas"`
}

func NewLedger(retention time.Duration) *Ledger {
	return &Ledger{retention: retention, now: time.Now, sent: map[string]map[time.Time]time.Time{}}
}

// SetClock replaces the wall clock that stamps when a batch landed.
func (l *Ledger) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

func (l *Ledger) Record(persona string, tickEnd time.Time) {
	l.mu.Lock()
	now := l.now()
	l.mu.Unlock()
	l.RecordAt(persona, tickEnd, now)
}

// RecordAt records a tick's batch as landed at a given instant.
func (l *Ledger) RecordAt(persona string, tickEnd, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ticks := l.sent[persona]
	if ticks == nil {
		ticks = map[time.Time]time.Time{}
		l.sent[persona] = ticks
	}
	ticks[tickEnd.UTC()] = at
	newest := tickEnd.UTC()
	for t := range ticks {
		if t.After(newest) {
			newest = t
		}
	}
	for t := range ticks {
		if newest.Sub(t) > l.retention {
			delete(ticks, t)
		}
	}
}

func (l *Ledger) Recorded(persona string, tickEnd time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.sent[persona][tickEnd.UTC()]
	return ok
}

// LastRecordedAt is when the newest batch inside the window landed.
func (l *Ledger) LastRecordedAt(persona string, w Window) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var last time.Time
	for t, at := range l.sent[persona] {
		if t.After(w.Start) && !t.After(w.End) && at.After(last) {
			last = at
		}
	}
	return last, !last.IsZero()
}

// Last is the newest tick recorded for the persona.
func (l *Ledger) Last(persona string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var last time.Time
	for t := range l.sent[persona] {
		if t.After(last) {
			last = t
		}
	}
	return last, !last.IsZero()
}

// Ticks lists the recorded tick ends in (w.Start, w.End], ascending.
func (l *Ledger) Ticks(persona string, w Window) []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []time.Time
	for t := range l.sent[persona] {
		if t.After(w.Start) && !t.After(w.End) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// Complete reports whether every tick of the window was sent.
func (l *Ledger) Complete(persona string, w Window, interval time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ticks := l.sent[persona]
	if ticks == nil {
		return false
	}
	for t := w.Start.UTC().Truncate(interval).Add(interval); !t.After(w.End); t = t.Add(interval) {
		if _, ok := ticks[t]; !ok {
			return false
		}
	}
	return true
}

func (l *Ledger) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := Stats{Personas: map[string]PersonaStats{}}
	for persona, ticks := range l.sent {
		ps := PersonaStats{Batches: len(ticks)}
		for t := range ticks {
			if t.After(ps.Last) {
				ps.Last = t
			}
		}
		st.Personas[persona] = ps
	}
	return st
}
