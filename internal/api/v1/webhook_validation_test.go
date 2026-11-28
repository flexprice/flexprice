package v1

import (
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestAllValidationErrors(t *testing.T) {
	validation := ierr.NewError("bad payment").Mark(ierr.ErrValidation)
	transient := errors.New("db timeout")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"single validation", validation, true},
		{"single transient", transient, false},
		{"all validation", errors.Join(validation, validation), true},
		{"mixed", errors.Join(validation, transient), false},
		{"nested mixed", errors.Join(validation, errors.Join(validation, transient)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, allValidationErrors(tt.err))
		})
	}
}
