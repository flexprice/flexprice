package checkout_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSession() *checkout.CheckoutSession {
	return &checkout.CheckoutSession{
		ID:             "cs_test",
		TenantID:       "tenant_1",
		CustomerID:     "cust_1",
		Action:         types.CheckoutActionCreateSubscription,
		CheckoutStatus: types.CheckoutStatusInitiated,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        "plan_1",
				Currency:      "usd",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Status:    types.StatusPublished,
	}
}

func TestCheckoutSession_Validate_Valid(t *testing.T) {
	s := validSession()
	require.NoError(t, s.Validate())
}

func TestCheckoutSession_Validate_MissingCustomerID(t *testing.T) {
	s := validSession()
	s.CustomerID = ""
	require.Error(t, s.Validate())
}

func TestCheckoutSession_Validate_InvalidAction(t *testing.T) {
	s := validSession()
	s.Action = types.CheckoutAction("bad_action")
	require.Error(t, s.Validate())
}

func TestCheckoutSession_Validate_InvalidStatus(t *testing.T) {
	s := validSession()
	s.CheckoutStatus = types.CheckoutStatus("bad_status")
	require.Error(t, s.Validate())
}

func TestCheckoutSession_Validate_ZeroExpiresAt(t *testing.T) {
	s := validSession()
	s.ExpiresAt = time.Time{}
	require.Error(t, s.Validate())
}

func TestCheckoutSession_Validate_InvalidProvider(t *testing.T) {
	s := validSession()
	provider := types.CheckoutPaymentProvider("paypal")
	s.PaymentProvider = &provider
	require.Error(t, s.Validate())
}

func TestCheckoutSession_Validate_ValidProvider(t *testing.T) {
	s := validSession()
	provider := types.CheckoutPaymentProviderStripe
	s.PaymentProvider = &provider
	assert.NoError(t, s.Validate())
}
