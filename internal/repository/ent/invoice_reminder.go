package ent

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoicereminder"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// The board reads below are raw SQL: invoices and invoice_reminders have no ent edge, and the totals need exact
// decimal sums per lower(currency). Every query takes $1 tenant, $2 environment and $3 the live record status.
// An empty currency, search text or customer and an empty status list mean "no filter".

// boardSummarySQL totals every board column in one pass.
// $4 as of, $5 start of the paid window, $6 currency, $7 search pattern.
const boardSummarySQL = `
SELECT 'not_due', lower(i.currency), count(*), sum(i.amount_remaining)
FROM invoices i
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND (i.due_date IS NULL OR i.due_date >= $4)
  AND ($6 = '' OR lower(i.currency) = $6)
  AND ($7 = '' OR i.invoice_number ILIKE $7 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $7 ESCAPE '\' OR c.email ILIKE $7 ESCAPE '\')))
GROUP BY 1, 2

UNION ALL

SELECT 'paid_this_week', lower(i.currency), count(*), sum(i.amount_due)
FROM invoices i
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status IN ('SUCCEEDED', 'OVERPAID')
  AND i.paid_at >= $5 AND i.paid_at <= $4
  AND ($6 = '' OR lower(i.currency) = $6)
  AND ($7 = '' OR i.invoice_number ILIKE $7 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $7 ESCAPE '\' OR c.email ILIKE $7 ESCAPE '\')))
GROUP BY 1, 2

UNION ALL

SELECT
  CASE coalesce(r.reminder_status, 'pending')
    WHEN 'approved' THEN 'chasing'
    WHEN 'held' THEN 'held'
    WHEN 'skipped' THEN 'held'
    ELSE 'overdue'
  END,
  lower(i.currency), count(*), sum(i.amount_remaining)
FROM invoices i
LEFT JOIN invoice_reminders r
  ON r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = $3
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND i.due_date < $4
  AND ($6 = '' OR lower(i.currency) = $6)
  AND ($7 = '' OR i.invoice_number ILIKE $7 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $7 ESCAPE '\' OR c.email ILIKE $7 ESCAPE '\')))
GROUP BY 1, 2`

// boardCurrenciesSQL lists the currencies of the open and recently paid invoices, ignoring the board's filters.
// $4 as of, $5 start of the paid window.
const boardCurrenciesSQL = `
SELECT DISTINCT lower(i.currency)
FROM invoices i
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED'
  AND (
    (i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0)
    OR (i.payment_status IN ('SUCCEEDED', 'OVERPAID') AND i.paid_at >= $5 AND i.paid_at <= $4)
  )
ORDER BY 1`

// listNotDueSQL pages the open invoices not yet due. It selects only invoice columns, so the covering index serves it.
// $4 as of, $5 currency, $6 search pattern, $7 limit, $8 offset.
const listNotDueSQL = `
SELECT i.id, coalesce(i.invoice_number, ''), i.customer_id, lower(i.currency),
       0::numeric, i.amount_remaining, i.due_date, NULL::timestamptz
FROM invoices i
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND (i.due_date IS NULL OR i.due_date >= $4)
  AND ($5 = '' OR lower(i.currency) = $5)
  AND ($6 = '' OR i.invoice_number ILIKE $6 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $6 ESCAPE '\' OR c.email ILIKE $6 ESCAPE '\')))
ORDER BY i.due_date ASC NULLS LAST, i.id ASC
LIMIT $7 OFFSET $8`

// listPaidSQL pages the invoices paid inside the window. It selects only invoice columns, so the covering index serves it.
// $4 as of, $5 start of the paid window, $6 currency, $7 search pattern, $8 limit, $9 offset.
const listPaidSQL = `
SELECT i.id, coalesce(i.invoice_number, ''), i.customer_id, lower(i.currency),
       i.amount_due, 0::numeric, NULL::timestamptz, i.paid_at
FROM invoices i
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status IN ('SUCCEEDED', 'OVERPAID')
  AND i.paid_at >= $5 AND i.paid_at <= $4
  AND ($6 = '' OR lower(i.currency) = $6)
  AND ($7 = '' OR i.invoice_number ILIKE $7 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $7 ESCAPE '\' OR c.email ILIKE $7 ESCAPE '\')))
ORDER BY i.paid_at DESC, i.id ASC
LIMIT $8 OFFSET $9`

