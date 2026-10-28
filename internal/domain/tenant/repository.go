package tenant

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, tenant *Tenant) error
	GetByID(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Update(ctx context.Context, tenant *Tenant) error
}
