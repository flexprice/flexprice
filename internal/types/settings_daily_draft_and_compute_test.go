package types

import (
	"testing"

	"github.com/flexprice/flexprice/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestDraftInvoiceRecomputeConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults, err := GetDefaultSettings()
	require.NoError(t, err)

	def, ok := defaults[SettingKeyDraftInvoiceRecomputeConfig]
	require.True(t, ok, "default settings must include SettingKeyDraftInvoiceRecomputeConfig")
	require.Equal(t, SettingKeyDraftInvoiceRecomputeConfig, def.Key)

	cfg, err := utils.ToStruct[DraftInvoiceRecomputeConfig](def.DefaultValue)
	require.NoError(t, err)
	require.False(t, cfg.Enabled, "must default to disabled so existing tenants see zero behavior change")
}

func TestDraftInvoiceRecomputeConfig_Validate(t *testing.T) {
	t.Parallel()
	require.NoError(t, DraftInvoiceRecomputeConfig{Enabled: true}.Validate())
	require.NoError(t, DraftInvoiceRecomputeConfig{Enabled: false}.Validate())
}

func TestSettingKeyDraftInvoiceRecomputeConfig_Validate(t *testing.T) {
	t.Parallel()
	key := SettingKeyDraftInvoiceRecomputeConfig
	require.NoError(t, key.Validate())
}

func TestValidateSettingValue_DraftInvoiceRecomputeConfig(t *testing.T) {
	t.Parallel()
	err := ValidateSettingValue(SettingKeyDraftInvoiceRecomputeConfig, map[string]interface{}{
		"enabled": true,
	})
	require.NoError(t, err)
}
