package invoice

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for invoice persistence operations
type Repository interface {
	// Core invoice operations
	Create(ctx context.Context, inv *Invoice) error
	Get(ctx context.Context, id string) (*Invoice, error)
	Update(ctx context.Context, inv *Invoice) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.InvoiceFilter) ([]*Invoice, error)
	Count(ctx context.Context, filter *types.InvoiceFilter) (int, error)

	// Edge-specific operations
	AddLineItems(ctx context.Context, invoiceID string, items []*InvoiceLineItem) error
	RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error

	// Bulk operations with edges
	CreateWithLineItems(ctx context.Context, inv *Invoice) error

	// Idempotency operations
	GetByIdempotencyKey(ctx context.Context, key string) (*Invoice, error)

	// Period validation
	ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error)

	// GetNextInvoiceNumber generates and returns the next invoice number for a tenant
	// Format: INV-YYYYMM-XXXXX
	GetNextInvoiceNumber(ctx context.Context, invoiceConfig *types.InvoiceConfig) (string, error)

	// GetNextBillingSequence returns the next billing sequence number for a subscription
	GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error)

	// GetInvoicesForExport retrieves invoices for export purposes with pagination
	GetInvoicesForExport(ctx context.Context, tenantID, envID string, startTime, endTime time.Time, limit, offset int) ([]*Invoice, error)

	// Dashboard methods
	GetRevenueTrend(ctx context.Context, windowCount int) ([]types.RevenueTrendWindow, error)
	GetInvoicePaymentStatus(ctx context.Context) (*types.InvoicePaymentStatus, error)
}
