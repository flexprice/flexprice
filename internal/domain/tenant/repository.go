package tenant

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, tenant *Tenant) error
	GetByID(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Update(ctx context.Context, tenant *Tenant) error
	// GetBillingEntityMode reads the mode from the database, never the cache.
	GetBillingEntityMode(ctx context.Context, tenantID string) (types.BillingEntityMode, error)
	SetBillingEntityMode(ctx context.Context, tenantID string, mode types.BillingEntityMode) error
}
