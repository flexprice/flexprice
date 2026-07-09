package types

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSettingKeyPaymentMandateLimits_Validates(t *testing.T) {
	t.Parallel()
	key := SettingKeyPaymentMandateLimits
	require.NoError(t, key.Validate())
}

func TestPaymentMandateLimits_Validate(t *testing.T) {
	t.Parallel()

	t.Run("valid with upi entry", func(t *testing.T) {
		t.Parallel()
		c := PaymentMandateLimits{
			MandateLimits: map[string]MandateLimit{
				"upi": {MaxAmount: decimal.NewFromInt(15000), Currency: "INR"},
			},
		}
		require.NoError(t, c.Validate())
	})

	t.Run("empty map is valid — presence of a key is what enables a rail", func(t *testing.T) {
		t.Parallel()
		c := PaymentMandateLimits{MandateLimits: map[string]MandateLimit{}}
		require.NoError(t, c.Validate())
	})

	t.Run("negative max_amount is invalid", func(t *testing.T) {
		t.Parallel()
		c := PaymentMandateLimits{
			MandateLimits: map[string]MandateLimit{
				"upi": {MaxAmount: decimal.NewFromInt(-1)},
			},
		}
		require.Error(t, c.Validate())
	})
}
