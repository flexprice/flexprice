package connection

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for connection data operations
type Repository interface {
	Create(ctx context.Context, connection *Connection) error
	Get(ctx context.Context, id string) (*Connection, error)
	GetByEnvironmentAndProvider(ctx context.Context, environmentID string, provider types.SecretProvider) (*Connection, error)
	List(ctx context.Context, filter *types.ConnectionFilter) ([]*Connection, error)
	Count(ctx context.Context, filter *types.ConnectionFilter) (int, error)
	Update(ctx context.Context, connection *Connection) error
	Delete(ctx context.Context, connection *Connection) error
}
