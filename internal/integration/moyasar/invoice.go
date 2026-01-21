package moyasar

import (
	"github.com/flexprice/flexprice/internal/logger"
)

// InvoiceSyncService handles Moyasar invoice synchronization
// Note: Moyasar doesn't have a first-class invoice API like Stripe or Razorpay.
// This service provides a stub for potential future invoice sync functionality.
type InvoiceSyncService struct {
	client      MoyasarClient
	customerSvc MoyasarCustomerService
	logger      *logger.Logger
}

// NewInvoiceSyncService creates a new Moyasar invoice sync service
func NewInvoiceSyncService(
	client MoyasarClient,
	customerSvc MoyasarCustomerService,
	logger *logger.Logger,
) *InvoiceSyncService {
	return &InvoiceSyncService{
		client:      client,
		customerSvc: customerSvc,
		logger:      logger,
	}
}

// Note: Moyasar does not have a native invoice API.
// Payment links are used instead to collect payments for invoices.
// The InvoiceSyncService is provided as a stub for future enhancements
// or for potential integration with external invoice systems.
