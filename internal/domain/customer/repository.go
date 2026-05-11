package customer

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for customer data access
type Repository interface {
	Create(ctx context.Context, customer *Customer) error
	Get(ctx context.Context, id string) (*Customer, error)
	List(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	Count(ctx context.Context, filter *types.CustomerFilter) (int, error)
	ListAll(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	Update(ctx context.Context, customer *Customer) error
	Delete(ctx context.Context, customer *Customer) error
	GetByLookupKey(ctx context.Context, lookupKey string) (*Customer, error)
	// MergeMetadata merges the given key-value pairs into the customer's existing
	// metadata without overwriting unrelated keys. Safe to call concurrently.
	MergeMetadata(ctx context.Context, customerID string, meta map[string]string) error
}
