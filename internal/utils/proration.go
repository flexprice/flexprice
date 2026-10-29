package utils

import "github.com/shopspring/decimal"

// AllocateProRata splits total across keys by weight, rounding each share to precision; the residual goes to the
// largest weight (ties by key).
// e.g. 12 over weights 60 and 40 → 7.20 and 4.80.
func AllocateProRata(weights map[string]decimal.Decimal, total decimal.Decimal, precision int32) map[string]decimal.Decimal {
	sum := decimal.Zero
	residualKey := ""
	for key, w := range weights {
		sum = sum.Add(w)
		largest := weights[residualKey].Abs()
		if residualKey == "" || w.Abs().GreaterThan(largest) || (w.Abs().Equal(largest) && key < residualKey) {
			residualKey = key
		}
	}
	if sum.IsZero() {
		return nil
	}

	shares := make(map[string]decimal.Decimal, len(weights))
	allocated := decimal.Zero
	for key, w := range weights {
		if key == residualKey {
			continue
		}
		shares[key] = total.Mul(w).Div(sum).Round(precision)
		allocated = allocated.Add(shares[key])
	}
	shares[residualKey] = total.Sub(allocated)
	return shares
}
