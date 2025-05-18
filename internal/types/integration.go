package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// IntegrationCapability represents features an integration can support
type IntegrationCapability string

const (
	CapabilityCustomer      IntegrationCapability = "customer"
	CapabilityPaymentMethod IntegrationCapability = "payment_method"
	CapabilityPayment       IntegrationCapability = "payment"
	CapabilityInvoice       IntegrationCapability = "invoice"
)

type SyncStatus string

const (
	SyncStatusPending SyncStatus = "pending"
	SyncStatusSuccess SyncStatus = "success"
	SyncStatusFailed  SyncStatus = "failed"
)

type SyncEventAction string

const (
	SyncEventActionCreate SyncEventAction = "create"
	SyncEventActionUpdate SyncEventAction = "update"
	SyncEventActionDelete SyncEventAction = "delete"
)

func (c IntegrationCapability) Validate() error {

	switch c {
	case CapabilityCustomer, CapabilityPaymentMethod, CapabilityPayment, CapabilityInvoice:
		return nil
	default:
		return ierr.NewError("invalid capability").
			WithHint("Please specify a valid capability").
			Mark(ierr.ErrValidation)
	}
}
