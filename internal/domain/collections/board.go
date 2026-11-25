package collections

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// DateRange bounds the board by due date (open columns) and paid date (paid column). From is inclusive and To is
// exclusive; a nil side is unbounded.
type DateRange struct {
	From *time.Time
	To   *time.Time
}

type Query struct {
	AsOf     time.Time
	Text     string
	Currency string
	Range    *DateRange
}

type InvoiceFilter struct {
	Column     types.CollectionsColumnKey
	Statuses   []types.ReminderStatus
	CustomerID string
}

type CurrencyTotal struct {
	Currency string
	Amount   decimal.Decimal
}

type ColumnSummary struct {
	Count  int
	Totals []CurrencyTotal
}

type BoardColumn struct {
	Summary ColumnSummary
	Rows    []InvoiceWithReminder
	HasMore bool
}

type Board struct {
	AvailableCurrencies []string
	Columns             map[types.CollectionsColumnKey]BoardColumn
}

type ReminderSummary struct {
	Totals        []CurrencyTotal
	InvoiceCount  int
	CustomerCount int
	Statuses      map[types.ReminderStatus]int
}

type columnSummaryBuilder struct {
	s *ColumnSummary
}

// NewColumnSummaryBuilder starts from a copy of s (or an empty summary when s is nil).
func NewColumnSummaryBuilder(s *ColumnSummary) *columnSummaryBuilder {
	if s == nil {
		return &columnSummaryBuilder{s: &ColumnSummary{}}
	}
	cp := *s
	return &columnSummaryBuilder{s: &cp}
}

func (b *columnSummaryBuilder) WithCount(count int) *columnSummaryBuilder {
	b.s.Count = count
	return b
}

func (b *columnSummaryBuilder) WithTotals(totals []CurrencyTotal) *columnSummaryBuilder {
	b.s.Totals = totals
	return b
}

func (b *columnSummaryBuilder) Build() *ColumnSummary {
	return b.s
}

type boardColumnBuilder struct {
	c *BoardColumn
}

// NewBoardColumnBuilder starts from a copy of c (or an empty column when c is nil).
func NewBoardColumnBuilder(c *BoardColumn) *boardColumnBuilder {
	if c == nil {
		return &boardColumnBuilder{c: &BoardColumn{}}
	}
	cp := *c
	return &boardColumnBuilder{c: &cp}
}

func (b *boardColumnBuilder) WithSummary(summary ColumnSummary) *boardColumnBuilder {
	b.c.Summary = summary
	return b
}

func (b *boardColumnBuilder) WithRows(rows []InvoiceWithReminder, hasMore bool) *boardColumnBuilder {
	b.c.Rows, b.c.HasMore = rows, hasMore
	return b
}

func (b *boardColumnBuilder) Build() *BoardColumn {
	return b.c
}

type boardBuilder struct {
	b *Board
}

// NewBoardBuilder starts from a copy of b (or an empty board when b is nil).
func NewBoardBuilder(b *Board) *boardBuilder {
	cp := Board{Columns: map[types.CollectionsColumnKey]BoardColumn{}}
	if b != nil {
		cp.AvailableCurrencies = b.AvailableCurrencies
		for key, column := range b.Columns {
			cp.Columns[key] = column
		}
	}
	return &boardBuilder{b: &cp}
}

func (b *boardBuilder) WithColumn(key types.CollectionsColumnKey, column BoardColumn) *boardBuilder {
	b.b.Columns[key] = column
	return b
}

func (b *boardBuilder) WithAvailableCurrencies(currencies []string) *boardBuilder {
	b.b.AvailableCurrencies = currencies
	return b
}

func (b *boardBuilder) Build() *Board {
	return b.b
}

type reminderSummaryBuilder struct {
	s *ReminderSummary
}

// NewReminderSummaryBuilder starts from a copy of s (or an empty summary when s is nil).
func NewReminderSummaryBuilder(s *ReminderSummary) *reminderSummaryBuilder {
	if s == nil {
		return &reminderSummaryBuilder{s: &ReminderSummary{Statuses: map[types.ReminderStatus]int{}}}
	}
	cp := *s
	return &reminderSummaryBuilder{s: &cp}
}

func (b *reminderSummaryBuilder) WithTotals(totals []CurrencyTotal) *reminderSummaryBuilder {
	b.s.Totals = totals
	return b
}

func (b *reminderSummaryBuilder) WithCounts(invoices, customers int) *reminderSummaryBuilder {
	b.s.InvoiceCount, b.s.CustomerCount = invoices, customers
	return b
}

func (b *reminderSummaryBuilder) WithStatuses(statuses map[types.ReminderStatus]int) *reminderSummaryBuilder {
	b.s.Statuses = statuses
	return b
}

func (b *reminderSummaryBuilder) Build() *ReminderSummary {
	return b.s
}

// CollectionsRepository reads the invoices of the Collections feature with their reminders: the board, its columns and the reminder list.
type CollectionsRepository interface {
	Board(ctx context.Context, q Query, limit int) (*Board, error)
	ListInvoicesWithReminders(ctx context.Context, q Query, f InvoiceFilter, offset, limit int) (rows []InvoiceWithReminder, hasMore bool, err error)
	// SummarizeReminders totals the open overdue invoices per reminder status, narrowed by the query's date range,
	// currency and search text like the board's totals.
	SummarizeReminders(ctx context.Context, q Query) (ReminderSummary, error)
	// AvailableCurrencies lists the currencies of the open and recently paid invoices, whatever the filters.
	AvailableCurrencies(ctx context.Context, asOf time.Time) ([]string, error)
}
