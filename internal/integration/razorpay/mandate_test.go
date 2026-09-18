package razorpay

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestRazorpaySubscriptionMethod(t *testing.T) {
	tests := []struct {
		name    string
		input   types.PaymentMethodType
		want    string
		wantErr bool
	}{
		{name: "empty defaults to upi", input: "", want: "upi", wantErr: false},
		{name: "UPI maps to upi", input: types.PaymentMethodTypeUPI, want: "upi", wantErr: false},
		{name: "Card maps to card", input: types.PaymentMethodTypeCard, want: "card", wantErr: false},
		{name: "unsupported method errors", input: types.PaymentMethodTypeACH, want: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := razorpaySubscriptionMethod(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, ierr.IsNotImplemented(err), "expected ErrNotImplemented-marked error")
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

type stubRazorpayCustomerSvc struct {
	tokens []*interfaces.ProviderPaymentMethod
	err    error
}

func (s *stubRazorpayCustomerSvc) EnsureCustomerSyncedToRazorpay(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error) {
	return nil, nil
}
func (s *stubRazorpayCustomerSvc) SyncCustomerToRazorpay(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error) {
	return "", nil
}
func (s *stubRazorpayCustomerSvc) GetRazorpayCustomerID(ctx context.Context, customerID string) (string, error) {
	return "", nil
}
func (s *stubRazorpayCustomerSvc) UpdateRazorpayCustomerNotes(ctx context.Context, razorpayCustomerID string, notes map[string]interface{}) error {
	return nil
}
func (s *stubRazorpayCustomerSvc) ListConfirmedCustomerTokens(ctx context.Context, customerID string) (string, []*interfaces.ProviderPaymentMethod, error) {
	if s.err != nil {
		return "", nil, s.err
	}
	return "cust_rzp_1", s.tokens, nil
}

func TestRazorpayHasAutoChargeableMethod(t *testing.T) {
	ctx := context.Background()

	t.Run("nil adapter or service returns false, nil", func(t *testing.T) {
		var nilAdapter *CheckoutAdapter
		has, err := nilAdapter.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.False(t, has)

		adapterWithoutSvc := &CheckoutAdapter{}
		has, err = adapterWithoutSvc.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.False(t, has)

		adapterWithoutCustomerSvc := &CheckoutAdapter{Svc: &PaymentService{}}
		has, err = adapterWithoutCustomerSvc.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns true when customer has confirmed recurring mandate token", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: true},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("returns false when no confirmed tokens exist", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false, nil when customer is not found", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					err: ierr.NewError("customer not found").Mark(ierr.ErrNotFound),
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, "cust_1")
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false, err when a real error occurs", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					err: ierr.NewError("api failure").Mark(ierr.ErrHTTPClient),
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, "cust_1")
		assert.Error(t, err)
		assert.True(t, ierr.IsHTTPClient(err))
		assert.False(t, has)
	})
}
