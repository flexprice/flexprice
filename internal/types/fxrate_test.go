package types

import (
	"testing"
	"time"

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

func TestFXRateSource_Validate(t *testing.T) {
	cases := []struct {
		name    string
		source  FXRateSource
		wantErr bool
	}{
		{"fixed", FXRateSourceFixed, false},
		{"market", FXRateSourceMarket, false},
		{"empty", FXRateSource(""), true},
		{"garbage", FXRateSource("live"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.source.Validate()
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

func TestFXRateWindowsOverlap(t *testing.T) {
	at := func(day int) *time.Time {
		v := time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)
		return &v
	}
	cases := []struct {
		name                   string
		aFrom, aTo, bFrom, bTo *time.Time
		want                   bool
	}{
		{"both open", nil, nil, nil, nil, true},
		{"open against closed", nil, nil, at(1), at(5), true},
		{"disjoint", at(1), at(5), at(6), at(9), false},
		{"touching end is exclusive", at(1), at(5), at(5), at(9), false},
		{"partial overlap", at(1), at(5), at(4), at(9), true},
		{"nested", at(1), at(9), at(3), at(4), true},
		{"open start before closed", nil, at(3), at(3), nil, false},
		{"open start overlaps", nil, at(4), at(3), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FXRateWindowsOverlap(tc.aFrom, tc.aTo, tc.bFrom, tc.bTo))
			assert.Equal(t, tc.want, FXRateWindowsOverlap(tc.bFrom, tc.bTo, tc.aFrom, tc.aTo), "symmetric")
		})
	}
}
