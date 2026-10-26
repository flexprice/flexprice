package revenuereport

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeRecognizer returns fixed dry-run entries per contract, schedules for a future AsOf and previews otherwise, and
// records the runs.
type fakeRecognizer struct {
	journalsvc.JournalService
	previews  map[string][]*domainJournal.Entry
	schedules map[string][]*domainJournal.Entry
	called    []string
	asOfs     []time.Time
}

func (f *fakeRecognizer) Recognize(_ context.Context, req journalsvc.RecognizeRequest) ([]*domainJournal.Entry, error) {
	if !req.DryRun {
		return nil, errors.New("reports must only dry-run recognition")
	}
	f.asOfs = append(f.asOfs, req.AsOf)
	if req.AsOf.After(time.Now().Add(time.Hour)) {
		return f.schedules[req.ContractID], nil
	}
	f.called = append(f.called, req.ContractID)
	return f.previews[req.ContractID], nil
}

// newTestService posts the ERD example: acme invoices 1,300 in Jan and earns 200 (100 per POB); in Feb beta invoices 100,
// acme and beta each earn 100, acme pays 1,300, and beta gets a 10 credit note on its POB. Gamma buys a 30 one-off in Jan,
// with no contract or POB.
func newTestService(t *testing.T, ctx context.Context) (RevenueReportService, *fakeRecognizer, *testutil.InMemoryContractStore) {
	periods := testutil.NewInMemoryAccountingPeriodStore()
	store := testutil.NewInMemoryJournalStore(periods)

	period := func(m time.Month) string {
		p, err := periods.GetOrCreate(ctx, time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		return p.ID()
	}
	jan, feb := period(time.January), period(time.February)

	line := func(account types.LedgerAccountKey, direction types.JournalDirection, amount int64, pobID string) *domainJournal.Line {
		return domainJournal.NewLineBuilder(nil).
			WithAccount(account).
			WithDirection(direction).
			WithAmount(decimal.NewFromInt(amount)).
			WithPOBID(pobID).
			Build()
	}
	debit := func(account types.LedgerAccountKey, amount int64, pobID string) *domainJournal.Line {
		return line(account, types.JournalDirectionDebit, amount, pobID)
	}
	credit := func(account types.LedgerAccountKey, amount int64, pobID string) *domainJournal.Line {
		return line(account, types.JournalDirectionCredit, amount, pobID)
	}
	post := func(eventType types.JournalEventType, periodID, customerID, contractID string, lines ...*domainJournal.Line) {
		require.NoError(t, store.CreateWithLines(ctx, domainJournal.NewBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_ENTRY)).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeInvoice, "inv_1").
			WithAccountingPeriodID(periodID).
			WithCustomerID(customerID).
			WithContractID(contractID).
			WithCurrency("usd").
			WithLines(lines).
			WithCreatedAt(time.Now().UTC()).
			Build()))
	}
	deferred, unbilled, revenue, ar := types.LedgerAccountDeferredRevenue, types.LedgerAccountUnbilledReceivable, types.LedgerAccountRevenue, types.LedgerAccountReceivable
	earn := func(periodID, customerID, contractID, pobID string, amount int64) {
		post(types.JournalEventRevenueEarned, periodID, customerID, contractID, debit(unbilled, amount, pobID), credit(revenue, amount, pobID))
		post(types.JournalEventRevenueOffset, periodID, customerID, contractID, debit(deferred, amount, pobID), credit(unbilled, amount, pobID))
	}

	post(types.JournalEventInvoiceFinalized, jan, "cust_acme", "contract_1",
		debit(ar, 1300, ""), credit(deferred, 1200, "pob_annual"), credit(deferred, 100, "pob_monthly"))
	earn(jan, "cust_acme", "contract_1", "pob_annual", 100)
	earn(jan, "cust_acme", "contract_1", "pob_monthly", 100)
	post(types.JournalEventInvoiceFinalized, feb, "cust_beta", "contract_2", debit(ar, 100, ""), credit(deferred, 100, "pob_beta"))
	earn(feb, "cust_acme", "contract_1", "pob_annual", 100)
	earn(feb, "cust_beta", "contract_2", "pob_beta", 100)
	post(types.JournalEventPaymentSucceeded, feb, "cust_acme", "contract_1", debit(types.LedgerAccountCash, 1300, ""), credit(ar, 1300, ""))
	post(types.JournalEventInvoiceFinalized, jan, "cust_gamma", "", debit(ar, 30, ""), credit(revenue, 30, ""))
	post(types.JournalEventCreditNoteFinalized, feb, "cust_beta", "contract_2", debit(types.LedgerAccountContraRevenue, 10, "pob_beta"), credit(ar, 10, ""))

	recognizer := &fakeRecognizer{previews: map[string][]*domainJournal.Entry{}, schedules: map[string][]*domainJournal.Entry{}}
	contracts := testutil.NewInMemoryContractStore()
	return NewRevenueReportService(store, recognizer, contracts), recognizer, contracts
}

