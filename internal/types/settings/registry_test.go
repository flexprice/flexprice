package settings

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSettingRegistry(t *testing.T) {
	registry := NewSettingRegistry()
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.types)
	assert.Equal(t, 0, len(registry.types))
}

func TestRegistry_Register(t *testing.T) {
	registry := NewSettingRegistry()

	// Register a simple config
	testConfig := types.SubscriptionConfig{
		GracePeriodDays:         5,
		AutoCancellationEnabled: true,
	}

	Register(
		registry,
		types.SettingKeySubscriptionConfig,
		testConfig,
		func(c types.SubscriptionConfig) error { return nil },
		"Test description",
	)

	// Verify registration
	assert.True(t, registry.Has(types.SettingKeySubscriptionConfig))
}

func TestRegistry_GetType_Success(t *testing.T) {
	registry := NewSettingRegistry()

	// Register config
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	validator := func(c types.SubscriptionConfig) error {
		if c.GracePeriodDays < 1 {
			return assert.AnError
		}
		return nil
	}

	Register(
		registry,
		types.SettingKeySubscriptionConfig,
		defaultConfig,
		validator,
		"Test subscription config",
	)

	// Retrieve with correct type
	settingType, err := GetType[types.SubscriptionConfig](registry, types.SettingKeySubscriptionConfig)
	require.NoError(t, err)

	assert.Equal(t, types.SettingKeySubscriptionConfig, settingType.Key)
	assert.Equal(t, 3, settingType.DefaultValue.GracePeriodDays)
	assert.Equal(t, false, settingType.DefaultValue.AutoCancellationEnabled)
	assert.Equal(t, "Test subscription config", settingType.Description)
	assert.True(t, settingType.Required)
	assert.NotNil(t, settingType.Validator)
}

func TestRegistry_GetType_UnknownKey(t *testing.T) {
	registry := NewSettingRegistry()

	// Try to get unregistered key
	_, err := GetType[types.SubscriptionConfig](registry, "unknown_key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting key")
}

func TestRegistry_GetType_TypeMismatch(t *testing.T) {
	registry := NewSettingRegistry()

	// Register with SubscriptionConfig
	Register(
		registry,
		types.SettingKeySubscriptionConfig,
		types.SubscriptionConfig{GracePeriodDays: 3},
		nil,
		"Test",
	)

	// Try to retrieve with wrong type
	_, err := GetType[types.InvoicePDFConfig](registry, types.SettingKeySubscriptionConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}

func TestRegistry_Has(t *testing.T) {
	registry := NewSettingRegistry()

	// Not registered
	assert.False(t, registry.Has(types.SettingKeyInvoiceConfig))

	// Register
	Register(
		registry,
		types.SettingKeyInvoiceConfig,
		types.InvoiceConfig{},
		nil,
		"Test",
	)

	// Now registered
	assert.True(t, registry.Has(types.SettingKeyInvoiceConfig))
}

func TestRegistry_Keys(t *testing.T) {
	registry := NewSettingRegistry()

	// Empty registry
	keys := registry.Keys()
	assert.Empty(t, keys)

	// Register multiple settings
	Register(registry, types.SettingKeyInvoiceConfig, types.InvoiceConfig{}, nil, "")
	Register(registry, types.SettingKeySubscriptionConfig, types.SubscriptionConfig{}, nil, "")

	keys = registry.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, types.SettingKeyInvoiceConfig)
	assert.Contains(t, keys, types.SettingKeySubscriptionConfig)
}

func TestRegistry_NilValidator(t *testing.T) {
	registry := NewSettingRegistry()

	// Register without validator
	Register(
		registry,
		types.SettingKeyInvoiceConfig,
		types.InvoiceConfig{},
		nil, // nil validator
		"Test without validator",
	)

	settingType, err := GetType[types.InvoiceConfig](registry, types.SettingKeyInvoiceConfig)
	require.NoError(t, err)
	assert.Nil(t, settingType.Validator)
}

func TestRegistry_EmptyDescription(t *testing.T) {
	registry := NewSettingRegistry()

	// Register with empty description
	Register(
		registry,
		types.SettingKeyEnvConfig,
		types.EnvConfig{},
		nil,
		"", // empty description
	)

	settingType, err := GetType[types.EnvConfig](registry, types.SettingKeyEnvConfig)
	require.NoError(t, err)
	assert.Empty(t, settingType.Description)
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	registry := NewSettingRegistry()

	// Register first time
	Register(
		registry,
		types.SettingKeySubscriptionConfig,
		types.SubscriptionConfig{GracePeriodDays: 3},
		nil,
		"First registration",
	)

	// Register again with different values - should overwrite
	Register(
		registry,
		types.SettingKeySubscriptionConfig,
		types.SubscriptionConfig{GracePeriodDays: 7},
		nil,
		"Second registration",
	)

	settingType, err := GetType[types.SubscriptionConfig](registry, types.SettingKeySubscriptionConfig)
	require.NoError(t, err)
	assert.Equal(t, 7, settingType.DefaultValue.GracePeriodDays)
	assert.Equal(t, "Second registration", settingType.Description)
}
