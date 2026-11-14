package contract

import "github.com/flexprice/flexprice/internal/ee/agents/extraction"

func needsCheck(t extraction.Term, threshold int) bool {
	if t.Derived || t.Page == 0 {
		return false
	}
	return len(t.Options) > 0 || (t.IsSet() && t.Confidence < threshold)
}

func countToCheck(r *extraction.Result, threshold int) *extraction.ToCheck {
	tc := &extraction.ToCheck{}
	eachTerm(r, func(step string, t *extraction.Term, _ []string) {
		if !needsCheck(*t, threshold) {
			return
		}
		tc.Total++
		switch step {
		case stepCustomer:
			tc.Customer++
		case stepCharges:
			tc.Charges++
		case stepBilling:
			tc.Billing++
		}
	})
	return tc
}
