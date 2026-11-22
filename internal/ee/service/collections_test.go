package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

var boardNow = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

func openReceivable(id, customerID, currency string, remaining int64, due *time.Time) collections.Receivable {
	return collections.Receivable{
		Source:          types.ReceivableSourceFlexprice,
		ExternalID:      id,
		Number:          "INV-" + id,
		Customer:        collections.CustomerRef{ID: customerID, Name: "name " + customerID},
		Currency:        currency,
		AmountTotal:     decimal.NewFromInt(remaining + 10),
		AmountRemaining: decimal.NewFromInt(remaining),
		DueDate:         due,
		Status:          types.ReceivableStatusOpen,
	}
}

func columnByKey(t *testing.T, resp *dto.CollectionsBoardResponse, key types.CollectionsColumnKey) dto.CollectionsBoardColumn {
	t.Helper()
	col, ok := lo.Find(resp.Columns, func(c dto.CollectionsBoardColumn) bool { return c.Key == key })
	require.True(t, ok, "column %s missing", key)
	return col
}

func cardIDs(col dto.CollectionsBoardColumn) []string {
	return lo.Map(col.Cards, func(c dto.CollectionsBoardCard, _ int) string { return c.ExternalID })
}

func cardCustomers(col dto.CollectionsBoardColumn) []string {
	return lo.Map(col.Cards, func(c dto.CollectionsBoardCard, _ int) string { return c.CustomerID + "/" + c.Currency })
}

type collectionsFixture struct {
	invoices  *testutil.InMemoryInvoiceStore
	customers *testutil.InMemoryCustomerStore
	reminders *testutil.InMemoryReminderDecisionStore
	source    collections.ReceivableSource
	svc       CollectionsService
}

func newCollectionsFixture() *collectionsFixture {
	f := &collectionsFixture{
		invoices:  testutil.NewInMemoryInvoiceStore(),
		customers: testutil.NewInMemoryCustomerStore(),
		reminders: testutil.NewInMemoryReminderDecisionStore(),
	}
	f.source = testutil.NewInMemoryReceivableSource(f.invoices, f.customers, f.reminders)
	f.svc = NewCollectionsService(ServiceParams{CustomerRepo: f.customers, ReminderDecisionRepo: f.reminders, ReceivableSource: f.source})
	return f
}

func collectionsCtx(tenant, env string) context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, tenant)
	return context.WithValue(ctx, types.CtxEnvironmentID, env)
}

func (f *collectionsFixture) addCustomer(t *testing.T, ctx context.Context, id, name string) {
	t.Helper()
	f.addCustomerWithEmail(t, ctx, id, name, "")
}

func (f *collectionsFixture) addCustomerWithEmail(t *testing.T, ctx context.Context, id, name, email string) {
	t.Helper()
	require.NoError(t, f.customers.Create(ctx, &customer.Customer{ID: id, Name: name, Email: email, BaseModel: types.GetDefaultBaseModel(ctx)}))
}

func (f *collectionsFixture) addInvoice(t *testing.T, ctx context.Context, id string, mutate func(*invoice.Invoice)) {
	t.Helper()
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      "cust_1",
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(100),
		AmountRemaining: decimal.NewFromInt(100),
		InvoiceNumber:   lo.ToPtr("INV-" + id),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	if mutate != nil {
		mutate(inv)
	}
	require.NoError(t, f.invoices.Create(ctx, inv))
}

// due adds an open invoice due at the given offset from now.
func (f *collectionsFixture) due(t *testing.T, ctx context.Context, id, customerID string, remaining int64, offset time.Duration) {
	t.Helper()
	at := clock.Now().Add(offset)
	f.addInvoice(t, ctx, id, func(i *invoice.Invoice) {
		i.CustomerID, i.DueDate = customerID, &at
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(remaining), decimal.NewFromInt(remaining)
	})
}

func (f *collectionsFixture) paid(t *testing.T, ctx context.Context, id, customerID string, total int64, ago time.Duration) {
	t.Helper()
	at := clock.Now().Add(-ago)
	f.addInvoice(t, ctx, id, func(i *invoice.Invoice) {
		i.CustomerID, i.PaymentStatus, i.PaidAt = customerID, types.PaymentStatusSucceeded, &at
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(total), decimal.Zero
	})
}

// board asks for up to 200 cards per column unless the test sets a limit; boardMeta sends the request as given.
func (f *collectionsFixture) board(t *testing.T, ctx context.Context, req dto.CollectionsBoardRequest) *dto.CollectionsBoardResponse {
	t.Helper()
	if req.Limit == 0 && req.CardLimit == 0 {
		req.Limit = dto.CollectionsBoardMaxCardLimit
	}
	return f.boardMeta(t, ctx, req)
}

