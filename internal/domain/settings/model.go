package settings

import (
	"encoding/json"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Setting represents a tenant and environment specific configuration setting
type Setting struct {
	// ID is the unique identifier for the setting
	ID string `json:"id"`

	// Key is the setting key
	Key types.SettingKey `json:"key"`

	// Value is the JSON value of the setting
	Value map[string]interface{} `json:"value"`

	// EnvironmentID is the environment identifier for the setting
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent setting to a domain setting
func FromEnt(s *ent.Settings) *Setting {
	if s == nil {
		return nil
	}

	// The value is now directly map[string]interface{} from Ent
	value := s.Value
	if value == nil {
		value = make(map[string]interface{})
	}

	return &Setting{
		ID:            s.ID,
		Key:           types.SettingKey(s.Key),
		Value:         value,
		EnvironmentID: s.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  s.TenantID,
			Status:    types.Status(s.Status),
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			CreatedBy: s.CreatedBy,
			UpdatedBy: s.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent settings to domain settings
func FromEntList(settings []*ent.Settings) []*Setting {
	if settings == nil {
		return nil
	}

	result := make([]*Setting, len(settings))
	for i, s := range settings {
		result[i] = FromEnt(s)
	}

	return result
}

// GetValue retrieves a value by key and unmarshals it into the target
func (s *Setting) GetValue(key string, target interface{}) error {
	if s.Value == nil {
		return ierr.NewErrorf("no value found for key '%s'", key).
			Mark(ierr.ErrNotFound)
	}

	value, exists := s.Value[key]
	if !exists {
		return ierr.NewErrorf("key '%s' not found in setting", key).
			Mark(ierr.ErrNotFound)
	}

	// Marshal and unmarshal to convert interface{} to target type
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("failed to marshal value for key '%s'", key).
			Mark(ierr.ErrValidation)
	}

	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("failed to unmarshal value for key '%s'", key).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// SetValue sets a value for a specific key
func (s *Setting) SetValue(key string, value interface{}) {
	if s.Value == nil {
		s.Value = make(map[string]interface{})
	}
	s.Value[key] = value
}

// Validate validates the setting
func (s *Setting) Validate() error {
	if s.Key == "" {
		return ierr.NewError("setting key is required").
			Mark(ierr.ErrValidation)
	}

	return nil
}
