package settings

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for settings persistence operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, setting *Setting) error
	Get(ctx context.Context, id string) (*Setting, error)
	Update(ctx context.Context, setting *Setting) error
	Delete(ctx context.Context, id string) error

	// Operations by key
	GetByKey(ctx context.Context, key types.SettingKey) (*Setting, error)
	// GetByKeyUncached reads one environment's setting from the database, never the cache; inside a transaction it reads that transaction.
	GetByKeyUncached(ctx context.Context, environmentID string, key types.SettingKey) (*Setting, error)
	GetTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) (*Setting, error)
	DeleteByKey(ctx context.Context, key types.SettingKey) error
	DeleteTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) error

	// Config operations
	ListAllTenantEnvSettingsByKey(ctx context.Context, key types.SettingKey) ([]*types.TenantEnvConfig, error)
}
