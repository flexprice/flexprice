// Package collections holds the source-agnostic receivable model behind the collections board.
package collections

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CustomerRef points at the Flexprice customer a receivable belongs to; sources fill only the ID.
type CustomerRef struct {
	ID    string
	Name  string
	Email string
}

// Receivable is one invoice-like claim for money, normalized across sources.
type Receivable struct {
	Source          types.ReceivableSourceType
	ExternalID      string
	Number          string
	Customer        CustomerRef
	Currency        string
	AmountTotal     decimal.Decimal
	AmountRemaining decimal.Decimal
	IssuedAt        *time.Time
	DueDate         *time.Time
	PaidAt          *time.Time
	Status          types.ReceivableStatus
}

// ReceivableGroup is one customer's overdue receivables in one currency, aggregated by the source.
type ReceivableGroup struct {
	Source       types.ReceivableSourceType
	CustomerID   string
	Currency     string
	Amount       decimal.Decimal
	InvoiceCount int
	// FirstExternalID identifies the oldest receivable of the group.
	FirstExternalID string
	// Numbers holds the first receivable numbers of the group, oldest due first.
	Numbers   []string
	OldestDue time.Time
	// Status is the reminder status that applies to the group; pending when nobody acted on it.
	Status     types.ReminderStatus
	HoldReason string
}

type GroupKey struct {
	CustomerID string
	Currency   string
}

// Query narrows everything a source returns; AsOf fixes "now" so counts and pages agree.
type Query struct {
	AsOf time.Time
	// Text keeps receivables whose customer name, customer email or number contains it, ignoring case.
	Text string
	// Currency keeps one lowercase currency.
	Currency string
}

// ToneCutoffs split oldest due dates into reminder tones: friendly from Friendly on, firm from Firm on, final before.
type ToneCutoffs struct {
	Friendly time.Time
	Firm     time.Time
}

// GroupFilter selects overdue groups.
type GroupFilter struct {
	// Statuses keeps groups whose applicable reminder status is any of these.
	Statuses []types.ReminderStatus
	// Tone keeps groups whose effective tone matches, judged with ToneCutoffs.
	Tone       types.ReminderTone
	ToneCutoff ToneCutoffs
	CustomerID string
	// WholeGroups applies Query.Text to pick groups and aggregates all their receivables;
	// otherwise Text narrows the receivables first and the aggregate covers only the matches.
	WholeGroups bool
}

type CurrencyTotal struct {
	Currency string
	Amount   decimal.Decimal
}

type ColumnSummary struct {
	Count        int
	InvoiceCount int
	Totals       []CurrencyTotal
}

// BoardColumn is the summary and first page of one board column.
type BoardColumn struct {
	Summary  ColumnSummary
	Invoices []Receivable
	Groups   []ReceivableGroup
	HasMore  bool
}

// Board is the summary and first page of every column, read from one snapshot.
type Board struct {
	AvailableCurrencies []string
	Columns             map[types.CollectionsColumnKey]BoardColumn
}

// GroupSummary totals every overdue group, ignoring any filter.
type GroupSummary struct {
	Totals        []CurrencyTotal
	InvoiceCount  int
	CustomerCount int
	Statuses      map[types.ReminderStatus]int
}

// ReceivableSource aggregates and pages the receivables of the current tenant and environment.
// Nothing is loaded wholesale; every method is bounded by its page size.
type ReceivableSource interface {
	// Board returns the summary and first limit rows of every column from one consistent snapshot; limit 0 reads no rows.
	Board(ctx context.Context, q Query, limit int) (*Board, error)
	// ListInvoices returns limit rows of the not due or paid this week column from offset; hasMore says a further row exists. The order is total, so offsets are stable.
	ListInvoices(ctx context.Context, q Query, column types.CollectionsColumnKey, offset, limit int) (rows []Receivable, hasMore bool, err error)
	// ListGroups returns limit overdue groups from offset, largest amount first (ties by currency, then customer id).
	ListGroups(ctx context.Context, q Query, filter GroupFilter, offset, limit int) (rows []ReceivableGroup, hasMore bool, err error)
	// SummarizeGroups totals all overdue groups.
	SummarizeGroups(ctx context.Context, asOf time.Time) (GroupSummary, error)
	// GroupInvoices returns every overdue receivable of the named groups.
	GroupInvoices(ctx context.Context, asOf time.Time, keys []GroupKey) ([]Receivable, error)
}

// IsOverdue reports whether a due date has passed; it is the single overdue rule shared with the customer invoice summary.
func IsOverdue(dueDate *time.Time, now time.Time) bool {
	return dueDate != nil && dueDate.Before(now)
}

// PaidWindow is how far back "paid this week" reaches.
const PaidWindow = 7 * 24 * time.Hour

// ColumnStatuses lists the reminder statuses that put an overdue group in a board column.
func ColumnStatuses(column types.CollectionsColumnKey) []types.ReminderStatus {
	switch column {
	case types.CollectionsColumnOverdue:
		return []types.ReminderStatus{types.ReminderStatusPending}
	case types.CollectionsColumnChasing:
		return []types.ReminderStatus{types.ReminderStatusApproved}
	case types.CollectionsColumnHeld:
		return []types.ReminderStatus{types.ReminderStatusHeld, types.ReminderStatusSkipped}
	}
	return nil
}

// StatusColumn is the board column of an overdue group with this reminder status.
func StatusColumn(status types.ReminderStatus) types.CollectionsColumnKey {
	switch status {
	case types.ReminderStatusApproved:
		return types.CollectionsColumnChasing
	case types.ReminderStatusHeld, types.ReminderStatusSkipped:
		return types.CollectionsColumnHeld
	}
	return types.CollectionsColumnOverdue
}
