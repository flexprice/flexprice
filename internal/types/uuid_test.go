package types

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestIsPrefixedULID(t *testing.T) {
	id := ulid.Make().String()

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "valid", id: "conn_" + id, want: true},
		{name: "not_a_ulid", id: "conn_123", want: false},
		{name: "other_prefix", id: "inv_" + id, want: false},
		{name: "invalid_character", id: "conn_" + strings.ToLower(id)[:25] + "!", want: false},
		{name: "empty", id: "", want: false},
		{name: "no_prefix", id: id, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsPrefixedULID(tt.id, UUID_PREFIX_CONNECTION))
		})
	}
}
