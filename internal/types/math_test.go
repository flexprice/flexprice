package types

import (
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func TestAddDecimalPtr(t *testing.T) {
	five := decimal.RequireFromString("5")
	seven := decimal.RequireFromString("7.25")

	tests := []struct {
		name string
		a, b *decimal.Decimal
		want *decimal.Decimal
	}{
		{"both set", &five, &seven, lo.ToPtr(decimal.RequireFromString("12.25"))},
		{"a nil", nil, &seven, &seven},
		{"b nil", &five, nil, &five},
		{"both nil stays nil", nil, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AddDecimalPtr(tt.a, tt.b)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("AddDecimalPtr nil-ness = %v, want %v", got, tt.want)
			}
			if got != nil && !got.Equal(*tt.want) {
				t.Errorf("AddDecimalPtr = %s, want %s", got, tt.want)
			}
		})
	}
}
