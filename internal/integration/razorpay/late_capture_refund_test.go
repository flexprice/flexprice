package razorpay

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type grantedLock struct{}

func (grantedLock) AcquiredSuccessfully() bool    { return true }
func (grantedLock) Release(context.Context) error { return nil }

type grantingLocker struct{}

func (grantingLocker) AcquireLock(context.Context, string, time.Duration) (cache.Lock, error) {
	return grantedLock{}, nil
}

type lateCaptureClient struct {
	RazorpayClient
	refunds int
}

func (c *lateCaptureClient) FetchPayment(context.Context, string) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "captured"}, nil
}

func (c *lateCaptureClient) RefundPayment(context.Context, string, int64, string) (map[string]interface{}, error) {
	c.refunds++
	return map[string]interface{}{"id": "rfnd_1"}, nil
}

type lateCapturePayments struct {
	interfaces.PaymentService
	status  types.PaymentStatus
	updates []types.PaymentStatus
}

func (p *lateCapturePayments) GetPayment(context.Context, string) (*dto.PaymentResponse, error) {
	return &dto.PaymentResponse{ID: "pay_fp", PaymentStatus: p.status, Amount: decimal.NewFromInt(136)}, nil
}

func (p *lateCapturePayments) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	target := types.PaymentStatus(lo.FromPtr(req.PaymentStatus))
	if err := p.status.ValidateTransitionTo(target); err != nil {
		return nil, err
	}
	p.status = target
	p.updates = append(p.updates, target)
	return &dto.PaymentResponse{ID: "pay_fp", PaymentStatus: target}, nil
}

func TestRefundLateCapturedPayment(t *testing.T) {
	tests := []struct {
		name        string
		status      types.PaymentStatus
		wantUpdates []types.PaymentStatus
	}{
		{
			name:        "pending checkout payment records the capture before the refund",
			status:      types.PaymentStatusPending,
			wantUpdates: []types.PaymentStatus{types.PaymentStatusSucceeded, types.PaymentStatusRefunded},
		},
		{
			name:        "payment already synced to succeeded is only refunded",
			status:      types.PaymentStatusSucceeded,
			wantUpdates: []types.PaymentStatus{types.PaymentStatusRefunded},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &lateCaptureClient{}
			payments := &lateCapturePayments{status: tt.status}
			svc := &PaymentService{client: client, locker: grantingLocker{}, logger: logger.NewNoopLogger()}

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_fp", "pay_rzp", payments)
			require.NoError(t, err)

			assert.Equal(t, 1, client.refunds)
			assert.Equal(t, tt.wantUpdates, payments.updates)
			assert.Equal(t, types.PaymentStatusRefunded, payments.status)
		})
	}
}
