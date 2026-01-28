package enterprise

import (
	"context"
)

// InvoiceGracePeriodService provides invoice grace periods (enterprise).
// Grace period logic exists in settings (GracePeriodDays) and subscription service
// (past-grace-period checks for auto-cancellation).
type InvoiceGracePeriodService interface {
	// GetDefaultGracePeriodDays returns the default grace period days from subscription config
	GetDefaultGracePeriodDays(ctx context.Context) int
	// IsValidGracePeriod validates grace period days
	IsValidGracePeriod(days int) bool
}

type invoiceGracePeriodService struct {
	EnterpriseParams
}

// NewInvoiceGracePeriodService creates a new enterprise invoice grace period service.
func NewInvoiceGracePeriodService(p EnterpriseParams) InvoiceGracePeriodService {
	return &invoiceGracePeriodService{EnterpriseParams: p}
}

func (s *invoiceGracePeriodService) GetDefaultGracePeriodDays(ctx context.Context) int {
	// Default from types.SubscriptionConfig default
	return 3
}

func (s *invoiceGracePeriodService) IsValidGracePeriod(days int) bool {
	return days >= 1
}
