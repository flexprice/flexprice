package checkout

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestJSONBCheckoutPaymentProviderConfig_ScanValueRoundTrip(t *testing.T) {
	t.Parallel()

	original := types.CheckoutPaymentProviderConfig{
		CollectionMethod: types.CollectionMethodChargeAutomatically,
		Razorpay: &types.RazorpayPaymentProviderConfig{
			PreferredPaymentMethod: types.PaymentMethodTypeUPI,
		},
	}
	wrapped := ToJSONBCheckoutPaymentProviderConfig(original)

	raw, err := wrapped.Value()
	require.NoError(t, err)
	rawBytes, ok := raw.([]byte)
	require.True(t, ok)

	var roundTripped JSONBCheckoutPaymentProviderConfig
	require.NoError(t, roundTripped.Scan(rawBytes))
	require.Equal(t, original, roundTripped.ToCheckoutPaymentProviderConfig())
}
