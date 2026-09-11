package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/ent"
)

func TestReadOnlyHook(t *testing.T) {
	sentinel := errors.New("would-mutate")
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
		return nil, sentinel
	})

	// enabled: mutation rejected with ErrReadOnly, next never called.
	got := newReadOnlyHook(true)(next)
	if _, err := got.Mutate(context.Background(), nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("enabled hook: want ErrReadOnly, got %v", err)
	}

	// disabled: passes through to next.
	got = newReadOnlyHook(false)(next)
	if _, err := got.Mutate(context.Background(), nil); !errors.Is(err, sentinel) {
		t.Fatalf("disabled hook: want passthrough sentinel, got %v", err)
	}
}
