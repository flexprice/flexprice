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
	Delete(ctx context.Context, id string) error
}