func TestGetRollforward(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, _, _ := newTestService(t, ctx)

	tests := []struct {
		name       string
		req        *dto.RollforwardRequest
		wantTotals []string
		wantItems  []string
		wantTotal  int // pagination total; 0 = no pagination
	}{
		{
			name: "ERD February deferred by customer and contract",
			req: &dto.RollforwardRequest{
				PeriodStart: "2026-02", Currency: "usd",
				GroupBy: []types.RevenueReportGroupBy{types.RevenueReportGroupByCustomer, types.RevenueReportGroupByContract},
			},
			wantTotals: []string{"2026-02|deferred_revenue||||1100|earned:-200,invoiced:100|1000"},
			wantItems: []string{
				"2026-02|deferred_revenue|cust_acme|contract_1||1100|earned:-100|1000",
				"2026-02|deferred_revenue|cust_beta|contract_2||0|earned:-100,invoiced:100|0",
			},
			wantTotal: 2,
		},
		{
			name: "January closing is February opening; no group_by leaves items empty",
			req:  &dto.RollforwardRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"},
			wantTotals: []string{
				"2026-01|deferred_revenue||||0|earned:-200,invoiced:1300|1100",
				"2026-02|deferred_revenue||||1100|earned:-200,invoiced:100|1000",
			},
			wantItems: []string{},
		},
		{
			name: "unbilled is debit-normal: earned adds, the offset counts as invoiced",
			req:  &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "usd", Accounts: []types.LedgerAccountKey{types.LedgerAccountUnbilledReceivable}},
			wantTotals: []string{
				"2026-02|unbilled_receivable||||0|earned:200,invoiced:-200|0",
			},
			wantItems: []string{},
		},
		{
			name: "receivable: invoiced and payments",
			req: &dto.RollforwardRequest{
				PeriodStart: "2026-02", Currency: "usd",
				Accounts: []types.LedgerAccountKey{types.LedgerAccountReceivable},
			},
			wantTotals: []string{"2026-02|accounts_receivable||||1330|adjustments:-10,invoiced:100,payments:-1300|120"},
			wantItems:  []string{},
		},
		{
			name: "contra revenue by POB: credit note lines carry the credited line's POB",
			req: &dto.RollforwardRequest{
				PeriodStart: "2026-02", Currency: "usd",
				Accounts: []types.LedgerAccountKey{types.LedgerAccountContraRevenue},
				GroupBy:  []types.RevenueReportGroupBy{types.RevenueReportGroupByPOB},
			},
			wantTotals: []string{"2026-02|contra_revenue||||0|adjustments:10|10"},
			wantItems:  []string{"2026-02|contra_revenue|||pob_beta|0|adjustments:10|10"},
			wantTotal:  1,
		},
		{
			name: "revenue by POB",
			req: &dto.RollforwardRequest{
				PeriodStart: "2026-02", Currency: "usd",
				Accounts: []types.LedgerAccountKey{types.LedgerAccountRevenue},
				GroupBy:  []types.RevenueReportGroupBy{types.RevenueReportGroupByPOB},
			},
			wantTotals: []string{"2026-02|revenue||||230|earned:200|430"},
			wantItems: []string{
				"2026-02|revenue||||30||30",
				"2026-02|revenue|||pob_annual|100|earned:100|200",
				"2026-02|revenue|||pob_beta|0|earned:100|100",
				"2026-02|revenue|||pob_monthly|100||100",
			},
			wantTotal: 4,
		},
		{
			name: "paging keeps totals over every group",
			req: &dto.RollforwardRequest{
				PeriodStart: "2026-02", Currency: "usd", Limit: 1, Offset: 1,
				GroupBy: []types.RevenueReportGroupBy{types.RevenueReportGroupByCustomer},
			},
			wantTotals: []string{"2026-02|deferred_revenue||||1100|earned:-200,invoiced:100|1000"},
			wantItems:  []string{"2026-02|deferred_revenue|cust_beta|||0|earned:-100,invoiced:100|0"},
			wantTotal:  2,
		},
		{
			name:       "posted_by before anything was posted: the books were empty then",
			req:        &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "usd", PostedBy: lo.ToPtr(time.Now().Add(-time.Hour))},
			wantTotals: []string{},
			wantItems:  []string{},
		},
		{
			name:       "other currency has nothing",
			req:        &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "eur"},
			wantTotals: []string{},
			wantItems:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.GetRollforward(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.wantTotals, lo.Map(resp.Totals, func(r *dto.RollforwardRow, _ int) string { return rowString(r) }))
			require.Equal(t, tt.wantItems, lo.Map(resp.Items, func(r *dto.RollforwardRow, _ int) string { return rowString(r) }))
			if tt.wantTotal == 0 {
				require.Nil(t, resp.Pagination)
				return
			}
			require.Equal(t, tt.wantTotal, resp.Pagination.Total)
		})
	}
}

