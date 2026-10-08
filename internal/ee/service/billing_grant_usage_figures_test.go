package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/shopspring/decimal"
)

func allowance(usage, quota int64, active, unlimited bool) *dto.GrantAllowanceState {
	return &dto.GrantAllowanceState{
		Usage:     decimal.NewFromInt(usage),
		Quota:     decimal.NewFromInt(quota),
		IsActive:  active,
		Unlimited: unlimited,
	}
}

// The usage summary has one figure for the ceiling and one for what is spent, and both
// have to describe the same span: the quota is per allowance, not per billing period.
func TestGrantUsageFigures(t *testing.T) {
	cases := []struct {
		name      string
		state     *dto.GrantState
		wantOK    bool
		usage     string
		quota     string
		unlimited bool
	}{
		{
			name:   "no grant state leaves the legacy figures alone",
			state:  nil,
			wantOK: false,
		},
		{
			name:   "an empty ledger has nothing to report",
			state:  &dto.GrantState{},
			wantOK: false,
		},
		{
			name: "an open allowance is reported on its own",
			state: &dto.GrantState{Allowances: []*dto.GrantAllowanceState{
				allowance(900, 1000, false, false),
				allowance(300, 1000, true, false),
			}},
			wantOK: true, usage: "300", quota: "1000",
		},
		{
			name: "between allowances the one that just closed is reported",
			state: &dto.GrantState{Allowances: []*dto.GrantAllowanceState{
				allowance(900, 1000, false, false),
				allowance(250, 1000, false, false),
			}},
			wantOK: true, usage: "250", quota: "1000",
		},
		{
			name: "quotas sum across open allowances, usage does not",
			state: &dto.GrantState{Allowances: []*dto.GrantAllowanceState{
				allowance(300, 1000, true, false),
				allowance(300, 500, true, false),
			}},
			// Both meter the same event stream: 300 events are 300 events.
			wantOK: true, usage: "300", quota: "1500",
		},
		{
			name: "one unlimited contributor makes the feature unlimited",
			state: &dto.GrantState{Allowances: []*dto.GrantAllowanceState{
				allowance(300, 0, true, true),
				allowance(300, 100, true, false),
			}},
			wantOK: true, usage: "300", quota: "100", unlimited: true,
		},
		{
			name: "a spent allowance reports zero, not the entitlement's ceiling",
			state: &dto.GrantState{Allowances: []*dto.GrantAllowanceState{
				allowance(0, 0, true, false),
			}},
			wantOK: true, usage: "0", quota: "0",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := grantUsageFigures(c.state)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if got.usage.String() != c.usage {
				t.Errorf("usage = %s, want %s", got.usage, c.usage)
			}
			if got.quota.String() != c.quota {
				t.Errorf("quota = %s, want %s", got.quota, c.quota)
			}
			if got.unlimited != c.unlimited {
				t.Errorf("unlimited = %v, want %v", got.unlimited, c.unlimited)
			}
		})
	}
}

// A non-positive ceiling is fully consumed, which is what a spent allowance means.
func TestGetUsagePercent_SpentAllowance(t *testing.T) {
	s := &billingService{}
	zero := int64(0)

	if got := s.getUsagePercent(decimal.Zero, &zero); got.String() != "100" {
		t.Errorf("zero limit = %s, want 100", got)
	}
	if got := s.getUsagePercent(decimal.NewFromInt(300), nil); got.String() != "0" {
		t.Errorf("unlimited = %s, want 0", got)
	}
	limit := int64(1000)
	if got := s.getUsagePercent(decimal.NewFromInt(250), &limit); got.String() != "0.25" {
		t.Errorf("bounded = %s, want 0.25", got)
	}
}
