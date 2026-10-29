package dto

import "github.com/flexprice/flexprice/internal/types"

// RecalculateInvoiceRequest selects reviewed replacement charges for a voided source invoice.
type RecalculateInvoiceRequest struct {
	BillingScheduleItemIDs []string `json:"billing_schedule_item_ids,omitempty"`
}

// Validate preserves bodyless recalculation and bounds an explicitly supplied selection.
func (r RecalculateInvoiceRequest) Validate() error {
	return types.ValidateBillingScheduleItemIDs(r.BillingScheduleItemIDs)
}
