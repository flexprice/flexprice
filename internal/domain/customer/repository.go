package customer

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Ref identifies a customer without loading the row.
type Ref struct {
	ID         string
	ExternalID string
}

// Repository defines the interface for customer data access
type Repository interface {
	Create(ctx context.Context, customer *Customer) error
	Get(ctx context.Context, id string) (*Customer, error)
	List(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	Count(ctx context.Context, filter *types.CustomerFilter) (int, error)
	ListAll(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	// ListByFilterPage pages matches by id.
	ListByFilterPage(ctx context.Context, filter *types.CustomerFilter, afterID string, limit int) ([]*Customer, error)
	// ListMetadataPageByFilter pages ID and metadata only.
	ListMetadataPageByFilter(ctx context.Context, filter *types.CustomerFilter, afterID string, limit int) ([]*Customer, error)
	// ListRefsByFilter returns id and external id of matches; errors when more than max (no cap when max <= 0).
	ListRefsByFilter(ctx context.Context, filter *types.CustomerFilter, max int) ([]Ref, error)
	Update(ctx context.Context, customer *Customer) error
	Delete(ctx context.Context, customer *Customer) error
	GetByLookupKey(ctx context.Context, lookupKey string) (*Customer, error)
	// AssignDefaultBillingEntities sets up to limit (all when limit <= 0) of the environment's customers
	// without an entity to its default entity, bypassing the activity log.
	AssignDefaultBillingEntities(ctx context.Context, limit int) (int, error)
}
