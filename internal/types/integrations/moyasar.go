package integrations

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type MoyasarPaymentStatus string

const (
	MoyasarPaymentStatusInitiated  MoyasarPaymentStatus = "initiated"
	MoyasarPaymentStatusAuthorized MoyasarPaymentStatus = "authorized"
	MoyasarPaymentStatusPaid       MoyasarPaymentStatus = "paid"
	MoyasarPaymentStatusFailed     MoyasarPaymentStatus = "failed"
	MoyasarPaymentStatusRefunded   MoyasarPaymentStatus = "refunded"
)

// ToFlexpricePaymentStatus maps a Moyasar payment status to a FlexPrice PaymentStatus.
func (s MoyasarPaymentStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case MoyasarPaymentStatusPaid:
		return types.PaymentStatusSucceeded, nil
	case MoyasarPaymentStatusFailed:
		return types.PaymentStatusFailed, nil
	default:
		return "", ierr.NewError("unmapped moyasar payment status").
			WithReportableDetails(map[string]interface{}{
				"moyasar_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}