func (f *collectionsFixture) boardMeta(t *testing.T, ctx context.Context, req dto.CollectionsBoardRequest) *dto.CollectionsBoardResponse {
	t.Helper()
	resp, err := f.svc.GetBoard(ctx, &req)
	require.NoError(t, err)
	return resp
}

const day = 24 * time.Hour

func TestBoard_FiveColumnsInOrderWithMetaAndNoNulls(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	resp := newCollectionsFixture().board(t, ctx, dto.CollectionsBoardRequest{})
	require.Equal(t, []types.CollectionsColumnKey{
		types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing,
		types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek,
	}, lo.Map(resp.Columns, func(c dto.CollectionsBoardColumn, _ int) types.CollectionsColumnKey { return c.Key }))
	require.Equal(t, "Overdue, waiting on you", resp.Columns[1].Title)
	require.Equal(t, "Reminder drafted, needs your approval", resp.Columns[1].Hint)
	require.Equal(t, "Reminder approved", resp.Columns[2].Title)
	require.Equal(t, "Approved, waiting to be sent", resp.Columns[2].Hint)
	require.NotNil(t, resp.AvailableCurrencies)
	for _, col := range resp.Columns {
		require.NotNil(t, col.Cards)
		require.NotNil(t, col.Totals)
		require.NotEmpty(t, col.Hint)
		require.Zero(t, col.Count)
	}
}

func TestBoard_ColumnRules(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.due(t, ctx, "later_today", "cust_1", 100, 3*time.Hour)
	f.due(t, ctx, "tomorrow", "cust_1", 50, day)
	f.addInvoice(t, ctx, "no_due_date", func(i *invoice.Invoice) { i.DueDate = nil })
	f.due(t, ctx, "just_overdue", "cust_1", 70, -time.Minute)
	f.due(t, ctx, "old_overdue", "cust_1", 30, -8*day)
	f.addInvoice(t, ctx, "partial", func(i *invoice.Invoice) {
		i.DueDate = lo.ToPtr(clock.Now().Add(-2 * day))
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(100), decimal.NewFromInt(40)
		i.CustomerID = "cust_partial"
	})
	f.addInvoice(t, ctx, "zero_remaining", func(i *invoice.Invoice) {
		i.DueDate, i.AmountRemaining = lo.ToPtr(clock.Now().Add(-day)), decimal.Zero
	})
	for name, status := range map[string]types.InvoiceStatus{"draft": types.InvoiceStatusDraft, "voided": types.InvoiceStatusVoided, "skipped": types.InvoiceStatusSkipped} {
		f.addInvoice(t, ctx, name, func(i *invoice.Invoice) { i.InvoiceStatus, i.DueDate = status, lo.ToPtr(clock.Now().Add(-day)) })
	}
	f.addInvoice(t, ctx, "deleted", func(i *invoice.Invoice) {
		i.BaseModel.Status, i.DueDate = types.StatusDeleted, lo.ToPtr(clock.Now().Add(-day))
	})

	resp := f.board(t, ctx, dto.CollectionsBoardRequest{})
	notDue := columnByKey(t, resp, types.CollectionsColumnNotDue)
	require.Equal(t, []string{"later_today", "tomorrow", "no_due_date"}, cardIDs(notDue), "due date asc, no due date last")
	require.Equal(t, 0, *notDue.Cards[0].DueInDays)
	require.Equal(t, 1, *notDue.Cards[1].DueInDays)
	require.Nil(t, notDue.Cards[2].DueInDays)
	require.Equal(t, 3, notDue.Count)

	overdue := columnByKey(t, resp, types.CollectionsColumnOverdue)
	require.Equal(t, []string{"cust_1/usd", "cust_partial/usd"}, cardCustomers(overdue), "amount desc")
	require.Equal(t, 3, overdue.InvoiceCount)
	require.True(t, decimal.NewFromInt(100).Equal(overdue.Cards[0].Amount), "70 + 30 for the customer")
	require.Equal(t, 8, *overdue.Cards[0].DaysOverdue)
	require.Empty(t, overdue.Cards[0].ExternalID, "a group of two has no single external id")
	require.True(t, decimal.NewFromInt(40).Equal(overdue.Cards[1].Amount), "a partly paid invoice counts its remaining balance")
	require.Equal(t, "partial", overdue.Cards[1].ExternalID)
}

func TestBoard_DueExactlyAtAsOfIsNotOverdue(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	due := boardNow
	f.addInvoice(t, ctx, "exact", func(i *invoice.Invoice) { i.DueDate = &due })
	f.addInvoice(t, ctx, "micro_before", func(i *invoice.Invoice) { i.DueDate = lo.ToPtr(boardNow.Add(-time.Microsecond)) })

	q := collections.Query{AsOf: boardNow}
	notDue, _, err := f.source.ListInvoices(ctx, q, types.CollectionsColumnNotDue, 0, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"exact"}, lo.Map(notDue, func(r collections.Receivable, _ int) string { return r.ExternalID }))
	groups, _, err := f.source.ListGroups(ctx, q, collections.GroupFilter{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, 1, groups[0].InvoiceCount)
	require.Equal(t, "micro_before", groups[0].FirstExternalID)
}

