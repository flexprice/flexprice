//go:build pgbench

package revenuereport

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestReportsAtScale runs the reports over the env_bench_rr data seeded by rr_seed.sql and checks every amount against
// the seed's closed form: customer bc_i (k = i % 10 + 1) has an annual POB (1200k prepaid each January) and a monthly
// POB (100k in advance), both period even from Jan 2025; recognition is posted through Sep 2026; reports run on
// 2026-10-15. BENCH_WATERFALL_CUSTOMERS limits the waterfall to bc_1..bc_W (0 = every customer).
func TestReportsAtScale(t *testing.T) {
	// The config loader searches up to three levels; this package is four deep.
	require.NoError(t, os.Chdir("../../../.."))
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_bench_rr")

	redis := cache.NewRedisCache()
	contracts := contractRepo.NewRepository(client, log)
	subs := entRepo.NewSubscriptionRepository(client, log, redis)
	invoices := entRepo.NewInvoiceRepository(client, log)
	taxes := entRepo.NewTaxAppliedRepository(client, log, redis)
	journals := journalRepo.NewRepository(client, log)
	pobs := pobRepo.NewRepository(client, log)
	pobService := pobsvc.NewPerformanceObligationService(pobs, contracts, subs, invoices, taxes, entRepo.NewRevenueFactRepository(client, log))
	journal := journalsvc.NewJournalService(client, journals, accountingperiodsvc.NewAccountingPeriodService(accountingPeriodRepo.NewRepository(client, log)),
		pobService, pobs, contractsvc.NewContractService(contracts), contracts, subs, entRepo.NewCustomerRepository(client, log, redis), invoices,
		entRepo.NewCreditNoteRepository(client, log, redis), entRepo.NewPaymentRepository(client, log, redis), entRepo.NewRefundRepository(client, log),
		taxes, entRepo.NewSettingsRepository(client, log, cache.NewInMemoryCache()))
	reports := NewRevenueReportService(journals, journal, contracts)

	n, err := contracts.Count(ctx, &contractRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter()})
	require.NoError(t, err)
	require.Positive(t, n, "seed env_bench_rr first")
	k := func(i int) int64 { return int64(i%10 + 1) }
	sumK := func(customers int) decimal.Decimal {
		var total int64
		for i := 1; i <= customers; i++ {
			total += k(i)
		}
		return decimal.NewFromInt(total)
	}
	d := decimal.NewFromInt
	all := sumK(n)
	timed := func(name string, fn func()) {
		start := time.Now()
		fn()
		t.Logf("%-60s %10s", name, time.Since(start).Round(time.Millisecond))
	}
	t.Logf("customers %d", n)

	t.Run("deferred rollforward Jan–Sep 2026: closing = 1200k − 100k·m, opening(P) = closing(P−1)", func(t *testing.T) {
		var resp *dto.RollforwardResponse
		timed("rollforward deferred, 9 months, ungrouped", func() {
			resp, err = reports.GetRollforward(ctx, &dto.RollforwardRequest{PeriodStart: "2026-01", PeriodEnd: "2026-09", Currency: "usd"})
			require.NoError(t, err)
		})
		require.Len(t, resp.Totals, 9)
		for m, row := range resp.Totals {
			month := int64(m + 1)
			require.Equal(t, d(1200-100*month).Mul(all).String(), row.Closing.String(), row.Period)
			require.Equal(t, d(-200).Mul(all).String(), row.Movements[types.MovementCategoryEarned].String(), row.Period)
			require.Equal(t, lo.Ternary(month == 1, d(1300), d(100)).Mul(all).String(), row.Movements[types.MovementCategoryInvoiced].String(), row.Period)
			if m > 0 {
				require.Equal(t, resp.Totals[m-1].Closing.String(), row.Opening.String(), "opening(P) = closing(P−1)")
			}
		}
	})

	t.Run("September rollforward of every account", func(t *testing.T) {
		var resp *dto.RollforwardResponse
		timed("rollforward 5 accounts, Sep, ungrouped", func() {
			resp, err = reports.GetRollforward(ctx, &dto.RollforwardRequest{PeriodStart: "2026-09", Currency: "usd", Accounts: []types.LedgerAccountKey{
				types.LedgerAccountReceivable, types.LedgerAccountUnbilledReceivable, types.LedgerAccountDeferredRevenue, types.LedgerAccountRevenue, types.LedgerAccountCash}})
			require.NoError(t, err)
		})
		byAccount := lo.KeyBy(resp.Totals, func(r *dto.RollforwardRow) types.LedgerAccountKey { return r.Account })
		require.Equal(t, d(4500).Mul(all).String(), byAccount[types.LedgerAccountReceivable].Closing.String(), "AR: 2 annual + 21 monthly invoices")
		require.Equal(t, d(4200).Mul(all).String(), byAccount[types.LedgerAccountRevenue].Closing.String(), "revenue: 21 months × 200k")
		require.Equal(t, d(300).Mul(all).String(), byAccount[types.LedgerAccountDeferredRevenue].Closing.String())
		require.True(t, byAccount[types.LedgerAccountUnbilledReceivable].Closing.IsZero(), "unbilled nets to zero when billed in advance")
		require.NotContains(t, byAccount, types.LedgerAccountCash, "no payments in the seed")
	})

	t.Run("double entry: every line nets to zero over all accounts", func(t *testing.T) {
		var sums []*journalRepo.LineSum
		timed("SumLines all accounts, Jan 2025 – Oct 2026", func() {
			sums, err = journals.SumLines(ctx, &journalRepo.SumFilter{Currency: "usd", PeriodStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
			require.NoError(t, err)
		})
		total := decimal.Sum(decimal.Zero, lo.Map(sums, func(s *journalRepo.LineSum, _ int) decimal.Decimal { return s.Net })...)
		require.True(t, total.IsZero(), total.String())
	})

	t.Run("deferred rollforward per customer", func(t *testing.T) {
		var resp *dto.RollforwardResponse
		timed("rollforward deferred, Sep, by customer + contract", func() {
			resp, err = reports.GetRollforward(ctx, &dto.RollforwardRequest{PeriodStart: "2026-09", Currency: "usd", Limit: 1000,
				GroupBy: []types.RevenueReportGroupBy{types.RevenueReportGroupByCustomer, types.RevenueReportGroupByContract}})
			require.NoError(t, err)
		})
		require.Equal(t, n, resp.Pagination.Total)
		require.Equal(t, "2026-09|deferred_revenue|bc_1|bk_1||800|earned:-400,invoiced:200|600", rowString(resp.Items[0]), "bc_1 has k = 2")
	})

	t.Run("recognition is coherent with the seeded journal: a real run posts nothing", func(t *testing.T) {
		for _, i := range []int{1, 7, n / 2, n} {
			entries, err := journal.Recognize(ctx, journalsvc.RecognizeRequest{ContractID: fmt.Sprintf("bk_%d", i), AsOf: time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)})
			require.NoError(t, err, "%+v", err)
			require.Empty(t, entries, "bk_%d", i)
		}
	})

	t.Run("dry-run latency per contract", func(t *testing.T) {
		const contractsTimed = 200
		asOf := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
		timed(fmt.Sprintf("%d dry runs (one per contract)", contractsTimed), func() {
			for i := 1; i <= contractsTimed; i++ {
				entries, err := journal.Recognize(ctx, journalsvc.RecognizeRequest{ContractID: fmt.Sprintf("bk_%d", i), AsOf: asOf, DryRun: true})
				require.NoError(t, err)
				require.Len(t, entries, 2, "one October entry per POB: both are billed ahead")
			}
		})
	})

	w := n
	if v, err := strconv.Atoi(os.Getenv("BENCH_WATERFALL_CUSTOMERS")); err == nil && v > 0 && v < n {
		w = v
	}
	t.Run(fmt.Sprintf("waterfall of %d customers on Oct 15", w), func(t *testing.T) {
		req := &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-12", Currency: "usd", Details: true, Limit: 1000,
			AsOf: lo.ToPtr(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))}
		if w < n {
			req.CustomerIDs = lo.Times(w, func(i int) string { return fmt.Sprintf("bc_%d", i+1) })
		}
		var resp *dto.WaterfallResponse
		timed(fmt.Sprintf("waterfall Jan–Dec 2026 with details, %d customers", w), func() {
			resp, err = reports.GetWaterfall(ctx, req)
			require.NoError(t, err)
		})
		require.Equal(t, w, resp.Pagination.Total)

		// October: each POB has 14 of 31 days pending, rounded per POB; the rest of the month is scheduled.
		pending := decimal.Zero
		for i := 1; i <= w; i++ {
			pending = pending.Add(d(100 * k(i)).Mul(d(14)).Div(d(31)).Round(2).Mul(d(2)))
		}
		group := sumK(w)
		for m, c := range resp.Totals {
			month := int64(m + 1)
			switch {
			case month <= 9:
				require.Equal(t, d(200).Mul(group).String(), c.Revenue.Actual.String(), c.Period)
				require.Equal(t, d(1200-100*month).Mul(group).String(), c.Deferred.String(), c.Period)
				require.True(t, c.Unbilled.IsZero(), c.Period)
			case month == 10:
				require.Equal(t, pending.String(), c.Revenue.Pending.String(), c.Period)
				require.Equal(t, d(200).Mul(group).Sub(pending).String(), c.Revenue.Scheduled.String(), c.Period)
				require.Nil(t, c.Deferred, "October has not ended")
			default:
				require.Equal(t, d(100).Mul(group).String(), c.Revenue.Scheduled.String(), "%s: the annual only; no monthly invoice yet", c.Period)
			}
			require.Equal(t, c.Revenue.Actual.Add(c.Revenue.Pending).Add(c.Revenue.Scheduled).String(), c.Revenue.Total.String())
		}
	})
}