func TestGetRollforwardValidation(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, _, _ := newTestService(t, ctx)
	openMonth := time.Now().UTC().Format("2006-01")

	tests := []struct {
		name string
		req  *dto.RollforwardRequest
	}{
		{name: "missing currency", req: &dto.RollforwardRequest{PeriodStart: "2026-02"}},
		{name: "bad period format", req: &dto.RollforwardRequest{PeriodStart: "2026-2", Currency: "usd"}},
		{name: "end before start", req: &dto.RollforwardRequest{PeriodStart: "2026-02", PeriodEnd: "2026-01", Currency: "usd"}},
		{name: "open month", req: &dto.RollforwardRequest{PeriodStart: "2026-02", PeriodEnd: openMonth, Currency: "usd"}},
		{name: "unknown account", req: &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "usd", Accounts: []types.LedgerAccountKey{"sales"}}},
		{name: "unknown group_by", req: &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "usd", GroupBy: []types.RevenueReportGroupBy{"plan_id"}}},
		{name: "POB grouping on receivable", req: &dto.RollforwardRequest{
			PeriodStart: "2026-02", Currency: "usd",
			Accounts: []types.LedgerAccountKey{types.LedgerAccountDeferredRevenue, types.LedgerAccountReceivable},
			GroupBy:  []types.RevenueReportGroupBy{types.RevenueReportGroupByPOB},
		}},
		{name: "POB filter on receivable", req: &dto.RollforwardRequest{
			PeriodStart: "2026-02", Currency: "usd",
			Accounts: []types.LedgerAccountKey{types.LedgerAccountReceivable},
			POBIDs:   []string{"pob_annual"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetRollforward(ctx, tt.req)
			require.True(t, ierr.IsValidation(err), err)
		})
	}
}

// rowString renders a row as period|account|customer|contract|pob|opening|category:amount,...|closing.
func rowString(r *dto.RollforwardRow) string {
	movements := lo.MapToSlice(r.Movements, func(c types.MovementCategory, amount decimal.Decimal) string {
		return string(c) + ":" + amount.String()
	})
	sort.Strings(movements)
	return strings.Join([]string{
		r.Period, string(r.Account), lo.FromPtr(r.CustomerID), lo.FromPtr(r.ContractID), lo.FromPtr(r.POBID),
		r.Opening.String(), strings.Join(movements, ","), r.Closing.String(),
	}, "|")
}

func TestGetWaterfall(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, _, _ := newTestService(t, ctx)
	janFeb := func(edit func(r *dto.WaterfallRequest)) *dto.WaterfallRequest {
		r := &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"}
		edit(r)
		return r
	}

	tests := []struct {
		name       string
		req        *dto.WaterfallRequest
		wantTotals string
		wantItems  []string
		wantTotal  int
	}{
		{
			name:       "actual by customer",
			req:        janFeb(func(r *dto.WaterfallRequest) {}),
			wantTotals: "2026-01:230/0/0,2026-02:200/0/0",
			wantItems: []string{
				"cust_acme|2026-01:200/0/0,2026-02:100/0/0",
				"cust_beta|2026-01:0/0/0,2026-02:100/0/0",
				"cust_gamma|2026-01:30/0/0,2026-02:0/0/0",
			},
			wantTotal: 3,
		},
		{
			name:       "by contract: a line without one collects under the null row",
			req:        janFeb(func(r *dto.WaterfallRequest) { r.GroupBy = types.RevenueReportGroupByContract }),
			wantTotals: "2026-01:230/0/0,2026-02:200/0/0",
			wantItems: []string{
				"|2026-01:30/0/0,2026-02:0/0/0",
				"contract_1|2026-01:200/0/0,2026-02:100/0/0",
				"contract_2|2026-01:0/0/0,2026-02:100/0/0",
			},
			wantTotal: 3,
		},
		{
			name:       "by POB",
			req:        janFeb(func(r *dto.WaterfallRequest) { r.GroupBy = types.RevenueReportGroupByPOB }),
			wantTotals: "2026-01:230/0/0,2026-02:200/0/0",
			wantItems: []string{
				"|2026-01:30/0/0,2026-02:0/0/0",
				"pob_annual|2026-01:100/0/0,2026-02:100/0/0",
				"pob_beta|2026-01:0/0/0,2026-02:100/0/0",
				"pob_monthly|2026-01:100/0/0,2026-02:0/0/0",
			},
			wantTotal: 4,
		},
		{
			name:       "filtered by customer",
			req:        janFeb(func(r *dto.WaterfallRequest) { r.CustomerIDs = []string{"cust_beta"} }),
			wantTotals: "2026-01:0/0/0,2026-02:100/0/0",
			wantItems:  []string{"cust_beta|2026-01:0/0/0,2026-02:100/0/0"},
			wantTotal:  1,
		},
		{
			name:       "paging keeps totals over every row",
			req:        janFeb(func(r *dto.WaterfallRequest) { r.Limit, r.Offset = 1, 1 }),
			wantTotals: "2026-01:230/0/0,2026-02:200/0/0",
			wantItems:  []string{"cust_beta|2026-01:0/0/0,2026-02:100/0/0"},
			wantTotal:  3,
		},
		{
			name: "months without revenue",
			req: janFeb(func(r *dto.WaterfallRequest) {
				r.PeriodStart, r.PeriodEnd = "2026-03", "2026-04"
			}),
			wantTotals: "2026-03:0/0/0,2026-04:0/0/0",
			wantItems:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.GetWaterfall(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.wantTotals, cellsString(resp.Totals))
			require.Equal(t, tt.wantItems, lo.Map(resp.Items, func(r *dto.WaterfallRow, _ int) string {
				key := lo.FromPtr(lo.CoalesceOrEmpty(r.CustomerID, r.ContractID, r.POBID))
				return key + "|" + cellsString(r.Cells)
			}))
			require.Equal(t, tt.wantTotal, resp.Pagination.Total)
		})
	}
}

