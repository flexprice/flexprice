package environment

import "context"

type Repository interface {
	Create(ctx context.Context, env *Environment) error
	GetByID(ctx context.Context, id string) (*Environment, error)
	GetDefault(ctx context.Context) (*Environment, error)
	List(ctx context.Context) ([]*Environment, error)
	Update(ctx context.Context, env *Environment) error
}
