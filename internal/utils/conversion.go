package utils

import (
	"bytes"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ToStruct converts a map[string]interface{} to a typed struct.
// Uses JSON marshal then decode with DisallowUnknownFields so that keys in the map
// that do not correspond to struct fields (e.g. typos) cause an error instead of being ignored.
func ToStruct[T any](value map[string]interface{}) (T, error) {
	var result T

	if value == nil {
		return result, nil
	}

	// Convert map to JSON bytes, then unmarshal directly to struct
	// This leverages Go's built-in JSON unmarshaling which handles:
	// - Nested structs and slices
	// - Custom types with UnmarshalJSON methods (like decimal.Decimal)
	// - Type conversions (string to decimal, etc.)
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to marshal map to JSON").
			Mark(ierr.ErrValidation)
	}

	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return result, ierr.WithError(err).
			WithHint("Failed to unmarshal JSON to struct").
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