func TestGetWaterfallValidation(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, _, _ := newTestService(t, ctx)

	tests := []struct {
		name string
		req  *dto.WaterfallRequest
	}{
		{name: "missing period_end", req: &dto.WaterfallRequest{PeriodStart: "2026-01", Currency: "usd"}},
		{name: "missing currency", req: &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02"}},
		{name: "end before start", req: &dto.WaterfallRequest{PeriodStart: "2026-02", PeriodEnd: "2026-01", Currency: "usd"}},
		{name: "37 months", req: &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2029-01", Currency: "usd"}},
		{name: "unknown group_by", req: &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd", GroupBy: "plan_id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetWaterfall(ctx, tt.req)
			require.True(t, ierr.IsValidation(err), err)
		})
	}

	_, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2028-12", Currency: "usd"})
	require.NoError(t, err, "36 months is allowed")
}

// cellsString renders cells as period:actual/pending/scheduled, comma-separated.
func cellsString(cells []*dto.WaterfallCell) string {
	return strings.Join(lo.Map(cells, func(c *dto.WaterfallCell, _ int) string {
		return c.Period + ":" + c.Revenue.Actual.String() + "/" + c.Revenue.Pending.String() + "/" + c.Revenue.Scheduled.String()
	}), ",")
}

func TestGetWaterfallDetails(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, _, _ := newTestService(t, ctx)
	openMonth := time.Now().UTC().Format("2006-01")

	// balances renders cells as period:deferred/unbilled, "-" when empty.
	balances := func(cells []*dto.WaterfallCell) string {
		return strings.Join(lo.Map(cells, func(c *dto.WaterfallCell, _ int) string {
			show := func(d *decimal.Decimal) string {
				return lo.TernaryF(d == nil, func() string { return "-" }, func() string { return d.String() })
			}
			return c.Period + ":" + show(c.Deferred) + "/" + show(c.Unbilled)
		}), ",")
	}

	t.Run("closing balances per customer", func(t *testing.T) {
		resp, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd", Details: true})
		require.NoError(t, err)
		require.Equal(t, "2026-01:1100/0,2026-02:1000/0", balances(resp.Totals))
		require.Equal(t, []string{
			"cust_acme|2026-01:1100/0,2026-02:1000/0",
			"cust_beta|2026-01:0/0,2026-02:0/0",
			"cust_gamma|2026-01:0/0,2026-02:0/0",
		}, lo.Map(resp.Items, func(r *dto.WaterfallRow, _ int) string { return lo.FromPtr(r.CustomerID) + "|" + balances(r.Cells) }))
	})

	t.Run("without details, no balances", func(t *testing.T) {
		resp, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"})
		require.NoError(t, err)
		require.Equal(t, "2026-01:-/-,2026-02:-/-", balances(resp.Totals))
	})

	t.Run("the open month has no balances", func(t *testing.T) {
		resp, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{PeriodStart: "2026-02", PeriodEnd: openMonth, Currency: "usd", Details: true})
		require.NoError(t, err)
		require.Equal(t, "2026-02:1000/0", balances(resp.Totals[:1]))
		require.Equal(t, openMonth+":-/-", balances(resp.Totals[len(resp.Totals)-1:]))
	})
}

