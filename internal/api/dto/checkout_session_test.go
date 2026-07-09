package dto

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCreateCheckoutSessionRequest_Validate_RejectsMismatchedProviderConfig(t *testing.T) {
	t.Parallel()
	req := &CreateCheckoutSessionRequest{
		CustomerExternalID: "cust_1",
		Action:             types.CheckoutActionCreateSubscription,
		// Only one CheckoutPaymentProvider constant exists today (razorpay), so use an
		// arbitrary non-razorpay literal to exercise the mismatch rejection path, mirroring
		// Task 6's own test in internal/types/checkout_configuration_test.go.
		PaymentProvider: types.CheckoutPaymentProvider("stripe"),
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID: "plan_1", Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
		PaymentProviderConfig: types.CheckoutPaymentProviderConfig{
			Razorpay: &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
		},
	}
	err := req.Validate()
	require.Error(t, err)
}

func TestCreateCheckoutSessionRequest_ToCheckoutSession_CarriesPaymentProviderConfig(t *testing.T) {
	t.Parallel()
	req := &CreateCheckoutSessionRequest{
		CustomerExternalID: "cust_1",
		Action:             types.CheckoutActionCreateSubscription,
		PaymentProvider:    types.CheckoutPaymentProviderRazorpay,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID: "plan_1", Currency: "inr", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
		PaymentProviderConfig: types.CheckoutPaymentProviderConfig{
			CollectionMethod: types.CollectionMethodChargeAutomatically,
			Razorpay:         &types.RazorpayPaymentProviderConfig{PreferredPaymentMethod: types.PaymentMethodTypeUPI},
		},
	}
	session := req.ToCheckoutSession(context.Background(), "cust_internal_1")
	require.NotNil(t, session.PaymentProviderConfig)
	require.Equal(t, types.CollectionMethodChargeAutomatically, session.PaymentProviderConfig.ToCheckoutPaymentProviderConfig().CollectionMethod)
}
