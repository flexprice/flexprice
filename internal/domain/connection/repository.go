package connection

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, connection *Connection) error
	Get(ctx context.Context, id string) (*Connection, error)
	GetByProviderType(ctx context.Context, providerType types.SecretProvider) (*Connection, error)
	GetByConnectionCode(ctx context.Context, connectionCode string) (*Connection, error)
	List(ctx context.Context, filter *types.ConnectionFilter) ([]*Connection, error)
	Count(ctx context.Context, filter *types.ConnectionFilter) (int, error)
	Update(ctx context.Context, connection *Connection) error
	Delete(ctx context.Context, id string) error
}
