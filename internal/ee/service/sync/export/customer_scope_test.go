package export

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

const overCapMessage = "Filter matches more than 1 customers; narrow it"

func scopedRequest(entity types.ScheduledTaskEntityType, ids ...string) *dto.ExportRequest {
	return &dto.ExportRequest{
		TenantID:     "t1",
		EnvID:        "e1",
		EntityType:   entity,
		StartTime:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		JobConfig:    &types.S3JobConfig{},
		SourceConfig: &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{CustomerIDs: ids}},
	}
}

func unscopedRequest(entity types.ScheduledTaskEntityType) *dto.ExportRequest {
	req := scopedRequest(entity)
	req.SourceConfig = nil
	return req
}

func twoCustomers(t *testing.T) (*testutil.InMemoryCustomerStore, context.Context) {
	t.Helper()
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "t1"), "e1")
	store := testutil.NewInMemoryCustomerStore()
	addCust(t, store, ctx, "a", "t1", "e1", types.StatusPublished, nil)
	addCust(t, store, ctx, "b", "t1", "e1", types.StatusPublished, nil)
	return store, ctx
}

func csvIDs(t *testing.T, out []byte, col int) []string {
	t.Helper()
	_, rows := parseCSVOutput(t, out)
	return lo.Map(rows, func(r []string, _ int) string { return r[col] })
}

type countingInvoiceRepo struct {
	invoice.Repository
	calls       int
	customerIDs []string
}

func (r *countingInvoiceRepo) GetInvoicesForExport(ctx context.Context, tenantID, envID string, start, end time.Time, limit, offset int, customerIDs []string) ([]*invoice.Invoice, error) {
	r.calls++
	r.customerIDs = customerIDs
	return r.Repository.GetInvoicesForExport(ctx, tenantID, envID, start, end, limit, offset, customerIDs)
}

func newInvoiceScopeEnv(t *testing.T) (*InvoiceExporter, *countingInvoiceRepo, context.Context) {
	t.Helper()
	customers, ctx := twoCustomers(t)
	store := testutil.NewInMemoryInvoiceStore()
	created := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, inv := range []struct{ id, customer string }{{"inv_a", "a"}, {"inv_b", "b"}} {
		require.NoError(t, store.Create(ctx, &invoice.Invoice{
			ID: inv.id, CustomerID: inv.customer, EnvironmentID: "e1",
			BaseModel: types.BaseModel{TenantID: "t1", Status: types.StatusPublished, CreatedAt: created, UpdatedAt: created},
		}))
	}
	repo := &countingInvoiceRepo{Repository: store}
	return NewInvoiceExporter(repo, customers, nil, logger.NewNoopLogger()), repo, ctx
}

func TestInvoiceExporter_CustomerScope(t *testing.T) {
	t.Run("filtered rows only", func(t *testing.T) {
		e, repo, ctx := newInvoiceScopeEnv(t)
		out, n, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeInvoice, "a"))
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, []string{"a"}, repo.customerIDs)
		require.Equal(t, []string{"inv_a"}, csvIDs(t, out, 0))
	})
	t.Run("unfiltered is unchanged", func(t *testing.T) {
		e, repo, ctx := newInvoiceScopeEnv(t)
		out, n, err := e.PrepareData(ctx, unscopedRequest(types.ScheduledTaskEntityTypeInvoice))
		require.NoError(t, err)
		require.Equal(t, 2, n)
		require.Nil(t, repo.customerIDs)
		require.ElementsMatch(t, []string{"inv_a", "inv_b"}, csvIDs(t, out, 0))
	})
	t.Run("zero matches is header only without a query", func(t *testing.T) {
		e, repo, ctx := newInvoiceScopeEnv(t)
		out, n, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeInvoice, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		require.Zero(t, repo.calls)
		h, rows := parseCSVOutput(t, out)
		require.Equal(t, "id", h[0])
		require.Empty(t, rows)
	})
	t.Run("over the cap fails", func(t *testing.T) {
		e, repo, ctx := newInvoiceScopeEnv(t)
		e.scope.max = 1
		_, _, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeInvoice))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
		require.Zero(t, repo.calls)
	})
}

type topupRepo struct {
	wallet.Repository
	calls       int
	customerIDs []string
}

func (r *topupRepo) GetCreditTopupsForExport(_ context.Context, _, _ string, _, _ time.Time, _, offset int, customerIDs []string) ([]*wallet.CreditTopupsExportData, error) {
	r.calls++
	r.customerIDs = customerIDs
	if offset > 0 {
		return nil, nil
	}
	return []*wallet.CreditTopupsExportData{{TopupID: "tx_1", CreatedAt: time.Now()}}, nil
}