func TestBoard_PaidWindow(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.paid(t, ctx, "recent", "cust_1", 10, time.Hour)
	f.paid(t, ctx, "six_days", "cust_1", 20, 6*day)
	f.paid(t, ctx, "eight_days", "cust_1", 30, 8*day)
	f.paid(t, ctx, "future", "cust_1", 40, -time.Hour)

	col := columnByKey(t, f.board(t, ctx, dto.CollectionsBoardRequest{}), types.CollectionsColumnPaidThisWeek)
	require.Equal(t, []string{"recent", "six_days"}, cardIDs(col), "paid_at desc")
	require.True(t, decimal.NewFromInt(30).Equal(col.Totals[0].Amount))
	require.NotNil(t, col.Cards[0].PaidAt)
	require.True(t, decimal.NewFromInt(10).Equal(col.Cards[0].Amount), "paid cards show the invoice total")

	f = newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	at := boardNow
	f.addInvoice(t, ctx, "edge_in", func(i *invoice.Invoice) {
		i.PaymentStatus, i.PaidAt, i.AmountRemaining = types.PaymentStatusSucceeded, lo.ToPtr(at.Add(-collections.PaidWindow)), decimal.Zero
	})
	f.addInvoice(t, ctx, "edge_out", func(i *invoice.Invoice) {
		i.PaymentStatus, i.PaidAt, i.AmountRemaining = types.PaymentStatusSucceeded, lo.ToPtr(at.Add(-collections.PaidWindow-time.Microsecond)), decimal.Zero
	})
	rows, _, err := f.source.ListInvoices(ctx, collections.Query{AsOf: boardNow}, types.CollectionsColumnPaidThisWeek, 0, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"edge_in"}, lo.Map(rows, func(r collections.Receivable, _ int) string { return r.ExternalID }), "exactly 7 days is inside, a microsecond more is outside")
}

func TestBoard_MultiCurrencyGroupsAndTotalsNeverMix(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.due(t, ctx, "usd1", "cust_1", 100, -day)
	f.addInvoice(t, ctx, "eur1", func(i *invoice.Invoice) {
		i.Currency, i.DueDate = "EUR", lo.ToPtr(clock.Now().Add(-day))
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(40), decimal.NewFromInt(40)
	})

	resp := f.board(t, ctx, dto.CollectionsBoardRequest{})
	overdue := columnByKey(t, resp, types.CollectionsColumnOverdue)
	require.Equal(t, 2, overdue.Count, "one customer in two currencies is two cards")
	require.Equal(t, []string{"eur", "usd"}, lo.Map(overdue.Totals, func(t dto.CollectionsCurrencyTotal, _ int) string { return t.Currency }))
	require.True(t, decimal.NewFromInt(40).Equal(overdue.Totals[0].Amount))
	require.True(t, decimal.NewFromInt(100).Equal(overdue.Totals[1].Amount))
	require.Equal(t, []string{"eur", "usd"}, resp.AvailableCurrencies)
}

