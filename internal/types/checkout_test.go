package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckoutPaymentProvider(t *testing.T) {
	t.Run("Validate", func(t *testing.T) {
		assert.NoError(t, CheckoutPaymentProviderRazorpay.Validate())
		assert.NoError(t, CheckoutPaymentProviderChargebee.Validate())
		assert.NoError(t, CheckoutPaymentProviderStripe.Validate())
		assert.NoError(t, CheckoutPaymentProvider("").Validate())
		assert.Error(t, CheckoutPaymentProvider("invalid").Validate())
	})

	t.Run("ToPaymentGateway", func(t *testing.T) {
		gw, ok := CheckoutPaymentProviderRazorpay.ToPaymentGateway()
		assert.True(t, ok)
		assert.Equal(t, PaymentGatewayTypeRazorpay, gw)

		gw, ok = CheckoutPaymentProviderChargebee.ToPaymentGateway()
		assert.True(t, ok)
		assert.Equal(t, PaymentGatewayTypeChargebee, gw)

		gw, ok = CheckoutPaymentProviderStripe.ToPaymentGateway()
		assert.True(t, ok)
		assert.Equal(t, PaymentGatewayTypeStripe, gw)

		_, ok = CheckoutPaymentProvider("invalid").ToPaymentGateway()
		assert.False(t, ok)
	})

	t.Run("CheckoutProviderFromGateway", func(t *testing.T) {
		p, ok := CheckoutProviderFromGateway(PaymentGatewayTypeRazorpay)
		assert.True(t, ok)
		assert.Equal(t, CheckoutPaymentProviderRazorpay, p)

		p, ok = CheckoutProviderFromGateway(PaymentGatewayTypeChargebee)
		assert.True(t, ok)
		assert.Equal(t, CheckoutPaymentProviderChargebee, p)

		p, ok = CheckoutProviderFromGateway(PaymentGatewayTypeStripe)
		assert.True(t, ok)
		assert.Equal(t, CheckoutPaymentProviderStripe, p)

		_, ok = CheckoutProviderFromGateway(PaymentGatewayTypeNomod)
		assert.False(t, ok)
	})

	t.Run("LinkExpiry", func(t *testing.T) {
		assert.Equal(t, 20*time.Minute, CheckoutPaymentProviderRazorpay.LinkExpiry())
		assert.Equal(t, 25*time.Minute, CheckoutPaymentProviderChargebee.LinkExpiry())
		assert.Equal(t, 30*time.Minute, CheckoutPaymentProviderStripe.LinkExpiry())
		assert.Equal(t, 30*time.Minute, CheckoutPaymentProvider("other").LinkExpiry())
	})
}
