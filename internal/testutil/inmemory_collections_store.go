package testutil

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryCollectionsStore implements collections.CollectionsRepository in plain Go over the in-memory invoice,
// reminder and customer stores, reading the same rows as the Postgres queries.
type InMemoryCollectionsStore struct {
	invoices  *InMemoryInvoiceStore
	reminders *InMemoryInvoiceReminderStore
	customers *InMemoryCustomerStore
}

func NewInMemoryCollectionsStore(invoices *InMemoryInvoiceStore, reminders *InMemoryInvoiceReminderStore, customers *InMemoryCustomerStore) *InMemoryCollectionsStore {
	return &InMemoryCollectionsStore{invoices: invoices, reminders: reminders, customers: customers}
}

// receivable is a finalized invoice of the environment with its live reminder, if any.
type receivable struct {
	inv      *invoice.Invoice
	reminder *collections.InvoiceReminder
}

func (r receivable) open() bool {
	return r.inv.PaymentStatus != types.PaymentStatusSucceeded && r.inv.AmountRemaining.IsPositive()
}

func (r receivable) paid() bool {
	return r.inv.PaidAt != nil && (r.inv.PaymentStatus == types.PaymentStatusSucceeded || r.inv.PaymentStatus == types.PaymentStatusOverpaid)
}

func (r receivable) status() types.ReminderStatus {
	if r.reminder == nil {
		return types.ReminderStatusPending
	}
	return r.reminder.ReminderStatus
}

func (r receivable) currency() string {
	return strings.ToLower(r.inv.Currency)
}

func (r receivable) row() collections.InvoiceWithReminder {
	return *collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: r.inv.ID}).
		WithInvoice(lo.FromPtr(r.inv.InvoiceNumber), r.inv.CustomerID, r.currency()).
		WithAmounts(r.inv.AmountDue, r.inv.AmountRemaining).
		WithDates(r.inv.DueDate, r.inv.PaidAt).
		WithReminder(r.reminder).
		Build()
}

// receivables lists the finalized invoices of the environment in ctx with their reminders.
func (s *InMemoryCollectionsStore) receivables(ctx context.Context) ([]receivable, error) {
	finalized := func(ctx context.Context, inv *invoice.Invoice, _ interface{}) bool {
		return inv.TenantID == types.GetTenantID(ctx) && inv.EnvironmentID == types.GetEnvironmentID(ctx) &&
			inv.Status == types.StatusPublished && inv.InvoiceStatus == types.InvoiceStatusFinalized
	}
	invoices, err := s.invoices.InMemoryStore.List(ctx, nil, finalized, nil)
	if err != nil {
		return nil, err
	}

	reminders := lo.KeyBy(s.reminders.All(ctx), func(r *collections.InvoiceReminder) string { return r.InvoiceID })
	return lo.Map(invoices, func(inv *invoice.Invoice, _ int) receivable {
		return receivable{inv: inv, reminder: reminders[inv.ID]}
	}), nil
}

// matches applies the currency and search text of q.
func (s *InMemoryCollectionsStore) matches(ctx context.Context, r receivable, q collections.Query) bool {
	if q.Currency != "" && r.currency() != q.Currency {
		return false
	}
	if q.Text == "" {
		return true
	}

	fields := []string{lo.FromPtr(r.inv.InvoiceNumber)}
	if c, err := s.customers.Get(ctx, r.inv.CustomerID); err == nil {
		fields = append(fields, c.Name, c.Email)
	}
	return lo.SomeBy(fields, func(field string) bool { return strings.Contains(strings.ToLower(field), strings.ToLower(q.Text)) })
}

// inRange reports whether an instant is inside q's range; no range takes anything, a missing instant is outside any range.
func (s *InMemoryCollectionsStore) inRange(t *time.Time, q collections.Query) bool {
	if q.Range == nil {
		return true
	}
	if t == nil {
		return false
	}
	return (q.Range.From == nil || !t.Before(*q.Range.From)) && (q.Range.To == nil || t.Before(*q.Range.To))
}

