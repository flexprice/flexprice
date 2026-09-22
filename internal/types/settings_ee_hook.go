package types

import "sync"

// EESettingDefinition describes a setting key contributed by the ee/ directory.
// The community build ships a closed key set; enterprise features register
// their own here rather than adding to the public enum.
type EESettingDefinition struct {
	Key          SettingKey
	DefaultValue map[string]interface{}
	Description  string
	// TenantLevel stores once per tenant rather than per environment.
	TenantLevel bool
	// Validate checks a stored value. Nil means any value is accepted.
	Validate func(value map[string]interface{}) error
}

var (
	eeSettingsMu  sync.RWMutex
	eeSettingDefs = map[SettingKey]EESettingDefinition{}
)

// RegisterEESetting is called from ee-tagged init() functions.
func RegisterEESetting(def EESettingDefinition) {
	if def.Key == "" {
		panic("ee setting must have a non-empty key")
	}
	if isCoreSettingKey(def.Key) {
		panic("ee setting may not override the built-in key: " + string(def.Key))
	}
	eeSettingsMu.Lock()
	defer eeSettingsMu.Unlock()
	if _, exists := eeSettingDefs[def.Key]; exists {
		panic("ee setting registered twice: " + string(def.Key))
	}
	eeSettingDefs[def.Key] = def
}

// LookupEESetting resolves an enterprise key. Always false in a community build.
func LookupEESetting(key SettingKey) (EESettingDefinition, bool) {
	eeSettingsMu.RLock()
	defer eeSettingsMu.RUnlock()
	def, ok := eeSettingDefs[key]
	return def, ok
}

// EESettingCount reports how many EE settings are registered.
func EESettingCount() int {
	eeSettingsMu.RLock()
	defer eeSettingsMu.RUnlock()
	return len(eeSettingDefs)
}

func isCoreSettingKey(key SettingKey) bool {
	for _, k := range coreSettingKeys {
		if k == key {
			return true
		}
	}
	return false
}
