package integration

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// StripeTenantConfig represents Stripe configuration for a tenant
type StripeTenantConfig struct {
	// ID is the unique identifier for the config
	ID string `db:"id" json:"id"`

	// APIKeyEncrypted stores the encrypted Stripe API key
	APIKeyEncrypted string `db:"api_key_encrypted" json:"-"` // Never include in JSON

	// SyncEnabled indicates if sync is enabled for this tenant
	SyncEnabled bool `db:"sync_enabled" json:"sync_enabled"`

	// AggregationWindowMinutes defines the aggregation window in minutes
	AggregationWindowMinutes int `db:"aggregation_window_minutes" json:"aggregation_window_minutes"`

	// WebhookConfig stores webhook configuration
	WebhookConfig map[string]interface{} `db:"webhook_config" json:"webhook_config"`

	// EnvironmentID is the environment identifier for the config
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// MeterProviderMapping represents a mapping between a FlexPrice meter and an external provider meter
type MeterProviderMapping struct {
	// ID is the unique identifier for the mapping
	ID string `db:"id" json:"id"`

	// MeterID is the FlexPrice meter identifier
	MeterID string `db:"meter_id" json:"meter_id"`

	// ProviderType is the external provider type (e.g., "stripe")
	ProviderType ProviderType `db:"provider_type" json:"provider_type"`

	// ProviderMeterID is the external provider's meter identifier
	ProviderMeterID string `db:"provider_meter_id" json:"provider_meter_id"`

	// SyncEnabled indicates if sync is enabled for this mapping
	SyncEnabled bool `db:"sync_enabled" json:"sync_enabled"`

	// Configuration stores provider-specific configuration
	Configuration map[string]interface{} `db:"configuration" json:"configuration"`

	// EnvironmentID is the environment identifier for the mapping
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// TODO: FromEnt and FromEntList will be implemented after Ent code generation

// ValidateStripeTenantConfig validates the Stripe tenant configuration
func (s *StripeTenantConfig) Validate() error {
	if s.APIKeyEncrypted == "" {
		return ierr.NewError("api_key_encrypted is required").
			WithHint("Encrypted API key must not be empty").
			Mark(ierr.ErrValidation)
	}

	if s.AggregationWindowMinutes <= 0 {
		return ierr.NewError("aggregation_window_minutes must be positive").
			WithHint("Aggregation window must be greater than 0").
			Mark(ierr.ErrValidation)
	}

	if s.AggregationWindowMinutes > 1440 { // 24 hours
		return ierr.NewError("aggregation_window_minutes too large").
			WithHint("Aggregation window must be less than 1440 minutes (24 hours)").
			Mark(ierr.ErrValidation)
	}

	// Validate commonly used aggregation windows
	validWindows := []int{1, 5, 15, 30, 60, 120, 240, 360, 720, 1440}
	isValidWindow := false
	for _, window := range validWindows {
		if s.AggregationWindowMinutes == window {
			isValidWindow = true
			break
		}
	}
	if !isValidWindow {
		return ierr.NewError("invalid aggregation_window_minutes").
			WithHint("Aggregation window must be one of: 1, 5, 15, 30, 60, 120, 240, 360, 720, 1440 minutes").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ValidateProviderMeterID validates the provider meter ID format
func ValidateProviderMeterID(providerMeterID string, providerType ProviderType) bool {
	if providerMeterID == "" {
		return false
	}

	switch providerType {
	case ProviderTypeStripe:
		// Stripe meter IDs have specific format requirements
		if len(providerMeterID) < 8 {
			return false
		}
		// Additional Stripe-specific validation can be added here
		return true
	default:
		return len(providerMeterID) > 0 && len(providerMeterID) <= 255
	}
}

// Validate validates all fields of the meter provider mapping
func (m *MeterProviderMapping) Validate() error {
	if m.MeterID == "" {
		return ierr.NewError("meter_id is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if !ValidateProviderType(m.ProviderType) {
		return ierr.NewError("invalid provider_type").
			WithHint("Provider type must be one of: stripe").
			Mark(ierr.ErrValidation)
	}

	if !ValidateProviderMeterID(m.ProviderMeterID, m.ProviderType) {
		return ierr.NewError("invalid provider_meter_id").
			WithHint("Provider meter ID format is invalid for the specified provider type").
			Mark(ierr.ErrValidation)
	}

	// Field length validations
	if len(m.MeterID) > 50 {
		return ierr.NewError("meter_id too long").
			WithHint("Meter ID must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(string(m.ProviderType)) > 50 {
		return ierr.NewError("provider_type too long").
			WithHint("Provider type must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(m.ProviderMeterID) > 255 {
		return ierr.NewError("provider_meter_id too long").
			WithHint("Provider meter ID must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// IsAPIKeySet returns true if an API key is configured
func (s *StripeTenantConfig) IsAPIKeySet() bool {
	return s.APIKeyEncrypted != ""
}

// IsEnabled returns true if sync is enabled and API key is set
func (s *StripeTenantConfig) IsEnabled() bool {
	return s.SyncEnabled && s.IsAPIKeySet()
}

// IsEnabled returns true if sync is enabled for this mapping
func (m *MeterProviderMapping) IsEnabled() bool {
	return m.SyncEnabled
}
