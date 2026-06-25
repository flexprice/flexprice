package service

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

func TestIsFallbackEligibleError(t *testing.T) {
	canceledCtx, cancelFn := context.WithCancel(context.Background())
	cancelFn()

	cases := []struct {
		name      string
		parentCtx context.Context
		err       error
		want      bool
	}{
		{"nil", context.Background(), nil, false},
		{"deadline", context.Background(), context.DeadlineExceeded, true},
		// context.Canceled from our own timeout (parent ctx NOT canceled) → fallback eligible.
		{"canceled_by_timeout_only", context.Background(), context.Canceled, true},
		// context.Canceled from client disconnect (parent ctx IS canceled) → NOT fallback eligible.
		{"canceled_by_client_disconnect", canceledCtx, context.Canceled, false},
		{"database", context.Background(), ierr.NewError("boom").Mark(ierr.ErrDatabase), true},
		{"system", context.Background(), ierr.NewError("boom").Mark(ierr.ErrSystem), true},
		{"internal", context.Background(), ierr.NewError("boom").Mark(ierr.ErrInternal), true},
		{"http_client", context.Background(), ierr.NewError("boom").Mark(ierr.ErrHTTPClient), true},
		{"validation", context.Background(), ierr.NewError("bad").Mark(ierr.ErrValidation), false},
		{"not_found", context.Background(), ierr.NewError("missing").Mark(ierr.ErrNotFound), false},
		{"random", context.Background(), errors.New("nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFallbackEligibleError(tc.parentCtx, tc.err); got != tc.want {
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