func TestBoard_SplitsOverdueByReminderDecision(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	names := []string{"none", "pending", "approved", "held", "skipped", "stale", "other_ccy"}
	for i, id := range names {
		f.addCustomer(t, ctx, "c_"+id, id)
		f.due(t, ctx, "inv_"+id, "c_"+id, int64(10*(i+1)), -day)
	}
	decide := func(id string, status types.ReminderStatus, reason string, invoiceIDs ...string) {
		require.NoError(t, f.reminders.Create(ctx, &collections.ReminderDecision{
			ID: "rdec_" + id, CustomerID: "c_" + id, Currency: "usd", Status: status, HoldReason: reason,
			InvoiceIDs: invoiceIDs, BaseModel: types.GetDefaultBaseModel(ctx),
		}))
	}
	decide("pending", types.ReminderStatusPending, "", "inv_pending")
	decide("approved", types.ReminderStatusApproved, "", "inv_approved")
	decide("held", types.ReminderStatusHeld, "Promised Friday", "inv_held")
	decide("skipped", types.ReminderStatusSkipped, "", "inv_skipped")
	decide("stale", types.ReminderStatusHeld, "old promise", "inv_gone")
	require.NoError(t, f.reminders.Create(ctx, &collections.ReminderDecision{
		ID: "rdec_gone", CustomerID: "c_nobody", Currency: "usd", Status: types.ReminderStatusApproved,
		InvoiceIDs: []string{"inv_x"}, BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	f.addInvoice(t, ctx, "inv_other_eur", func(i *invoice.Invoice) {
		i.CustomerID, i.Currency, i.DueDate = "c_approved", "eur", lo.ToPtr(clock.Now().Add(-day))
	})

	resp := f.board(t, ctx, dto.CollectionsBoardRequest{})
	overdue := columnByKey(t, resp, types.CollectionsColumnOverdue)
	require.ElementsMatch(t, []string{"c_none/usd", "c_pending/usd", "c_stale/usd", "c_other_ccy/usd", "c_approved/eur"}, cardCustomers(overdue),
		"no decision, pending, a decision on invoices that are gone, and a currency the decision does not cover")
	require.Equal(t, []string{"c_approved/usd"}, cardCustomers(columnByKey(t, resp, types.CollectionsColumnChasing)))
	held := columnByKey(t, resp, types.CollectionsColumnHeld)
	require.Equal(t, []string{"c_skipped/usd", "c_held/usd"}, cardCustomers(held), "held and skipped, amount desc")

	none, _ := lo.Find(overdue.Cards, func(c dto.CollectionsBoardCard) bool { return c.CustomerID == "c_none" })
	require.Equal(t, "rmd_usd_c_none", none.ReminderID)
	require.Equal(t, types.ReminderStatusPending, none.ReminderStatus)
	require.Empty(t, none.ReminderHoldReason)
	heldCard, _ := lo.Find(held.Cards, func(c dto.CollectionsBoardCard) bool { return c.CustomerID == "c_held" })
	require.Equal(t, "Promised Friday", heldCard.ReminderHoldReason)
	skippedCard, _ := lo.Find(held.Cards, func(c dto.CollectionsBoardCard) bool { return c.CustomerID == "c_skipped" })
	require.Equal(t, types.ReminderStatusSkipped, skippedCard.ReminderStatus)
	require.Empty(t, skippedCard.ReminderHoldReason)
}

func cardKeys(cards []dto.CollectionsBoardCard) []string {
	return lo.Map(cards, func(c dto.CollectionsBoardCard, _ int) string {
		return c.CustomerID + "|" + c.ExternalID + "|" + c.Currency
	})
}

// walkColumn pages a column to its end and returns every card in order and the number of pages read.
func (f *collectionsFixture) walkColumn(t *testing.T, ctx context.Context, key types.CollectionsColumnKey, req dto.CollectionsBoardRequest) ([]dto.CollectionsBoardCard, int) {
	t.Helper()
	var all []dto.CollectionsBoardCard
	for page := 1; page <= 1000; page++ {
		req.Page = page
		resp, err := f.svc.GetColumn(ctx, string(key), &req)
		require.NoError(t, err)
		require.Equal(t, page, resp.Page)
		all = append(all, resp.Cards...)
		if !resp.HasMore {
			return all, page
		}
		require.Len(t, resp.Cards, req.PageLimit(), "a page that has more is full")
	}
	t.Fatal("never reached the end of the column")
	return nil, 0
}

func TestBoard_PagesWalkEveryColumnToTheEndLikeOneBigPage(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	for i := 0; i < 22; i++ {
		id := fmt.Sprintf("c%02d", i)
		f.addCustomer(t, ctx, "cust_"+id, id)
		f.due(t, ctx, "nd"+id, "cust_"+id, 10, 2*day)
		f.due(t, ctx, "od"+id, "cust_"+id, 10, -2*day)
		f.paid(t, ctx, "pd"+id, "cust_"+id, 10, time.Hour)
	}
	f.addInvoice(t, ctx, "nd_nulldue", func(i *invoice.Invoice) { i.DueDate = nil })
	f.addInvoice(t, ctx, "nd_nulldue2", func(i *invoice.Invoice) { i.DueDate = nil })
	meta := f.boardMeta(t, ctx, dto.CollectionsBoardRequest{})

	for _, key := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnPaidThisWeek} {
		everything, pages := f.walkColumn(t, ctx, key, dto.CollectionsBoardRequest{Limit: 200, AsOf: meta.AsOf.Format(time.RFC3339Nano)})
		require.Equal(t, 1, pages)
		want := columnByKey(t, meta, key).Count
		require.Len(t, everything, want, "%s: the big page holds every card", key)

		for _, size := range []int{1, 2, 3, 4, 7, 15} {
			got, pages := f.walkColumn(t, ctx, key, dto.CollectionsBoardRequest{Limit: size, AsOf: meta.AsOf.Format(time.RFC3339Nano)})
			require.Equal(t, cardKeys(everything), cardKeys(got), "%s at page size %d", key, size)
			require.Len(t, lo.Uniq(cardKeys(got)), want, "no card twice")
			require.Equal(t, (want+size-1)/size, pages, "%s at page size %d reads exactly as many pages as needed", key, size)
		}
	}

	last, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 200})
	require.NoError(t, err)
	ids := lo.Map(last.Cards, func(c dto.CollectionsBoardCard, _ int) string { return c.ExternalID })
	require.Equal(t, []string{"nd_nulldue", "nd_nulldue2"}, ids[len(ids)-2:], "no due date sorts last, ties by id")
	require.False(t, last.HasMore)
}

