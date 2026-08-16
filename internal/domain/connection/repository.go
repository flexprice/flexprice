package connection

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for connection data operations
type Repository interface {
	Create(ctx context.Context, connection *Connection) error
	Get(ctx context.Context, id string) (*Connection, error)
	GetByProvider(ctx context.Context, provider types.SecretProvider) (*Connection, error)
	// ListPublishedByProvider returns every published connection for a provider across all tenants and environments.
	ListPublishedByProvider(ctx context.Context, provider types.SecretProvider) ([]*Connection, error)
	// ListAllPublished returns every published connection for the current tenant and environment
	ListAllPublished(ctx context.Context) ([]*Connection, error)
	List(ctx context.Context, filter *types.ConnectionFilter) ([]*Connection, error)
	Count(ctx context.Context, filter *types.ConnectionFilter) (int, error)
	Update(ctx context.Context, connection *Connection) error
	Delete(ctx context.Context, connection *Connection) error
}
