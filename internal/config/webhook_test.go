package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhook_TenantConfig_NilReceiver(t *testing.T) {
	t.Parallel()

	var w *Webhook
	cfg, ok := w.TenantConfig("tenant_01KE93H5A5S0S3DMBJT4YP589M")
	require.False(t, ok)
	require.Equal(t, TenantWebhookConfig{}, cfg)
}

func TestWebhook_TenantConfig_NilMap(t *testing.T) {
	t.Parallel()

	w := &Webhook{}
	cfg, ok := w.TenantConfig("tenant_01KE93H5A5S0S3DMBJT4YP589M")
	require.False(t, ok)
	require.Equal(t, TenantWebhookConfig{}, cfg)
}

func TestWebhook_TenantConfig_CaseInsensitiveLookup(t *testing.T) {
	t.Parallel()

	// Mimic Viper's post-load state: map key is lowercase. Real tenant IDs
	// arrive uppercase (ULID / Crockford Base32), so the lookup must
	// normalize before indexing.
	w := &Webhook{
		Tenants: map[string]TenantWebhookConfig{
			"tenant_01ke93h5a5s0s3dmbjt4yp589m": {
				Enabled:  true,
				Endpoint: "http://localhost:8080/health",
			},
		},
	}

	cfg, ok := w.TenantConfig("tenant_01KE93H5A5S0S3DMBJT4YP589M")
	require.True(t, ok)
	require.True(t, cfg.Enabled)
	require.Equal(t, "http://localhost:8080/health", cfg.Endpoint)
}

func TestWebhook_TenantConfig_Miss(t *testing.T) {
	t.Parallel()

	w := &Webhook{
		Tenants: map[string]TenantWebhookConfig{
			"tenant_known": {Enabled: true},
		},
	}

	_, ok := w.TenantConfig("tenant_unknown")
	require.False(t, ok)
}

func TestWebhook_normalizeTenantKeys(t *testing.T) {
	t.Parallel()

	w := &Webhook{
		Tenants: map[string]TenantWebhookConfig{
			"tenant_01KE93H5A5S0S3DMBJT4YP589M": {Enabled: true, Endpoint: "a"},
			"already_lowercase":                 {Enabled: false, Endpoint: "b"},
		},
	}

	w.normalizeTenantKeys()

	require.Len(t, w.Tenants, 2)
	upper, ok := w.Tenants["tenant_01ke93h5a5s0s3dmbjt4yp589m"]
	require.True(t, ok)
	require.Equal(t, "a", upper.Endpoint)
	lower, ok := w.Tenants["already_lowercase"]
	require.True(t, ok)
	require.Equal(t, "b", lower.Endpoint)

	// Idempotent.
	require.NotPanics(t, func() { w.normalizeTenantKeys() })
	require.Len(t, w.Tenants, 2)
}

func TestWebhook_normalizeTenantKeys_NilAndEmptySafe(t *testing.T) {
	t.Parallel()

	var nilWebhook *Webhook
	require.NotPanics(t, func() { nilWebhook.normalizeTenantKeys() })

	emptyWebhook := &Webhook{}
	require.NotPanics(t, func() { emptyWebhook.normalizeTenantKeys() })
	require.Nil(t, emptyWebhook.Tenants)
}
