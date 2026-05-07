package invoice

import (
	"context"
	"time"

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

	// GetRevenueByCustomer aggregates invoice line item amounts grouped by
	// customer_id and price_type for DRAFT/FINALIZED invoices within the given period.
	// When customerIDs is non-empty, results are scoped to those customers only.
	GetRevenueByCustomer(ctx context.Context, periodStart, periodEnd time.Time, customerIDs []string) ([]RevenueByCustomerRow, error)

	// GetVoiceMinutesByCustomer aggregates invoice line item quantity (in milliseconds)
	// grouped by customer_id for a specific meter within the given period.
	// When customerIDs is non-empty, results are scoped to those customers only.
	GetVoiceMinutesByCustomer(ctx context.Context, periodStart, periodEnd time.Time, meterID string, customerIDs []string) ([]VoiceMinutesRow, error)

	// GetRevenueTimeSeries aggregates invoice line item amounts grouped by time bucket
	// (date_trunc) and price_type. dateTruncPart must be "day" or "month" for PostgreSQL.
	GetRevenueTimeSeries(ctx context.Context, periodStart, periodEnd time.Time, dateTruncPart string, customerIDs []string) ([]RevenueTimeSeriesRow, error)

	// GetVoiceMinutesTimeSeries aggregates quantity (ms) by time bucket for a meter.
	GetVoiceMinutesTimeSeries(ctx context.Context, periodStart, periodEnd time.Time, meterID, dateTruncPart string, customerIDs []string) ([]VoiceMinutesTimeSeriesRow, error)
}
