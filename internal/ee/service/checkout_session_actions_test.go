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
			types.PaymentMethodTypeUPI:  {MaxAmount: decimal.NewFromInt(100000), Currency: "INR"},
			types.PaymentMethodTypeCard: {MaxAmount: decimal.NewFromInt(50000), Currency: "INR"},
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
			name:        "UPI, no caller limit, capped to ceiling",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: nil,
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "UPI, caller above ceiling, clamped",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(500000)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "UPI, caller below ceiling, unchanged",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(5000)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(5000)),
		},
		{
			name:        "empty method defaults to UPI",
			method:      "",
			callerLimit: nil,
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(100000)),
		},
		{
			name:        "UPI, currency mismatch, ceiling not applied",
			method:      types.PaymentMethodTypeUPI,
			callerLimit: lo.ToPtr(decimal.NewFromInt(500000)),
			currency:    "USD",
			want:        lo.ToPtr(decimal.NewFromInt(500000)),
		},
		{
			name:        "Card ignores its own configured ceiling",
			method:      types.PaymentMethodTypeCard,
			callerLimit: lo.ToPtr(decimal.NewFromInt(999999)),
			currency:    "INR",
			want:        lo.ToPtr(decimal.NewFromInt(999999)),
		},
		{
			name:        "Card, no caller limit, stays unbounded",
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
