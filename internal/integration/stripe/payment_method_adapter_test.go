package stripe

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripeapi "github.com/stripe/stripe-go/v82"
)

type mockCustomerServiceForPMAdapter struct {
	interfaces.CustomerService
	cust *dto.CustomerResponse
	err  error
}

func (m *mockCustomerServiceForPMAdapter) GetCustomer(_ context.Context, _ string) (*dto.CustomerResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cust, nil
}

func TestPaymentMethodAdapter_Unconfigured(t *testing.T) {
	ctx := context.Background()
	adapter := &PaymentMethodAdapter{}

	methods, err := adapter.ListSavedMethods(ctx, "cust_123")
	require.Error(t, err)
	assert.Nil(t, methods)

	err = adapter.DeleteSavedMethod(ctx, "cust_123", "pm_123")
	require.Error(t, err)

	err = adapter.SetDefaultSavedMethod(ctx, "cust_123", "pm_123")
	require.Error(t, err)

	_, err = adapter.CreateSetupLink(ctx, interfaces.SetupLinkRequest{CustomerID: "cust_123"})
	require.Error(t, err)
}

func TestPaymentMethodAdapter_ListSavedMethods_CustomerNotSynced(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	mockCust := &mockCustomerServiceForPMAdapter{
		cust: &dto.CustomerResponse{
			Customer: &customerDomain.Customer{
				ID:       "cust_123",
				Metadata: map[string]string{}, // no stripe_customer_id
			},
		},
	}
	adapter := &PaymentMethodAdapter{
		Client:            &Client{},
		StripeCustomerSvc: &CustomerService{},
		CustomerSvc:       mockCust,
		Logger:            log,
	}

	methods, err := adapter.ListSavedMethods(ctx, "cust_123")
	assert.NoError(t, err)
	assert.Nil(t, methods)
}

func TestPaymentMethodAdapter_ListSavedMethods_CustomerLookupFailurePropagates(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNoopLogger()
	mockCust := &mockCustomerServiceForPMAdapter{
		err: ierr.NewError("database unavailable").Mark(ierr.ErrSystem),
	}
	adapter := &PaymentMethodAdapter{
		Client:            &Client{},
		StripeCustomerSvc: &CustomerService{},
		CustomerSvc:       mockCust,
		Logger:            log,
	}

	// A genuine lookup failure must not be reported as "no saved methods" - the
	// portal needs to tell the two apart.
	methods, err := adapter.ListSavedMethods(ctx, "cust_123")
	require.Error(t, err)
	assert.Nil(t, methods)
}

func TestPaymentMethodAdapter_ValidateMethodIDRequired(t *testing.T) {
	ctx := context.Background()
	adapter := &PaymentMethodAdapter{
		CustomerSvc: &mockCustomerServiceForPMAdapter{},
	}

	err := adapter.DeleteSavedMethod(ctx, "cust_123", "")
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	err = adapter.SetDefaultSavedMethod(ctx, "cust_123", "")
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

// A card is chargeable through the last day of its expiry month, and Stripe reports
// nothing else that would make an attached method unusable.
func TestPaymentMethodUsable(t *testing.T) {
	now := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	card := func(year, month int64) *stripeapi.PaymentMethod {
		return &stripeapi.PaymentMethod{Card: &stripeapi.PaymentMethodCard{ExpYear: year, ExpMonth: month}}
	}

	assert.False(t, paymentMethodUsable(nil, now))
	assert.True(t, paymentMethodUsable(&stripeapi.PaymentMethod{}, now), "non-card methods carry no expiry")
	assert.True(t, paymentMethodUsable(card(2027, 1), now))
	assert.True(t, paymentMethodUsable(card(2026, 6), now), "valid through the end of the expiry month")
	assert.True(t, paymentMethodUsable(card(2026, 12), now))
	assert.False(t, paymentMethodUsable(card(2026, 5), now))
	assert.False(t, paymentMethodUsable(card(2025, 12), now))
}
