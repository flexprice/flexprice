package types

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCheckoutPaymentProviderConfig_Validate(t *testing.T) {
	t.Parallel()

	t.Run("matching provider sub-object is valid", func(t *testing.T) {
		t.Parallel()
		maxAmount := decimal.NewFromInt(15000)
		c := &CheckoutPaymentProviderConfig{
			CollectionMethod: CollectionMethodChargeAutomatically,
			Razorpay: &RazorpayPaymentProviderConfig{
				PreferredPaymentMethod: PaymentMethodTypeUPI,
				MaxAmount:              &maxAmount,
			},
		}
		require.NoError(t, c.Validate(CheckoutPaymentProviderRazorpay))
	})

	t.Run("mismatched provider sub-object is rejected", func(t *testing.T) {
		t.Parallel()
		c := &CheckoutPaymentProviderConfig{
			Razorpay: &RazorpayPaymentProviderConfig{PreferredPaymentMethod: PaymentMethodTypeUPI},
		}
		// There is currently only one CheckoutPaymentProvider constant (razorpay),
		// so we use an arbitrary non-razorpay value to exercise the mismatch path.
		err := c.Validate(CheckoutPaymentProvider("stripe"))
		require.Error(t, err)
	})

	t.Run("empty config is valid — nothing declared, baseline behavior", func(t *testing.T) {
		t.Parallel()
		c := &CheckoutPaymentProviderConfig{}
		require.NoError(t, c.Validate(CheckoutPaymentProviderRazorpay))
	})

	t.Run("invalid preferred_payment_method is rejected", func(t *testing.T) {
		t.Parallel()
		c := &CheckoutPaymentProviderConfig{
			Razorpay: &RazorpayPaymentProviderConfig{PreferredPaymentMethod: PaymentMethodType("BOGUS")},
		}
		require.Error(t, c.Validate(CheckoutPaymentProviderRazorpay))
	})
}
