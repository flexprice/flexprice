package ui

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a buffer safe for the spinner goroutine to write to while the
// test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSpinner_InertWhenNotAnimating(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: false, Term: "dumb"})

	sp := u.Spinner("Fetching customers")
	time.Sleep(30 * time.Millisecond)
	sp.Stop()

	if buf.String() != "" {
		t.Errorf("non-animating spinner wrote %q, want nothing", buf.String())
	}
}

func TestSpinner_AnimatesAndCleansUp(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color", Color: true})

	sp := u.Spinner("Fetching customers")
	time.Sleep(150 * time.Millisecond)
	sp.Stop()

	got := buf.String()
	if !strings.Contains(got, "Fetching customers") {
		t.Errorf("spinner never wrote its message: %q", got)
	}
	if !strings.Contains(got, hideCursor) {
		t.Error("spinner did not hide the cursor")
	}
	// The sharp edge: failing to restore leaves the user with an invisible
	// cursor in their shell, which outlives the process.
	if !strings.HasSuffix(got, showCursor) {
		t.Errorf("spinner must restore the cursor last, got tail %q", tail(got, 20))
	}
}

// Stop must be safe to call twice: teardown paths (normal return and the
// signal handler) can both reach it.
func TestSpinner_StopIsIdempotent(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	sp := u.Spinner("working")
	sp.Stop()
	sp.Stop() // must not panic or double-write

	if n := strings.Count(buf.String(), showCursor); n != 1 {
		t.Errorf("cursor restored %d times, want exactly 1", n)
	}
}

func TestSpinner_UpdateChangesMessage(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	sp := u.Spinner("fetched 0")
	time.Sleep(120 * time.Millisecond)
	sp.Update("fetched 20 of 60")
	time.Sleep(120 * time.Millisecond)
	sp.Stop()

	if !strings.Contains(buf.String(), "fetched 20 of 60") {
		t.Errorf("Update did not reach the output: %q", buf.String())
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
