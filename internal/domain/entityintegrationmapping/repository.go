package entityintegrationmapping

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// ScopedClaim identifies an idempotency-claim mapping row (InvoiceCharge or
// TokenCycleCharge) along with its tenant/environment, so a reconciliation
// cron running outside any single tenant's scope can re-establish context per
// claim before resolving it.
type ScopedClaim struct {
	MappingID     string
	TenantID      string
	EnvironmentID string
	EntityID      string
	EntityType    string
	ProviderType  string
	Metadata      map[string]interface{}
	CreatedAt     time.Time
}

// Repository defines the interface for entity integration mapping data access
type Repository interface {
	Create(ctx context.Context, mapping *EntityIntegrationMapping) error
	Get(ctx context.Context, id string) (*EntityIntegrationMapping, error)
	List(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
	Count(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error)
	Update(ctx context.Context, mapping *EntityIntegrationMapping) error
	Delete(ctx context.Context, mapping *EntityIntegrationMapping) error

	// ListScopedClaimedByEntityTypesAndProvider returns claim rows (status
	// "claimed" per Metadata["status"]) across ALL tenants and environments for
	// the given entity types + provider type. Used by the reconciliation sweep
	// cron, which runs outside any single tenant's scope.
	ListScopedClaimedByEntityTypesAndProvider(ctx context.Context, entityTypes []types.IntegrationEntityType, providerType string) ([]ScopedClaim, error)
}
