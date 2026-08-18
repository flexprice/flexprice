package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	// Clears to end of line, so a shorter message cannot leave the tail of a
	// longer one behind.
	eraseLine = "\r\x1b[K"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const frameInterval = 80 * time.Millisecond

// A Spinner returned when the UI is not animating is inert: every method is a
// no-op, so callers never branch on whether one is live.
type Spinner struct {
	ui   *UI
	mu   sync.Mutex
	msg  string
	done chan struct{}
	once sync.Once
	live bool
}

// Returns an inert handle when animation is suppressed (not a TTY, TERM=dumb,
// or --quiet). Always pair with Stop.
func (u *UI) Spinner(msg string) *Spinner {
	s := &Spinner{ui: u, msg: msg}
	if !u.animate {
		return s
	}
	s.live = true
	s.done = make(chan struct{})

	fmt.Fprint(u.err, hideCursor)
	go s.run()
	return s
}

func (s *Spinner) run() {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for i := 0; ; i++ {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			fmt.Fprintf(s.ui.err, "%s%s %s", eraseLine,
				s.ui.palette.Accent(frames[i%len(frames)]), msg)
		}
	}
}

// Callers tick this on completed work rather than on a timer, so a stall shows
// as a frozen count rather than an animation implying progress.
func (s *Spinner) Update(msg string) {
	if !s.live {
		return
	}
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop erases the line and restores the cursor. Safe to call more than once:
// both the normal return path and the signal handler reach it. Failing to
// restore leaves the user's shell with an invisible cursor after we exit.
func (s *Spinner) Stop() {
	if !s.live {
		return
	}
	s.once.Do(func() {
		close(s.done)
		fmt.Fprint(s.ui.err, eraseLine+showCursor)
	})
}

// For debugging; never rendered to the user.
func (s *Spinner) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.msg)
}
