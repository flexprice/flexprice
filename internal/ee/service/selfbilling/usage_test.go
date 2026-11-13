package selfbilling

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
	d.usage.AddEnvironment("env_1", types.EnvironmentProduction)

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
				"count":            "42",
				"environment_id":   "env_1",
				"environment_type": "production",
				"region":           "us",
			},
		},
		{
			id:        "inv:us:t1:env_1:USD:2026100109",
			name:      eventNameInvoiceValue,
			extID:     "us:t1",
			timestamp: hour,
			properties: map[string]string{
				"amount":                        "114.5",
				"usd_amount":                    "114.5",
				"usd_rate":                      "1",
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
				"environment_type":              "production",
				"region":                        "us",
			},
		},
	}
	assert.Equal(t, want, allIngested(fc), "the billing tenant never bills itself")
	assert.Zero(t, fc.usdRateCalls, "a USD-only push never reads FX rates")
}

func TestPushUsage_USDConversion(t *testing.T) {
	hour := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		currency   string
		total      string
		rates      map[string]decimal.Decimal
		wantAmount string
		wantRate   string
	}{
		{"inr_converted", "inr", "100000", map[string]decimal.Decimal{"inr": decimal.RequireFromString("0.012")}, "1200", "0.012"},
		{"rounded_to_cents", "inr", "1234.56", map[string]decimal.Decimal{"inr": decimal.RequireFromString("0.0119")}, "14.69", "0.0119"},
		{"currency_case_ignored", "EUR", "100", map[string]decimal.Decimal{"eur": decimal.RequireFromString("1.08")}, "108", "1.08"},
		{"usd_total_kept_exact", "usd", "1.005", nil, "1.005", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.usdRates = tt.rates
			s, d := newTestService(fc)
			d.usage.AddInvoices(&domainsb.TenantHourInvoices{
				TenantID:      "t1",
				EnvironmentID: "env_1",
				Currency:      tt.currency,
				Hour:          hour,
				InvoiceCount:  1,
				Total:         decimal.RequireFromString(tt.total),
			})

			require.NoError(t, s.pushUsage(context.Background(), usageNow))

			events := allIngested(fc)
			require.Len(t, events, 1)
			currency := strings.ToUpper(tt.currency)
			assert.Equal(t, "inv:us:t1:env_1:"+currency+":2026100109", events[0].id, "the ID keeps the invoice currency")
			assert.Equal(t, tt.wantAmount, events[0].properties["amount"])
			assert.Equal(t, tt.wantAmount, events[0].properties["usd_amount"])
			assert.Equal(t, tt.wantRate, events[0].properties["usd_rate"])
			assert.Equal(t, tt.total, events[0].properties["total"], "total stays in the invoice currency")
			assert.Equal(t, currency, events[0].properties["currency"])
		})
	}
}

func TestPushUsage_MissingUSDRateSendsZero(t *testing.T) {
	hour := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	core, logs := observer.New(zap.ErrorLevel)
	fc := newFakeClient()
	fc.usdRates = map[string]decimal.Decimal{"inr": decimal.RequireFromString("0.012")}
	s, d := newTestService(fc)
	s.Logger = logger.NewFromSugared(zap.New(core).Sugar())

	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          hour,
		Count:         1,
	})
	for _, inv := range []struct{ env, currency string }{{"env_1", "inr"}, {"env_1", "eur"}, {"env_2", "eur"}} {
		d.usage.AddInvoices(&domainsb.TenantHourInvoices{
			TenantID:      "t1",
			EnvironmentID: inv.env,
			Currency:      inv.currency,
			Hour:          hour,
			InvoiceCount:  1,
			Total:         decimal.RequireFromString("10"),
		})
	}

	require.NoError(t, s.pushUsage(context.Background(), usageNow))

	byID := lo.KeyBy(allIngested(fc), func(e usageEvent) string { return e.id })
	assert.ElementsMatch(t, []string{
		"evt:us:t1:env_1:2026100109",
		"inv:us:t1:env_1:INR:2026100109",
		"inv:us:t1:env_1:EUR:2026100109",
		"inv:us:t1:env_2:EUR:2026100109",
	}, lo.Keys(byID), "a row without a rate is still sent")

	assert.Equal(t, "0.12", byID["inv:us:t1:env_1:INR:2026100109"].properties["amount"])
	for _, id := range []string{"inv:us:t1:env_1:EUR:2026100109", "inv:us:t1:env_2:EUR:2026100109"} {
		props := byID[id].properties
		assert.Equal(t, "0", props["amount"], "nothing is billed until the rate exists")
		assert.Equal(t, "10", props["total"], "the invoice amounts are kept for reprocessing")
		assert.Equal(t, "EUR", props["currency"])
		assert.NotContains(t, props, "usd_amount")
		assert.NotContains(t, props, "usd_rate")
	}

	require.Equal(t, 1, logs.Len(), "one log names every currency without a rate")
	assert.Equal(t, "[EUR]", fmt.Sprint(logs.All()[0].ContextMap()["currencies"]))
}

