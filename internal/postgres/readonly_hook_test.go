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

// TestNewEntClientsInstallsReadOnlyHook mirrors how NewEntClients wires the
// hook onto a real *ent.Client (no driver — no query ever reaches it, since
// the hook rejects the mutation before the driver is touched).
func TestNewEntClientsInstallsReadOnlyHook(t *testing.T) {
	client := ent.NewClient()
	client.Use(newReadOnlyHook(true))

	_, err := client.Environment.Create().SetName("test").SetType("dev").Save(context.Background())
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("readOnly=true: want ErrReadOnly, got %v", err)
	}

	client2 := ent.NewClient()
	client2.Use(newReadOnlyHook(false))

	_, err = client2.Environment.Create().SetName("test").SetType("dev").Save(context.Background())
	if errors.Is(err, ErrReadOnly) {
		t.Fatalf("readOnly=false: hook must not block the mutation, got ErrReadOnly")
	}
}