func TestGetWaterfallPending(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, recognizer, contracts := newTestService(t, ctx)

	addContract := func(id, customerID, currency string, status types.ContractStatus) {
		c := domainContract.NewBuilder(nil).WithID(id).WithCustomerID(customerID).WithCurrency(currency).
			WithContractStatus(status).WithStatus(types.StatusPublished).Build()
		a := domainContract.NewAssociationBuilder(nil).WithID("assoc_" + id).WithEntityType(types.RevRecEntityTypeSubscription).
			WithEntityID("subs_" + id).WithStatus(types.StatusPublished).Build()
		require.NoError(t, contracts.CreateWithAssociation(ctx, c, a))
	}
	addContract("contract_1", "cust_acme", "usd", types.ContractStatusActive)
	addContract("contract_2", "cust_beta", "usd", types.ContractStatusEnded)
	addContract("contract_eur", "cust_acme", "eur", types.ContractStatusActive)
	addContract("contract_draft", "cust_acme", "usd", types.ContractStatusDraft)

	// previewed builds a dry-run entry dated day with one revenue line (negative = debit) and its counter line.
	previewed := func(eventType types.JournalEventType, customerID, contractID, pobID string, day time.Time, amount int64) *domainJournal.Entry {
		direction, counter := types.JournalDirectionCredit, types.JournalDirectionDebit
		if amount < 0 {
			direction, counter, amount = counter, direction, -amount
		}
		account := lo.Ternary(eventType == types.JournalEventRevenueEarned, types.LedgerAccountRevenue, types.LedgerAccountUnbilledReceivable)
		return domainJournal.NewBuilder(nil).WithEventType(eventType).WithBusinessDate(day).WithCustomerID(customerID).WithContractID(contractID).
			WithLines([]*domainJournal.Line{
				domainJournal.NewLineBuilder(nil).WithAccount(account).WithDirection(direction).WithAmount(decimal.NewFromInt(amount)).WithPOBID(pobID).Build(),
				domainJournal.NewLineBuilder(nil).WithAccount(types.LedgerAccountDeferredRevenue).WithDirection(counter).WithAmount(decimal.NewFromInt(amount)).WithPOBID(pobID).Build(),
			}).Build()
	}
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	recognizer.previews["contract_1"] = []*domainJournal.Entry{
		previewed(types.JournalEventRevenueEarned, "cust_acme", "contract_1", "pob_annual", day(time.February, 28), 40),
		previewed(types.JournalEventRevenueOffset, "cust_acme", "contract_1", "pob_annual", day(time.February, 28), 40),
		previewed(types.JournalEventRevenueEarned, "cust_acme", "contract_1", "pob_monthly", day(time.February, 28), -5),
		previewed(types.JournalEventRevenueEarned, "cust_acme", "contract_1", "pob_annual", day(time.March, 14), 45),
	}
	recognizer.previews["contract_2"] = []*domainJournal.Entry{
		previewed(types.JournalEventRevenueEarned, "cust_beta", "contract_2", "pob_beta", day(time.February, 28), 10),
	}
	recognizer.previews["contract_eur"] = []*domainJournal.Entry{
		previewed(types.JournalEventRevenueEarned, "cust_acme", "contract_eur", "pob_eur", day(time.February, 28), 999),
	}

	tests := []struct {
		name       string
		req        *dto.WaterfallRequest
		wantItems  []string
		wantCalled []string
	}{
		{
			name: "revenue lines of earned entries land in their month; offsets, other currencies and out-of-range months are left out",
			req:  &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"},
			wantItems: []string{
				"cust_acme|2026-01:200/0/0,2026-02:100/35/0",
				"cust_beta|2026-01:0/0/0,2026-02:100/10/0",
				"cust_gamma|2026-01:30/0/0,2026-02:0/0/0",
			},
			wantCalled: []string{"contract_1", "contract_2"},
		},
		{
			name: "by POB, filtered",
			req: &dto.WaterfallRequest{PeriodStart: "2026-02", PeriodEnd: "2026-03", Currency: "usd",
				GroupBy: types.RevenueReportGroupByPOB, POBIDs: []string{"pob_annual"}},
			wantItems:  []string{"pob_annual|2026-02:100/40/0,2026-03:0/45/0"},
			wantCalled: []string{"contract_1", "contract_2"},
		},
		{
			name:       "only the filtered customer's contracts are previewed",
			req:        &dto.WaterfallRequest{PeriodStart: "2026-02", PeriodEnd: "2026-02", Currency: "usd", CustomerIDs: []string{"cust_beta"}},
			wantItems:  []string{"cust_beta|2026-02:100/10/0"},
			wantCalled: []string{"contract_2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recognizer.called = nil
			resp, err := svc.GetWaterfall(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.wantItems, lo.Map(resp.Items, func(r *dto.WaterfallRow, _ int) string {
				return lo.FromPtr(lo.CoalesceOrEmpty(r.CustomerID, r.ContractID, r.POBID)) + "|" + cellsString(r.Cells)
			}))
			require.ElementsMatch(t, tt.wantCalled, recognizer.called)
		})
	}
}

func TestGetWaterfallScheduled(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	svc, recognizer, contracts := newTestService(t, ctx)
	c := domainContract.NewBuilder(nil).WithID("contract_1").WithCustomerID("cust_acme").WithCurrency("usd").
		WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	a := domainContract.NewAssociationBuilder(nil).WithID("assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).
		WithEntityID("subs_1").WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, a))

	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextMonth := thisMonth.AddDate(0, 1, 0)
	earned := func(day time.Time, amount string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).WithEventType(types.JournalEventRevenueEarned).WithBusinessDate(day).
			WithCustomerID("cust_acme").WithContractID("contract_1").
			WithLines([]*domainJournal.Line{
				domainJournal.NewLineBuilder(nil).WithAccount(types.LedgerAccountRevenue).WithDirection(types.JournalDirectionCredit).
					WithAmount(decimal.RequireFromString(amount)).WithPOBID("pob_annual").Build(),
				domainJournal.NewLineBuilder(nil).WithAccount(types.LedgerAccountUnbilledReceivable).WithDirection(types.JournalDirectionDebit).
					WithAmount(decimal.RequireFromString(amount)).WithPOBID("pob_annual").Build(),
			}).Build()
	}
	// As on Mar 15: the open month to date now, the full months by the range's end.
	recognizer.previews["contract_1"] = []*domainJournal.Entry{earned(thisMonth, "45.16")}
	recognizer.schedules["contract_1"] = []*domainJournal.Entry{earned(thisMonth.AddDate(0, 1, -1), "100"), earned(nextMonth.AddDate(0, 1, -1), "100")}

	resp, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{
		PeriodStart: thisMonth.Format("2006-01"), PeriodEnd: nextMonth.Format("2006-01"), Currency: "usd", CustomerIDs: []string{"cust_acme"},
	})
	require.NoError(t, err)

	this, next := thisMonth.Format("2006-01"), nextMonth.Format("2006-01")
	require.Equal(t, []string{"cust_acme|" + this + ":0/45.16/54.84," + next + ":0/0/100"}, lo.Map(resp.Items, func(r *dto.WaterfallRow, _ int) string {
		return lo.FromPtr(r.CustomerID) + "|" + cellsString(r.Cells)
	}))
	require.Equal(t, "100", resp.Items[0].Cells[0].Revenue.Total.String(), "pending + scheduled is the month's revenue")
	require.Len(t, recognizer.asOfs, 2)
	require.Equal(t, nextMonth.AddDate(0, 1, 0).Add(12*time.Hour), recognizer.asOfs[1], "the schedule runs at noon UTC on the 1st after the range")

	t.Run("a range of ended months runs no schedule", func(t *testing.T) {
		recognizer.asOfs = nil
		_, err := svc.GetWaterfall(ctx, &dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"})
		require.NoError(t, err)
		require.Len(t, recognizer.asOfs, 1)
	})
}

