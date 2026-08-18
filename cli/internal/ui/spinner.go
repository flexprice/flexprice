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
	// eraseLine clears from the cursor to the end of the line, so a shorter
	// message cannot leave the tail of a longer one behind.
	eraseLine = "\r\x1b[K"
)

// frames is the braille cycle used by most modern CLIs. It renders as a single
// cell in every terminal that reports UTF-8.
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const frameInterval = 80 * time.Millisecond

// Spinner is an inline progress indicator. A Spinner returned when the UI is
// not animating is inert: every method is a no-op, so callers never branch on
// whether a spinner is live.
type Spinner struct {
	ui   *UI
	mu   sync.Mutex
	msg  string
	done chan struct{}
	once sync.Once
	live bool
}

// Spinner starts an indicator, or returns an inert handle when animation is
// suppressed (not a TTY, TERM=dumb, or --quiet). Always pair with Stop, and
// prefer defer.
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

// Update changes the message in place. Used by the --all pagination loop to
// tick on each completed page, so a stalled page shows as a frozen count
// rather than an animation that implies progress it is not making.
func (s *Spinner) Update(msg string) {
	if !s.live {
		return
	}
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts the spinner, erases its line and restores the cursor. It is safe
// to call more than once: both the normal return path and the signal handler
// can reach it.
func (s *Spinner) Stop() {
	if !s.live {
		return
	}
	s.once.Do(func() {
		close(s.done)
		// Erase before restoring the cursor so the final frame never survives
		// as a stray line above whatever is printed next.
		fmt.Fprint(s.ui.err, eraseLine+showCursor)
	})
}

// String satisfies fmt.Stringer for debugging; it never renders to the user.
func (s *Spinner) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.msg)
}