func TestPushUsage_USDRatesErrorReturned(t *testing.T) {
	fc := newFakeClient()
	fc.usdRatesErr = errors.New("flexprice cloud down")
	s, d := newTestService(fc)
	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC),
		Count:         1,
	})
	d.usage.AddInvoices(&domainsb.TenantHourInvoices{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Currency:      "inr",
		Hour:          time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC),
		InvoiceCount:  1,
		Total:         decimal.RequireFromString("10"),
	})

	err := s.pushUsage(context.Background(), usageNow)
	assert.ErrorIs(t, err, fc.usdRatesErr)
	assert.Empty(t, allIngested(fc), "nothing is sent without rates")
}

func TestPushUsage_BillingTenantInvoiceNeedsNoRates(t *testing.T) {
	hour := time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)

	fc := newFakeClient()
	fc.usdRatesErr = errors.New("flexprice cloud down")
	s, d := newTestService(fc)
	s.Config.Billing.TenantID = "t_billing"

	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          hour,
		Count:         1,
	})
	d.usage.AddInvoices(&domainsb.TenantHourInvoices{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Currency:      "usd",
		Hour:          hour,
		InvoiceCount:  1,
		Total:         decimal.RequireFromString("10"),
	})
	d.usage.AddInvoices(&domainsb.TenantHourInvoices{
		TenantID:      "t_billing",
		EnvironmentID: "env_b",
		Currency:      "eur",
		Hour:          hour,
		InvoiceCount:  1,
		Total:         decimal.RequireFromString("10"),
	})

	require.NoError(t, s.pushUsage(context.Background(), usageNow))
	assert.Zero(t, fc.usdRateCalls, "the billing tenant's own invoices are never sent, so they need no rate")
	assert.Len(t, allIngested(fc), 2)
}

func TestPushUsage_EnvironmentType(t *testing.T) {
	hour := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		envType types.EnvironmentType
		want    string
	}{
		{"production_env", types.EnvironmentProduction, "production"},
		{"development_env", types.EnvironmentDevelopment, "development"},
		{"unknown_env_billed_as_production", "", "production"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			if tt.envType != "" {
				d.usage.AddEnvironment("env_1", tt.envType)
			}
			d.usage.AddEvents(&domainsb.TenantHourEvents{
				TenantID:      "t1",
				EnvironmentID: "env_1",
				Hour:          hour,
				Count:         1,
			})
			d.usage.AddInvoices(&domainsb.TenantHourInvoices{
				TenantID:      "t1",
				EnvironmentID: "env_1",
				Currency:      "usd",
				Hour:          hour,
				InvoiceCount:  1,
				Total:         decimal.RequireFromString("1"),
			})

			require.NoError(t, s.pushUsage(context.Background(), usageNow))

			got := lo.Map(allIngested(fc), func(e usageEvent, _ int) string {
				return e.properties["environment_type"]
			})
			assert.Equal(t, []string{tt.want, tt.want}, got)
		})
	}
}

type envLookupFailingStore struct {
	*testutil.InMemorySelfBillingStore
	err error
}

func (s *envLookupFailingStore) ListEnvironmentTypes(context.Context, []string) (map[string]types.EnvironmentType, error) {
	return nil, s.err
}

func TestPushUsage_EnvironmentLookupErrorReturned(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	lookupErr := errors.New("postgres down")
	s.SelfBillingPostgresRepo = &envLookupFailingStore{
		InMemorySelfBillingStore: d.usage,
		err:                      lookupErr,
	}
	d.usage.AddEvents(&domainsb.TenantHourEvents{
		TenantID:      "t1",
		EnvironmentID: "env_1",
		Hour:          time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC),
		Count:         1,
	})

	err := s.pushUsage(context.Background(), usageNow)
	assert.ErrorIs(t, err, lookupErr)
	assert.Empty(t, allIngested(fc), "nothing is sent without environment types")
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