func TestBoard_ExactFitAndPastTheEnd(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 6; i++ {
		f.due(t, ctx, fmt.Sprintf("a%d", i), "cust_1", 10, time.Duration(i+1)*day)
	}
	page, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 3, Page: 2})
	require.NoError(t, err)
	require.Len(t, page.Cards, 3)
	require.False(t, page.HasMore, "the last page fits exactly")
	past, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 3, Page: 3})
	require.NoError(t, err)
	require.NotNil(t, past.Cards)
	require.Empty(t, past.Cards)
	require.False(t, past.HasMore)
	require.Equal(t, 3, past.Page)
}

func TestBoard_PageRules(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.due(t, ctx, "a0", "cust_1", 10, day)

	for name, req := range map[string]dto.CollectionsBoardRequest{
		"negative page":           {Page: -1},
		"limit above the maximum": {Limit: 201},
		"negative limit":          {Limit: -1},
		"too deep with default":   {Page: 334},
		"too deep with limit 10":  {Page: 501, Limit: 10},
		"too deep with limit 200": {Page: 26, Limit: 200},
		"malformed as_of":         {AsOf: "yesterday"},
		"as_of date only":         {AsOf: "2026-11-20"},
		"as_of too old":           {AsOf: time.Now().Add(-25 * time.Hour).Format(time.RFC3339Nano)},
		"as_of in the future":     {AsOf: time.Now().Add(time.Hour).Format(time.RFC3339Nano)},
		"as_of on a later page":   {Page: 2, AsOf: "nope"},
	} {
		_, err := f.svc.GetColumn(ctx, "not_due", &req)
		requireKind(t, err, ierrValidation)
		_ = name
	}
	for name, req := range map[string]dto.CollectionsBoardRequest{
		"the deepest allowed page": {Page: 333},
		"limit 200 page 25":        {Page: 25, Limit: 200},
		"page zero is page one":    {Page: 0},
		"a little clock skew":      {AsOf: time.Now().Add(20 * time.Second).Format(time.RFC3339Nano)},
	} {
		_, err := f.svc.GetColumn(ctx, "not_due", &req)
		require.NoError(t, err, name)
	}
	_, err := f.svc.GetColumn(ctx, "nope", &dto.CollectionsBoardRequest{})
	requireKind(t, err, ierrNotFound)
	require.Equal(t, 1, f.boardMeta(t, ctx, dto.CollectionsBoardRequest{Page: 7}).Columns[0].Count, "the board ignores page")
}

func TestBoard_AsOfOnEveryPageKeepsTheBoundariesStill(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 4; i++ {
		f.due(t, ctx, fmt.Sprintf("a%d", i), "cust_1", 10, 30*time.Minute+time.Duration(i)*time.Minute)
	}
	meta := f.boardMeta(t, ctx, dto.CollectionsBoardRequest{})
	asOf := meta.AsOf.Format(time.RFC3339Nano)
	later := meta.AsOf.Add(2 * time.Hour).Format(time.RFC3339Nano)
	require.Equal(t, 4, columnByKey(t, meta, types.CollectionsColumnNotDue).Count)

	for page := 1; page <= 2; page++ {
		resp, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 2, Page: page, AsOf: asOf})
		require.NoError(t, err)
		require.True(t, resp.AsOf.Equal(meta.AsOf), "page %d is read at the board clock", page)
		require.Len(t, resp.Cards, 2)
	}
	// Judged two hours later, the same invoices would already be overdue.
	_, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{AsOf: later})
	requireKind(t, err, ierrValidation)
}

func TestBoard_ARowPaidBetweenPagesCanShiftTheNextPage(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 5; i++ {
		f.due(t, ctx, fmt.Sprintf("a%d", i), "cust_1", 10, time.Duration(i+1)*day)
	}
	first, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"a0", "a1"}, cardIDs(dto.CollectionsBoardColumn{Cards: first.Cards}))

	inv, err := f.invoices.Get(ctx, "a0")
	require.NoError(t, err)
	inv.PaymentStatus, inv.AmountRemaining = types.PaymentStatusSucceeded, decimal.Zero
	require.NoError(t, f.invoices.Update(ctx, inv))

	second, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 2, Page: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"a3", "a4"}, cardIDs(dto.CollectionsBoardColumn{Cards: second.Cards}),
		"a2 slid onto page one and is skipped: the accepted cost of page numbers, so the client reloads when the board changes")
}

