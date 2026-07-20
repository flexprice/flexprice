package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckoutAction_ValidateIncludesWalletTopup(t *testing.T) {
	require.NoError(t, CheckoutActionWalletTopup.Validate())
	require.NoError(t, CheckoutActionCreateSubscription.Validate())
	require.Error(t, CheckoutAction("unknown").Validate())
}

func TestWalletTopupParams_Validate(t *testing.T) {
	t.Run("nil_rejected", func(t *testing.T) {
		var p *WalletTopupParams
		require.Error(t, p.Validate())
	})

	t.Run("empty_wallet_id_rejected", func(t *testing.T) {
		p := &WalletTopupParams{}
		err := p.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "wallet_id")
	})

	t.Run("wallet_id_ok", func(t *testing.T) {
		p := &WalletTopupParams{WalletID: "wallet_1"}
		require.NoError(t, p.Validate())
	})
}

func TestCheckoutConfiguration_ValidateWalletTopup(t *testing.T) {
	t.Run("missing_params_rejected", func(t *testing.T) {
		cfg := &CheckoutConfiguration{}
		err := cfg.Validate(CheckoutActionWalletTopup)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wallet_topup_params")
	})

	t.Run("valid_params_ok", func(t *testing.T) {
		cfg := &CheckoutConfiguration{
			WalletTopupParams: &WalletTopupParams{WalletID: "wallet_1"},
		}
		require.NoError(t, cfg.Validate(CheckoutActionWalletTopup))
	})
}
