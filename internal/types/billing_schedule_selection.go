package types

import (
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ValidateBillingScheduleItemIDs accepts an omitted selection or 1–500 distinct source IDs.
func ValidateBillingScheduleItemIDs(ids []string) error {
	if ids == nil {
		return nil
	}
	if len(ids) == 0 || len(ids) > 500 {
		return ierr.Validation("billing_schedule_item_ids must contain between 1 and 500 IDs")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 50 || seen[id] {
			return ierr.Validation("billing_schedule_item_ids must contain valid unique IDs")
		}
		seen[id] = true
	}
	return nil
}