// e2eStack is the reports over a real journal service and in-memory stores: cust_1 (UTC) with subs_1 under contract_1
// (usd), one POB per subscription line item.
type e2eStack struct {
	t        *testing.T
	ctx      context.Context
	journal  journalsvc.JournalService
	reports  RevenueReportService
	store    *testutil.InMemoryJournalStore
	invoices *testutil.InMemoryInvoiceStore
	taxes    *testutil.InMemoryTaxAppliedStore
}

// e2ePOB is a POB on subscription line item sli, amortized by method from start.
type e2ePOB struct {
	id, sli string
	method  types.AmortizationMethod
	start   time.Time
}

func newE2EStack(t *testing.T, pobs ...e2ePOB) *e2eStack {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_e2e")
	periodStore := testutil.NewInMemoryAccountingPeriodStore()
	store := testutil.NewInMemoryJournalStore(periodStore)
	contracts := testutil.NewInMemoryContractStore()
	pobStore := testutil.NewInMemoryPOBStore(contracts)
	subs := testutil.NewInMemorySubscriptionStore()
	customers := testutil.NewInMemoryCustomerStore()
	invoices := testutil.NewInMemoryInvoiceStore()
	taxes := testutil.NewInMemoryTaxAppliedStore()

	require.NoError(t, subs.Create(ctx, &subscription.Subscription{ID: "subs_1", Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx)}))
	require.NoError(t, customers.Create(ctx, &customer.Customer{ID: "cust_1", Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx)}))
	c := domainContract.NewBuilder(nil).WithID("contract_1").WithCustomerID("cust_1").WithCurrency("usd").
		WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, domainContract.NewAssociationBuilder(nil).
		WithID("contract_assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID("subs_1").WithStatus(types.StatusPublished).Build()))
	for _, p := range pobs {
		require.NoError(t, pobStore.CreateWithAssociation(ctx, domainPOB.NewBuilder(nil).WithID(p.id).WithContractID(c.ID()).
			WithRecognitionMethod(types.RecognitionMethodStraightLine).WithAmortizationMethod(p.method).
			WithServiceStart(p.start).WithPOBStatus(types.PerformanceObligationStatusActive).WithStatus(types.StatusPublished).Build(),
			domainPOB.NewAssociationBuilder(nil).WithID("assoc_"+p.id).WithEntityType(types.RevRecEntityTypeSubscriptionLineItem).
				WithEntityID(p.sli).WithStatus(types.StatusPublished).Build()))
	}

	pobService := pobsvc.NewPerformanceObligationService(pobStore, contracts, subs, invoices, taxes, testutil.NewInMemoryRevenueFactStore())
	journal := journalsvc.NewJournalService(testutil.NewMockPostgresClient(nil), store, accountingperiodsvc.NewAccountingPeriodService(periodStore),
		pobService, pobStore, contractsvc.NewContractService(contracts), contracts, subs, customers, invoices, testutil.NewInMemoryCreditNoteStore(),
		testutil.NewInMemoryPaymentStore(), testutil.NewInMemoryRefundStore(), taxes, testutil.NewInMemorySettingsStore())
	return &e2eStack{t: t, ctx: ctx, journal: journal, reports: NewRevenueReportService(store, journal, contracts),
		store: store, invoices: invoices, taxes: taxes}
}

// invoice finalizes and posts a subs_1 invoice with one line on sli over [start, end); edit adjusts it before saving.
func (e *e2eStack) invoice(id, sli string, amount int64, finalized, start, end time.Time, edit func(inv *invoice.Invoice)) *invoice.Invoice {
	inv := &invoice.Invoice{ID: id, CustomerID: "cust_1", SubscriptionID: lo.ToPtr("subs_1"), InvoiceType: types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized, Currency: "usd", Subtotal: decimal.NewFromInt(amount), Total: decimal.NewFromInt(amount),
		FinalizedAt: lo.ToPtr(finalized), EnvironmentID: types.GetEnvironmentID(e.ctx), BaseModel: types.GetDefaultBaseModel(e.ctx),
		LineItems: []*invoice.InvoiceLineItem{{ID: id + "_line", SubscriptionLineItemID: lo.ToPtr(sli), Amount: decimal.NewFromInt(amount),
			PeriodStart: lo.ToPtr(start), PeriodEnd: lo.ToPtr(end), EnvironmentID: types.GetEnvironmentID(e.ctx), BaseModel: types.GetDefaultBaseModel(e.ctx)}}}
	if edit != nil {
		edit(inv)
	}
	require.NoError(e.t, e.invoices.CreateWithLineItems(e.ctx, inv))
	require.NoError(e.t, e.journal.PostInvoice(e.ctx, inv))
	return inv
}

