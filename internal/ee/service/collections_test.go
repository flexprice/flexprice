package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

// fakeBoard is a canned CollectionsRepository that records how it was called; the real queries are tested against Postgres.
type fakeBoard struct {
	board   *collections.Board
	rows    []collections.Row
	hasMore bool
	summary collections.ReminderSummary

	query         collections.Query
	filter        collections.RowFilter
	offset, limit int
}

func (f *fakeBoard) Board(_ context.Context, q collections.Query, limit int) (*collections.Board, error) {
	f.query, f.limit = q, limit
	return f.board, nil
}

func (f *fakeBoard) ListRows(_ context.Context, q collections.Query, filter collections.RowFilter, offset, limit int) ([]collections.Row, bool, error) {
	f.query, f.filter, f.offset, f.limit = q, filter, offset, limit
	return f.rows, f.hasMore, nil
}

func (f *fakeBoard) SummarizeReminders(context.Context, time.Time) (collections.ReminderSummary, error) {
	return f.summary, nil
}

func boardFixture(t *testing.T) (CollectionsService, *fakeBoard, context.Context) {
	t.Helper()
	ctx := reminderCtxFor("tenant_a", "env_a")
	customers := testutil.NewInMemoryCustomerStore()
	require.NoError(t, customers.Create(ctx, &customer.Customer{ID: "cust_1", Name: "Acme", BaseModel: types.GetDefaultBaseModel(ctx)}))
	fake := &fakeBoard{}
	return NewCollectionsService(ServiceParams{CustomerRepo: customers, CollectionsRepo: fake}), fake, ctx
}

func rowDue(id string, remaining int64, due *time.Time, rem *collections.InvoiceReminder) collections.Row {
	return *collections.NewRowBuilder(&collections.Row{InvoiceID: id}).
		WithInvoice("INV-"+id, "cust_1", "usd").
		WithAmounts(decimal.NewFromInt(remaining+10), decimal.NewFromInt(remaining)).
		WithDates(due, nil).
		WithReminder(rem).
		Build()
}

func fullBoard(counts map[types.CollectionsColumnKey]int) *collections.Board {
	board := collections.NewBoardBuilder(nil).WithAvailableCurrencies([]string{"eur", "usd"})
	for _, key := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek} {
		summary := collections.NewColumnSummaryBuilder(nil).
			WithCount(counts[key]).
			WithTotals([]collections.CurrencyTotal{{Currency: "usd", Amount: decimal.NewFromInt(int64(counts[key]) * 10)}}).
			Build()
		board = board.WithColumn(key, *collections.NewBoardColumnBuilder(nil).WithSummary(*summary).Build())
	}
	return board.Build()
}

func TestBoard_LimitZeroReturnsCountsAndTotalsOnly(t *testing.T) {
	svc, fake, ctx := boardFixture(t)
	fake.board = fullBoard(map[types.CollectionsColumnKey]int{types.CollectionsColumnNotDue: 3, types.CollectionsColumnOverdue: 7})

	for _, req := range []dto.CollectionsBoardRequest{{}, {Limit: 0}, {CardLimit: 0}} {
		resp, err := svc.GetBoard(ctx, &req)
		require.NoError(t, err)
		require.Zero(t, fake.limit, "the repository is asked for no rows")
		require.Equal(t, []string{"eur", "usd"}, resp.AvailableCurrencies)
		require.Equal(t, []types.CollectionsColumnKey{
			types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing,
			types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek,
		}, lo.Map(resp.Columns, func(c dto.CollectionsBoardColumn, _ int) types.CollectionsColumnKey { return c.Key }))
		for _, col := range resp.Columns {
			require.NotNil(t, col.Cards)
			require.Empty(t, col.Cards)
			require.NotEmpty(t, col.Hint)
			require.Equal(t, col.Count > 0, col.HasMore, "has_more says the first page still has to be fetched: %s", col.Key)
		}
		require.Equal(t, 7, resp.Columns[1].Count)
		require.True(t, decimal.NewFromInt(70).Equal(resp.Columns[1].Totals[0].Amount))
	}
	require.Equal(t, "Overdue, waiting on you", boardColumns[1].title)
	require.Equal(t, "Reminder approved", boardColumns[2].title)
}

