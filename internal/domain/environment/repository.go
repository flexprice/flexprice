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
	// GetDefaultByType returns the tenant's oldest published environment of the
	// given type, or a not-found error when it has none. Used to resolve a
	// tenant's default environment without paging through its full list.
	GetDefaultByType(ctx context.Context, envType types.EnvironmentType) (*Environment, error)
}
