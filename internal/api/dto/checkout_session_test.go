package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCheckoutParams_ValidateRequiresPaymentProvider(t *testing.T) {
	t.Run("nil_checkout_ok", func(t *testing.T) {
		var p *CheckoutParams
		require.NoError(t, p.Validate())
	})

	t.Run("empty_provider_rejected", func(t *testing.T) {
		p := &CheckoutParams{}
		err := p.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "payment_provider")
	})

	t.Run("razorpay_ok", func(t *testing.T) {
		p := &CheckoutParams{
			PaymentParams: PaymentParams{
				PaymentProvider: types.CheckoutPaymentProviderRazorpay,
			},
		}
		require.NoError(t, p.Validate())
	})
}
