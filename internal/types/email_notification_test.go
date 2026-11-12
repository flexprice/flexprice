package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailDeliveryStatus_CanMoveTo(t *testing.T) {
	tests := []struct {
		from, to EmailDeliveryStatus
		want     bool
	}{
		{EmailDeliveryStatusQueued, EmailDeliveryStatusSent, true},
		{EmailDeliveryStatusQueued, EmailDeliveryStatusDelivered, true},
		{EmailDeliveryStatusSent, EmailDeliveryStatusDeliveryDelayed, true},
		{EmailDeliveryStatusDeliveryDelayed, EmailDeliveryStatusDelivered, true},
		{EmailDeliveryStatusDelivered, EmailDeliveryStatusComplained, true},
		{EmailDeliveryStatusDelivered, EmailDeliveryStatusSent, false},
		{EmailDeliveryStatusDelivered, EmailDeliveryStatusDelivered, false},
		{EmailDeliveryStatusDeliveryDelayed, EmailDeliveryStatusBounced, true},
		{EmailDeliveryStatusQueued, EmailDeliveryStatusFailed, true},
		{EmailDeliveryStatusBounced, EmailDeliveryStatusDelivered, false},
		{EmailDeliveryStatusFailed, EmailDeliveryStatusSuppressed, false},
		{EmailDeliveryStatusSkipped, EmailDeliveryStatusSent, false},
		{EmailDeliveryStatusQueued, EmailDeliveryStatusSkipped, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.from.CanMoveTo(tt.to))
		})
	}
}
