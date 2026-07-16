package integrations

import "github.com/flexprice/flexprice/internal/types"

// MapMoyasarPaymentStatus maps a Moyasar payment status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func MapMoyasarPaymentStatus(moyasarStatus string) (types.PaymentStatus, bool) {
	switch moyasarStatus {
	case "paid":
		return types.PaymentStatusSucceeded, true
	case "failed":
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