// listOverdueSQL pages the open overdue invoices with their reminders; no reminder row means pending.
// $4 as of, $5 currency, $6 search pattern, $7 customer, $8 reminder statuses, $9 limit, $10 offset.
const listOverdueSQL = `
SELECT i.id, coalesce(i.invoice_number, ''), i.customer_id, lower(i.currency),
       0::numeric, i.amount_remaining, i.due_date, NULL::timestamptz,
       r.id, r.reminder_status, r.status_reason, r.approved_at, r.metadata,
       r.tone, r.subject, r.body, r.created_at, r.updated_at, r.created_by, r.updated_by
FROM invoices i
LEFT JOIN invoice_reminders r
  ON r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = $3
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND i.due_date < $4
  AND ($5 = '' OR lower(i.currency) = $5)
  AND ($6 = '' OR i.invoice_number ILIKE $6 ESCAPE '\' OR i.customer_id IN (
        SELECT c.id FROM customers c
        WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
          AND (c.name ILIKE $6 ESCAPE '\' OR c.email ILIKE $6 ESCAPE '\')))
  AND ($7 = '' OR i.customer_id = $7)
  AND (cardinality($8::text[]) = 0 OR coalesce(r.reminder_status, 'pending') = ANY($8::text[]))
ORDER BY i.amount_remaining DESC, i.id ASC
LIMIT $9 OFFSET $10`

// reminderSummarySQL counts the open overdue invoices per reminder status and currency, and their distinct customers.
// $4 as of.
const reminderSummarySQL = `
SELECT grouping(coalesce(r.reminder_status, 'pending'), lower(i.currency)),
       coalesce(r.reminder_status, 'pending'), lower(i.currency),
       count(*), sum(i.amount_remaining), count(DISTINCT i.customer_id)
FROM invoices i
LEFT JOIN invoice_reminders r
  ON r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = $3
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND i.due_date < $4
GROUP BY GROUPING SETS ((coalesce(r.reminder_status, 'pending'), lower(i.currency)), ())`

// invoicesWithRemindersSQL reads the given invoices that are still open and overdue, with their reminders.
// $4 as of, $5 invoice ids.
const invoicesWithRemindersSQL = `
SELECT i.id, coalesce(i.invoice_number, ''), i.customer_id, lower(i.currency),
       i.amount_due, i.amount_remaining, i.due_date, i.paid_at,
       r.id, r.reminder_status, r.status_reason, r.approved_at, r.metadata,
       r.tone, r.subject, r.body, r.created_at, r.updated_at, r.created_by, r.updated_by
FROM invoices i
LEFT JOIN invoice_reminders r
  ON r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = $3
WHERE i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.id = ANY($5::text[])
  AND i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0
  AND i.due_date < $4
ORDER BY i.id ASC`

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

var boardColumns = []types.CollectionsColumnKey{
	types.CollectionsColumnNotDue,
	types.CollectionsColumnOverdue,
	types.CollectionsColumnChasing,
	types.CollectionsColumnHeld,
	types.CollectionsColumnPaidThisWeek,
}

type invoiceReminderRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewInvoiceReminderRepository(client postgres.IClient, log *logger.Logger) collections.InvoiceReminderRepository {
	return &invoiceReminderRepository{client: client, log: log}
}

type collectionsRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewCollectionsRepository(client postgres.IClient, log *logger.Logger) collections.CollectionsRepository {
	return &collectionsRepository{client: client, log: log}
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// scopeArgs are $1, $2 and $3 of every board query.
func scopeArgs(ctx context.Context) []any {
	return []any{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), string(types.StatusPublished)}
}

// searchPattern is the ILIKE pattern of the search text, with its wildcards taken literally; empty text gives "".
func searchPattern(text string) string {
	if text == "" {
		return ""
	}
	return "%" + likeEscaper.Replace(text) + "%"
}

func paidSince(asOf time.Time) time.Time {
	return asOf.Add(-types.CollectionsPaidWindow)
}

func dbError(err error, hint string) error {
	return ierr.WithError(err).WithHint(hint).Mark(ierr.ErrDatabase)
}

