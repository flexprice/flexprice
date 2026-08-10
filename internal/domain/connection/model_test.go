package connection

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestIsPriceOutboundEnabled(t *testing.T) {
	tests := []struct {
		name string
		conn *Connection
		want bool
	}{
		{"nil SyncConfig falls back to DefaultSyncConfig (off)", &Connection{}, false},
		{
			"SyncConfig set but Price unset is off",
			&Connection{SyncConfig: &types.SyncConfig{Invoice: &types.EntitySyncConfig{Outbound: true}}},
			false,
		},
		{
			"Price.Outbound false is off",
			&Connection{SyncConfig: &types.SyncConfig{Price: &types.EntitySyncConfig{Outbound: false}}},
			false,
		},
		{
			"Price.Outbound true is on",
			&Connection{SyncConfig: &types.SyncConfig{Price: &types.EntitySyncConfig{Outbound: true}}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.conn.IsPriceOutboundEnabled())
		})
	}
}
