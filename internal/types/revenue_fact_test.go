package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRevenueSource_Validate(t *testing.T) {
	for _, s := range []RevenueSource{RevenueSourceUsage, RevenueSourceFixed, RevenueSourceCommitmentTrueup} {
		assert.NoError(t, s.Validate())
	}
	assert.Error(t, RevenueSource("credit_breakage").Validate()) // out of slice-1 scope
	assert.Error(t, RevenueSource("").Validate())
}

func TestDecompositionMode_Validate(t *testing.T) {
	for _, m := range []DecompositionMode{Marginal, PeriodOnly} {
		assert.NoError(t, m.Validate())
	}
	assert.Error(t, DecompositionMode("invalid").Validate())
	assert.Error(t, DecompositionMode("").Validate())
}

func TestFactStatus_Validate(t *testing.T) {
	for _, s := range []FactStatus{FactProvisional, FactFinal} {
		assert.NoError(t, s.Validate())
	}
	assert.Error(t, FactStatus("invalid").Validate())
	assert.Error(t, FactStatus("").Validate())
}