// paidFrom is where the paid column starts: the range start, the beginning of time when only an end is given, or the
// last week when there is no range.
func (s *InMemoryCollectionsStore) paidFrom(q collections.Query) time.Time {
	if q.Range == nil {
		return q.AsOf.Add(-types.CollectionsPaidWindow)
	}
	if q.Range.From == nil {
		return time.Unix(0, 0).UTC()
	}
	return *q.Range.From
}

// column is the board column a receivable sits in for q, or "" when it is on none.
func (s *InMemoryCollectionsStore) column(r receivable, q collections.Query) types.CollectionsColumnKey {
	due := r.inv.DueDate
	switch {
	case r.open() && (due == nil || !due.Before(q.AsOf)):
		if s.inRange(due, q) {
			return types.CollectionsColumnNotDue
		}
	case r.open():
		if !s.inRange(due, q) || (r.status() == types.ReminderStatusApproved && (r.reminder.ApprovedAt == nil || r.reminder.ApprovedAt.After(q.AsOf))) {
			return ""
		}
		switch r.status() {
		case types.ReminderStatusApproved:
			return types.CollectionsColumnChasing
		case types.ReminderStatusHeld, types.ReminderStatusSkipped:
			return types.CollectionsColumnHeld
		}
		return types.CollectionsColumnOverdue
	case r.paid():
		paidAt := *r.inv.PaidAt
		if !paidAt.Before(s.paidFrom(q)) && !paidAt.After(q.AsOf) && (q.Range == nil || q.Range.To == nil || paidAt.Before(*q.Range.To)) {
			return types.CollectionsColumnPaidThisWeek
		}
	}
	return ""
}

// overdueColumn reports whether the column holds overdue invoices, whatever their reminder status.
func (s *InMemoryCollectionsStore) overdueColumn(column types.CollectionsColumnKey) bool {
	return column != "" && column != types.CollectionsColumnNotDue && column != types.CollectionsColumnPaidThisWeek
}

// rows lists the receivables matching q and the filter, in the order of the column.
func (s *InMemoryCollectionsStore) rows(ctx context.Context, q collections.Query, f collections.InvoiceFilter) ([]receivable, error) {
	all, err := s.receivables(ctx)
	if err != nil {
		return nil, err
	}

	var rows []receivable
	for _, r := range all {
		column := s.column(r, q)
		if column == "" || !s.matches(ctx, r, q) {
			continue
		}
		if !s.overdueColumn(f.Column) {
			if column == f.Column {
				rows = append(rows, r)
			}
			continue
		}
		if s.overdueColumn(column) && (len(f.Statuses) == 0 || lo.Contains(f.Statuses, r.status())) &&
			(f.CustomerID == "" || r.inv.CustomerID == f.CustomerID) {
			rows = append(rows, r)
		}
	}

	byApproval := len(f.Statuses) == 1 && f.Statuses[0] == types.ReminderStatusApproved
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case f.Column == types.CollectionsColumnNotDue:
			if (a.inv.DueDate == nil) != (b.inv.DueDate == nil) {
				return a.inv.DueDate != nil
			}
			if a.inv.DueDate != nil && !a.inv.DueDate.Equal(*b.inv.DueDate) {
				return a.inv.DueDate.Before(*b.inv.DueDate)
			}
		case f.Column == types.CollectionsColumnPaidThisWeek:
			if !a.inv.PaidAt.Equal(*b.inv.PaidAt) {
				return a.inv.PaidAt.After(*b.inv.PaidAt)
			}
		case byApproval:
			if !a.reminder.ApprovedAt.Equal(*b.reminder.ApprovedAt) {
				return a.reminder.ApprovedAt.After(*b.reminder.ApprovedAt)
			}
		default:
			if !a.inv.AmountRemaining.Equal(b.inv.AmountRemaining) {
				return a.inv.AmountRemaining.GreaterThan(b.inv.AmountRemaining)
			}
		}
		return a.inv.ID < b.inv.ID
	})
	return rows, nil
}

