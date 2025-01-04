package invoice

import (
	"context"

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
}