func TestBoard_Filters(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomerWithEmail(t, ctx, "c_acme", "Acme Robotics", "ap@acme.example")
	f.addCustomerWithEmail(t, ctx, "c_beta", "Beta 100% Labs", "billing@beta.example")
	f.addCustomerWithEmail(t, ctx, "c_gamma", "Gamma_Co", "ap@gamma.example")
	f.addCustomerWithEmail(t, ctx, "c_delta", "Delta Ltd", "ap@delta.example")
	f.due(t, ctx, "100", "c_acme", 100, -5*day)
	f.due(t, ctx, "101", "c_acme", 200, -5*day)
	f.addInvoice(t, ctx, "200", func(i *invoice.Invoice) {
		i.CustomerID, i.Currency, i.DueDate = "c_beta", "eur", lo.ToPtr(clock.Now().Add(5*day))
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(50), decimal.NewFromInt(50)
	})
	f.due(t, ctx, "300", "c_gamma", 70, 5*day)
	f.paid(t, ctx, "099", "c_acme", 300, day)
	f.addInvoice(t, ctx, "400", func(i *invoice.Invoice) {
		i.CustomerID, i.Currency, i.PaymentStatus, i.PaidAt = "c_delta", "GBP", types.PaymentStatusSucceeded, lo.ToPtr(clock.Now().Add(-2*day))
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(40), decimal.Zero
	})

	type want map[types.CollectionsColumnKey][]string
	tests := []struct {
		name string
		req  dto.CollectionsBoardRequest
		want want
	}{
		{"no filter", dto.CollectionsBoardRequest{}, want{types.CollectionsColumnNotDue: {"200", "300"}, types.CollectionsColumnOverdue: {""}, types.CollectionsColumnPaidThisWeek: {"099", "400"}}},
		{"customer name", dto.CollectionsBoardRequest{Q: "acme"}, want{types.CollectionsColumnOverdue: {""}, types.CollectionsColumnPaidThisWeek: {"099"}}},
		{"case-insensitive", dto.CollectionsBoardRequest{Q: " ROBOTICS "}, want{types.CollectionsColumnOverdue: {""}, types.CollectionsColumnPaidThisWeek: {"099"}}},
		{"email", dto.CollectionsBoardRequest{Q: "billing@beta"}, want{types.CollectionsColumnNotDue: {"200"}}},
		{"invoice number", dto.CollectionsBoardRequest{Q: "inv-300"}, want{types.CollectionsColumnNotDue: {"300"}}},
		{"number narrows a group to that invoice", dto.CollectionsBoardRequest{Q: "INV-101"}, want{types.CollectionsColumnOverdue: {"101"}}},
		{"currency", dto.CollectionsBoardRequest{Currency: "EUR"}, want{types.CollectionsColumnNotDue: {"200"}}},
		{"currency is normalized", dto.CollectionsBoardRequest{Currency: " gbp "}, want{types.CollectionsColumnPaidThisWeek: {"400"}}},
		{"both", dto.CollectionsBoardRequest{Q: "acme", Currency: "usd"}, want{types.CollectionsColumnOverdue: {""}, types.CollectionsColumnPaidThisWeek: {"099"}}},
		{"both, nothing matches", dto.CollectionsBoardRequest{Q: "acme", Currency: "eur"}, nil},
		{"no match", dto.CollectionsBoardRequest{Q: "zzz"}, nil},
		{"unknown currency", dto.CollectionsBoardRequest{Currency: "xyz"}, nil},
		{"percent is literal", dto.CollectionsBoardRequest{Q: "100%"}, want{types.CollectionsColumnNotDue: {"200"}}},
		{"underscore is literal", dto.CollectionsBoardRequest{Q: "gamma_"}, want{types.CollectionsColumnNotDue: {"300"}}},
		{"percent alone", dto.CollectionsBoardRequest{Q: "%"}, want{types.CollectionsColumnNotDue: {"200"}}},
		{"underscore alone", dto.CollectionsBoardRequest{Q: "_"}, want{types.CollectionsColumnNotDue: {"300"}}},
		{"blank q is no filter", dto.CollectionsBoardRequest{Q: "   "}, want{types.CollectionsColumnNotDue: {"200", "300"}, types.CollectionsColumnOverdue: {""}, types.CollectionsColumnPaidThisWeek: {"099", "400"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.board(t, ctx, tc.req)
			for _, col := range resp.Columns {
				require.ElementsMatch(t, tc.want[col.Key], cardIDs(col), "column %s", col.Key)
				require.Equal(t, len(tc.want[col.Key]), col.Count, "count follows the filter in %s", col.Key)
				require.Equal(t, len(tc.want[col.Key]), len(col.Cards))
			}
			require.Equal(t, []string{"eur", "gbp", "usd"}, resp.AvailableCurrencies, "available currencies ignore the filters")
		})
	}

	filtered := columnByKey(t, f.board(t, ctx, dto.CollectionsBoardRequest{Q: "acme"}), types.CollectionsColumnOverdue)
	require.True(t, decimal.NewFromInt(300).Equal(filtered.Totals[0].Amount))
	require.Equal(t, 2, filtered.InvoiceCount)
}

func TestBoard_TotalsAndHasMoreFollowTheFilterWhilePagesAreCapped(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 5; i++ {
		f.due(t, ctx, fmt.Sprintf("a%d", i), "cust_1", 10, day)
	}
	f.addInvoice(t, ctx, "b0", func(i *invoice.Invoice) {
		i.DueDate, i.InvoiceNumber, i.AmountRemaining = lo.ToPtr(clock.Now().Add(day)), lo.ToPtr("B-0"), decimal.NewFromInt(1000)
	})

	col := columnByKey(t, f.board(t, ctx, dto.CollectionsBoardRequest{Limit: 2, Q: "inv-a"}), types.CollectionsColumnNotDue)
	require.Equal(t, 5, col.Count)
	require.Len(t, col.Cards, 2)
	require.True(t, col.HasMore)
	require.True(t, decimal.NewFromInt(50).Equal(col.Totals[0].Amount), "total excludes the filtered-out invoice")
	exact := columnByKey(t, f.board(t, ctx, dto.CollectionsBoardRequest{Limit: 5, Q: "inv-a"}), types.CollectionsColumnNotDue)
	require.False(t, exact.HasMore)
}

func TestBoard_TenantAndEnvironmentIsolation(t *testing.T) {
	ctxA := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctxA, "cust_1", "Acme")
	f.due(t, ctxA, "mine", "cust_1", 100, -day)
	f.due(t, collectionsCtx("tenant_b", "env_a"), "other_tenant", "cust_1", 100, -day)
	f.due(t, collectionsCtx("tenant_a", "env_b"), "other_env", "cust_1", 100, -day)
	require.NoError(t, f.reminders.Create(collectionsCtx("tenant_b", "env_a"), &collections.ReminderDecision{
		ID: "rdec_b", CustomerID: "cust_1", Currency: "usd", Status: types.ReminderStatusApproved,
		InvoiceIDs: []string{"mine"}, BaseModel: types.GetDefaultBaseModel(collectionsCtx("tenant_b", "env_a")),
	}))

	resp := f.board(t, ctxA, dto.CollectionsBoardRequest{})
	require.Equal(t, 1, columnByKey(t, resp, types.CollectionsColumnOverdue).Count, "another tenant's decision does not apply")
	require.Zero(t, columnByKey(t, resp, types.CollectionsColumnChasing).Count)
	require.Equal(t, 1, columnByKey(t, resp, types.CollectionsColumnOverdue).InvoiceCount)
	empty := f.board(t, collectionsCtx("tenant_c", "env_a"), dto.CollectionsBoardRequest{})
	for _, col := range empty.Columns {
		require.Zero(t, col.Count)
	}
	require.Empty(t, empty.AvailableCurrencies)
}

