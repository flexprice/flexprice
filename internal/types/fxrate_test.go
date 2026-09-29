package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFXRateScope_Validate(t *testing.T) {
	cases := []struct {
		name    string
		scope   FXRateScope
		wantErr bool
	}{
		{"tenant", FXRateScopeTenant, false},
		{"customer", FXRateScopeCustomer, false},
		{"subscription", FXRateScopeSubscription, false},
		{"empty", FXRateScope(""), true},
		{"garbage", FXRateScope("global"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate()
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewFXRateFilter_Defaults(t *testing.T) {
	f := NewFXRateFilter()
	assert.NotNil(t, f.QueryFilter)
	assert.NoError(t, f.Validate())
	assert.False(t, NewNoLimitFXRateFilter().IsUnlimited() == false, "no-limit filter must be unlimited")
}
