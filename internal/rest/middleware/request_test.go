package middleware

import (
	"strings"
	"testing"
)

func TestRequestIDPattern(t *testing.T) {
	unsafe := []string{"", "bad id", "a\r\nInjected: 1", "x" + strings.Repeat("y", 200), "a/b"}
	for _, s := range unsafe {
		if requestIDPattern.MatchString(s) {
			t.Fatalf("accepted unsafe request id: %q", s)
		}
	}
	safe := []string{"REQ-123", "abc_def.1", "0123456789"}
	for _, s := range safe {
		if !requestIDPattern.MatchString(s) {
			t.Fatalf("rejected safe request id: %q", s)
		}
	}
}