func scanRows(rows *sql.Rows, scan func() error) error {
	defer rows.Close()
	for rows.Next() {
		if err := scan(); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// decodeMetadata reads the nullable metadata jsonb column; an empty or invalid value gives nil.
func decodeMetadata(raw []byte) map[string]interface{} {
	var metadata map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	return metadata
}

// scanInvoices reads rows of the eight invoice columns.
func scanInvoices(rows *sql.Rows) ([]collections.InvoiceWithReminder, error) {
	list := []collections.InvoiceWithReminder{}
	err := scanRows(rows, func() error {
		var (
			id, number, customerID, currency string
			amountDue, amountRemaining       decimal.Decimal
			dueDate, paidAt                  sql.NullTime
		)
		if err := rows.Scan(&id, &number, &customerID, &currency, &amountDue, &amountRemaining, &dueDate, &paidAt); err != nil {
			return err
		}

		row := collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: id}).
			WithInvoice(number, customerID, currency).
			WithAmounts(amountDue, amountRemaining).
			WithDates(nullTime(dueDate), nullTime(paidAt)).
			Build()
		list = append(list, *row)
		return nil
	})
	return list, err
}

// scanInvoicesWithReminders reads rows of the eight invoice columns followed by the twelve reminder columns.
func scanInvoicesWithReminders(ctx context.Context, rows *sql.Rows) ([]collections.InvoiceWithReminder, error) {
	list := []collections.InvoiceWithReminder{}
	err := scanRows(rows, func() error {
		var (
			id, number, customerID, currency string
			amountDue, amountRemaining       decimal.Decimal
			dueDate, paidAt                  sql.NullTime
			reminderID, status, reason       sql.NullString
			tone, subject, body              sql.NullString
			createdBy, updatedBy             sql.NullString
			approvedAt, createdAt, updatedAt sql.NullTime
			metadata                         []byte
		)
		if err := rows.Scan(&id, &number, &customerID, &currency, &amountDue, &amountRemaining, &dueDate, &paidAt,
			&reminderID, &status, &reason, &approvedAt, &metadata, &tone, &subject, &body, &createdAt, &updatedAt, &createdBy, &updatedBy); err != nil {
			return err
		}

		row := collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: id}).
			WithInvoice(number, customerID, currency).
			WithAmounts(amountDue, amountRemaining).
			WithDates(nullTime(dueDate), nullTime(paidAt))

		if reminderID.Valid {
			reminder := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
				ID:            reminderID.String,
				InvoiceID:     id,
				EnvironmentID: types.GetEnvironmentID(ctx),
				BaseModel: types.BaseModel{
					TenantID:  types.GetTenantID(ctx),
					Status:    types.StatusPublished,
					CreatedAt: createdAt.Time,
					UpdatedAt: updatedAt.Time,
					CreatedBy: createdBy.String,
					UpdatedBy: updatedBy.String,
				},
			}).
				WithStatus(types.ReminderStatus(status.String), reason.String, nullTime(approvedAt)).
				WithMetadata(decodeMetadata(metadata)).
				WithText(types.ReminderTone(tone.String), subject.String, body.String).
				Build()
			row = row.WithReminder(reminder)
		}

		list = append(list, *row.Build())
		return nil
	})
	return list, err
}

