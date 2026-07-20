package razorpay

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestSelectAutoChargeToken(t *testing.T) {
	now := time.Now().UTC()
	upiToken := &interfaces.ProviderPaymentMethod{GatewayMethodID: "tok_upi", Method: types.PaymentMethodTypeUPI, CreatedAt: now}
	cardToken := &interfaces.ProviderPaymentMethod{GatewayMethodID: "tok_card", Method: types.PaymentMethodTypeCard, CreatedAt: now}

	tests := []struct {
		name      string
		tokens    []*interfaces.ProviderPaymentMethod
		wantID    string
		wantFound bool
	}{
		{name: "UPI only", tokens: []*interfaces.ProviderPaymentMethod{upiToken}, wantID: "tok_upi", wantFound: true},
		{name: "Card only", tokens: []*interfaces.ProviderPaymentMethod{cardToken}, wantID: "tok_card", wantFound: true},
		{name: "both present, Card wins", tokens: []*interfaces.ProviderPaymentMethod{upiToken, cardToken}, wantID: "tok_card", wantFound: true},
		{name: "neither present", tokens: nil, wantID: "", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectAutoChargeToken(tt.tokens, decimal.NewFromInt(100))
			assert.Equal(t, tt.wantFound, ok)
			if tt.wantFound {
				assert.Equal(t, tt.wantID, got.GatewayMethodID)
			}
		})
	}
}
