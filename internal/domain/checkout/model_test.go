package checkout

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCheckoutSession_Validate(t *testing.T) {
	validAction := types.CheckoutActionCreateSubscription
	validStatus := types.CheckoutStatusInitiated
	validProvider := types.CheckoutPaymentProviderStripe
	future := time.Now().Add(time.Hour)

	tests := []struct {
		name    string
		session *CheckoutSession
		wantErr bool
	}{
		{
			name: "valid session",
			session: &CheckoutSession{
				CustomerID:     "cust_123",
				Action:         validAction,
				CheckoutStatus: validStatus,
				ExpiresAt:      future,
			},
			wantErr: false,
		},
		{
			name: "missing customer_id",
			session: &CheckoutSession{
				Action:         validAction,
				CheckoutStatus: validStatus,
				ExpiresAt:      future,
			},
			wantErr: true,
		},
		{
			name: "invalid action",
			session: &CheckoutSession{
				CustomerID:     "cust_123",
				Action:         types.CheckoutAction("bad_action"),
				CheckoutStatus: validStatus,
				ExpiresAt:      future,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			session: &CheckoutSession{
				CustomerID:     "cust_123",
				Action:         validAction,
				CheckoutStatus: types.CheckoutStatus("bad_status"),
				ExpiresAt:      future,
			},
			wantErr: true,
		},
		{
			name: "zero expires_at",
			session: &CheckoutSession{
				CustomerID:     "cust_123",
				Action:         validAction,
				CheckoutStatus: validStatus,
				ExpiresAt:      time.Time{},
			},
			wantErr: true,
		},
		{
			name: "invalid payment provider",
			session: &CheckoutSession{
				CustomerID:      "cust_123",
				Action:          validAction,
				CheckoutStatus:  validStatus,
				ExpiresAt:       future,
				PaymentProvider: types.CheckoutPaymentProvider("bad"),
			},
			wantErr: true,
		},
		{
			name: "valid with payment provider",
			session: &CheckoutSession{
				CustomerID:      "cust_123",
				Action:          validAction,
				CheckoutStatus:  validStatus,
				ExpiresAt:       future,
				PaymentProvider: validProvider,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
