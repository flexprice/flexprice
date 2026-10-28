package environment

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, environment *Environment) error
	Get(ctx context.Context, id string) (*Environment, error)
	List(ctx context.Context, filter types.Filter) ([]*Environment, error)
	Update(ctx context.Context, environment *Environment) error
	CountByType(ctx context.Context, envType types.EnvironmentType) (int, error)
	// GetBillingEntityMode reads the mode from the database, never the cache; inside a transaction it reads that transaction.
	GetBillingEntityMode(ctx context.Context, id string) (types.BillingEntityMode, error)
	SetBillingEntityMode(ctx context.Context, id string, mode types.BillingEntityMode) error
}
