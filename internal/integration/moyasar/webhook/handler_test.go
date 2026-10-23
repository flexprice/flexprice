package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubMoyasarClient records void and refund calls; other methods are unused.
type stubMoyasarClient struct {
	moyasar.MoyasarClient
	voidErr   error
	refundErr error
	voided    []string
	refunded  []string
}

func (c *stubMoyasarClient) VoidPayment(_ context.Context, paymentID string) (*moyasar.MoyasarPayment, error) {
	c.voided = append(c.voided, paymentID)
	return &moyasar.MoyasarPayment{ID: paymentID}, c.voidErr
}

func (c *stubMoyasarClient) RefundPayment(_ context.Context, paymentID string, _ int) (*moyasar.RefundPaymentResponse, error) {
	c.refunded = append(c.refunded, paymentID)
	return &moyasar.RefundPaymentResponse{}, c.refundErr
}

// stubPaymentService serves one payment; other methods are unused.
type stubPaymentService struct {
	interfaces.PaymentService
	payment *dto.PaymentResponse
}

func (s *stubPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	return s.payment, nil
}

func cardCheck(status types.PaymentStatus, gatewayPaymentID string) *dto.PaymentResponse {
	return &dto.PaymentResponse{
		ID:               "pay_card_check",
		DestinationType:  types.PaymentDestinationTypeCustomer,
		DestinationID:    "cust_1",
		PaymentStatus:    status,
		GatewayPaymentID: lo.EmptyableToPtr(gatewayPaymentID),
	}
}

func TestIsOrphanedCardCheck(t *testing.T) {
	tests := []struct {
		name          string
		flexprice     *dto.PaymentResponse
		moyasarStatus moyasar.MoyasarPaymentStatus
		want          bool
	}{
		{
			name:          "paid retry on a failed attempt",
			flexprice:     cardCheck(types.PaymentStatusFailed, "moy_declined"),
			moyasarStatus: moyasar.MoyasarPaymentStatusPaid,
			want:          true,
		},
		{
			name:          "captured retry on a voided attempt",
			flexprice:     cardCheck(types.PaymentStatusVoided, "moy_first"),
			moyasarStatus: moyasar.MoyasarPaymentStatusCaptured,
			want:          true,
		},
		{
			name:          "open card check is recorded normally",
			flexprice:     cardCheck(types.PaymentStatusInitiated, ""),
			moyasarStatus: moyasar.MoyasarPaymentStatusPaid,
			want:          false,
		},
		{
			name:          "redelivery for the payment that closed the record",
			flexprice:     cardCheck(types.PaymentStatusVoided, "moy_retry"),
			moyasarStatus: moyasar.MoyasarPaymentStatusPaid,
			want:          false,
		},
		{
			name:          "charge already released at Moyasar",
			flexprice:     cardCheck(types.PaymentStatusFailed, "moy_declined"),
			moyasarStatus: moyasar.MoyasarPaymentStatusVoided,
			want:          false,
		},
		{
			name: "invoice payments are out of scope",
			flexprice: &dto.PaymentResponse{
				DestinationType:  types.PaymentDestinationTypeInvoice,
				PaymentStatus:    types.PaymentStatusFailed,
				GatewayPaymentID: lo.ToPtr("moy_declined"),
			},
			moyasarStatus: moyasar.MoyasarPaymentStatusPaid,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &moyasar.MoyasarPayment{ID: "moy_retry", Status: string(tt.moyasarStatus)}
			assert.Equal(t, tt.want, isOrphanedCardCheck(tt.flexprice, payment))
		})
	}
}

func TestHandlePaymentLifecycle_ReleasesOrphanedCardCheck(t *testing.T) {
	tests := []struct {
		name         string
		voidErr      error
		refundErr    error
		wantRefunded bool
	}{
		{name: "voids the charge"},
		{name: "refunds when the void fails", voidErr: errors.New("void window closed"), wantRefunded: true},
		{name: "logs when both fail", voidErr: errors.New("void failed"), refundErr: errors.New("refund failed"), wantRefunded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubMoyasarClient{voidErr: tt.voidErr, refundErr: tt.refundErr}
			// No lifecycle: the orphaned path must not record anything.
			h := NewHandler(client, nil, nil, nil, nil, logger.NewNoopLogger())
			services := &ServiceDependencies{
				PaymentService: &stubPaymentService{payment: cardCheck(types.PaymentStatusFailed, "moy_declined")},
			}
			payment := &moyasar.MoyasarPayment{ID: "moy_retry", Status: string(moyasar.MoyasarPaymentStatusPaid)}

			err := h.handlePaymentLifecycle(context.Background(), payment, "pay_card_check", services)

			require.NoError(t, err)
			assert.Equal(t, []string{"moy_retry"}, client.voided)
			if tt.wantRefunded {
				assert.Equal(t, []string{"moy_retry"}, client.refunded)
			} else {
				assert.Empty(t, client.refunded)
			}
		})
	}
}
