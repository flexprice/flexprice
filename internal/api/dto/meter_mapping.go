package dto

import "time"

// CreateMeterMappingRequest represents payload to create a meter mapping
// swagger:model
// ---
// required:
//   - meter_id
//   - provider_type
//   - provider_meter_id
type CreateMeterMappingRequest struct {
	MeterID         string                 `json:"meter_id" binding:"required"`
	ProviderType    string                 `json:"provider_type" binding:"required"` // enum in service layer
	ProviderMeterID string                 `json:"provider_meter_id" binding:"required"`
	SyncEnabled     bool                   `json:"sync_enabled"`
	Configuration   map[string]interface{} `json:"configuration,omitempty"`
}

// MeterMappingResponse is returned after create/get mapping
// swagger:model
// ---
type MeterMappingResponse struct {
	ID              string                 `json:"id"`
	MeterID         string                 `json:"meter_id"`
	ProviderType    string                 `json:"provider_type"`
	ProviderMeterID string                 `json:"provider_meter_id"`
	SyncEnabled     bool                   `json:"sync_enabled"`
	Configuration   map[string]interface{} `json:"configuration"`
	TenantID        string                 `json:"tenant_id"`
	EnvironmentID   string                 `json:"environment_id"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}
