package invoice

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// LineItemRepository defines the interface for invoice line item operations.
// Callers that need both an invoice and its line items should fetch them
// independently and compose at the service layer.
type LineItemRepository interface {
	// Create creates a single invoice line item (used for addons / adjustments)
	Create(ctx context.Context, item *InvoiceLineItem) error

	// CreateBulk creates multiple line items; implementations must batch to
	// avoid hitting PostgreSQL's 65 535-parameter limit.
	CreateBulk(ctx context.Context, items []*InvoiceLineItem) error

	// Get retrieves a single line item by ID (tenant-scoped).
	Get(ctx context.Context, id string) (*InvoiceLineItem, error)

	// Update updates mutable fields on a line item: PrepaidCreditsApplied,
	// LineItemDiscount, InvoiceLevelDiscount, Metadata, Status, timestamps.
	Update(ctx context.Context, item *InvoiceLineItem) error

	// Delete soft-deletes a line item.
	Delete(ctx context.Context, id string) error

	// ListByInvoiceID retrieves all published line items for a given invoice.
	// Query uses the (tenant_id, environment_id, invoice_id, status) index.
	ListByInvoiceID(ctx context.Context, invoiceID string) ([]*InvoiceLineItem, error)

	// List retrieves invoice line items matching the filter.
	List(ctx context.Context, filter *types.InvoiceLineItemFilter) ([]*InvoiceLineItem, error)
}