func TestCreditTopupExporter_CustomerScope(t *testing.T) {
	newEnv := func(t *testing.T) (*CreditTopupExporter, *topupRepo, context.Context) {
		customers, ctx := twoCustomers(t)
		repo := &topupRepo{}
		return NewCreditTopupExporter(repo, customers, nil, logger.NewNoopLogger()), repo, ctx
	}

	t.Run("filtered ids reach the query", func(t *testing.T) {
		e, repo, ctx := newEnv(t)
		_, n, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeCreditTopups, "a"))
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, []string{"a"}, repo.customerIDs)
	})
	t.Run("unfiltered passes nil", func(t *testing.T) {
		e, repo, ctx := newEnv(t)
		_, _, err := e.PrepareData(ctx, unscopedRequest(types.ScheduledTaskEntityTypeCreditTopups))
		require.NoError(t, err)
		require.Equal(t, 1, repo.calls)
		require.Nil(t, repo.customerIDs)
	})
	t.Run("zero matches is header only without a query", func(t *testing.T) {
		e, repo, ctx := newEnv(t)
		out, n, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeCreditTopups, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		require.Zero(t, repo.calls)
		h, rows := parseCSVOutput(t, out)
		require.Equal(t, "topup_id", h[0])
		require.Empty(t, rows)
	})
	t.Run("over the cap fails", func(t *testing.T) {
		e, repo, ctx := newEnv(t)
		e.scope.max = 1
		_, _, err := e.PrepareData(ctx, scopedRequest(types.ScheduledTaskEntityTypeCreditTopups))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
		require.Zero(t, repo.calls)
	})
}

type ledgerScopeRepo struct {
	ledgerRepo
	listCalls int
}

func (r *ledgerScopeRepo) ListWalletIDsForLedgerExport(ctx context.Context, tenant, env string, start, end time.Time, ids []string) ([]string, error) {
	r.listCalls++
	return r.ledgerRepo.ListWalletIDsForLedgerExport(ctx, tenant, env, start, end, ids)
}