func TestBoard_PositiveLimitReturnsTheFirstPage(t *testing.T) {
	svc, fake, ctx := boardFixture(t)
	due := clock.Now().Add(-5 * day)
	board := fullBoard(map[types.CollectionsColumnKey]int{types.CollectionsColumnOverdue: 3})
	col := board.Columns[types.CollectionsColumnOverdue]
	held := collections.NewInvoiceReminderBuilder(nil).
		WithStatus(types.ReminderStatusHeld, "Promised Friday", nil).
		WithOriginal(types.ReminderToneFriendly, "", "").
		WithText(types.ReminderToneFinal, "", "").
		Build()
	rows := []collections.Row{rowDue("a1", 100, &due, nil), rowDue("a2", 50, &due, held)}
	board.Columns[types.CollectionsColumnOverdue] = *collections.NewBoardColumnBuilder(&col).WithRows(rows, true).Build()
	fake.board = board

	resp, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 2, Q: " acme ", Currency: " USD "})
	require.NoError(t, err)
	require.Equal(t, 2, fake.limit)
	require.Equal(t, "acme", fake.query.Text)
	require.Equal(t, "usd", fake.query.Currency)
	overdue := resp.Columns[1]
	require.Len(t, overdue.Cards, 2)
	require.True(t, overdue.HasMore)
	first, second := overdue.Cards[0], overdue.Cards[1]
	require.Equal(t, "a1", first.InvoiceID)
	require.Equal(t, "INV-a1", first.InvoiceNumber)
	require.Equal(t, "Acme", first.CustomerName)
	require.Equal(t, 5, *first.DaysOverdue)
	require.Equal(t, types.ReminderStatusPending, first.ReminderStatus, "no reminder row means pending")
	require.Equal(t, types.ReminderToneFriendly, first.ReminderTone, "the tone is derived from the age")
	require.Empty(t, first.ReminderStatusReason)
	require.Equal(t, types.ReminderStatusHeld, second.ReminderStatus)
	require.Equal(t, types.ReminderToneFinal, second.ReminderTone, "an edited or approved reminder shows its own tone")
	require.Equal(t, "Promised Friday", second.ReminderStatusReason)
	require.True(t, decimal.NewFromInt(100).Equal(first.Amount), "the amount is the remaining balance")
}

func TestBoard_NotDueAndPaidCardsHaveNoReminderFields(t *testing.T) {
	svc, fake, ctx := boardFixture(t)
	soon, paidAt := utcDay(clock.Now()).Add(2*day+12*time.Hour), clock.Now().Add(-time.Hour)
	board := fullBoard(nil)
	nd := board.Columns[types.CollectionsColumnNotDue]
	nd.Rows = []collections.Row{rowDue("n1", 10, &soon, nil), rowDue("n2", 10, nil, nil)}
	board.Columns[types.CollectionsColumnNotDue] = nd
	pd := board.Columns[types.CollectionsColumnPaidThisWeek]
	paidRow := collections.NewRowBuilder(&collections.Row{InvoiceID: "p1"}).WithInvoice("INV-p1", "cust_1", "usd").
		WithAmounts(decimal.NewFromInt(10), decimal.Zero).WithDates(nil, &paidAt).Build()
	pd.Rows = []collections.Row{*paidRow}
	board.Columns[types.CollectionsColumnPaidThisWeek] = pd
	fake.board = board

	resp, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 5})
	require.NoError(t, err)
	notDue := resp.Columns[0].Cards
	require.Equal(t, 2, *notDue[0].DueInDays)
	require.NotNil(t, notDue[0].DueDate)
	require.Nil(t, notDue[1].DueInDays)
	for _, c := range notDue {
		require.Empty(t, c.ReminderStatus)
		require.Nil(t, c.DaysOverdue)
	}
	paid := resp.Columns[4].Cards[0]
	require.NotNil(t, paid.PaidAt)
	require.True(t, decimal.NewFromInt(10).Equal(paid.Amount), "paid cards show the invoice total")
	require.Nil(t, paid.DueDate)
	require.Empty(t, paid.ReminderStatus)
}

