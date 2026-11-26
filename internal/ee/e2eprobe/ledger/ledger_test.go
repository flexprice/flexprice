package ledger

import (
	"testing"
	"time"
)

func hourOf(t *testing.T) Window {
	t.Helper()
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	return Window{Start: start, End: start.Add(time.Hour)}
}

func TestLedger_TicksWithinWindow(t *testing.T) {
	l := NewLedger(48 * time.Hour)
	w := hourOf(t)
	for i := 0; i <= 7; i++ {
		l.Record("taxed", w.Start.Add(time.Duration(i)*tick))
	}
	l.Record("other", w.Start.Add(tick))
	ticks := l.Ticks("taxed", w)
	if len(ticks) != 6 || !ticks[0].Equal(w.Start.Add(tick)) || !ticks[5].Equal(w.End) {
		t.Errorf("ticks in (start, end] = %v", ticks)
	}
	if !l.Recorded("taxed", w.End) || l.Recorded("taxed", w.End.Add(time.Minute)) {
		t.Error("Recorded must answer per tick end")
	}
}

func TestLedger_CompleteIsFalseWhenATickIsMissing(t *testing.T) {
	l := NewLedger(48 * time.Hour)
	w := hourOf(t)
	for i := 1; i <= 6; i++ {
		if i == 4 {
			continue
		}
		l.Record("taxed", w.Start.Add(time.Duration(i)*tick))
	}
	if l.Complete("taxed", w, tick) {
		t.Fatal("five of six ticks is not complete")
	}
	l.Record("taxed", w.Start.Add(4*tick))
	if !l.Complete("taxed", w, tick) {
		t.Fatal("six of six ticks must be complete")
	}
	if l.Complete("ghost", w, tick) {
		t.Error("an unknown persona is never complete")
	}
}

func TestLedger_RetentionEvicts(t *testing.T) {
	l := NewLedger(time.Hour)
	w := hourOf(t)
	l.Record("taxed", w.Start)
	l.Record("taxed", w.Start.Add(2*time.Hour))
	if l.Recorded("taxed", w.Start) {
		t.Error("a tick older than the retention must be evicted")
	}
	st := l.Stats()
	if st.Personas["taxed"].Batches != 1 || !st.Personas["taxed"].Last.Equal(w.Start.Add(2*time.Hour)) {
		t.Errorf("stats = %+v", st.Personas["taxed"])
	}
}

func TestLedger_Last(t *testing.T) {
	l := NewLedger(48 * time.Hour)
	if _, ok := l.Last("taxed"); ok {
		t.Fatal("an unknown persona has no last tick")
	}
	w := hourOf(t)
	l.Record("taxed", w.Start.Add(2*tick))
	l.Record("taxed", w.Start.Add(tick))
	if last, ok := l.Last("taxed"); !ok || !last.Equal(w.Start.Add(2*tick)) {
		t.Errorf("Last = %s %v", last, ok)
	}
}

func TestLedger_LastRecordedAtTracksWhenTheBatchLanded(t *testing.T) {
	l := NewLedger(48 * time.Hour)
	clock := time.Date(2026, 11, 19, 10, 3, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return clock })
	tick := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	l.Record("taxed", tick)
	w := Window{Start: tick.Add(-time.Hour), End: tick}
	if at, ok := l.LastRecordedAt("taxed", w); !ok || !at.Equal(clock) {
		t.Fatalf("recorded at = %v ok=%v, want %v", at, ok, clock)
	}
	if _, ok := l.LastRecordedAt("taxed", Window{Start: tick, End: tick.Add(time.Hour)}); ok {
		t.Fatal("no batch of the next hour has landed")
	}
	earlier := clock.Add(-20 * time.Minute)
	l.RecordAt("taxed", tick.Add(-10*time.Minute), earlier)
	if at, _ := l.LastRecordedAt("taxed", w); !at.Equal(clock) {
		t.Fatalf("the newest landing wins, got %v", at)
	}
}
