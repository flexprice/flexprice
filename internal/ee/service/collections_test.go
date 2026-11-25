package service

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// boardFixture is the reminder fixture with a board service reading the same in-memory stores.
func boardFixture(t *testing.T) (CollectionsService, *reminderFixture, context.Context) {
	t.Helper()
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.example")
	return NewCollectionsService(f.params(f.reminders)), f, ctx
}

// notDue adds an open invoice due at noon UTC daysAhead days from today.
func (f *reminderFixture) notDue(t *testing.T, ctx context.Context, id string, amount int64, daysAhead int) {
	t.Helper()
	due := types.UTCDay(clock.Now()).Add(time.Duration(daysAhead)*day + 12*time.Hour)
	f.invoice(t, ctx, id, func(i *invoice.Invoice) {
		i.DueDate, i.AmountDue, i.AmountRemaining = &due, decimal.NewFromInt(amount), decimal.NewFromInt(amount)
	})
}

// paidAt adds an invoice of total in currency that was paid at the given time.
func (f *reminderFixture) paidAt(t *testing.T, ctx context.Context, id, currency string, total int64, at time.Time) {
	t.Helper()
	f.invoice(t, ctx, id, func(i *invoice.Invoice) {
		i.Currency, i.PaymentStatus, i.PaidAt = currency, types.PaymentStatusSucceeded, &at
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(total), decimal.Zero
	})
}

// dueAt adds an open overdue invoice due at the given time.
func (f *reminderFixture) dueAt(t *testing.T, ctx context.Context, id string, amount int64, due time.Time) {
	t.Helper()
	f.invoice(t, ctx, id, func(i *invoice.Invoice) {
		i.DueDate, i.AmountDue, i.AmountRemaining = &due, decimal.NewFromInt(amount), decimal.NewFromInt(amount)
	})
}

// approvedAt stores an approved reminder for the invoice, approved at the given time.
func (f *reminderFixture) approvedAt(t *testing.T, ctx context.Context, invoiceID string, at time.Time) {
	t.Helper()
	r := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID: types.GenerateUUIDWithPrefix("irem"), InvoiceID: invoiceID, BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithText(types.ReminderToneFriendly, "subject", "body").WithStatus(types.ReminderStatusApproved, "", &at).Build()
	require.NoError(t, f.reminders.Create(ctx, r))
}

func cardIDs(cards []dto.CollectionsItem) []string {
	return lo.Map(cards, func(c dto.CollectionsItem, _ int) string { return c.InvoiceID })
}

func TestBoard_LimitZeroReturnsCountsAndTotalsOnly(t *testing.T) {
	svc, f, ctx := boardFixture(t)
	for i := 0; i < 3; i++ {
		f.notDue(t, ctx, fmt.Sprintf("n%d", i), 10, 2)
	}
	for i := 0; i < 7; i++ {
		f.overdue(t, ctx, fmt.Sprintf("o%d", i), "cust_1", 10, 5)
	}
	f.paidAt(t, ctx, "p1", "EUR", 40, clock.Now().Add(-time.Hour))

	for _, req := range []dto.CollectionsBoardRequest{{}, {Limit: 0}} {
		resp, err := svc.GetBoard(ctx, &req)
		require.NoError(t, err)
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
		require.Equal(t, 3, resp.Columns[0].Count)
		require.Equal(t, 7, resp.Columns[1].Count)
		require.True(t, decimal.NewFromInt(70).Equal(resp.Columns[1].Totals[0].Amount))
		require.Equal(t, []dto.CollectionsCurrencyTotal{{Currency: "eur", Amount: decimal.NewFromInt(40)}}, resp.Columns[4].Totals, "paid shows the invoice total")
	}
	require.Equal(t, "Overdue, waiting on you", boardColumns[1].title)
	require.Equal(t, "Reminder approved", boardColumns[2].title)
}

