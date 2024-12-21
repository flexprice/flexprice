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
}
