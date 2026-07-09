package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestResolveCheckoutPaymentAction(t *testing.T) {
	t.Parallel()
	upiCeiling := decimal.NewFromInt(15000)

	tests := []struct {
		name               string
		mandateLimits      map[types.PaymentMethodType]types.MandateLimit
		providerConfig     types.CheckoutPaymentProviderConfig
		existingTokenFound bool
		want               checkoutPaymentAction
	}{
		{
			name:          "unset collection_method (normalized to send_invoice) falls back to payment link",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{types.PaymentMethodTypeUPI: {MaxAmount: upiCeiling}},
			providerConfig: types.CheckoutPaymentProviderConfig{
				Razorpay: &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
			},
			want: checkoutActionPaymentLink,
		},
		{
			name:          "explicit send_invoice falls back to payment link even with everything else configured",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{types.PaymentMethodTypeUPI: {MaxAmount: upiCeiling}},
			providerConfig: types.CheckoutPaymentProviderConfig{
				CollectionMethod: types.CollectionMethodSendInvoice,
				Razorpay:         &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
			},
			want: checkoutActionPaymentLink,
		},
		{
			name:          "charge_automatically but no preferred_payment_method set falls back to payment link",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{types.PaymentMethodTypeUPI: {MaxAmount: upiCeiling}},
			providerConfig: types.CheckoutPaymentProviderConfig{
				CollectionMethod: types.CollectionMethodChargeAutomatically,
				Razorpay:         nil,
			},
			want: checkoutActionPaymentLink,
		},
		{
			name:          "no settings entry for preferred method falls back to payment link",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{},
			providerConfig: types.CheckoutPaymentProviderConfig{
				CollectionMethod: types.CollectionMethodChargeAutomatically,
				Razorpay:         &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
			},
			want: checkoutActionPaymentLink,
		},
		{
			name:          "charge_automatically + preferred_payment_method + configured + no existing token creates authorization link",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{types.PaymentMethodTypeUPI: {MaxAmount: upiCeiling}},
			providerConfig: types.CheckoutPaymentProviderConfig{
				CollectionMethod: types.CollectionMethodChargeAutomatically,
				Razorpay:         &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
			},
			existingTokenFound: false,
			want:               checkoutActionAuthorizationLink,
		},
		{
			name:          "charge_automatically + preferred_payment_method + configured + existing confirmed token skips straight to autocharge",
			mandateLimits: map[types.PaymentMethodType]types.MandateLimit{types.PaymentMethodTypeUPI: {MaxAmount: upiCeiling}},
			providerConfig: types.CheckoutPaymentProviderConfig{
				CollectionMethod: types.CollectionMethodChargeAutomatically,
				Razorpay:         &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
			},
			existingTokenFound: true,
			want:               checkoutActionAutoCharge,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveCheckoutPaymentAction(tt.mandateLimits, tt.providerConfig, tt.existingTokenFound)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeCheckoutPaymentProviderConfig_DefaultsCollectionMethodToSendInvoice(t *testing.T) {
	t.Parallel()
	cfg := types.CheckoutPaymentProviderConfig{} // CollectionMethod deliberately unset
	normalized := normalizeCheckoutPaymentProviderConfig(cfg)
	require.Equal(t, types.CollectionMethodSendInvoice, normalized.CollectionMethod)
}

func TestNormalizeCheckoutPaymentProviderConfig_PreservesExplicitValue(t *testing.T) {
	t.Parallel()
	cfg := types.CheckoutPaymentProviderConfig{CollectionMethod: types.CollectionMethodChargeAutomatically}
	normalized := normalizeCheckoutPaymentProviderConfig(cfg)
	require.Equal(t, types.CollectionMethodChargeAutomatically, normalized.CollectionMethod)
}