func TestBoard_WritesNothing(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.due(t, ctx, "a1", "cust_1", 100, -day)
	f.board(t, ctx, dto.CollectionsBoardRequest{})
	_, err := f.svc.GetColumn(ctx, "overdue", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Zero(t, f.reminders.Writes())
}

func TestCollectionsBoardRequest_Validate(t *testing.T) {
	req := dto.CollectionsBoardRequest{}
	require.NoError(t, req.Validate())
	require.Zero(t, req.BoardCardLimit(), "the board returns no cards unless asked")
	require.Equal(t, dto.CollectionsColumnDefaultPageSize, req.PageLimit())
	require.Equal(t, 15, req.PageLimit(), "a column page defaults to 15 cards")

	alias := dto.CollectionsBoardRequest{CardLimit: 7}
	require.NoError(t, alias.Validate())
	require.Equal(t, 7, alias.BoardCardLimit(), "card_limit is an alias of limit")
	require.Equal(t, 7, alias.PageLimit())
	both := dto.CollectionsBoardRequest{CardLimit: 7, Limit: 9}
	require.NoError(t, both.Validate())
	require.Equal(t, 9, both.BoardCardLimit(), "limit wins")

	for _, bad := range []dto.CollectionsBoardRequest{{Limit: -1}, {CardLimit: -1}, {Limit: 201}, {CardLimit: 201}} {
		require.Error(t, bad.Validate())
	}
	for _, ok := range []dto.CollectionsBoardRequest{{Limit: 0}, {Limit: 200}} {
		require.NoError(t, ok.Validate())
	}

	trimmed := dto.CollectionsBoardRequest{Q: "  acme  ", Currency: " USD "}
	require.NoError(t, trimmed.Validate())
	require.Equal(t, "acme", trimmed.Q)
	require.Equal(t, "USD", trimmed.Currency)
	long := dto.CollectionsBoardRequest{Q: strings.Repeat("é", 150)}
	require.NoError(t, long.Validate())
	require.Len(t, []rune(long.Q), dto.CollectionsBoardMaxQueryLength)
}

func TestBoard_ColumnPageDefaultsToFifteenCards(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 20; i++ {
		f.due(t, ctx, fmt.Sprintf("a%02d", i), "cust_1", 10, time.Duration(i+1)*day)
	}

	first, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Len(t, first.Cards, 15)
	require.True(t, first.HasMore)
	require.Equal(t, 1, first.Page)

	rest, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Page: 2})
	require.NoError(t, err)
	require.Len(t, rest.Cards, 5)
	require.False(t, rest.HasMore)

	exact, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Limit: 20})
	require.NoError(t, err)
	require.Len(t, exact.Cards, 20)
	require.False(t, exact.HasMore)
}