func (e *e2eStack) recognize(asOf time.Time) {
	_, err := e.journal.Recognize(e.ctx, journalsvc.RecognizeRequest{ContractID: "contract_1", AsOf: asOf})
	require.NoError(e.t, err)
}

// waterfall returns the rows as key|cells and the totals, checking that no dry run saved anything.
func (e *e2eStack) waterfall(req *dto.WaterfallRequest) ([]string, string, *dto.WaterfallResponse) {
	before, err := e.store.Count(e.ctx, journalRepo.NewFilter())
	require.NoError(e.t, err)
	req.Currency = "usd"
	resp, err := e.reports.GetWaterfall(e.ctx, req)
	require.NoError(e.t, err)
	after, err := e.store.Count(e.ctx, journalRepo.NewFilter())
	require.NoError(e.t, err)
	require.Equal(e.t, before, after, "a waterfall posts nothing")
	return lo.Map(resp.Items, func(r *dto.WaterfallRow, _ int) string {
		return lo.FromPtr(lo.CoalesceOrEmpty(r.CustomerID, r.ContractID, r.POBID)) + "|" + cellsString(r.Cells)
	}), cellsString(resp.Totals), resp
}

func utc(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }

// TestReportsERDExample is the ERD's Mar 15 example: $1,200 annual prepaid and $100/month recurring in advance, both
// period even from Jan 1.
func TestReportsERDExample(t *testing.T) {
	e := newE2EStack(t,
		e2ePOB{id: "pob_annual", sli: "sli_annual", method: types.AmortizationPeriodEven, start: utc(time.January, 1)},
		e2ePOB{id: "pob_monthly", sli: "sli_monthly", method: types.AmortizationPeriodEven, start: utc(time.January, 1)})
	e.invoice("inv_annual", "sli_annual", 1200, utc(time.January, 1), utc(time.January, 1), utc(time.January, 1).AddDate(1, 0, 0), nil)
	for m := time.January; m <= time.March; m++ {
		e.invoice("inv_monthly_"+m.String(), "sli_monthly", 100, utc(m, 1), utc(m, 1), utc(m+1, 1), nil)
	}
	e.recognize(utc(time.March, 1).Add(2 * time.Hour))

	months := func(from, through time.Month, cell string) []string {
		var cells []string
		for m := from; m <= through; m++ {
			cells = append(cells, utc(m, 1).Format("2006-01")+":"+cell)
		}
		return cells
	}
	join := func(parts ...[]string) string { return strings.Join(lo.Flatten(parts), ",") }

	t.Run("waterfall by POB on Mar 15: January and February actual, March pending and scheduled, the annual scheduled to December", func(t *testing.T) {
		items, totals, _ := e.waterfall(&dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-12",
			GroupBy: types.RevenueReportGroupByPOB, AsOf: lo.ToPtr(utc(time.March, 15))})
		require.Equal(t, []string{
			"pob_annual|" + join(months(time.January, time.February, "100/0/0"), months(time.March, time.March, "0/45.16/54.84"), months(time.April, time.December, "0/0/100")),
			"pob_monthly|" + join(months(time.January, time.February, "100/0/0"), months(time.March, time.March, "0/45.16/54.84"), months(time.April, time.December, "0/0/0")),
		}, items)
		require.Equal(t, join(months(time.January, time.February, "200/0/0"), months(time.March, time.March, "0/90.32/109.68"), months(time.April, time.December, "0/0/100")), totals)
	})

	t.Run("details: deferred 1,100 then 1,000; March has not ended", func(t *testing.T) {
		_, _, resp := e.waterfall(&dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-03", Details: true, AsOf: lo.ToPtr(utc(time.March, 15))})
		require.Equal(t, "1100", resp.Totals[0].Deferred.String())
		require.Equal(t, "1000", resp.Totals[1].Deferred.String())
		require.Equal(t, "0", resp.Totals[1].Unbilled.String())
		require.Nil(t, resp.Totals[2].Deferred)
	})

	t.Run("deferred rollforward for February: 1,100 + 100 invoiced − 200 earned = 1,000", func(t *testing.T) {
		resp, err := e.reports.GetRollforward(e.ctx, &dto.RollforwardRequest{PeriodStart: "2026-01", PeriodEnd: "2026-02", Currency: "usd"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"2026-01|deferred_revenue||||0|earned:-200,invoiced:1300|1100",
			"2026-02|deferred_revenue||||1100|earned:-200,invoiced:100|1000",
		}, lo.Map(resp.Totals, func(r *dto.RollforwardRow, _ int) string { return rowString(r) }))
	})

	t.Run("Σ actual equals the revenue balance", func(t *testing.T) {
		_, _, resp := e.waterfall(&dto.WaterfallRequest{PeriodStart: "2026-01", PeriodEnd: "2026-12", AsOf: lo.ToPtr(utc(time.March, 15))})
		actual := decimal.Sum(decimal.Zero, lo.Map(resp.Totals, func(c *dto.WaterfallCell, _ int) decimal.Decimal { return c.Revenue.Actual })...)
		balance, err := e.reports.GetRollforward(e.ctx, &dto.RollforwardRequest{PeriodStart: "2026-02", Currency: "usd",
			Accounts: []types.LedgerAccountKey{types.LedgerAccountRevenue}})
		require.NoError(t, err)
		require.Equal(t, balance.Totals[0].Closing.String(), actual.String())
	})

	t.Run("once March is recognized, its pending + scheduled is its actual", func(t *testing.T) {
		e.recognize(utc(time.April, 1).Add(2 * time.Hour))
		_, totals, _ := e.waterfall(&dto.WaterfallRequest{PeriodStart: "2026-03", PeriodEnd: "2026-04", AsOf: lo.ToPtr(utc(time.April, 1))})
		require.Equal(t, "2026-03:200/0/0,2026-04:0/0/100", totals, "April: the annual is scheduled, the monthly has no April invoice")
	})
}

