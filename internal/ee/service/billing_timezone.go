package enterprise

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// BillingTimezoneService provides billing in customers' timezones (enterprise).
// Timezone logic exists in subscription (CustomerTimezone), settings (InvoiceNumberTimezone),
// and invoice repo (getYearMonth with timezone).
type BillingTimezoneService interface {
	// ResolveTimezone resolves a timezone abbreviation to IANA identifier
	ResolveTimezone(timezone string) string
	// ValidateTimezone validates that a timezone is valid
	ValidateTimezone(ctx context.Context, timezone string) error
}

type billingTimezoneService struct {
	EnterpriseParams
}

// NewBillingTimezoneService creates a new enterprise billing timezone service.
func NewBillingTimezoneService(p EnterpriseParams) BillingTimezoneService {
	return &billingTimezoneService{EnterpriseParams: p}
}

func (s *billingTimezoneService) ResolveTimezone(timezone string) string {
	return types.ResolveTimezone(timezone)
}

func (s *billingTimezoneService) ValidateTimezone(ctx context.Context, timezone string) error {
	return types.ValidateTimezone(timezone)
}
