package types

import "sync"

// EESettingDefinition describes a setting key contributed by the ee/ submodule.
//
// The community build ships a closed set of keys (see SettingKey.Validate).
// Enterprise features need their own, and adding them to that enum would
// advertise enterprise functionality in the public repository. Registration
// keeps the key out of the community build entirely.
type EESettingDefinition struct {
	Key SettingKey
	// DefaultValue is returned when a tenant has not stored the setting.
	// GetSetting errors on a missing default, so this must be populated.
	DefaultValue map[string]interface{}
	Description  string
	// TenantLevel stores the setting once per tenant rather than per
	// environment. Tenant-level settings are readable without an environment in
	// context, which pre-login flows such as SSO require.
	TenantLevel bool
	// Validate checks a stored value. Nil means any value is accepted.
	Validate func(value map[string]interface{}) error
}

var (
	eeSettingsMu  sync.RWMutex
	eeSettingDefs = map[SettingKey]EESettingDefinition{}
)

// RegisterEESetting is called from ee-tagged init() functions, before any
// request is served.
//
// Registering a key the community build already owns panics: the two
// definitions would disagree about defaults and scope, and the winner would
// depend on init order.
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

// LookupEESetting resolves an enterprise-contributed key. Always false in a
// community build, so every caller falls through to its existing behaviour.
func LookupEESetting(key SettingKey) (EESettingDefinition, bool) {
	eeSettingsMu.RLock()
	defer eeSettingsMu.RUnlock()

	def, ok := eeSettingDefs[key]
	return def, ok
}

// EESettingCount reports how many enterprise settings are registered, so a
// build-level test can assert the ee import chain reached init().
func EESettingCount() int {
	eeSettingsMu.RLock()
	defer eeSettingsMu.RUnlock()

	return len(eeSettingDefs)
}
