package stripe

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Client:      &Client{},
		CustomerSvc: mockCust,
		Logger:      log,
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
		Client:      &Client{},
		CustomerSvc: mockCust,
		Logger:      log,
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
