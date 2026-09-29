package fxrate

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestFXRateBuilder_PreservesFieldsAndOverridesRate(t *testing.T) {
	orig := &FXRate{
		ID:           "fxr_1",
		Scope:        types.FXRateScopeTenant,
		ScopeID:      types.FXRateScopeIDTenant,
		FromCurrency: "usd",
		ToCurrency:   "inr",
		Rate:         decimal.RequireFromString("83.00"),
	}

	got := NewFXRateBuilder(orig).WithRate(decimal.RequireFromString("85.00")).Build()

	assert.Equal(t, "fxr_1", got.ID)
	assert.Equal(t, "usd", got.FromCurrency)
	assert.True(t, decimal.RequireFromString("85.00").Equal(got.Rate), "builder overrides rate")
	assert.True(t, decimal.RequireFromString("83.00").Equal(orig.Rate), "original is not mutated")
}

func TestFromEnt_MapsFields(t *testing.T) {
	assert.Nil(t, FromEnt(nil))

	vf := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	e := &ent.FXRate{
		ID:            "fxr_2",
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		Scope:         "customer",
		ScopeID:       "cust_a",
		FromCurrency:  "usd",
		ToCurrency:    "inr",
		Rate:          decimal.RequireFromString("84.50"),
		ValidFrom:     &vf,
		Status:        "published",
		Metadata:      map[string]string{"contract": "ACME-2026"},
	}

	got := FromEnt(e)
	assert.Equal(t, "fxr_2", got.ID)
	assert.Equal(t, types.FXRateScopeCustomer, got.Scope)
	assert.Equal(t, "cust_a", got.ScopeID)
	assert.True(t, decimal.RequireFromString("84.50").Equal(got.Rate))
	assert.Equal(t, "tenant_1", got.TenantID)
	assert.Equal(t, &vf, got.ValidFrom)
	assert.Equal(t, "ACME-2026", got.Metadata["contract"])
}
