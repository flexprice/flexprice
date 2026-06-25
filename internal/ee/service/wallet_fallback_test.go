package service

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

func TestIsFallbackEligibleError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline", context.DeadlineExceeded, true},
		{"canceled", context.Canceled, true},
		{"database", ierr.NewError("boom").Mark(ierr.ErrDatabase), true},
		{"system", ierr.NewError("boom").Mark(ierr.ErrSystem), true},
		{"internal", ierr.NewError("boom").Mark(ierr.ErrInternal), true},
		{"http_client", ierr.NewError("boom").Mark(ierr.ErrHTTPClient), true},
		{"validation", ierr.NewError("bad").Mark(ierr.ErrValidation), false},
		{"not_found", ierr.NewError("missing").Mark(ierr.ErrNotFound), false},
		{"random", errors.New("nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFallbackEligibleError(tc.err); got != tc.want {
				t.Errorf("isFallbackEligibleError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyFallbackReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "canceled"},
		{"db", ierr.NewError("boom").Mark(ierr.ErrDatabase), "db_error"},
		{"system", ierr.NewError("boom").Mark(ierr.ErrSystem), "system"},
		{"internal", ierr.NewError("boom").Mark(ierr.ErrInternal), "internal"},
		{"http", ierr.NewError("boom").Mark(ierr.ErrHTTPClient), "http_client"},
		{"other", errors.New("nope"), "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFallbackReason(tc.err); got != tc.want {
				t.Errorf("classifyFallbackReason(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
