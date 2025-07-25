package integration

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// EntityIntegrationMappingRepository defines the interface for entity integration mapping data access
type EntityIntegrationMappingRepository interface {
	Create(ctx context.Context, mapping *EntityIntegrationMapping) error
	Get(ctx context.Context, id string) (*EntityIntegrationMapping, error)
	List(ctx context.Context, filter *EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
	Count(ctx context.Context, filter *EntityIntegrationMappingFilter) (int, error)
	Update(ctx context.Context, mapping *EntityIntegrationMapping) error
	Delete(ctx context.Context, mapping *EntityIntegrationMapping) error

	// Integration-specific methods
	GetByEntityAndProvider(ctx context.Context, entityID string, entityType EntityType, providerType ProviderType) (*EntityIntegrationMapping, error)
	GetByProviderEntityID(ctx context.Context, providerType ProviderType, providerEntityID string) (*EntityIntegrationMapping, error)
	ListByProvider(ctx context.Context, providerType ProviderType, filter *EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
	ListByEntityType(ctx context.Context, entityType EntityType, filter *EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
	BulkCreate(ctx context.Context, mappings []*EntityIntegrationMapping) error

	// Backward compatibility methods for customer mappings
	GetByCustomerAndProvider(ctx context.Context, customerID string, providerType ProviderType) (*EntityIntegrationMapping, error)
	GetByProviderCustomerID(ctx context.Context, providerType ProviderType, providerCustomerID string) (*EntityIntegrationMapping, error)
	ListCustomerMappings(ctx context.Context, filter *EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
}

// StripeSyncBatchRepository defines the interface for stripe sync batch data access
type StripeSyncBatchRepository interface {
	Create(ctx context.Context, batch *StripeSyncBatch) error
	Get(ctx context.Context, id string) (*StripeSyncBatch, error)
	List(ctx context.Context, filter *StripeSyncBatchFilter) ([]*StripeSyncBatch, error)
	Count(ctx context.Context, filter *StripeSyncBatchFilter) (int, error)
	Update(ctx context.Context, batch *StripeSyncBatch) error
	Delete(ctx context.Context, batch *StripeSyncBatch) error

	// Sync-specific methods
	GetByTimeWindow(ctx context.Context, entityID string, entityType EntityType, meterID string, windowStart, windowEnd time.Time) (*StripeSyncBatch, error)
	ListByStatus(ctx context.Context, status SyncStatus, filter *StripeSyncBatchFilter) ([]*StripeSyncBatch, error)
	ListFailedBatches(ctx context.Context, filter *StripeSyncBatchFilter) ([]*StripeSyncBatch, error)
	ListRetryableBatches(ctx context.Context, filter *StripeSyncBatchFilter) ([]*StripeSyncBatch, error)
	UpdateStatus(ctx context.Context, id string, status SyncStatus, errorMessage string) error
	CleanupOldBatches(ctx context.Context, olderThan time.Time) (int, error)
	BulkCreate(ctx context.Context, batches []*StripeSyncBatch) error
}

// StripeTenantConfigRepository defines the interface for stripe tenant configuration data access
type StripeTenantConfigRepository interface {
	Create(ctx context.Context, config *StripeTenantConfig) error
	Get(ctx context.Context, id string) (*StripeTenantConfig, error)
	List(ctx context.Context, filter *StripeTenantConfigFilter) ([]*StripeTenantConfig, error)
	Count(ctx context.Context, filter *StripeTenantConfigFilter) (int, error)
	Update(ctx context.Context, config *StripeTenantConfig) error
	Delete(ctx context.Context, config *StripeTenantConfig) error

	// Config-specific methods
	GetByTenantAndEnvironment(ctx context.Context, tenantID, environmentID string) (*StripeTenantConfig, error)
	ListActiveTenants(ctx context.Context, filter *StripeTenantConfigFilter) ([]*StripeTenantConfig, error)
}

// MeterProviderMappingRepository defines the interface for meter provider mapping data access
type MeterProviderMappingRepository interface {
	Create(ctx context.Context, mapping *MeterProviderMapping) error
	Get(ctx context.Context, id string) (*MeterProviderMapping, error)
	List(ctx context.Context, filter *MeterProviderMappingFilter) ([]*MeterProviderMapping, error)
	Count(ctx context.Context, filter *MeterProviderMappingFilter) (int, error)
	Update(ctx context.Context, mapping *MeterProviderMapping) error
	Delete(ctx context.Context, mapping *MeterProviderMapping) error

	// Mapping-specific methods
	GetByMeterAndProvider(ctx context.Context, meterID string, providerType ProviderType) (*MeterProviderMapping, error)
	GetByProviderMeterID(ctx context.Context, providerType ProviderType, providerMeterID string) (*MeterProviderMapping, error)
	ListByProvider(ctx context.Context, providerType ProviderType, filter *MeterProviderMappingFilter) ([]*MeterProviderMapping, error)
	ListEnabledMappings(ctx context.Context, filter *MeterProviderMappingFilter) ([]*MeterProviderMapping, error)
	BulkCreate(ctx context.Context, mappings []*MeterProviderMapping) error
}

// Filter types for repository queries

// EntityIntegrationMappingFilter defines filtering options for entity integration mapping queries
type EntityIntegrationMappingFilter struct {
	types.QueryFilter
	EntityIDs         []string       `json:"entity_ids,omitempty"`
	EntityTypes       []EntityType   `json:"entity_types,omitempty"`
	ProviderTypes     []ProviderType `json:"provider_types,omitempty"`
	ProviderEntityIDs []string       `json:"provider_entity_ids,omitempty"`
}

// StripeSyncBatchFilter defines filtering options for stripe sync batch queries
type StripeSyncBatchFilter struct {
	types.QueryFilter
	EntityIDs         []string     `json:"entity_ids,omitempty"`
	EntityTypes       []EntityType `json:"entity_types,omitempty"`
	MeterIDs          []string     `json:"meter_ids,omitempty"`
	EventTypes        []string     `json:"event_types,omitempty"`
	SyncStatuses      []SyncStatus `json:"sync_statuses,omitempty"`
	WindowStartAfter  *time.Time   `json:"window_start_after,omitempty"`
	WindowStartBefore *time.Time   `json:"window_start_before,omitempty"`
	WindowEndAfter    *time.Time   `json:"window_end_after,omitempty"`
	WindowEndBefore   *time.Time   `json:"window_end_before,omitempty"`
	SyncedAfter       *time.Time   `json:"synced_after,omitempty"`
	SyncedBefore      *time.Time   `json:"synced_before,omitempty"`
	RetryCountMax     *int         `json:"retry_count_max,omitempty"`
}

func NewStripeSyncBatchFilter() *StripeSyncBatchFilter {
	return &StripeSyncBatchFilter{
		QueryFilter: types.QueryFilter{},
	}
}

// StripeTenantConfigFilter defines filtering options for stripe tenant config queries
type StripeTenantConfigFilter struct {
	types.QueryFilter
	SyncEnabled *bool `json:"sync_enabled,omitempty"`
}

// MeterProviderMappingFilter defines filtering options for meter provider mapping queries
type MeterProviderMappingFilter struct {
	types.QueryFilter
	MeterIDs         []string       `json:"meter_ids,omitempty"`
	ProviderTypes    []ProviderType `json:"provider_types,omitempty"`
	ProviderMeterIDs []string       `json:"provider_meter_ids,omitempty"`
	SyncEnabled      *bool          `json:"sync_enabled,omitempty"`
}
