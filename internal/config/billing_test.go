package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBillingConfig(t *testing.T) {
	full := BillingConfig{Enabled: true, Region: "us", APIHost: "https://api.flexprice.io/v1", APIKey: "sk", PlanID: "plan_1", DashboardURL: "https://us.app.flexprice.io"}
	off := func(mut func(*BillingConfig)) BillingConfig { c := full; mut(&c); return c }
	tests := []struct {
		name string
		cfg  BillingConfig
		want bool
	}{
		{"complete", full, true},
		{"disabled", off(func(c *BillingConfig) { c.Enabled = false }), false},
		{"no host", off(func(c *BillingConfig) { c.APIHost = "" }), false},
		{"no key", off(func(c *BillingConfig) { c.APIKey = "" }), false},
		{"no region", off(func(c *BillingConfig) { c.Region = "" }), false},
		{"no plan", off(func(c *BillingConfig) { c.PlanID = "" }), false},
		{"no dashboard", off(func(c *BillingConfig) { c.DashboardURL = "" }), false},
		{"legacy only", BillingConfig{TenantID: "t_billing", EnvironmentID: "env"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, tt.want, tt.cfg.IsSelfBillingEnabled()) })
	}
	assert.Equal(t, 6*time.Hour, BillingConfig{}.UsageLookback(), "default")
	assert.Equal(t, 12*time.Hour, BillingConfig{UsageLookbackHours: 12}.UsageLookback())
	assert.Equal(t, 6*time.Hour, BillingConfig{UsageLookbackHours: -1}.UsageLookback())
	assert.True(t, BillingConfig{TenantID: "t_b"}.IsBillingTenant("t_b"))
	assert.False(t, BillingConfig{}.IsBillingTenant(""))
	assert.False(t, BillingConfig{TenantID: "t_b"}.IsBillingTenant("t_x"))
}