func TestBoard_ColumnPages(t *testing.T) {
	svc, fake, ctx := boardFixture(t)
	due := clock.Now().Add(-5 * day)
	fake.rows = []collections.Row{rowDue("a1", 100, &due, nil)}
	fake.hasMore = true

	resp, err := svc.GetColumn(ctx, "held", &dto.CollectionsBoardRequest{Page: 3, Limit: 4, Q: "x", Currency: "EUR"})
	require.NoError(t, err)
	require.Equal(t, 8, fake.offset)
	require.Equal(t, 4, fake.limit)
	require.Equal(t, types.CollectionsColumnHeld, fake.filter.Column)
	require.Equal(t, []types.ReminderStatus{types.ReminderStatusHeld, types.ReminderStatusSkipped}, fake.filter.Statuses)
	require.Equal(t, "eur", fake.query.Currency)
	require.Equal(t, 3, resp.Page)
	require.True(t, resp.HasMore)
	require.Len(t, resp.Cards, 1)

	_, err = svc.GetColumn(ctx, "overdue", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Equal(t, 15, fake.limit, "a column page defaults to 15 cards")
	require.Equal(t, 0, fake.offset)
	require.Equal(t, []types.ReminderStatus{types.ReminderStatusPending}, fake.filter.Statuses)
	_, err = svc.GetColumn(ctx, "chasing", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Equal(t, []types.ReminderStatus{types.ReminderStatusApproved}, fake.filter.Statuses)
	_, err = svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Equal(t, types.CollectionsColumnNotDue, fake.filter.Column)
}

func TestBoard_ColumnPageRules(t *testing.T) {
	svc, fake, ctx := boardFixture(t)
	for name, req := range map[string]dto.CollectionsBoardRequest{
		"negative page": {Page: -1}, "limit above the maximum": {Limit: 201}, "negative limit": {Limit: -1},
		"too deep with default": {Page: 334}, "too deep with limit 10": {Page: 501, Limit: 10}, "too deep with limit 200": {Page: 26, Limit: 200},
		"too old":                  {AsOf: lo.ToPtr(time.Now().Add(-25 * time.Hour))},
		"in the future":            {AsOf: lo.ToPtr(time.Now().Add(time.Hour))},
		"too old on a later page":  {Page: 2, AsOf: lo.ToPtr(time.Now().Add(-48 * time.Hour))},
		"page that would overflow": {Page: math.MaxInt, Limit: 200},
	} {
		_, err := svc.GetColumn(ctx, "not_due", &req)
		requireKind(t, err, ierr.ErrValidation)
		_ = name
	}
	for name, req := range map[string]dto.CollectionsBoardRequest{
		"deepest allowed page": {Page: 333}, "limit 200 page 25": {Page: 25, Limit: 200}, "page zero is page one": {Page: 0},
		"a little clock skew": {AsOf: lo.ToPtr(time.Now().Add(20 * time.Second))},
	} {
		_, err := svc.GetColumn(ctx, "not_due", &req)
		require.NoError(t, err, name)
	}
	_, err := svc.GetColumn(ctx, "nope", &dto.CollectionsBoardRequest{})
	requireKind(t, err, ierr.ErrNotFound)

	asOf := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	for _, page := range []int{1, 2, 7} {
		resp, err := svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{Page: page, AsOf: &asOf})
		require.NoError(t, err)
		require.True(t, resp.AsOf.Equal(asOf), "page %d is read at the board clock", page)
		require.True(t, fake.query.AsOf.Equal(asOf))
	}
	_, err = svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 201})
	requireKind(t, err, ierr.ErrValidation)
}

func TestCollectionsBoardRequest_Validate(t *testing.T) {
	req := dto.CollectionsBoardRequest{}
	require.NoError(t, req.Validate())
	require.Zero(t, req.BoardCardLimit(), "the board returns no cards unless asked")
	require.Equal(t, 15, req.PageLimit(), "a column page defaults to 15 cards")
	require.Equal(t, dto.CollectionsColumnDefaultPageSize, req.PageLimit())

	alias := dto.CollectionsBoardRequest{CardLimit: 7}
	require.NoError(t, alias.Validate())
	require.Equal(t, 7, alias.BoardCardLimit(), "card_limit is an alias of limit")
	both := dto.CollectionsBoardRequest{CardLimit: 7, Limit: 9}
	require.Equal(t, 9, both.BoardCardLimit(), "limit wins")

	for _, bad := range []dto.CollectionsBoardRequest{{Limit: -1}, {CardLimit: -1}, {Limit: 201}, {CardLimit: 201}, {Page: -1}} {
		require.Error(t, bad.Validate())
	}
	trimmed := dto.CollectionsBoardRequest{Q: "  acme  ", Currency: " USD "}
	require.NoError(t, trimmed.Validate())
	require.Equal(t, "acme", trimmed.Q)
	long := dto.CollectionsBoardRequest{Q: string(make([]rune, 150))}
	require.NoError(t, long.Validate())
	require.Len(t, []rune(long.Q), dto.CollectionsBoardMaxQueryLength)
}

func TestParsePageAsOf(t *testing.T) {
	now := time.Now()
	asOf, err := parsePageAsOf(nil, now)
	require.NoError(t, err)
	require.True(t, asOf.Equal(now))
	good := now.Add(-time.Hour).UTC().Truncate(time.Microsecond)
	got, err := parsePageAsOf(&good, now)
	require.NoError(t, err)
	require.True(t, got.Equal(good))
}
