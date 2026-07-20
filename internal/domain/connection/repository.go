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
	// ListPublishedByProvider returns every published connection for a provider across all tenants
	// and environments. Unlike List, it applies no tenant/environment scoping, so scheduled jobs
	// that run without a tenant context can discover the connections to process; each returned
	// connection carries its own tenant and environment for the caller to apply.
	ListPublishedByProvider(ctx context.Context, provider types.SecretProvider) ([]*Connection, error)
	List(ctx context.Context, filter *types.ConnectionFilter) ([]*Connection, error)
	Count(ctx context.Context, filter *types.ConnectionFilter) (int, error)
	Update(ctx context.Context, connection *Connection) error
	Delete(ctx context.Context, connection *Connection) error
}
