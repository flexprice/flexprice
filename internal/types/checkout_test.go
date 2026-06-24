package types_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCheckoutStatus_Validate(t *testing.T) {
	tests := []struct {
		name    string
		status  types.CheckoutStatus
		wantErr bool
	}{
		{"initiated is valid", types.CheckoutStatusInitiated, false},
		{"pending is valid", types.CheckoutStatusPending, false},
		{"completed is valid", types.CheckoutStatusCompleted, false},
		{"failed is valid", types.CheckoutStatusFailed, false},
		{"expired is valid", types.CheckoutStatusExpired, false},
		{"empty is valid (optional)", "", false},
		{"unknown is invalid", types.CheckoutStatus("unknown"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.status.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckoutAction_Validate(t *testing.T) {
	tests := []struct {
		name    string
		action  types.CheckoutAction
		wantErr bool
	}{
		{"create_subscription is valid", types.CheckoutActionCreateSubscription, false},
		{"change_plan is valid", types.CheckoutActionChangePlan, false},
		{"setup is valid", types.CheckoutActionSetup, false},
		{"empty is valid", "", false},
		{"unknown is invalid", types.CheckoutAction("new_subscription"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.action.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckoutPaymentProvider_Validate(t *testing.T) {
	tests := []struct {
		name     string
		provider types.CheckoutPaymentProvider
		wantErr  bool
	}{
		{"stripe is valid", types.CheckoutPaymentProviderStripe, false},
		{"razorpay is valid", types.CheckoutPaymentProviderRazorpay, false},
		{"moyasar is valid", types.CheckoutPaymentProviderMoyasar, false},
		{"empty is valid", "", false},
		{"unknown is invalid", types.CheckoutPaymentProvider("paypal"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.provider.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPlanChangeEffective_Validate(t *testing.T) {
	tests := []struct {
		name      string
		effective types.PlanChangeEffective
		wantErr   bool
	}{
		{"immediate is valid", types.PlanChangeEffectiveImmediate, false},
		{"period_end is valid", types.PlanChangeEffectivePeriodEnd, false},
		{"empty is valid", "", false},
		{"unknown is invalid", types.PlanChangeEffective("now"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.effective.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPlanChangeProrationBehavior_Validate(t *testing.T) {
	tests := []struct {
		name     string
		behavior types.PlanChangeProrationBehavior
		wantErr  bool
	}{
		{"none is valid", types.PlanChangeProrationBehaviorNone, false},
		{"create_prorations is valid", types.PlanChangeProrationBehaviorCreateProrations, false},
		{"empty is valid", "", false},
		{"unknown is invalid", types.PlanChangeProrationBehavior("always_invoice"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.behavior.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPaymentActionType_Validate(t *testing.T) {
	tests := []struct {
		name       string
		actionType types.PaymentActionType
		wantErr    bool
	}{
		{"checkout_url is valid", types.PaymentActionTypeCheckoutURL, false},
		{"payment_link is valid", types.PaymentActionTypePaymentLink, false},
		{"empty is valid", "", false},
		{"unknown is invalid", types.PaymentActionType("redirect"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.actionType.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckoutSessionFilter_Validate(t *testing.T) {
	t.Run("valid filter", func(t *testing.T) {
		f := types.NewDefaultCheckoutSessionFilter()
		f.Statuses = []types.CheckoutStatus{types.CheckoutStatusInitiated, types.CheckoutStatusPending}
		f.Actions = []types.CheckoutAction{types.CheckoutActionCreateSubscription}
		require.NoError(t, f.Validate())
	})

	t.Run("invalid status in filter", func(t *testing.T) {
		f := types.NewDefaultCheckoutSessionFilter()
		f.Statuses = []types.CheckoutStatus{types.CheckoutStatus("bogus")}
		require.Error(t, f.Validate())
	})

	t.Run("invalid action in filter", func(t *testing.T) {
		f := types.NewDefaultCheckoutSessionFilter()
		f.Actions = []types.CheckoutAction{types.CheckoutAction("bogus")}
		require.Error(t, f.Validate())
	})
}