func TestBoard_LimitZeroReturnsCountsAndTotalsOnly(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 3; i++ {
		f.due(t, ctx, fmt.Sprintf("nd%d", i), "cust_1", 10, day)
		f.due(t, ctx, fmt.Sprintf("od%d", i), fmt.Sprintf("cust_o%d", i), 20, -day)
		f.addCustomer(t, ctx, fmt.Sprintf("cust_o%d", i), "o")
	}
	f.paid(t, ctx, "pd", "cust_1", 7, time.Hour)

	full := f.board(t, ctx, dto.CollectionsBoardRequest{})
	for _, req := range []dto.CollectionsBoardRequest{{}, {Limit: 0}, {CardLimit: 0}} {
		meta := f.boardMeta(t, ctx, req)
		require.Equal(t, full.AvailableCurrencies, meta.AvailableCurrencies)
		for _, col := range meta.Columns {
			want := columnByKey(t, full, col.Key)
			require.Equal(t, want.Count, col.Count, col.Key)
			require.Equal(t, want.InvoiceCount, col.InvoiceCount, col.Key)
			require.Equal(t, want.Totals, col.Totals, col.Key)
			require.NotNil(t, col.Cards)
			require.Empty(t, col.Cards, "no cards at limit 0 in %s", col.Key)
			require.Equal(t, col.Count > 0, col.HasMore, "has_more says the first page still has to be fetched: %s", col.Key)
		}
	}
	require.Equal(t, 3, columnByKey(t, f.boardMeta(t, ctx, dto.CollectionsBoardRequest{}), types.CollectionsColumnNotDue).Count)
	require.False(t, columnByKey(t, f.boardMeta(t, ctx, dto.CollectionsBoardRequest{}), types.CollectionsColumnChasing).HasMore, "an empty column has nothing to fetch")

	withCards := f.boardMeta(t, ctx, dto.CollectionsBoardRequest{Limit: 2})
	notDue := columnByKey(t, withCards, types.CollectionsColumnNotDue)
	require.Len(t, notDue.Cards, 2)
	require.True(t, notDue.HasMore)
	exact := columnByKey(t, f.boardMeta(t, ctx, dto.CollectionsBoardRequest{Limit: 3}), types.CollectionsColumnNotDue)
	require.False(t, exact.HasMore, "a positive limit behaves as before")
	require.Zero(t, f.reminders.Writes())
}

func TestBoard_ColumnFirstPageTakesTheBoardClock(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newCollectionsFixture()
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.due(t, ctx, "soon", "cust_1", 10, 2*time.Hour)

	meta := f.boardMeta(t, ctx, dto.CollectionsBoardRequest{})
	asOf := meta.AsOf.Format(time.RFC3339Nano)
	page, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{AsOf: asOf})
	require.NoError(t, err)
	require.True(t, page.AsOf.Equal(meta.AsOf), "the first page uses the clock the counts were read at")
	require.Equal(t, []string{"soon"}, lo.Map(page.Cards, func(c dto.CollectionsBoardCard, _ int) string { return c.ExternalID }))

	// A page pinned to an earlier clock judges "not due" at that clock.
	f.due(t, ctx, "due_in_half_an_hour", "cust_1", 10, 30*time.Minute)
	pinned := meta.AsOf.Add(-time.Hour)
	older, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{AsOf: pinned.Format(time.RFC3339Nano)})
	require.NoError(t, err)
	require.True(t, older.AsOf.Equal(pinned.Truncate(time.Microsecond)))
	require.ElementsMatch(t, []string{"soon", "due_in_half_an_hour"}, lo.Map(older.Cards, func(c dto.CollectionsBoardCard, _ int) string { return c.ExternalID }))

	for name, asOf := range map[string]string{
		"malformed":     "yesterday",
		"date only":     "2026-11-20",
		"too old":       time.Now().Add(-25 * time.Hour).Format(time.RFC3339Nano),
		"in the future": time.Now().Add(time.Hour).Format(time.RFC3339Nano),
	} {
		_, err := f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{AsOf: asOf})
		requireKind(t, err, ierrValidation)
		_ = name
	}
	skew := time.Now().Add(20 * time.Second).Format(time.RFC3339Nano)
	_, err = f.svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{AsOf: skew})
	require.NoError(t, err, "a little clock skew is allowed")
}

var (
	ierrValidation = ierr.ErrValidation
	ierrNotFound   = ierr.ErrNotFound
)