// TestReportsStripeWaterfallExamples runs Stripe's revenue waterfall examples: a 31 USD line for Jul 21 – Aug 20,
// finalized Jul 14, recognized daily.
func TestReportsStripeWaterfallExamples(t *testing.T) {
	jul21, aug21, jul14 := utc(time.July, 21), utc(time.August, 21), utc(time.July, 14)
	report := func(e *e2eStack) []string {
		items, _, _ := e.waterfall(&dto.WaterfallRequest{PeriodStart: "2026-06", PeriodEnd: "2026-09", AsOf: lo.ToPtr(utc(time.October, 1))})
		return items
	}
	stack := func() *e2eStack {
		return newE2EStack(t, e2ePOB{id: "pob_1", sli: "sli_1", method: types.AmortizationDaily, start: jul21})
	}
	stripe := []string{"cust_1|2026-06:0/0/0,2026-07:11/0/0,2026-08:20/0/0,2026-09:0/0/0"}

	t.Run("simple invoice: 11 in July, 20 in August", func(t *testing.T) {
		e := stack()
		e.invoice("inv_1", "sli_1", 31, jul14, jul21, aug21, nil)
		e.recognize(utc(time.October, 1).Add(2 * time.Hour))
		require.Equal(t, stripe, report(e))
	})

	t.Run("revenue excludes inclusive tax: 35 with 4 tax is 31", func(t *testing.T) {
		e := stack()
		require.NoError(t, e.taxes.Create(e.ctx, &taxapplied.TaxApplied{ID: "ta_1", EntityType: types.TaxRateEntityTypeInvoice, EntityID: "inv_1",
			TaxBehavior: types.TaxBehaviorInclusive, TaxAmount: decimal.NewFromInt(4), BaseModel: types.GetDefaultBaseModel(e.ctx)}))
		e.invoice("inv_1", "sli_1", 35, jul14, jul21, aug21, func(inv *invoice.Invoice) { inv.TotalTax = decimal.NewFromInt(4) })
		e.recognize(utc(time.October, 1).Add(2 * time.Hour))
		require.Equal(t, stripe, report(e))
	})

	t.Run("the amount due is not revenue: 10 paid from customer balance is still 31", func(t *testing.T) {
		e := stack()
		e.invoice("inv_1", "sli_1", 31, jul14, jul21, aug21, func(inv *invoice.Invoice) {
			inv.TotalPrepaidCreditsApplied, inv.Total = decimal.NewFromInt(10), decimal.NewFromInt(21)
		})
		e.recognize(utc(time.October, 1).Add(2 * time.Hour))
		require.Equal(t, stripe, report(e))
	})

	t.Run("voided Sep 12: without period close, July and August are restated to 0", func(t *testing.T) {
		// Stripe closes periods, so it shows 11, 20 and −31 in September; with close (8.28) the corrections would land
		// in the first open month the same way.
		e := stack()
		inv := e.invoice("inv_1", "sli_1", 31, jul14, jul21, aug21, nil)
		e.recognize(utc(time.September, 1).Add(2 * time.Hour))
		require.Equal(t, stripe, report(e))

		inv.InvoiceStatus, inv.VoidedAt = types.InvoiceStatusVoided, lo.ToPtr(utc(time.September, 12))
		require.NoError(t, e.invoices.Update(e.ctx, inv))
		require.NoError(t, e.journal.PostInvoice(e.ctx, inv))
		e.recognize(utc(time.October, 1).Add(2 * time.Hour))
		require.Equal(t, []string{}, report(e))

		resp, err := e.reports.GetRollforward(e.ctx, &dto.RollforwardRequest{PeriodStart: "2026-09", Currency: "usd",
			Accounts: []types.LedgerAccountKey{types.LedgerAccountDeferredRevenue, types.LedgerAccountUnbilledReceivable, types.LedgerAccountRevenue}})
		require.NoError(t, err)
		require.True(t, lo.EveryBy(resp.Totals, func(r *dto.RollforwardRow) bool { return r.Closing.IsZero() }), "every balance nets to zero")
	})
}
