package idempotency

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateKey_TokenCycleCharge_DeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	g := NewGenerator()

	params := map[string]interface{}{
		"gateway_method_id": "token_abc123",
		"subscription_id":   "sub_1",
		"cycle_start":       "2026-07-01T00:00:00Z",
	}

	key1 := g.GenerateKey(ScopeTokenCycleCharge, params)
	key2 := g.GenerateKey(ScopeTokenCycleCharge, params)
	require.Equal(t, key1, key2, "same params must produce the same key")

	otherCycle := map[string]interface{}{
		"gateway_method_id": "token_abc123",
		"subscription_id":   "sub_1",
		"cycle_start":       "2026-08-01T00:00:00Z",
	}
	key3 := g.GenerateKey(ScopeTokenCycleCharge, otherCycle)
	require.NotEqual(t, key1, key3, "different cycle_start must produce a different key")
}
