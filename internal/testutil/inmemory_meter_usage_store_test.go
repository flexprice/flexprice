package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

func TestGetEarliestUsageTimestamp_ScopedByTenantAndEnvironment(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryMeterUsageStore()

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seed := func(id, tenant, env string, at time.Time) *events.MeterUsage {
		return &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           tenant,
				EnvironmentID:      env,
				ExternalCustomerID: "cust_ext",
				EventName:          "api_call",
				Timestamp:          at,
			},
			MeterID:  "meter_1",
			QtyTotal: decimal.NewFromInt(1),
		}
	}
	if err := store.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		seed("ev_other_tenant", "tenant_b", "env_1", base),                   // earlier, wrong tenant
		seed("ev_other_env", "tenant_a", "env_2", base.Add(1*time.Hour)),    // earlier, wrong env
		seed("ev_in_scope", "tenant_a", "env_1", base.Add(2*time.Hour)),     // the one that must win
		seed("ev_in_scope_later", "tenant_a", "env_1", base.Add(3*time.Hour)),
	}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	got, err := store.GetEarliestUsageTimestamp(ctx, &events.MeterUsageQueryParams{
		TenantID:            "tenant_a",
		EnvironmentID:       "env_1",
		ExternalCustomerIDs: []string{"cust_ext"},
		MeterID:             "meter_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("earliest must come from the requested tenant+env only; got %v", got)
	}

	// Missing scope is malformed input, never a match-everything wildcard.
	for name, params := range map[string]*events.MeterUsageQueryParams{
		"missing tenant": {EnvironmentID: "env_1", MeterID: "meter_1"},
		"missing env":    {TenantID: "tenant_a", MeterID: "meter_1"},
		"nil params":     nil,
	} {
		if _, err := store.GetEarliestUsageTimestamp(ctx, params); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}
