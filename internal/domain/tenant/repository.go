package tenant

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, tenant *Tenant) error
	GetByID(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	// ListIDs returns published tenant ids in ascending order.
	ListIDs(ctx context.Context) ([]string, error)
	Update(ctx context.Context, tenant *Tenant) error
}
