package billingterm

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// InstalmentFilter selects instalments for List; empty fields do not constrain.
type InstalmentFilter struct {
	OwnerType types.BillingTermOwnerType
	OwnerIDs  []string

	SubscriptionIDs []string
	Statuses        []types.InstalmentStatus
	Deliveries      []types.InstalmentDelivery

	// BillOnOrBefore selects instalments whose bill_on is at or before it.
	BillOnOrBefore *time.Time

	InvoiceIDs []string
	IDs        []string
}
