package settings

import (
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ConvertToType converts map[string]interface{} to typed struct with defaults merged
func ConvertToType[T any](
	value map[string]interface{},
	defaultValue T,
) (T, error) {
	var result T

	// If value is nil, return defaults
	if value == nil {
		return defaultValue, nil
	}

	// Merge with defaults (value takes precedence)
	merged := mergeWithDefaults(value, defaultValue)

	// JSON marshal/unmarshal for type conversion
	// This handles type coercion (float64 to int, etc.)
	jsonBytes, err := json.Marshal(merged)
	if err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to marshal setting value to JSON").
			Mark(ierr.ErrValidation)
	}

	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to unmarshal setting value to target type").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// ConvertFromType converts typed struct to map[string]interface{}
func ConvertFromType[T any](value T) (map[string]interface{}, error) {
	// JSON marshal/unmarshal for conversion
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal value to JSON").
			Mark(ierr.ErrValidation)
	}

	var result map[string]interface{}
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to unmarshal JSON to map").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// mergeWithDefaults merges value map with defaults from typed struct
// Value map takes precedence over defaults
func mergeWithDefaults[T any](
	value map[string]interface{},
	defaults T,
) map[string]interface{} {
	// Convert defaults to map
	defaultMap, err := ConvertFromType(defaults)
	if err != nil {
		// If conversion fails, just use the value as-is
		return value
	}

	merged := make(map[string]interface{})

	// Copy defaults first
	for k, v := range defaultMap {
		merged[k] = v
	}

	// Override with actual values
	for k, v := range value {
		merged[k] = v
	}

	return merged
}
