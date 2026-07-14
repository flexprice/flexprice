package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCapMandateLimit(t *testing.T) {
	limits := types.PaymentMandateLimits{
		MandateLimits: map[types.PaymentMethodType]types.MandateLimit{
			types.PaymentMethodTypeUPI: {MaxAmount: decimal.NewFromInt(100000), Currency: "INR"},
		},
	}

	tests := []struct {
		name        string
		method      types.PaymentMethodType
		callerLimit *decimal.Decimal
		currency    string
		want        *decimal.Decimal
	}{
		{
			name:        "caller omits limit entirely -> capped to tenant ceiling, not unbounded",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: nil,
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "caller requests above tenant ceiling -> clamped down",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(500000)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "caller requests below tenant ceiling -> caller value respected",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(5000)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(5000)),
		},
		{
			name:        "empty method defaults to UPI's configured ceiling",
			method:      "",
			callerLimit: nil,
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "currency mismatch -> tenant ceiling does not apply, caller value passes through",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(500000)),
			currency:    "USD",
			want:        lo.ToPtr(decimal.NewFromInt(500000)),
		},
		{
			name:        "no configured ceiling for method (Card) -> caller value passes through unchanged",
			method:      types.PaymentMethodTypeCard,
			callerLimit: lo.ToPtr(decimal.NewFromInt(999999)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(999999)),
		},
		{
			name:        "no configured ceiling for method (Card) and caller omits limit -> nil (no ceiling)",
			method:      types.PaymentMethodTypeCard,
			callerLimit: nil,
			currency:    "INR",
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capMandateLimit(tt.method, tt.callerLimit, tt.currency, limits)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.True(t, tt.want.Equal(*got), "expected %s, got %s", tt.want, got)
		})
	}
}
