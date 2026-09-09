package types

import (
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// AddDecimalPtr sums two optional decimals. A nil side contributes nothing;
// two nils stay nil so an unset field is not turned into an explicit zero.
func AddDecimalPtr(a, b *decimal.Decimal) *decimal.Decimal {
	if a == nil && b == nil {
		return nil
	}
	return lo.ToPtr(lo.FromPtr(a).Add(lo.FromPtr(b)))
}
