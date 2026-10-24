package journal

import (
	"context"

	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
)

// Repository persists immutable journal entries and their lines.
type Repository interface {
	// CreateWithLines saves the entry and lines; a posted idempotency key returns ErrAlreadyExists.
	CreateWithLines(ctx context.Context, e *domainJournal.Entry) error
	// GetIDByIdempotencyKey returns the ID of the entry posted under the key.
	GetIDByIdempotencyKey(ctx context.Context, key string) (string, error)
	// Get returns the entry with its lines.
	Get(ctx context.Context, id string) (*domainJournal.Entry, error)
	// List returns the matching entries with their lines.
	List(ctx context.Context, filter *Filter) ([]*domainJournal.Entry, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
