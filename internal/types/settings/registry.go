package settings

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/types"
)

// SettingType defines a type-safe setting configuration
type SettingType[T any] struct {
	Key          types.SettingKey
	DefaultValue T
	Validator    func(T) error
	Description  string
	Required     bool
}

// SettingRegistry manages type-safe setting definitions
// Note: This registry is populated once at service initialization and never modified after.
// No mutex needed since it's read-only after initialization.
type SettingRegistry struct {
	types map[types.SettingKey]interface{}
}

// NewSettingRegistry creates a new setting registry
func NewSettingRegistry() *SettingRegistry {
	return &SettingRegistry{
		types: make(map[types.SettingKey]interface{}),
	}
}

// Register registers a new setting type with compile-time type safety
// Note: This should only be called during service initialization
func Register[T any](
	r *SettingRegistry,
	key types.SettingKey,
	defaultValue T,
	validator func(T) error,
	description string,
) {
	r.types[key] = SettingType[T]{
		Key:          key,
		DefaultValue: defaultValue,
		Validator:    validator,
		Description:  description,
		Required:     true,
	}
}

// GetType returns the SettingType for a given key with compile-time type safety
func GetType[T any](r *SettingRegistry, key types.SettingKey) (SettingType[T], error) {
	typ, exists := r.types[key]
	if !exists {
		return SettingType[T]{}, fmt.Errorf("unknown setting key: %s", key)
	}

	settingType, ok := typ.(SettingType[T])
	if !ok {
		return SettingType[T]{}, fmt.Errorf("type mismatch for key %s: expected different type", key)
	}

	return settingType, nil
}

// Has checks if a setting key is registered
func (r *SettingRegistry) Has(key types.SettingKey) bool {
	_, exists := r.types[key]
	return exists
}

// Keys returns all registered setting keys
func (r *SettingRegistry) Keys() []types.SettingKey {
	keys := make([]types.SettingKey, 0, len(r.types))
	for key := range r.types {
		keys = append(keys, key)
	}
	return keys
}
