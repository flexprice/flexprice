package dto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Rates are decimals on the wire contract (documented as strings); both JSON forms must decode exactly.
func TestFXRateInputs_DecodeAsDecimal(t *testing.T) {
	for _, raw := range []string{`"83.33335"`, `83.33335`} {
		var create CreateFXRateRequest
		require.NoError(t, json.Unmarshal([]byte(`{"rate":`+raw+`}`), &create), raw)
		require.NotNil(t, create.Rate)
		require.True(t, decimal.RequireFromString("83.33335").Equal(*create.Rate), raw)

		var update UpdateFXRateRequest
		require.NoError(t, json.Unmarshal([]byte(`{"rate":`+raw+`}`), &update), raw)
		require.True(t, decimal.RequireFromString("83.33335").Equal(*update.Rate), raw)

		var inline InlineFXRate
		require.NoError(t, json.Unmarshal([]byte(`{"rate":`+raw+`}`), &inline), raw)
		require.True(t, decimal.RequireFromString("83.33335").Equal(inline.Rate), raw)
	}

	var create CreateFXRateRequest
	require.Error(t, json.Unmarshal([]byte(`{"rate":"abc"}`), &create), "a non-numeric rate is rejected at decode")
	var inline InlineFXRate
	require.Error(t, json.Unmarshal([]byte(`{"rate":"abc"}`), &inline))
}

func TestCreateFXRateRequest_Validate_RateBySource(t *testing.T) {
	rate := decimal.RequireFromString("83")
	cases := []struct {
		name    string
		source  types.FXRateSource
		rate    *decimal.Decimal
		wantErr bool
	}{
		{name: "fixed with rate", source: types.FXRateSourceFixed, rate: &rate},
		{name: "fixed without rate", source: types.FXRateSourceFixed, wantErr: true},
		{name: "default source is fixed, so rate required", wantErr: true},
		{name: "market without rate", source: types.FXRateSourceMarket},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "inr", Source: tc.source, Rate: tc.rate}
			err := req.Validate()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateSubscriptionRequest_Validate_InlineFXRate(t *testing.T) {
	cases := []struct {
		name    string
		fxRate  *InlineFXRate
		wantErr bool
	}{
		{name: "no fx_rate", fxRate: nil},
		{name: "positive rate", fxRate: &InlineFXRate{Rate: decimal.RequireFromString("90")}},
		{name: "zero rate (or rate omitted)", fxRate: &InlineFXRate{}, wantErr: true},
		{name: "negative rate", fxRate: &InlineFXRate{Rate: decimal.RequireFromString("-5")}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseCreateSubscriptionRequest()
			req.FxRate = tc.fxRate
			err := req.Validate()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
