package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestValidateMandateCeiling_RequestExceedsSettings_Rejected(t *testing.T) {
	t.Parallel()
	settingsCeiling := decimal.NewFromInt(15000)
	requestCeiling := decimal.NewFromInt(20000)

	err := validateMandateCeiling(&requestCeiling, settingsCeiling)
	require.Error(t, err)
}

func TestValidateMandateCeiling_RequestUnderSettings_Allowed(t *testing.T) {
	t.Parallel()
	settingsCeiling := decimal.NewFromInt(15000)
	requestCeiling := decimal.NewFromInt(10000)

	err := validateMandateCeiling(&requestCeiling, settingsCeiling)
	require.NoError(t, err)
}

func TestValidateMandateCeiling_NoRequestOverride_Allowed(t *testing.T) {
	t.Parallel()
	settingsCeiling := decimal.NewFromInt(15000)
	err := validateMandateCeiling(nil, settingsCeiling)
	require.NoError(t, err)
}

func TestResolveMandateCeiling_UsesRequestOverrideWhenPresent(t *testing.T) {
	t.Parallel()
	settingsCeiling := decimal.NewFromInt(15000)
	requestCeiling := decimal.NewFromInt(10000)
	require.True(t, resolveMandateCeiling(&requestCeiling, settingsCeiling).Equal(requestCeiling))
}

func TestResolveMandateCeiling_FallsBackToSettings(t *testing.T) {
	t.Parallel()
	settingsCeiling := decimal.NewFromInt(15000)
	require.True(t, resolveMandateCeiling(nil, settingsCeiling).Equal(settingsCeiling))
}
