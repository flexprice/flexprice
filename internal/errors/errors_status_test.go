package errors

import (
	"fmt"
	"testing"
)

func TestIsServerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"validation is client", NewError("bad").Mark(ErrValidation), false},
		{"invalid operation is client", NewError("no fx rate").Mark(ErrInvalidOperation), false},
		{"not found is client", NewError("missing").Mark(ErrNotFound), false},
		{"already exists is client", NewError("dup").Mark(ErrAlreadyExists), false},
		{"database is server", NewError("db down").Mark(ErrDatabase), true},
		{"unmarked is server", fmt.Errorf("boom"), true},
		{"wrapped validation stays client", fmt.Errorf("ctx: %w", NewError("bad").Mark(ErrValidation)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsServerError(tt.err); got != tt.want {
				t.Errorf("IsServerError() = %v, want %v", got, tt.want)
			}
		})
	}
}
