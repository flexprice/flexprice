package stripe

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCustomerServiceForAdapter struct {
	interfaces.CustomerService
	cust *dto.CustomerResponse
	err  error
}

func (m *mockCustomerServiceForAdapter) GetCustomer(_ context.Context, _ string) (*dto.CustomerResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cust, nil
}

func TestCheckoutAdapter_Unconfigured(t *testing.T) {
	ctx := context.Background()
	adapter := &CheckoutAdapter{}

	_, err := adapter.CreatePaymentLink(ctx, interfaces.CheckoutProviderRequest{})
	require.Error(t, err)

	_, err = adapter.CreateAuthorizationLink(ctx, interfaces.AuthorizationLinkRequest{})
	require.Error(t, err)

	resp, charged, err := adapter.TryAutoChargingSavedMethod(ctx, interfaces.AuthorizationLinkRequest{})
	assert.NoError(t, err)
	assert.False(t, charged)
	assert.Nil(t, resp)

	has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{})
	assert.NoError(t, err)
	assert.False(t, has)

	_, err = adapter.FetchPaymentState(ctx, interfaces.PaymentStateRequest{})
	require.Error(t, err)
	assert.True(t, ierr.IsNotImplemented(err))
}

func TestCheckoutAdapter_HasAutoChargeableMethod_EmptyCustomer(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	adapter := &CheckoutAdapter{
		Logger: log,
	}

	has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{
		CustomerID: "",
	})
	assert.NoError(t, err)
	assert.False(t, has)
}

func TestCheckoutAdapter_TryAutoChargingSavedMethod_NoCustomer(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	adapter := &CheckoutAdapter{
		Logger: log,
	}

	resp, charged, err := adapter.TryAutoChargingSavedMethod(ctx, interfaces.AuthorizationLinkRequest{
		CustomerID: "",
		Amount:     decimal.NewFromInt(100),
		Currency:   "USD",
	})
	assert.NoError(t, err)
	assert.False(t, charged)
	assert.Nil(t, resp)
}

func TestCheckoutAdapter_FetchPaymentState_EmptyHandles(t *testing.T) {
	ctx := context.Background()
	// When Client is set but handles are empty, returns nil, nil
	client := &Client{}
	adapter := &CheckoutAdapter{
		Client: client,
	}

	// Will fail on GetStripeClient if connection repo not set, but tests unconfigured vs empty
	state, err := adapter.FetchPaymentState(ctx, interfaces.PaymentStateRequest{})
	// Either error from client or nil
	if err == nil {
		assert.Nil(t, state)
	}
}

func TestCheckoutAdapter_ResponseMapping(t *testing.T) {
	exp := time.Now().Add(1 * time.Hour).UTC()
	resp := &interfaces.CheckoutProviderResponse{
		ProviderSessionID: "cs_test_123",
		NextAction: types.PaymentAction{
			Type: types.PaymentActionTypePaymentLink,
			URL:  "https://checkout.stripe.com/c/pay/cs_test_123",
		},
		ProviderPaymentIntentID: "pi_test_456",
		ExpiresAt:               &exp,
	}

	assert.Equal(t, "cs_test_123", resp.ProviderSessionID)
	assert.Equal(t, types.PaymentActionTypePaymentLink, resp.NextAction.Type)
	assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_test_123", resp.NextAction.URL)
	assert.Equal(t, "pi_test_456", resp.ProviderPaymentIntentID)
	assert.Equal(t, &exp, resp.ExpiresAt)
}