// totals sums the column's amounts per currency: the invoice total when paid, the remaining balance otherwise.
func (s *InMemoryCollectionsStore) totals(rows []receivable, paid bool) []collections.CurrencyTotal {
	sums := map[string]decimal.Decimal{}
	for _, r := range rows {
		amount := r.inv.AmountRemaining
		if paid {
			amount = r.inv.AmountDue
		}
		sums[r.currency()] = sums[r.currency()].Add(amount)
	}

	out := make([]collections.CurrencyTotal, 0, len(sums))
	for currency, amount := range sums {
		out = append(out, collections.CurrencyTotal{Currency: currency, Amount: amount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// page cuts rows to one page and tells whether more follow.
func (s *InMemoryCollectionsStore) page(rows []receivable, offset, limit int) ([]collections.InvoiceWithReminder, bool) {
	start := min(offset, len(rows))
	end := min(start+limit, len(rows))
	return lo.Map(rows[start:end], func(r receivable, _ int) collections.InvoiceWithReminder { return r.row() }), end < len(rows)
}

// currencies lists the currencies of the open invoices and of those paid from paidFrom to asOf.
func (s *InMemoryCollectionsStore) currencies(ctx context.Context, asOf, paidFrom time.Time) ([]string, error) {
	all, err := s.receivables(ctx)
	if err != nil {
		return nil, err
	}

	currencies := []string{}
	for _, r := range all {
		if r.open() || (r.paid() && !r.inv.PaidAt.Before(paidFrom) && !r.inv.PaidAt.After(asOf)) {
			currencies = append(currencies, r.currency())
		}
	}
	currencies = lo.Uniq(currencies)
	sort.Strings(currencies)
	return currencies, nil
}

func (s *InMemoryCollectionsStore) Board(ctx context.Context, q collections.Query, limit int) (*collections.Board, error) {
	board := collections.NewBoardBuilder(nil)
	for _, column := range []types.CollectionsColumnKey{
		types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing,
		types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek,
	} {
		rows, err := s.rows(ctx, q, collections.InvoiceFilter{Column: column, Statuses: types.ColumnStatuses(column)})
		if err != nil {
			return nil, err
		}

		summary := collections.NewColumnSummaryBuilder(nil).
			WithCount(len(rows)).
			WithTotals(s.totals(rows, column == types.CollectionsColumnPaidThisWeek)).
			Build()
		col := collections.NewBoardColumnBuilder(nil).WithSummary(*summary)
		if limit > 0 {
			col = col.WithRows(s.page(rows, 0, limit))
		}
		board = board.WithColumn(column, *col.Build())
	}

	currencies, err := s.currencies(ctx, q.AsOf, s.paidFrom(q))
	if err != nil {
		return nil, err
	}
	return board.WithAvailableCurrencies(currencies).Build(), nil
}

func (s *InMemoryCollectionsStore) ListInvoicesWithReminders(ctx context.Context, q collections.Query, f collections.InvoiceFilter, offset, limit int) ([]collections.InvoiceWithReminder, bool, error) {
	rows, err := s.rows(ctx, q, f)
	if err != nil {
		return nil, false, err
	}
	list, hasMore := s.page(rows, offset, limit)
	return list, hasMore, nil
}

func (s *InMemoryCollectionsStore) SummarizeReminders(ctx context.Context, q collections.Query) (collections.ReminderSummary, error) {
	rows, err := s.rows(ctx, q, collections.InvoiceFilter{Column: types.CollectionsColumnOverdue})
	if err != nil {
		return collections.ReminderSummary{}, err
	}

	statuses := map[types.ReminderStatus]int{}
	for _, r := range rows {
		statuses[r.status()]++
	}
	customers := lo.Uniq(lo.Map(rows, func(r receivable, _ int) string { return r.inv.CustomerID }))
	return *collections.NewReminderSummaryBuilder(nil).
		WithTotals(s.totals(rows, false)).
		WithCounts(len(rows), len(customers)).
		WithStatuses(statuses).
		Build(), nil
}

func (s *InMemoryCollectionsStore) AvailableCurrencies(ctx context.Context, asOf time.Time) ([]string, error) {
	return s.currencies(ctx, asOf, asOf.Add(-types.CollectionsPaidWindow))
}