func TestWalletLedgerExporter_CustomerScope(t *testing.T) {
	run := func(t *testing.T, mutate func(*WalletLedgerExporter), req *dto.ExportRequest) (*ledgerScopeRepo, int, error) {
		customers, ctx := twoCustomers(t)
		repo := &ledgerScopeRepo{ledgerRepo: ledgerRepo{walletIDs: []string{"w1"}}}
		e := NewWalletLedgerExporter(repo, customers, logger.NewNoopLogger())
		if mutate != nil {
			mutate(e)
		}
		var buf bytes.Buffer
		n, err := e.WriteData(ctx, req, &buf)
		return repo, n, err
	}

	t.Run("filter becomes external ids", func(t *testing.T) {
		repo, n, err := run(t, nil, scopedRequest(types.ScheduledTaskEntityTypeWalletLedger, "a"))
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, []string{"ext-a"}, repo.customerIDs)
	})
	t.Run("legacy external ids intersect", func(t *testing.T) {
		req := scopedRequest(types.ScheduledTaskEntityTypeWalletLedger, "a", "b")
		req.CustomerExternalIDs = []string{"ext-b", "ext-z"}
		repo, _, err := run(t, nil, req)
		require.NoError(t, err)
		require.Equal(t, []string{"ext-b"}, repo.customerIDs)
	})
	t.Run("empty intersection is header only without a query", func(t *testing.T) {
		req := scopedRequest(types.ScheduledTaskEntityTypeWalletLedger, "a")
		req.CustomerExternalIDs = []string{"ext-b"}
		repo, n, err := run(t, nil, req)
		require.NoError(t, err)
		require.Zero(t, n)
		require.Zero(t, repo.listCalls)
	})
	t.Run("unfiltered keeps legacy ids", func(t *testing.T) {
		req := unscopedRequest(types.ScheduledTaskEntityTypeWalletLedger)
		req.CustomerExternalIDs = []string{"acme"}
		repo, _, err := run(t, nil, req)
		require.NoError(t, err)
		require.Equal(t, []string{"acme"}, repo.customerIDs)
	})
	t.Run("zero matches is header only without a query", func(t *testing.T) {
		repo, n, err := run(t, nil, scopedRequest(types.ScheduledTaskEntityTypeWalletLedger, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		require.Zero(t, repo.listCalls)
	})
	t.Run("over the cap fails", func(t *testing.T) {
		repo, _, err := run(t, func(e *WalletLedgerExporter) { e.scope.max = 1 }, scopedRequest(types.ScheduledTaskEntityTypeWalletLedger))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
		require.Zero(t, repo.listCalls)
	})
}

func TestCreditUsageExporter_CustomerScope(t *testing.T) {
	setup := func(t *testing.T) *creditUsageTestEnv {
		env := newCreditUsageTestEnv(t)
		for _, id := range []string{"a", "b"} {
			c := env.addCustomer(t, id, "ext-"+id, "N "+id, nil)
			w := env.addWallet(t, "w-"+id, c.ID, 10, nil)
			env.setBalance(w.ID, w, 10)
		}
		return env
	}
	scoped := func(env *creditUsageTestEnv, ids ...string) *dto.ExportRequest {
		env.req.SourceConfig = &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{CustomerIDs: ids}}
		return env.req
	}

	t.Run("filtered rows only", func(t *testing.T) {
		env := setup(t)
		out, n, err := env.exporter.PrepareData(env.ctx, scoped(env, "a"))
		require.NoError(t, err)
		require.Equal(t, 1, n)
		h, rows := parseCSVOutput(t, out)
		require.Equal(t, "a", colVal(t, h, rows[0], string(wallet.CreditUsageCSVHeadersCustomerID)))
	})
	t.Run("unfiltered is unchanged", func(t *testing.T) {
		env := setup(t)
		_, n, err := env.exporter.PrepareData(env.ctx, env.req)
		require.NoError(t, err)
		require.Equal(t, 2, n)
	})
	t.Run("zero matches is header only", func(t *testing.T) {
		env := setup(t)
		out, n, err := env.exporter.PrepareData(env.ctx, scoped(env, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		h, rows := parseCSVOutput(t, out)
		require.NotEmpty(t, h)
		require.Empty(t, rows)
	})
	t.Run("over the cap fails", func(t *testing.T) {
		env := setup(t)
		env.exporter.scope.max = 1
		_, _, err := env.exporter.PrepareData(env.ctx, scoped(env))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
	})
}

func TestUsageAnalyticsExporter_CustomerScope(t *testing.T) {
	setup := func(t *testing.T) *usageAnalyticsTestEnv {
		env := newUsageAnalyticsTestEnv(t)
		for _, id := range []string{"a", "b"} {
			c := env.addCustomer(t, id, "ext-"+id, "N "+id, nil)
			env.addEvent(t, c.ExternalID, env.req.StartTime.Add(time.Minute))
			env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{{FeatureID: "f", FeatureName: "F", EventName: "e", EventCount: 1}})
		}
		return env
	}
	scoped := func(env *usageAnalyticsTestEnv, ids ...string) *dto.ExportRequest {
		env.req.SourceConfig = &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{CustomerIDs: ids}}
		return env.req
	}

	t.Run("filtered rows only", func(t *testing.T) {
		env := setup(t)
		out, n, err := env.exporter.PrepareData(env.ctx, scoped(env, "a"))
		require.NoError(t, err)
		require.Equal(t, 1, n)
		h, rows := parseCSVOutput(t, out)
		require.Equal(t, "a", colVal(t, h, rows[0], string(UsageAnalyticsCSVHeadersCustomerID)))
		require.Len(t, env.analyticsGetter.requests, 1)
	})
	t.Run("unfiltered is unchanged", func(t *testing.T) {
		env := setup(t)
		_, n, err := env.exporter.PrepareData(env.ctx, env.req)
		require.NoError(t, err)
		require.Equal(t, 2, n)
	})
	t.Run("zero matches is header only without analytics", func(t *testing.T) {
		env := setup(t)
		out, n, err := env.exporter.PrepareData(env.ctx, scoped(env, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		require.Empty(t, env.analyticsGetter.requests)
		h, rows := parseCSVOutput(t, out)
		require.NotEmpty(t, h)
		require.Empty(t, rows)
	})
	t.Run("over the cap fails", func(t *testing.T) {
		env := setup(t)
		env.exporter.scope.max = 1
		_, _, err := env.exporter.PrepareData(env.ctx, scoped(env))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
		require.Empty(t, env.analyticsGetter.requests)
	})
}

func newReportScopeEnv(t *testing.T, runner *fakeRunner) (*ReportExporter, context.Context) {
	t.Helper()
	customers, ctx := twoCustomers(t)
	return NewReportExporter(runner, types.ScheduledTaskEntityTypeReport, customers), ctx
}

func reportScopedRequest(params *types.ReportParams, ids ...string) *dto.ExportRequest {
	req := scopedRequest(types.ScheduledTaskEntityTypeReport, ids...)
	req.SourceConfig.ReportKey = "invoice_total"
	req.SourceConfig.ReportParams = params
	return req
}

func TestReportExporter_CustomerScope(t *testing.T) {
	result := &dto.AnalyticsQueryResult{Columns: []*dto.AnalyticsColumn{{Name: "customer_id"}}, Rows: [][]string{{"a"}}}

	t.Run("filter is appended to the params", func(t *testing.T) {
		runner := &fakeRunner{result: result}
		e, ctx := newReportScopeEnv(t, runner)
		params := &types.ReportParams{Shape: "breakdown", Dimensions: []string{"customer_id"},
			Filters: []types.ReportParamFilter{{Field: "currency", Op: "eq", Value: []string{"usd"}}}}
		req := reportScopedRequest(params, "a", "b")

		_, n, err := e.PrepareData(ctx, req)

		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Len(t, runner.gotParams.Filters, 2)
		got := runner.gotParams.Filters[1]
		require.Equal(t, "customer_id", got.Field)
		require.Equal(t, "in", got.Op)
		require.ElementsMatch(t, []string{"a", "b"}, got.Value)
		require.Len(t, params.Filters, 1, "stored params are not modified")
	})
	t.Run("appended filter passes report validation", func(t *testing.T) {
		entry, ok := analytics.LookupReport("invoice_total")
		require.True(t, ok)
		hasCustomer := false
		for _, d := range entry.Dimensions {
			hasCustomer = hasCustomer || d.Key == "customer_id"
		}
		require.True(t, hasCustomer)
		params := withCustomerFilter(&types.ReportParams{Shape: "breakdown", Dimensions: []string{"customer_id"}}, []string{"a"})
		req := &dto.ReportQueryRequest{Shape: params.Shape, Dimensions: params.Dimensions, Range: "last_30_days"}
		for _, f := range params.Filters {
			req.Filters = append(req.Filters, dto.ReportFilter{Field: f.Field, Op: f.Op, Value: f.Value})
		}
		_, err := req.ValidateForReport("invoice_total", 1000)
		require.NoError(t, err)
	})
	t.Run("unfiltered passes params through", func(t *testing.T) {
		runner := &fakeRunner{result: result}
		e, ctx := newReportScopeEnv(t, runner)
		params := &types.ReportParams{Shape: "scalar"}
		req := reportScopedRequest(params)
		req.SourceConfig.CustomerFilter = nil

		_, _, err := e.PrepareData(ctx, req)

		require.NoError(t, err)
		require.Same(t, params, runner.gotParams)
	})
	t.Run("zero matches is header only without running the report", func(t *testing.T) {
		runner := &fakeRunner{result: result}
		e, ctx := newReportScopeEnv(t, runner)
		out, n, err := e.PrepareData(ctx, reportScopedRequest(&types.ReportParams{Shape: "breakdown", Dimensions: []string{"customer_id"}}, "nobody"))
		require.NoError(t, err)
		require.Zero(t, n)
		require.Zero(t, runner.calls)
		require.Equal(t, "customer_id,invoice_total\n", string(out))
	})
	t.Run("over the cap fails without running the report", func(t *testing.T) {
		runner := &fakeRunner{result: result}
		e, ctx := newReportScopeEnv(t, runner)
		e.scope.max = 1
		_, _, err := e.PrepareData(ctx, reportScopedRequest(&types.ReportParams{Shape: "scalar"}))
		require.Error(t, err)
		require.Contains(t, err.Error(), overCapMessage)
		require.Zero(t, runner.calls)
	})
	t.Run("view exports reject a customer filter", func(t *testing.T) {
		runner := &fakeRunner{result: result}
		customers, ctx := twoCustomers(t)
		e := NewReportExporter(runner, types.ScheduledTaskEntityTypeAnalyticsView, customers)
		req := scopedRequest(types.ScheduledTaskEntityTypeAnalyticsView, "a")
		req.SourceConfig.ViewID = "v1"
		_, _, err := e.PrepareData(ctx, req)
		require.Error(t, err)
		require.Zero(t, runner.calls)
	})
}

func TestExport_CustomerFilterOnUnsupportedEntity(t *testing.T) {
	store := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	svc := managedService(store, nil, &panicConnRepo{})
	req := scopedRequest(types.ScheduledTaskEntityTypeWalletDebits, "a")
	req.JobConfig = platformCfg("exports/t1/e1/st_1")

	_, err := svc.Export(context.Background(), req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "customer_filter not supported")
	require.Empty(t, store.keys)
}