func TestBoard_PositiveLimitReturnsTheFirstPage(t *testing.T) {
	svc, f, ctx := boardFixture(t)
	f.customer(t, ctx, "cust_2", "Beta", "ap@beta.example")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.overdue(t, ctx, "a3", "cust_1", 30, 5)
	f.overdue(t, ctx, "a4", "cust_1", 20, 5)
	f.overdue(t, ctx, "b1", "cust_2", 500, 5)
	f.invoice(t, ctx, "e1", func(i *invoice.Invoice) {
		i.Currency, i.DueDate = "eur", lo.ToPtr(types.UTCDay(clock.Now()).Add(-5*day))
	})
	f.overdue(t, ctx, "a2", "cust_1", 50, 5)
	_, err := f.svc.UpdateReminder(ctx, "a2", &dto.UpdateReminderRequest{Status: types.ReminderStatusHeld, StatusReason: "Promised Friday", Tone: lo.ToPtr("final")})
	require.NoError(t, err)

	resp, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 2, Q: " acme ", Currency: " USD "})
	require.NoError(t, err)
	overdue := resp.Columns[1]
	require.Equal(t, 3, overdue.Count, "q and currency leave out Beta and the EUR invoice")
	require.Equal(t, []string{"a1", "a3"}, cardIDs(overdue.Cards), "the largest balance first, cut at the limit")
	require.True(t, overdue.HasMore)
	first := overdue.Cards[0]
	require.Equal(t, "INV-a1", first.InvoiceNumber)
	require.Equal(t, "Acme", first.CustomerName)
	require.Equal(t, 5, *first.DaysOverdue)
	require.Equal(t, types.ReminderStatusPending, first.ReminderStatus, "no reminder row means pending")
	require.Equal(t, types.ReminderToneFriendly, first.ReminderTone, "the tone is derived from the age")
	require.Empty(t, first.ReminderStatusReason)
	require.True(t, decimal.NewFromInt(100).Equal(first.Amount), "the amount is the remaining balance")

	held := resp.Columns[3]
	require.False(t, held.HasMore)
	require.Len(t, held.Cards, 1)
	require.Equal(t, types.ReminderStatusHeld, held.Cards[0].ReminderStatus)
	require.Equal(t, types.ReminderToneFinal, held.Cards[0].ReminderTone, "an edited or approved reminder shows its own tone")
	require.Equal(t, "Promised Friday", held.Cards[0].ReminderStatusReason)
}

func TestBoard_NotDueAndPaidCardsHaveNoReminderFields(t *testing.T) {
	svc, f, ctx := boardFixture(t)
	f.notDue(t, ctx, "n1", 10, 2)
	f.invoice(t, ctx, "n2", func(i *invoice.Invoice) {
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(10), decimal.NewFromInt(10)
	})
	f.paidAt(t, ctx, "p1", "usd", 10, clock.Now().Add(-time.Hour))

	resp, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 5})
	require.NoError(t, err)
	notDue := resp.Columns[0].Cards
	require.Equal(t, []string{"n1", "n2"}, cardIDs(notDue), "by due date, an invoice without one last")
	require.NotNil(t, notDue[0].DueDate)
	require.Equal(t, types.CalendarDaysBetween(resp.AsOf, *notDue[0].DueDate), *notDue[0].DueInDays, "days are counted from the board clock")
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
	svc, f, ctx := boardFixture(t)
	for i := 0; i < 13; i++ {
		id := fmt.Sprintf("x%02d", i)
		f.invoice(t, ctx, id, func(inv *invoice.Invoice) {
			inv.Currency, inv.DueDate = "eur", lo.ToPtr(types.UTCDay(clock.Now()).Add(-5*day))
			inv.AmountDue, inv.AmountRemaining = decimal.NewFromInt(int64(100+i)), decimal.NewFromInt(int64(100+i))
		})
		status := types.ReminderStatusHeld
		reason := "wait"
		if i%2 == 1 {
			status, reason = types.ReminderStatusSkipped, ""
		}
		_, err := f.svc.UpdateReminder(ctx, id, setStatus(status, reason))
		require.NoError(t, err)
	}
	f.overdue(t, ctx, "y1", "cust_1", 999, 5)
	_, err := f.svc.UpdateReminder(ctx, "y1", setStatus(types.ReminderStatusHeld, "usd"))
	require.NoError(t, err)
	for i := 0; i < 16; i++ {
		f.overdue(t, ctx, fmt.Sprintf("p%02d", i), "cust_1", 10, 5)
	}
	f.overdue(t, ctx, "c1", "cust_1", 10, 5)
	_, err = f.svc.UpdateReminder(ctx, "c1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.notDue(t, ctx, "n1", 10, 3)

	resp, err := svc.GetColumn(ctx, "held", &dto.CollectionsBoardRequest{Page: 3, Limit: 4, Q: "x", Currency: "EUR"})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Page)
	require.Equal(t, []string{"x04", "x03", "x02", "x01"}, cardIDs(resp.Cards), "held and skipped, largest balance first, rows 9 to 12")
	require.True(t, resp.HasMore)

	overdue, err := svc.GetColumn(ctx, "overdue", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Len(t, overdue.Cards, 15, "a column page defaults to 15 cards")
	require.True(t, overdue.HasMore)
	for _, c := range overdue.Cards {
		require.Equal(t, types.ReminderStatusPending, c.ReminderStatus)
	}

	chasing, err := svc.GetColumn(ctx, "chasing", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"c1"}, cardIDs(chasing.Cards))
	require.Equal(t, types.ReminderStatusApproved, chasing.Cards[0].ReminderStatus)

	notDue, err := svc.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"n1"}, cardIDs(notDue.Cards))
}