func totalsOf(totals map[string]decimal.Decimal) []collections.CurrencyTotal {
	out := make([]collections.CurrencyTotal, 0, len(totals))
	for currency, amount := range totals {
		out = append(out, collections.CurrencyTotal{Currency: currency, Amount: amount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// Board reads every column in one REPEATABLE READ read-only transaction, so the columns agree with each other.
func (r *collectionsRepository) Board(ctx context.Context, q collections.Query, limit int) (*collections.Board, error) {
	span := StartRepositorySpan(ctx, "board", "board", nil)
	defer FinishSpan(span)

	var db queryer = r.client.Reader(ctx)
	if r.client.TxFromContext(ctx) != nil {
		if err := requireSnapshotIsolation(ctx, db); err != nil {
			SetSpanError(span, err)
			return nil, err
		}
	} else {
		tx, err := r.client.Reader(ctx).BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			SetSpanError(span, err)
			return nil, dbError(err, "Failed to start the board snapshot")
		}
		defer func() { _ = tx.Rollback() }()
		db = tx.Client()
	}

	board, err := r.board(ctx, db, q, limit)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	return board, nil
}

// requireSnapshotIsolation refuses a caller's transaction that cannot give every column of the board one snapshot.
func requireSnapshotIsolation(ctx context.Context, db queryer) error {
	rows, err := db.QueryContext(ctx, `SELECT current_setting('transaction_isolation')`)
	if err != nil {
		return dbError(err, "Failed to check the board snapshot")
	}
	var level string
	if err := scanRows(rows, func() error { return rows.Scan(&level) }); err != nil {
		return dbError(err, "Failed to check the board snapshot")
	}
	if level != "repeatable read" && level != "serializable" {
		return ierr.NewError("board needs a snapshot transaction").
			WithHint("Run the board outside a transaction, or in a REPEATABLE READ one.").
			WithReportableDetails(map[string]any{"isolation": level}).
			Mark(ierr.ErrInternal)
	}
	return nil
}

func (r *collectionsRepository) board(ctx context.Context, db queryer, q collections.Query, limit int) (*collections.Board, error) {
	summaries, err := summarizeBoard(ctx, db, q)
	if err != nil {
		return nil, err
	}

	board := collections.NewBoardBuilder(nil)
	for _, column := range boardColumns {
		col := collections.NewBoardColumnBuilder(nil).WithSummary(summaries[column])

		if limit > 0 {
			filter := collections.InvoiceFilter{Column: column, Statuses: types.ColumnStatuses(column)}
			rows, hasMore, err := listInvoices(ctx, db, q, filter, 0, limit)
			if err != nil {
				return nil, err
			}
			col = col.WithRows(rows, hasMore)
		}

		board = board.WithColumn(column, *col.Build())
	}

	currencies, err := boardCurrencies(ctx, db, q, summaries)
	if err != nil {
		return nil, err
	}

	return board.WithAvailableCurrencies(currencies).Build(), nil
}

// summarizeBoard counts and totals every board column with one query.
func summarizeBoard(ctx context.Context, db queryer, q collections.Query) (map[types.CollectionsColumnKey]collections.ColumnSummary, error) {
	args := append(scopeArgs(ctx), q.AsOf, paidSince(q.AsOf), q.Currency, searchPattern(q.Text))
	rows, err := db.QueryContext(ctx, boardSummarySQL, args...)
	if err != nil {
		return nil, dbError(err, "Failed to total the board")
	}

	counts := map[types.CollectionsColumnKey]int{}
	totals := map[types.CollectionsColumnKey]map[string]decimal.Decimal{}
	err = scanRows(rows, func() error {
		var (
			column, currency string
			count            int
			total            decimal.Decimal
		)
		if err := rows.Scan(&column, &currency, &count, &total); err != nil {
			return err
		}

		key := types.CollectionsColumnKey(column)
		if totals[key] == nil {
			totals[key] = map[string]decimal.Decimal{}
		}
		counts[key] += count
		totals[key][currency] = totals[key][currency].Add(total)
		return nil
	})
	if err != nil {
		return nil, dbError(err, "Failed to total the board")
	}

	summaries := map[types.CollectionsColumnKey]collections.ColumnSummary{}
	for _, column := range boardColumns {
		summaries[column] = *collections.NewColumnSummaryBuilder(nil).WithCount(counts[column]).WithTotals(totalsOf(totals[column])).Build()
	}
	return summaries, nil
}

// boardCurrencies lists every currency the board can be filtered by. An unfiltered board already has them in its totals.
func boardCurrencies(ctx context.Context, db queryer, q collections.Query, summaries map[types.CollectionsColumnKey]collections.ColumnSummary) ([]string, error) {
	if q.Text == "" && q.Currency == "" {
		currencies := []string{}
		for _, summary := range summaries {
			for _, total := range summary.Totals {
				currencies = append(currencies, total.Currency)
			}
		}
		currencies = lo.Uniq(currencies)
		sort.Strings(currencies)
		return currencies, nil
	}

	args := append(scopeArgs(ctx), q.AsOf, paidSince(q.AsOf))
	rows, err := db.QueryContext(ctx, boardCurrenciesSQL, args...)
	if err != nil {
		return nil, dbError(err, "Failed to list board currencies")
	}

	currencies := []string{}
	err = scanRows(rows, func() error {
		var currency string
		if err := rows.Scan(&currency); err != nil {
			return err
		}
		currencies = append(currencies, currency)
		return nil
	})
	if err != nil {
		return nil, dbError(err, "Failed to list board currencies")
	}
	return currencies, nil
}

func (r *collectionsRepository) ListInvoicesWithReminders(ctx context.Context, q collections.Query, f collections.InvoiceFilter, offset, limit int) ([]collections.InvoiceWithReminder, bool, error) {
	span := StartRepositorySpan(ctx, "board", "list_rows", map[string]interface{}{"column": f.Column})
	defer FinishSpan(span)

	rows, hasMore, err := listInvoices(ctx, r.client.Reader(ctx), q, f, offset, limit)
	if err != nil {
		SetSpanError(span, err)
		return nil, false, err
	}
	SetSpanSuccess(span)
	return rows, hasMore, nil
}

// listInvoices pages one board column. It reads one row past the limit to tell whether there is a next page.
func listInvoices(ctx context.Context, db queryer, q collections.Query, f collections.InvoiceFilter, offset, limit int) ([]collections.InvoiceWithReminder, bool, error) {
	var (
		list []collections.InvoiceWithReminder
		err  error
	)
	switch f.Column {
	case types.CollectionsColumnNotDue:
		list, err = listNotDue(ctx, db, q, offset, limit+1)
	case types.CollectionsColumnPaidThisWeek:
		list, err = listPaid(ctx, db, q, offset, limit+1)
	default:
		list, err = listOverdue(ctx, db, q, f, offset, limit+1)
	}
	if err != nil {
		return nil, false, dbError(err, "Failed to list board rows")
	}

	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}

func listNotDue(ctx context.Context, db queryer, q collections.Query, offset, limit int) ([]collections.InvoiceWithReminder, error) {
	args := append(scopeArgs(ctx), q.AsOf, q.Currency, searchPattern(q.Text), limit, offset)
	rows, err := db.QueryContext(ctx, listNotDueSQL, args...)
	if err != nil {
		return nil, err
	}
	return scanInvoices(rows)
}

func listPaid(ctx context.Context, db queryer, q collections.Query, offset, limit int) ([]collections.InvoiceWithReminder, error) {
	args := append(scopeArgs(ctx), q.AsOf, paidSince(q.AsOf), q.Currency, searchPattern(q.Text), limit, offset)
	rows, err := db.QueryContext(ctx, listPaidSQL, args...)
	if err != nil {
		return nil, err
	}
	return scanInvoices(rows)
}

func listOverdue(ctx context.Context, db queryer, q collections.Query, f collections.InvoiceFilter, offset, limit int) ([]collections.InvoiceWithReminder, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, status := range f.Statuses {
		statuses = append(statuses, string(status))
	}

	args := append(scopeArgs(ctx), q.AsOf, q.Currency, searchPattern(q.Text), f.CustomerID, pq.Array(statuses), limit, offset)
	rows, err := db.QueryContext(ctx, listOverdueSQL, args...)
	if err != nil {
		return nil, err
	}
	return scanInvoicesWithReminders(ctx, rows)
}

func (r *collectionsRepository) SummarizeReminders(ctx context.Context, asOf time.Time) (collections.ReminderSummary, error) {
	span := StartRepositorySpan(ctx, "board", "summarize_reminders", nil)
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, reminderSummarySQL, append(scopeArgs(ctx), asOf)...)
	if err != nil {
		SetSpanError(span, err)
		return collections.ReminderSummary{}, dbError(err, "Failed to total overdue invoices")
	}

	statuses := map[types.ReminderStatus]int{}
	totals := map[string]decimal.Decimal{}
	invoices, customers := 0, 0
	err = scanRows(rows, func() error {
		var (
			grandTotal       int
			status, currency sql.NullString
			count, distinct  int
			total            decimal.NullDecimal
		)
		if err := rows.Scan(&grandTotal, &status, &currency, &count, &total, &distinct); err != nil {
			return err
		}

		// The grand-total row only carries the distinct customer count.
		if grandTotal != 0 {
			customers = distinct
			return nil
		}

		statuses[types.ReminderStatus(status.String)] += count
		invoices += count
		totals[currency.String] = totals[currency.String].Add(total.Decimal)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return collections.ReminderSummary{}, dbError(err, "Failed to total overdue invoices")
	}

	SetSpanSuccess(span)
	return *collections.NewReminderSummaryBuilder(nil).WithTotals(totalsOf(totals)).WithCounts(invoices, customers).WithStatuses(statuses).Build(), nil
}

func (r *invoiceReminderRepository) GetInvoicesWithReminders(ctx context.Context, asOf time.Time, invoiceIDs []string) ([]collections.InvoiceWithReminder, error) {
	if len(invoiceIDs) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "invoice_reminder", "get_rows", map[string]interface{}{"invoices": len(invoiceIDs)})
	defer FinishSpan(span)

	args := append(scopeArgs(ctx), asOf, pq.Array(invoiceIDs))
	rows, err := r.client.Reader(ctx).QueryContext(ctx, invoicesWithRemindersSQL, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to read overdue invoices")
	}

	list, err := scanInvoicesWithReminders(ctx, rows)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to read overdue invoices")
	}

	SetSpanSuccess(span)
	return list, nil
}

func (r *invoiceReminderRepository) createBuilder(ctx context.Context, d *collections.InvoiceReminder) *ent.InvoiceReminderCreate {
	if d.EnvironmentID == "" {
		d.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return r.client.Writer(ctx).InvoiceReminder.Create().
		SetID(d.ID).
		SetInvoiceID(d.InvoiceID).
		SetReminderStatus(d.ReminderStatus).
		SetStatusReason(d.StatusReason).
		SetNillableApprovedAt(d.ApprovedAt).
		SetMetadata(d.Metadata).
		SetTone(d.Tone).
		SetSubject(d.Subject).
		SetBody(d.Body).
		SetTenantID(d.TenantID).
		SetEnvironmentID(d.EnvironmentID).
		SetStatus(string(types.StatusPublished)).
		SetCreatedAt(d.CreatedAt).
		SetUpdatedAt(d.UpdatedAt).
		SetCreatedBy(d.CreatedBy).
		SetUpdatedBy(d.UpdatedBy)
}

func (r *invoiceReminderRepository) Create(ctx context.Context, d *collections.InvoiceReminder) error {
	span := StartRepositorySpan(ctx, "invoice_reminder", "create", map[string]interface{}{"invoice_id": d.InvoiceID})
	defer FinishSpan(span)

	created, err := r.createBuilder(ctx, d).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("This reminder was changed by someone else").Mark(ierr.ErrAlreadyExists)
		}
		return dbError(err, "Failed to save the reminder")
	}
	SetSpanSuccess(span)
	*d = *reminderFromEnt(created)
	return nil
}

