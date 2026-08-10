package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultSyncConfig_PriceOffByDefault(t *testing.T) {
	cfg := DefaultSyncConfig()

	require.NotNil(t, cfg.Price)
	require.False(t, cfg.Price.Inbound)
	require.False(t, cfg.Price.Outbound)
}

func TestProviderBaseSyncConfig_StripePriceOffByDefault(t *testing.T) {
	cfg := ProviderBaseSyncConfig(SecretProviderStripe)

	require.NotNil(t, cfg.Price)
	require.False(t, cfg.Price.Inbound)
	require.False(t, cfg.Price.Outbound)
}

func TestSyncConfig_Validate_RejectsPriceInbound(t *testing.T) {
	cfg := &SyncConfig{Price: &EntitySyncConfig{Inbound: true}}

	err := cfg.Validate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "price inbound sync is not allowed")
}

func TestSyncConfig_Validate_AllowsPriceOutbound(t *testing.T) {
	cfg := &SyncConfig{Price: &EntitySyncConfig{Outbound: true}}

	require.NoError(t, cfg.Validate())
}

func TestSyncConfig_Validate_NilPriceIsFine(t *testing.T) {
	require.NoError(t, (&SyncConfig{}).Validate())
}
