package integrations

import "github.com/flexprice/flexprice/internal/types"

type MoyasarPaymentStatus string

const (
	MoyasarPaymentStatusInitiated  MoyasarPaymentStatus = "initiated"
	MoyasarPaymentStatusAuthorized MoyasarPaymentStatus = "authorized"
	MoyasarPaymentStatusPaid       MoyasarPaymentStatus = "paid"
	MoyasarPaymentStatusFailed     MoyasarPaymentStatus = "failed"
	MoyasarPaymentStatusRefunded   MoyasarPaymentStatus = "refunded"
)

// ToFlexPricePaymentStatus maps a Moyasar payment status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func (s MoyasarPaymentStatus) ToFlexPricePaymentStatus() (types.PaymentStatus, bool) {
	switch s {
	case MoyasarPaymentStatusPaid:
		return types.PaymentStatusSucceeded, true
	case MoyasarPaymentStatusFailed:
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
