package testutil

import (
	"context"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemorySelfBillingStore_FiltersHalfOpenWindow(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	tests := []struct {
		name string
		hour time.Time
		want bool
	}{
		{"before_window", from.Add(-time.Hour), false},
		{"window_start_included", from, true},
		{"inside_window", from.Add(time.Hour), true},
		{"window_end_excluded", to, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewInMemorySelfBillingStore()
			s.AddEvents(&domainsb.TenantHourEvents{
				TenantID: "t1",
				Hour:     tt.hour,
			})
			s.AddInvoices(&domainsb.TenantHourInvoices{
				TenantID: "t1",
				Hour:     tt.hour,
			})

			events, err := s.CountEventsByTenantHour(context.Background(), from, to)
			require.NoError(t, err)
			invoices, err := s.SumInvoicesByTenantHour(context.Background(), from, to)
			require.NoError(t, err)

			assert.Equal(t, tt.want, len(events) == 1)
			assert.Equal(t, tt.want, len(invoices) == 1)
		})
	}
}
