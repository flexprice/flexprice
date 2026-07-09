package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestEvaluateMandateUsability(t *testing.T) {
	t.Parallel()
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	ceiling := decimal.NewFromInt(15000)

	tests := []struct {
		name         string
		methods      []*interfaces.ProviderPaymentMethod
		invoiceTotal decimal.Decimal
		wantUsable   bool
		wantExpired  bool
	}{
		{
			name:         "no methods at all",
			methods:      nil,
			invoiceTotal: decimal.NewFromInt(100),
			wantUsable:   false,
		},
		{
			name: "confirmed, unexpired, under ceiling — usable",
			methods: []*interfaces.ProviderPaymentMethod{
				{GatewayMethodID: "t1", Method: types.PaymentMethodTypeUPI, Status: types.PaymentMethodStatusActive, MaxAmount: &ceiling, ExpiresAt: &future, CreatedAt: time.Now()},
			},
			invoiceTotal: decimal.NewFromInt(100),
			wantUsable:   true,
		},
		{
			name: "expired token — not usable, flagged expired",
			methods: []*interfaces.ProviderPaymentMethod{
				{GatewayMethodID: "t1", Method: types.PaymentMethodTypeUPI, Status: types.PaymentMethodStatusActive, MaxAmount: &ceiling, ExpiresAt: &past, CreatedAt: time.Now()},
			},
			invoiceTotal: decimal.NewFromInt(100),
			wantUsable:   false,
			wantExpired:  true,
		},
		{
			name: "over ceiling — not usable, not expired",
			methods: []*interfaces.ProviderPaymentMethod{
				{GatewayMethodID: "t1", Method: types.PaymentMethodTypeUPI, Status: types.PaymentMethodStatusActive, MaxAmount: &ceiling, ExpiresAt: &future, CreatedAt: time.Now()},
			},
			invoiceTotal: decimal.NewFromInt(99999),
			wantUsable:   false,
			wantExpired:  false,
		},
		{
			name: "rejected status — not usable",
			methods: []*interfaces.ProviderPaymentMethod{
				{GatewayMethodID: "t1", Method: types.PaymentMethodTypeUPI, Status: types.PaymentMethodStatusInactive, MaxAmount: &ceiling, ExpiresAt: &future, CreatedAt: time.Now()},
			},
			invoiceTotal: decimal.NewFromInt(100),
			wantUsable:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := evaluateMandateUsability(tt.methods, types.PaymentMethodTypeUPI, tt.invoiceTotal)
			require.Equal(t, tt.wantUsable, result.Usable)
			require.Equal(t, tt.wantExpired, result.Expired)
		})
	}
}

// fakeLock implements cache.Lock for testing.
type fakeLock struct{ acquired bool }

func (f *fakeLock) AcquiredSuccessfully() bool     { return f.acquired }
func (f *fakeLock) Release(_ context.Context) error { return nil }

// fakeLocker implements cache.Locker for testing.
type fakeLocker struct {
	lock cache.Lock
	err  error
}

func (f *fakeLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	return f.lock, f.err
}

// fakeProvider implements interfaces.CheckoutProvider for testing.
// Only ChargeSavedPaymentMethod is exercised; all other methods panic.
type fakeProvider struct {
	chargeResult *interfaces.ChargeResult
	chargeErr    error
}

func (f *fakeProvider) ChargeSavedPaymentMethod(_ context.Context, _ interfaces.ChargeSavedPaymentMethodRequest) (*interfaces.ChargeResult, error) {
	return f.chargeResult, f.chargeErr
}

func (f *fakeProvider) CreatePaymentLink(_ context.Context, _ interfaces.CheckoutProviderRequest) (*interfaces.CheckoutProviderResponse, error) {
	panic("not implemented")
}

func (f *fakeProvider) CreateAuthorizationLink(_ context.Context, _ interfaces.AuthorizationLinkRequest) (*interfaces.CheckoutProviderResponse, error) {
	panic("not implemented")
}

func (f *fakeProvider) ListSavedPaymentMethods(_ context.Context, _ interfaces.ListSavedPaymentMethodsRequest) ([]*interfaces.ProviderPaymentMethod, error) {
	panic("not implemented")
}

func TestAutoChargeInvoice(t *testing.T) {
	t.Parallel()

	inv := &invoice.Invoice{
		ID:            "inv_123",
		EnvironmentID: "env_abc",
		CustomerID:    "cust_xyz",
		Total:         decimal.NewFromInt(500),
		Currency:      "INR",
	}

	lockErr := errors.New("redis unavailable")
	chargeErr := errors.New("charge failed")

	tests := []struct {
		name     string
		locker   cache.Locker
		provider interfaces.CheckoutProvider
		wantErr  bool
	}{
		{
			name:     "lock_acquire_fails_returns_error",
			locker:   &fakeLocker{lock: nil, err: lockErr},
			provider: &fakeProvider{},
			wantErr:  true,
		},
		{
			name:     "lock_not_acquired_skips_charge",
			locker:   &fakeLocker{lock: &fakeLock{acquired: false}, err: nil},
			provider: &fakeProvider{},
			wantErr:  false,
		},
		{
			name:     "charge_error_returns_nil",
			locker:   &fakeLocker{lock: &fakeLock{acquired: true}, err: nil},
			provider: &fakeProvider{chargeResult: nil, chargeErr: chargeErr},
			wantErr:  false,
		},
		{
			name:   "charge_success_returns_nil",
			locker: &fakeLocker{lock: &fakeLock{acquired: true}, err: nil},
			provider: &fakeProvider{
				chargeResult: &interfaces.ChargeResult{
					ProviderPaymentIntentID: "pay_abc",
					Status:                  types.PaymentStatusProcessing,
				},
				chargeErr: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &invoiceService{
				ServiceParams: ServiceParams{
					Locker: tt.locker,
					Logger: logger.NewNoopLogger(),
				},
			}
			ctx := types.SetTenantID(context.Background(), "tenant_test")
			err := svc.AutoChargeInvoice(ctx, inv, "gateway_method_1", tt.provider)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
