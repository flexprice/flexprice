package integration

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// IntegrationEntity represents a connection between a FlexPrice entity and an external system.
type IntegrationEntity struct {
	ID string `json:"id"`
	// ConnectionID is the ID of the connection in the flexprice system
	ConnectionID string `json:"connection_id"`
	// EntityType is the type of entity being connected (e.g., customer, payment)
	EntityType types.EntityType `json:"entity_type"`
	// EntityID is the ID of the FlexPrice entity
	EntityID string `json:"entity_id"`
	// ProviderType is the type of external provider (e.g., stripe, razorpay)
	ProviderType types.SecretProvider `json:"provider_type"`
	// ProviderID is the ID of the entity in the external system
	ProviderID string `json:"provider_id"`
	// SyncStatus is the status of the synchronization
	SyncStatus   types.SyncStatus `json:"sync_status"`
	LastSyncedAt *time.Time       `json:"last_synced_at"`
	LastErrorMsg *string          `json:"last_error_msg"`
	// SyncHistory is the history of the synchronization
	SyncHistory []SyncEvent `json:"sync_history"`
	// Metadata is the metadata of the integration
	Metadata types.Metadata `json:"metadata"`
	// EnvironmentID is the ID of the environment
	EnvironmentID string `json:"environment_id"`
	types.BaseModel
}

// SyncEvent represents a single synchronization event.
type SyncEvent struct {
	Action    types.SyncEventAction
	Status    types.SyncStatus
	Timestamp int64
	ErrorMsg  *string
}

// IntegrationEntityFilter defines filter options for listing entity connections.
type IntegrationEntityFilter struct {
	*types.QueryFilter
	EntityType   *types.EntityType
	EntityID     *string
	ProviderType *types.SecretProvider
	ProviderID   *string
	SyncStatus   *types.SyncStatus
}

// NewIntegrationEntityFilter creates a new IntegrationEntityFilter with default values.
func NewIntegrationEntityFilter() *IntegrationEntityFilter {
	return &IntegrationEntityFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	}
}

func (f *IntegrationEntityFilter) NewNoLimitIntegrationEntityFilter() *IntegrationEntityFilter {
	return &IntegrationEntityFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter.
func (f *IntegrationEntityFilter) Validate() error {
	if f.QueryFilter != nil {
		return f.QueryFilter.Validate()
	}
	return nil
}

// GetLimit implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetSort() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements the BaseFilter interface.
func (f *IntegrationEntityFilter) GetExpand() types.Expand {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements the BaseFilter interface.
func (f *IntegrationEntityFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// FromEnt converts an ent.IntegrationEntity to a domain IntegrationEntity.
func FromEnt(e *ent.IntegrationEntity) *IntegrationEntity {
	if e == nil {
		return nil
	}

	syncHistory := make([]SyncEvent, 0, len(e.SyncHistory))
	for _, event := range e.SyncHistory {
		syncEvent := SyncEvent{
			Action:    types.SyncEventAction(event.Action),
			Status:    event.Status,
			Timestamp: event.Timestamp,
			ErrorMsg:  event.ErrorMsg,
		}
		syncHistory = append(syncHistory, syncEvent)
	}

	return &IntegrationEntity{
		ID:            e.ID,
		ConnectionID:  e.ConnectionID,
		EntityType:    e.EntityType,
		EntityID:      e.EntityID,
		ProviderType:  e.ProviderType,
		ProviderID:    e.ProviderID,
		SyncStatus:    e.SyncStatus,
		LastSyncedAt:  lo.ToPtr(e.LastSyncedAt),
		LastErrorMsg:  lo.ToPtr(e.LastErrorMsg),
		SyncHistory:   syncHistory,
		Metadata:      e.Metadata,
		EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent.IntegrationEntity to a list of domain IntegrationEntity.
func FromEntList(list []*ent.IntegrationEntity) []*IntegrationEntity {
	return lo.Map(list, func(e *ent.IntegrationEntity, _ int) *IntegrationEntity {
		return FromEnt(e)
	})
}

// ToEntSyncHistory converts domain SyncEvent list to schema.SyncEvent list for storage.
func ToEntSyncHistory(events []SyncEvent) []schema.SyncEvent {
	result := make([]schema.SyncEvent, 0, len(events))
	for _, event := range events {
		schemaEvent := schema.SyncEvent{
			Action:    string(event.Action),
			Status:    event.Status,
			Timestamp: event.Timestamp,
			ErrorMsg:  event.ErrorMsg,
		}
		result = append(result, schemaEvent)
	}
	return result
}
