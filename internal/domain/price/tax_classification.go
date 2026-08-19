package price

import "github.com/flexprice/flexprice/internal/types"

// HSNSAC returns the Indian GST service code recorded on this price, if any.
func (p *Price) HSNSAC() string {
	if p == nil {
		return ""
	}
	return types.TaxMetadataFromMap(p.Metadata).HSNSAC()
}

// ResolveHSNSAC picks the code to send for a price: its own code, else the code
// on the root plan price it was derived from.
func ResolveHSNSAC(p *Price, pricesByID map[string]*Price) string {
	if p == nil {
		return ""
	}

	if code := p.HSNSAC(); code != "" {
		return code
	}

	if rootID := p.GetRootPriceID(); rootID != "" && rootID != p.ID {
		if root, ok := pricesByID[rootID]; ok {
			return root.HSNSAC()
		}
	}

	return ""
}
