package entitlement

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for entitlement storage operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, entitlement *Entitlement) (*Entitlement, error)
	Get(ctx context.Context, id string) (*Entitlement, error)
	List(ctx context.Context, filter *types.EntitlementFilter) ([]*Entitlement, error)
	Count(ctx context.Context, filter *types.EntitlementFilter) (int, error)
	Update(ctx context.Context, entitlement *Entitlement) (*Entitlement, error)
	Delete(ctx context.Context, id string) error

	// Bulk operations
	CreateBulk(ctx context.Context, entitlements []*Entitlement) ([]*Entitlement, error)
	DeleteBulk(ctx context.Context, ids []string) error

	// Specific filter operations
	ListByPlanIDs(ctx context.Context, planIDs []string) ([]*Entitlement, error)
	ListByAddonIDs(ctx context.Context, addonIDs []string) ([]*Entitlement, error)
	ListByFeatureIDs(ctx context.Context, featureIDs []string) ([]*Entitlement, error)
}
