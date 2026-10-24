package webhook

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

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
