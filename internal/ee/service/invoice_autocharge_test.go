package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/interfaces"
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
