package errors

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

func TestErrNotImplemented_Marks(t *testing.T) {
	t.Parallel()
	err := NewError("capability not supported").Mark(ErrNotImplemented)
	require.True(t, errors.Is(err, ErrNotImplemented))
}
