package ledger

import (
	"sort"
	"sync"
	"time"
)

// Ledger remembers which ticks were sent for each persona, so a window with a
// missing batch is reported as partial instead of asserted against a wrong total.
type Ledger struct {
	mu        sync.Mutex
	retention time.Duration
	sent      map[string]map[time.Time]bool
}

type PersonaStats struct {
	Batches int       `json:"batches"`
	Last    time.Time `json:"last,omitzero"`
}

type Stats struct {
	Personas map[string]PersonaStats `json:"personas"`
}

func NewLedger(retention time.Duration) *Ledger {
	return &Ledger{retention: retention, sent: map[string]map[time.Time]bool{}}
}

func (l *Ledger) Record(persona string, tickEnd time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ticks := l.sent[persona]
	if ticks == nil {
		ticks = map[time.Time]bool{}
		l.sent[persona] = ticks
	}
	ticks[tickEnd.UTC()] = true
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
	return l.sent[persona][tickEnd.UTC()]
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
		if !ticks[t] {
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
