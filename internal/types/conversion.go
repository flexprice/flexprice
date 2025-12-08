package types

import (
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/go-viper/mapstructure/v2"
)

// ToStruct converts a map[string]interface{} to a typed struct
// Completely stateless - just give it a value and it returns the typed struct
func ToStruct[T any](value map[string]interface{}) (T, error) {
	var result T

	if value == nil {
		return result, nil
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &result,
		TagName:          "json",
		WeaklyTypedInput: true, // Allows type coercion (e.g., float64 to int)
	})
	if err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to create mapstructure decoder").
			Mark(ierr.ErrValidation)
	}

	if err := decoder.Decode(value); err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to decode map to struct").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// ToMap converts a typed struct to map[string]interface{}
// Completely stateless - just give it a struct and it returns the map
func ToMap[T any](value T) (map[string]interface{}, error) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal value to JSON").
			Mark(ierr.ErrValidation)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to unmarshal JSON to map").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}
