package selfbilling

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var usageNow = time.Date(2026, 10, 1, 12, 34, 0, 0, time.UTC)

func allIngested(f *fakeClient) []usageEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return lo.Flatten(f.ingested)
}

func TestPushUsage_Window(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		lookback int
		hour     time.Time
		want     bool
	}{
		{"open_hour_not_sent", usageNow, 0, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), false},
		{"last_closed_hour_sent", usageNow, 0, time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC), true},
		{"oldest_hour_in_default_lookback_sent", usageNow, 0, time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC), true},
		{"hour_before_default_lookback_not_sent", usageNow, 0, time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC), false},
		{"configured_lookback_widens_window", usageNow, 12, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), true},
		{"just_closed_hour_waits_to_settle", time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC), 0, time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC), false},
		{"settled_hour_sent", time.Date(2026, 10, 1, 12, 15, 0, 0, time.UTC), 0, time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			s.Config.Billing.UsageLookbackHours = tt.lookback
			d.usage.AddEvents(&domainsb.TenantHourEvents{
				TenantID:      "t1",
				EnvironmentID: "env_1",
				Hour:          tt.hour,
				Count:         1,
			})

			require.NoError(t, s.pushUsage(context.Background(), tt.now))
			assert.Equal(t, tt.want, len(allIngested(fc)) == 1)
		})
	}
}

func TestPushUsage_Events(t *testing.T) {
	hour := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	fc := newFakeClient()
	s, d := newTestService(fc)
	s.Config.Billing.TenantID = "t_billing"

	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          hour,
		Count:         42,
	})
	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t_billing",
		EnvironmentID: "env_b",
		Hour:          hour,
		Count:         7,
	})
	d.usage.AddInvoices(&domainsb.TenantHourInvoices{
		TenantID:                   "t1",
		EnvironmentID:              "env_1",
		Currency:                   "usd",
		Hour:                       hour,
		InvoiceCount:               2,
		Subtotal:                   decimal.RequireFromString("120"),
		TotalDiscount:              decimal.RequireFromString("10"),
		TotalPrepaidCreditsApplied: decimal.RequireFromString("5"),
		TotalTax:                   decimal.RequireFromString("9.5"),
		Total:                      decimal.RequireFromString("114.5"),
		AmountDue:                  decimal.RequireFromString("114.5"),
		AmountPaid:                 decimal.RequireFromString("100"),
	})
	d.usage.AddInvoices(&domainsb.TenantHourInvoices{
		TenantID:      "t_billing",
		EnvironmentID: "env_b",
		Currency:      "usd",
		Hour:          hour,
		InvoiceCount:  1,
		Total:         decimal.RequireFromString("1"),
	})

	require.NoError(t, s.pushUsage(context.Background(), usageNow))

	want := []usageEvent{
		{
			id:        "evt:us:t1:env_1:2026100109",
			name:      eventNameEventsIngested,
			extID:     "us:t1",
			timestamp: hour,
			properties: map[string]string{
				"count":          "42",
				"environment_id": "env_1",
				"region":         "us",
			},
		},
		{
			id:        "inv:us:t1:env_1:USD:2026100109",
			name:      eventNameInvoiceValue,
			extID:     "us:t1",
			timestamp: hour,
			properties: map[string]string{
				"amount":                        "114.5",
				"total":                         "114.5",
				"subtotal":                      "120",
				"total_discount":                "10",
				"total_prepaid_credits_applied": "5",
				"total_tax":                     "9.5",
				"amount_due":                    "114.5",
				"amount_paid":                   "100",
				"invoice_count":                 "2",
				"currency":                      "USD",
				"environment_id":                "env_1",
				"region":                        "us",
			},
		},
	}
	assert.Equal(t, want, allIngested(fc), "the billing tenant never bills itself")
}

func TestPushUsage_Chunking(t *testing.T) {
	tests := []struct {
		name       string
		events     int
		wantChunks []int
	}{
		{"nothing_to_send", 0, []int{}},
		{"exactly_one_chunk", maxBulkEvents, []int{maxBulkEvents}},
		{"spills_into_second_chunk", maxBulkEvents + 1, []int{maxBulkEvents, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			for i := 0; i < tt.events; i++ {
				d.usage.AddEvents(&domainsb.TenantHourEvents{
					TenantID:      fmt.Sprintf("t%d", i),
					EnvironmentID: "env_1",
					Hour:          time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC),
					Count:         1,
				})
			}

			require.NoError(t, s.pushUsage(context.Background(), usageNow))

			fc.mu.Lock()
			defer fc.mu.Unlock()
			got := lo.Map(fc.ingested, func(chunk []usageEvent, _ int) int { return len(chunk) })
			assert.Equal(t, tt.wantChunks, got)
		})
	}
}

func TestPushUsage_ClientErrorReturned(t *testing.T) {
	fc := newFakeClient()
	fc.ingestErr = errors.New("flexprice cloud down")
	s, d := newTestService(fc)
	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC),
		Count:         1,
	})

	err := s.pushUsage(context.Background(), usageNow)
	assert.ErrorIs(t, err, fc.ingestErr)
}
