package payload

import "github.com/flexprice/flexprice/internal/service"

// Services container for all services needed by payload builders
type Services struct {
	InvoiceService service.InvoiceService
	// Add other services as needed
}

// NewServices creates a new Services container
func NewServices(
	invoiceService service.InvoiceService,
	// Add other services as needed
) *Services {
	return &Services{
		InvoiceService: invoiceService,
	}
}
