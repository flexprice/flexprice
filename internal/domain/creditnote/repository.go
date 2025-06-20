package creditnote

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for invoice persistence operations
type Repository interface {
	// Core invoice operations
	Create(ctx context.Context, inv *CreditNote) error
	Get(ctx context.Context, id string) (*CreditNote, error)
	Update(ctx context.Context, inv *CreditNote) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.CreditNoteFilter) ([]*CreditNote, error)
	Count(ctx context.Context, filter *types.CreditNoteFilter) (int, error)

	// Edge-specific operations
	AddLineItems(ctx context.Context, creditNoteID string, items []*CreditNoteLineItem) error
	RemoveLineItems(ctx context.Context, creditNoteID string, itemIDs []string) error

	// Bulk operations with edges
	CreateWithLineItems(ctx context.Context, inv *CreditNote) error

	// Idempotency operations
	GetByIdempotencyKey(ctx context.Context, key string) (*CreditNote, error)
}
