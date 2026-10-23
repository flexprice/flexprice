package accountingperiod

import (
	"context"
	"testing"
	"time"

	domainAccountingPeriod "github.com/flexprice/flexprice/internal/domain/accountingperiod"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// closedMonthsStore reports the given months as closed; the in-memory store only creates open periods.
type closedMonthsStore struct {
	*testutil.InMemoryAccountingPeriodStore
	closed map[time.Month]bool
}

func (s *closedMonthsStore) GetOrCreate(ctx context.Context, periodStart time.Time) (*domainAccountingPeriod.AccountingPeriod, error) {
	p, err := s.InMemoryAccountingPeriodStore.GetOrCreate(ctx, periodStart)
	if err != nil || !s.closed[periodStart.Month()] {
		return p, err
	}
	return domainAccountingPeriod.NewBuilder(p).WithPeriodStatus(types.AccountingPeriodStatusClosed).Build(), nil
}

func TestPeriodFor(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	ist := time.FixedZone("IST", 5*3600+1800)
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		businessDate time.Time
		closed       map[time.Month]bool
		wantStart    time.Time
	}{
		{name: "mid month", businessDate: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), wantStart: oct},
		{name: "first day", businessDate: oct, wantStart: oct},
		{name: "last day", businessDate: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), wantStart: oct},
		{name: "local date keeps its month", businessDate: time.Date(2026, 10, 1, 1, 0, 0, 0, ist), wantStart: oct},
		{name: "closed month moves to next open", businessDate: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), closed: map[time.Month]bool{time.October: true, time.November: true}, wantStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewAccountingPeriodService(&closedMonthsStore{InMemoryAccountingPeriodStore: testutil.NewInMemoryAccountingPeriodStore(), closed: tt.closed})
			got, err := svc.PeriodFor(ctx, tt.businessDate)
			require.NoError(t, err)
			require.True(t, tt.wantStart.Equal(got.PeriodStart()), "got %s", got.PeriodStart())
			require.Equal(t, types.AccountingPeriodStatusOpen, got.PeriodStatus())
		})
	}

	t.Run("same month resolves to one period", func(t *testing.T) {
		svc := NewAccountingPeriodService(testutil.NewInMemoryAccountingPeriodStore())
		a, err := svc.PeriodFor(ctx, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		b, err := svc.PeriodFor(ctx, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Equal(t, a.ID(), b.ID())
	})
}

func TestGetAccountingPeriods(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc := NewAccountingPeriodService(testutil.NewInMemoryAccountingPeriodStore())
	for _, m := range []time.Month{time.October, time.November, time.December} {
		_, err := svc.PeriodFor(ctx, time.Date(2026, m, 10, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
	}

	filter := accountingPeriodRepo.NewFilter()
	filter.PeriodStatuses = []types.AccountingPeriodStatus{types.AccountingPeriodStatusOpen}
	filter.TimeRangeFilter = &types.TimeRangeFilter{
		StartTime: lo.ToPtr(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
		EndTime:   lo.ToPtr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
	resp, err := svc.GetAccountingPeriods(ctx, filter)
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	require.Equal(t, 2, resp.Pagination.Total)
	for _, item := range resp.Items {
		require.True(t, item.PeriodEnd.Equal(item.PeriodStart.AddDate(0, 1, 0)))
	}
}
