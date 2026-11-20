package incident

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type sink struct {
	calls  []string
	err    error
	panics bool
}

func (s *sink) record(name string) error {
	if s.panics {
		panic("boom")
	}
	s.calls = append(s.calls, name)
	return s.err
}

func (s *sink) Opened(context.Context, Incident, BindingState) error   { return s.record("opened") }
func (s *sink) Reminded(context.Context, Incident, BindingState) error { return s.record("reminded") }
func (s *sink) Resolved(context.Context, Incident, BindingState, time.Time) error {
	return s.record("resolved")
}
func (s *sink) Digest(context.Context, Digest) error { return s.record("digest") }

func TestMulti_CallsEverySinkAndJoinsFailures(t *testing.T) {
	ok, bad, boom := &sink{}, &sink{err: errors.New("slack 429")}, &sink{panics: true}
	m := Multi(bad, boom, nil, ok)
	err := m.Opened(context.Background(), Incident{}, BindingState{})
	if err == nil || !strings.Contains(err.Error(), "slack 429") || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v, want both failures reported", err)
	}
	if len(ok.calls) != 1 || ok.calls[0] != "opened" {
		t.Fatal("a failing or panicking sink must not stop the others")
	}
	if err := Multi(ok).Digest(context.Background(), Digest{}); err != nil || len(ok.calls) != 2 {
		t.Fatalf("digest err = %v, calls = %v", err, ok.calls)
	}
}
