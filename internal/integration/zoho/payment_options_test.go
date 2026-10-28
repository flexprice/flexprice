package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestPaymentOptions(t *testing.T) {
	settingsWith := func(enabled bool, gateways ...types.ZohoPaymentGateway) *types.InvoiceSyncSettings {
		return &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
				PaymentGateways: &types.ZohoPaymentGatewaySettings{Enabled: enabled, Gateways: gateways},
			},
		}
	}

	cases := []struct {
		name     string
		settings *types.InvoiceSyncSettings
		want     *PaymentOptions
	}{
		{
			name:     "no settings attaches nothing",
			settings: nil,
		},
		{
			name:     "no payment gateway settings attaches nothing",
			settings: &types.InvoiceSyncSettings{},
		},
		{
			name:     "disabled attaches nothing even with gateways selected",
			settings: settingsWith(false, types.ZohoPaymentGatewayStripe),
		},
		{
			name:     "enabled with no gateway selected attaches nothing",
			settings: settingsWith(true),
		},
		{
			name:     "enabled attaches the selected gateway",
			settings: settingsWith(true, types.ZohoPaymentGatewayRazorpay),
			want: &PaymentOptions{PaymentGateways: []PaymentGateway{
				{Configured: true, GatewayName: types.ZohoPaymentGatewayRazorpay},
			}},
		},
		{
			name:     "enabled attaches every selected gateway in order",
			settings: settingsWith(true, types.ZohoPaymentGatewayStripe, types.ZohoPaymentGatewayBraintree),
			want: &PaymentOptions{PaymentGateways: []PaymentGateway{
				{Configured: true, GatewayName: types.ZohoPaymentGatewayStripe},
				{Configured: true, GatewayName: types.ZohoPaymentGatewayBraintree},
			}},
		},
		{
			name: "paypal carries its payment method, other gateways do not",
			settings: &types.InvoiceSyncSettings{
				ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
					PaymentGateways: &types.ZohoPaymentGatewaySettings{
						Enabled:             true,
						Gateways:            []types.ZohoPaymentGateway{types.ZohoPaymentGatewayPayPal, types.ZohoPaymentGatewayStripe},
						PayPalPaymentMethod: types.ZohoPayPalPaymentMethodAdaptive,
					},
				},
			},
			want: &PaymentOptions{PaymentGateways: []PaymentGateway{
				{Configured: true, GatewayName: types.ZohoPaymentGatewayPayPal, AdditionalField1: "adaptive"},
				{Configured: true, GatewayName: types.ZohoPaymentGatewayStripe},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &InvoiceService{logger: logger.NewNoopLogger()}

			got := svc.paymentOptions(context.Background(), "inv_1", c.settings)

			assert.Equal(t, c.want, got)
		})
	}
}
