package service

import "github.com/shopspring/decimal"

// SpreadAmount splits total across parts in proportion to weights, assigning
// the rounding remainder to the last positive-weight part so the parts always
// sum exactly to total. All-zero weights put the whole total on the last part.
func SpreadAmount(total decimal.Decimal, weights []decimal.Decimal) []decimal.Decimal {
	parts := make([]decimal.Decimal, len(weights))
	if len(weights) == 0 || total.IsZero() {
		return parts
	}

	weightSum := decimal.Zero
	lastPositive := -1
	for i, w := range weights {
		if w.IsPositive() {
			weightSum = weightSum.Add(w)
			lastPositive = i
		}
	}
	if lastPositive == -1 {
		parts[len(parts)-1] = total
		return parts
	}

	allocated := decimal.Zero
	for i, w := range weights {
		if !w.IsPositive() || i == lastPositive {
			continue
		}
		parts[i] = total.Mul(w).Div(weightSum)
		allocated = allocated.Add(parts[i])
	}
	parts[lastPositive] = total.Sub(allocated)
	return parts
}
