package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBillingConfig_OfferedPlanIDs(t *testing.T) {
	tests := []struct {
		name string
		cfg  BillingConfig
		want []string
	}{
		{"default_only", BillingConfig{PlanID: "plan_1"}, []string{"plan_1"}},
		{
			"default_first_then_extras_without_repeats_or_blanks",
			BillingConfig{PlanID: "plan_1", PlanIDs: []string{"plan_2", " ", "plan_1", " plan_3 ", "plan_2"}},
			[]string{"plan_1", "plan_2", "plan_3"},
		},
		{"no_default", BillingConfig{PlanIDs: []string{"plan_2"}}, []string{"plan_2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.OfferedPlanIDs())
		})
	}
}

// TestBillingPlanIDs_FromEnv proves a comma-separated FLEXPRICE_BILLING_PLAN_IDS becomes the list of offered plans.
func TestBillingPlanIDs_FromEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_BILLING_PLAN_ID", "plan_1")
	t.Setenv("FLEXPRICE_BILLING_PLAN_IDS", "plan_2,plan_3")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	assert.Equal(t, []string{"plan_1", "plan_2", "plan_3"}, cfg.Billing.OfferedPlanIDs())
}