func TestBoard_ColumnPageRules(t *testing.T) {
	svc, _, ctx := boardFixture(t)
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

	sized := dto.CollectionsBoardRequest{Limit: 9}
	require.NoError(t, sized.Validate())
	require.Equal(t, 9, sized.BoardCardLimit())
	require.Equal(t, 9, sized.PageLimit())

	for _, bad := range []dto.CollectionsBoardRequest{{Limit: -1}, {Limit: 201}, {Page: -1}} {
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

func TestCollectionsBoardRequest_DateRangeRules(t *testing.T) {
	day := func(s string) *time.Time {
		d, err := time.Parse("2006-01-02", s)
		require.NoError(t, err)
		return &d
	}

	valid := map[string]dto.CollectionsBoardRequest{
		"from only":          {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2026-08-01")}},
		"to only":            {CollectionsDateFilter: dto.CollectionsDateFilter{To: day("2026-11-24")}},
		"a single day":       {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2026-11-24"), To: day("2026-11-24")}},
		"exactly five years": {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2021-11-24"), To: day("2026-11-24")}},
		"no range":           {},
		"to earlier in the same day": {CollectionsDateFilter: dto.CollectionsDateFilter{
			From: lo.ToPtr(time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)), To: lo.ToPtr(time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)),
		}},
		"five years counted in days": {CollectionsDateFilter: dto.CollectionsDateFilter{
			From: lo.ToPtr(time.Date(2021, 11, 24, 23, 0, 0, 0, time.UTC)), To: lo.ToPtr(time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)),
		}},
	}
	for name, req := range valid {
		require.NoError(t, req.Validate(), name)
	}

	invalid := map[string]dto.CollectionsBoardRequest{
		"from after to":       {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2026-11-25"), To: day("2026-11-24")}},
		"over five years":     {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2021-11-23"), To: day("2026-11-24")}},
		"far over five years": {CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2000-01-01"), To: day("2026-11-24")}},
		"to on the day before from": {CollectionsDateFilter: dto.CollectionsDateFilter{
			From: lo.ToPtr(time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)), To: lo.ToPtr(time.Date(2026, 11, 24, 23, 0, 0, 0, time.UTC)),
		}},
	}
	for name, req := range invalid {
		requireKind(t, req.Validate(), ierr.ErrValidation)
		_ = name
	}

	empty := dto.CollectionsBoardRequest{CollectionsDateFilter: dto.CollectionsDateFilter{From: day("2026-11-01"), To: &time.Time{}}}
	require.NoError(t, empty.Validate(), "?to= with no value")
	require.Nil(t, empty.To, "an empty date is no bound")
	require.True(t, empty.From.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
}

func TestBoard_DateRangeBecomesHalfOpenUTCDays(t *testing.T) {
	svc, f, ctx := boardFixture(t)
	today := types.UTCDay(clock.Now())
	from, to := today.Add(-20*day+15*time.Hour), today.Add(-3*day+9*time.Hour)
	f.dueAt(t, ctx, "before", 10, today.Add(-20*day-time.Microsecond))
	f.dueAt(t, ctx, "first", 10, today.Add(-20*day))
	f.dueAt(t, ctx, "last", 10, today.Add(-3*day+23*time.Hour))
	f.dueAt(t, ctx, "after", 10, today.Add(-2*day))
	echo := func(t *time.Time) *string { return lo.ToPtr(t.Format("2006-01-02")) }

	resp, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 10, CollectionsDateFilter: dto.CollectionsDateFilter{From: &from, To: &to}})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "last"}, cardIDs(resp.Columns[1].Cards), "from is the start of its day and to covers its whole day")
	require.Equal(t, &dto.CollectionsRange{From: echo(&from), To: echo(&to)}, resp.Range)

	open, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 10, CollectionsDateFilter: dto.CollectionsDateFilter{From: &from}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first", "last", "after"}, cardIDs(open.Columns[1].Cards), "an open end stays open")
	require.Equal(t, &dto.CollectionsRange{From: echo(&from)}, open.Range)

	none, err := svc.GetBoard(ctx, &dto.CollectionsBoardRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, none.Columns[1].Cards, 4, "no range: open columns unbounded")
	require.Nil(t, none.Range)
}

func TestBoard_ColumnPagesCarryTheSameRangeAndClock(t *testing.T) {
	svc, f, ctx := boardFixture(t)
	today := types.UTCDay(clock.Now())
	from, to := today.Add(-20*day), today.Add(20*day)
	asOf := clock.Now().Add(-time.Hour)

	f.notDue(t, ctx, "not_due_in", 10, 3)
	f.notDue(t, ctx, "not_due_out", 10, 25)
	f.overdue(t, ctx, "overdue_in", "cust_1", 10, 3)
	f.overdue(t, ctx, "overdue_out", "cust_1", 10, 25)
	f.overdue(t, ctx, "chasing_in", "cust_1", 10, 4)
	f.approvedAt(t, ctx, "chasing_in", asOf.Add(-time.Minute))
	f.overdue(t, ctx, "chasing_out", "cust_1", 10, 26)
	f.approvedAt(t, ctx, "chasing_out", asOf.Add(-time.Minute))
	f.overdue(t, ctx, "chasing_late", "cust_1", 10, 4)
	f.approvedAt(t, ctx, "chasing_late", asOf.Add(time.Minute))
	f.overdue(t, ctx, "held_in", "cust_1", 10, 5)
	_, err := f.svc.UpdateReminder(ctx, "held_in", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)
	f.overdue(t, ctx, "held_out", "cust_1", 10, 27)
	_, err = f.svc.UpdateReminder(ctx, "held_out", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)
	f.paidAt(t, ctx, "paid_this_week_in", "usd", 10, asOf.Add(-time.Hour))
	f.paidAt(t, ctx, "paid_this_week_out", "usd", 10, today.Add(-25*day))
	f.paidAt(t, ctx, "paid_this_week_late", "usd", 10, asOf.Add(time.Minute))

	for _, column := range []string{"not_due", "overdue", "chasing", "held", "paid_this_week"} {
		for page, want := range map[int][]string{1: {column + "_in"}, 2: {}} {
			resp, err := svc.GetColumn(ctx, column, &dto.CollectionsBoardRequest{Page: page, Limit: 1, AsOf: &asOf, CollectionsDateFilter: dto.CollectionsDateFilter{From: &from, To: &to}})
			require.NoError(t, err, column)
			require.Equal(t, want, cardIDs(resp.Cards), "page %d of %s keeps the range and as_of", page, column)
			require.False(t, resp.HasMore, column)
			require.True(t, resp.AsOf.Equal(asOf), "as_of stays pinned on page %d of %s", page, column)
			require.Equal(t, &dto.CollectionsRange{From: lo.ToPtr(from.Format("2006-01-02")), To: lo.ToPtr(to.Format("2006-01-02"))}, resp.Range)
		}
	}
}