// CreateMany inserts the reminders in one statement. If any invoice already has a reminder, nothing is inserted.
func (r *invoiceReminderRepository) CreateMany(ctx context.Context, reminders []*collections.InvoiceReminder) error {
	if len(reminders) == 0 {
		return nil
	}
	span := StartRepositorySpan(ctx, "invoice_reminder", "create_many", map[string]interface{}{"reminders": len(reminders)})
	defer FinishSpan(span)

	builders := make([]*ent.InvoiceReminderCreate, 0, len(reminders))
	for _, d := range reminders {
		builders = append(builders, r.createBuilder(ctx, d))
	}

	if _, err := r.client.Writer(ctx).InvoiceReminder.CreateBulk(builders...).Save(ctx); err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("One of these reminders was changed by someone else").Mark(ierr.ErrAlreadyExists)
		}
		return dbError(err, "Failed to save the reminders")
	}

	SetSpanSuccess(span)
	return nil
}

func (r *invoiceReminderRepository) Update(ctx context.Context, d *collections.InvoiceReminder, previous *collections.InvoiceReminder) error {
	span := StartRepositorySpan(ctx, "invoice_reminder", "update", map[string]interface{}{"invoice_id": d.InvoiceID})
	defer FinishSpan(span)

	update := r.client.Writer(ctx).InvoiceReminder.UpdateOneID(d.ID).
		Where(
			invoicereminder.TenantID(types.GetTenantID(ctx)),
			invoicereminder.EnvironmentID(types.GetEnvironmentID(ctx)),
			invoicereminder.Status(string(types.StatusPublished)),
			invoicereminder.ReminderStatus(previous.ReminderStatus),
			invoicereminder.UpdatedAt(previous.UpdatedAt),
		).
		SetReminderStatus(d.ReminderStatus).
		SetStatusReason(d.StatusReason).
		SetNillableApprovedAt(d.ApprovedAt).
		SetMetadata(d.Metadata).
		SetTone(d.Tone).
		SetSubject(d.Subject).
		SetBody(d.Body).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))
	if d.ApprovedAt == nil {
		update = update.ClearApprovedAt()
	}
	updated, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).WithHint("This reminder was changed by someone else. Reload and try again.").Mark(ierr.ErrVersionConflict)
		}
		return dbError(err, "Failed to update the reminder")
	}
	SetSpanSuccess(span)
	*d = *reminderFromEnt(updated)
	return nil
}

func reminderFromEnt(e *ent.InvoiceReminder) *collections.InvoiceReminder {
	return collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID:            e.ID,
		InvoiceID:     e.InvoiceID,
		EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}).
		WithStatus(e.ReminderStatus, e.StatusReason, e.ApprovedAt).
		WithMetadata(e.Metadata).
		WithText(e.Tone, e.Subject, e.Body).
		Build()
}
