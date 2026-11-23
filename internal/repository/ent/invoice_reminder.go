package ent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicereminder"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	openInvoice = "i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0"
	likeEscape  = ` ESCAPE '\'`
	// effectiveStatus is the reminder status of an invoice; no reminder row means pending.
	effectiveStatus = "coalesce(r.reminder_status, 'pending')"
	// overdueOrder is a total order, so offsets are stable.
	overdueOrder = "i.amount_remaining DESC, i.id ASC"
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

type invoiceReminderRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewInvoiceReminderRepository(client postgres.IClient, log *logger.Logger) collections.InvoiceReminderRepository {
	return &invoiceReminderRepository{client: client, log: log}
}

type boardRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewBoardRepository(client postgres.IClient, log *logger.Logger) collections.BoardRepository {
	return &boardRepository{client: client, log: log}
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// sqlArgs collects bound arguments and hands out their placeholders.
type sqlArgs struct {
	values []any
}

func (a *sqlArgs) bind(v any) string {
	a.values = append(a.values, v)
	return "$" + strconv.Itoa(len(a.values))
}

// scope binds the tenant, environment and record status every query must carry.
type scope struct {
	tenant, environment, status string
}

func newScope(ctx context.Context, a *sqlArgs) scope {
	return scope{
		tenant:      a.bind(types.GetTenantID(ctx)),
		environment: a.bind(types.GetEnvironmentID(ctx)),
		status:      a.bind(string(types.StatusPublished)),
	}
}

func (s scope) invoices() string {
	return fmt.Sprintf("i.tenant_id = %s AND i.environment_id = %s AND i.status = %s", s.tenant, s.environment, s.status)
}

func (s scope) reminderJoin() string {
	return fmt.Sprintf(`LEFT JOIN invoice_reminders r ON r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = %s`, s.status)
}

// textMatch matches the customer name, customer email or invoice number, with LIKE wildcards taken literally.
func (s scope) textMatch(a *sqlArgs, text string) string {
	p := a.bind("%" + likeEscaper.Replace(text) + "%")
	return fmt.Sprintf(`(i.invoice_number ILIKE %[1]s%[2]s OR i.customer_id IN (SELECT c.id FROM customers c`+
		` WHERE c.tenant_id = %[3]s AND c.environment_id = %[4]s AND c.status = %[5]s`+
		` AND (c.name ILIKE %[1]s%[2]s OR c.email ILIKE %[1]s%[2]s)))`, p, likeEscape, s.tenant, s.environment, s.status)
}

func and(conditions ...string) string {
	return strings.Join(lo.Filter(conditions, func(c string, _ int) bool { return c != "" }), " AND ")
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

func paidWindow(asOfP, sinceP string) string {
	return fmt.Sprintf("i.invoice_status = 'FINALIZED' AND i.payment_status = 'SUCCEEDED' AND i.paid_at >= %s AND i.paid_at <= %s", sinceP, asOfP)
}

// rowColumns selects an invoice and, when joined, its reminder.
func rowColumns(joined bool) string {
	invoice := `i.id, coalesce(i.invoice_number, ''), i.customer_id, lower(i.currency), i.amount_due, i.amount_remaining, i.due_date, i.paid_at`
	if !joined {
		return invoice + `, NULL::text, NULL::text, NULL::text, NULL::timestamptz, NULL::jsonb, NULL::text, NULL::text, NULL::text, NULL::timestamptz, NULL::timestamptz`
	}
	return invoice + `, r.id, r.reminder_status, r.status_reason, r.approved_at, r.metadata, r.tone, r.subject, r.body, r.created_at, r.updated_at`
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func scanRow(ctx context.Context, rows *sql.Rows) (collections.Row, error) {
	var (
		invoiceID, number, customerID, currency string
		total, remaining                        decimal.Decimal
		due, paid, approved, created, updated   sql.NullTime
		rid, status, reason                     sql.NullString
		tone, subject, body                     sql.NullString
		metadata                                []byte
	)
	if err := rows.Scan(&invoiceID, &number, &customerID, &currency, &total, &remaining, &due, &paid,
		&rid, &status, &reason, &approved, &metadata, &tone, &subject, &body, &created, &updated); err != nil {
		return collections.Row{}, err
	}
	row := collections.NewRowBuilder(&collections.Row{InvoiceID: invoiceID}).
		WithInvoice(number, customerID, currency).
		WithAmounts(total, remaining).
		WithDates(nullTime(due), nullTime(paid))
	if rid.Valid {
		row = row.WithReminder(collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
			ID:            rid.String,
			InvoiceID:     invoiceID,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.BaseModel{TenantID: types.GetTenantID(ctx), Status: types.StatusPublished, CreatedAt: created.Time, UpdatedAt: updated.Time},
		}).
			WithStatus(types.ReminderStatus(status.String), reason.String, nullTime(approved)).
			WithMetadata(decodeMetadata(metadata)).
			WithText(types.ReminderTone(tone.String), subject.String, body.String).
			Build())
	}
	return *row.Build(), nil
}

// decodeMetadata reads the nullable metadata jsonb column; an empty or invalid value gives nil.
func decodeMetadata(raw []byte) map[string]interface{} {
	var metadata map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	return metadata
}

func scanRowList(ctx context.Context, rows *sql.Rows) ([]collections.Row, error) {
	list := []collections.Row{}
	err := scanRows(rows, func() error {
		row, err := scanRow(ctx, rows)
		if err != nil {
			return err
		}
		list = append(list, row)
		return nil
	})
	return list, err
}

// Raw SQL: ent has no LEFT JOIN of invoices to invoice_reminders with a coalesce() filter and ordering (no edge between them).
// overdueFrom is the open overdue invoices with their reminders, narrowed by the query and the filter.
func overdueFrom(sc scope, a *sqlArgs, q collections.Query, f collections.RowFilter) string {
	conditions := []string{sc.invoices(), openInvoice, "i.due_date < " + a.bind(q.AsOf)}
	if q.Currency != "" {
		conditions = append(conditions, "lower(i.currency) = "+a.bind(q.Currency))
	}
	if q.Text != "" {
		conditions = append(conditions, sc.textMatch(a, q.Text))
	}
	if f.CustomerID != "" {
		conditions = append(conditions, "i.customer_id = "+a.bind(f.CustomerID))
	}
	if len(f.Statuses) > 0 {
		conditions = append(conditions, effectiveStatus+" = ANY("+a.bind(pq.Array(lo.Map(f.Statuses, func(s types.ReminderStatus, _ int) string { return string(s) })))+"::text[])")
	}
	return "FROM invoices i " + sc.reminderJoin() + " WHERE " + and(conditions...)
}

// Raw SQL: shares the filter builder with overdueFrom and runs on the snapshot transaction handle, which ent clients cannot use.
// invoiceFrom is the not due or paid invoices of a column, narrowed by the query.
func invoiceFrom(sc scope, a *sqlArgs, q collections.Query, column types.CollectionsColumnKey) string {
	asOfP := a.bind(q.AsOf)
	predicate := fmt.Sprintf("%s AND (i.due_date IS NULL OR i.due_date >= %s)", openInvoice, asOfP)
	if column == types.CollectionsColumnPaidThisWeek {
		predicate = paidWindow(asOfP, a.bind(q.AsOf.Add(-types.CollectionsPaidWindow)))
	}
	conditions := []string{sc.invoices(), predicate}
	if q.Currency != "" {
		conditions = append(conditions, "lower(i.currency) = "+a.bind(q.Currency))
	}
	if q.Text != "" {
		conditions = append(conditions, sc.textMatch(a, q.Text))
	}
	return "FROM invoices i WHERE " + and(conditions...)
}

// Raw SQL: ent cannot open a REPEATABLE READ read-only transaction, which keeps every column of the board consistent.
func (r *boardRepository) Board(ctx context.Context, q collections.Query, limit int) (*collections.Board, error) {
	span := StartRepositorySpan(ctx, "board", "board", nil)
	defer FinishSpan(span)

	var db queryer = r.client.Reader(ctx)
	if r.client.TxFromContext(ctx) == nil {
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

func (r *boardRepository) board(ctx context.Context, db queryer, q collections.Query, limit int) (*collections.Board, error) {
	board := collections.NewBoardBuilder(nil)
	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek} {
		summary, err := summarizeInvoices(ctx, db, q, column)
		if err != nil {
			return nil, err
		}
		board = board.WithColumn(column, *collections.NewBoardColumnBuilder(nil).WithSummary(summary).Build())
	}
	overdue, err := summarizeOverdue(ctx, db, q)
	if err != nil {
		return nil, err
	}
	for column, summary := range overdue {
		board = board.WithColumn(column, *collections.NewBoardColumnBuilder(nil).WithSummary(summary).Build())
	}
	built := board.Build()

	if limit > 0 {
		for column, col := range built.Columns {
			rows, hasMore, err := listRows(ctx, db, q, filterOf(column), 0, limit)
			if err != nil {
				return nil, err
			}
			built.Columns[column] = *collections.NewBoardColumnBuilder(&col).WithRows(rows, hasMore).Build()
		}
	}
	if q.Text == "" && q.Currency == "" {
		return collections.NewBoardBuilder(built).WithAvailableCurrencies(currenciesOf(built)).Build(), nil
	}
	currencies, err := availableCurrencies(ctx, db, q.AsOf)
	if err != nil {
		return nil, err
	}
	return collections.NewBoardBuilder(built).WithAvailableCurrencies(currencies).Build(), nil
}

func filterOf(column types.CollectionsColumnKey) collections.RowFilter {
	return collections.RowFilter{Column: column, Statuses: types.ColumnStatuses(column)}
}

// currenciesOf lists the currencies of an unfiltered board from its column totals.
func currenciesOf(board *collections.Board) []string {
	currencies := []string{}
	for _, column := range board.Columns {
		for _, total := range column.Summary.Totals {
			currencies = append(currencies, total.Currency)
		}
	}
	currencies = lo.Uniq(currencies)
	sort.Strings(currencies)
	return currencies
}

// Raw SQL: UNION of two filtered selects of lower(currency) on the snapshot transaction handle.
func availableCurrencies(ctx context.Context, db queryer, asOf time.Time) ([]string, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	asOfP, sinceP := a.bind(asOf), a.bind(asOf.Add(-types.CollectionsPaidWindow))
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT lower(i.currency) FROM invoices i WHERE %[1]s AND %[2]s
		 UNION
		 SELECT lower(i.currency) FROM invoices i WHERE %[1]s AND %[3]s`,
		sc.invoices(), openInvoice, paidWindow(asOfP, sinceP)), a.values...)
	if err != nil {
		return nil, dbError(err, "Failed to list board currencies")
	}
	currencies := []string{}
	if err := scanRows(rows, func() error {
		var c string
		if err := rows.Scan(&c); err != nil {
			return err
		}
		currencies = append(currencies, c)
		return nil
	}); err != nil {
		return nil, dbError(err, "Failed to list board currencies")
	}
	sort.Strings(currencies)
	return currencies, nil
}

func totalsOf(totals map[string]decimal.Decimal) []collections.CurrencyTotal {
	out := lo.MapToSlice(totals, func(currency string, amount decimal.Decimal) collections.CurrencyTotal {
		return collections.CurrencyTotal{Currency: currency, Amount: amount}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// Raw SQL: sum() per lower(currency) as exact decimals; ent aggregates return float64 and cannot group by an expression.
func summarizeInvoices(ctx context.Context, db queryer, q collections.Query, column types.CollectionsColumnKey) (collections.ColumnSummary, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	amount := "i.amount_remaining"
	if column == types.CollectionsColumnPaidThisWeek {
		amount = "i.amount_due"
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT lower(i.currency), count(*), sum(%s) %s GROUP BY 1`, amount, invoiceFrom(sc, &a, q, column)), a.values...)
	if err != nil {
		return collections.ColumnSummary{}, dbError(err, "Failed to total a board column")
	}
	totals := map[string]decimal.Decimal{}
	count := 0
	if err := scanRows(rows, func() error {
		var (
			currency string
			n        int
			total    decimal.Decimal
		)
		if err := rows.Scan(&currency, &n, &total); err != nil {
			return err
		}
		count += n
		totals[currency] = total
		return nil
	}); err != nil {
		return collections.ColumnSummary{}, dbError(err, "Failed to total a board column")
	}
	return *collections.NewColumnSummaryBuilder(nil).WithCount(count).WithTotals(totalsOf(totals)).Build(), nil
}

// columnOf is the board column of an overdue invoice with this reminder status.
func columnOf(status types.ReminderStatus) types.CollectionsColumnKey {
	switch status {
	case types.ReminderStatusApproved:
		return types.CollectionsColumnChasing
	case types.ReminderStatusHeld, types.ReminderStatusSkipped:
		return types.CollectionsColumnHeld
	}
	return types.CollectionsColumnOverdue
}

// Raw SQL: GROUP BY over a LEFT JOIN with coalesce(); not expressible with ent.
// summarizeOverdue totals the three overdue-type columns in one pass over the invoices and their reminders.
func summarizeOverdue(ctx context.Context, db queryer, q collections.Query) (map[types.CollectionsColumnKey]collections.ColumnSummary, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT %s, lower(i.currency), count(*), sum(i.amount_remaining) %s GROUP BY 1, 2`,
		effectiveStatus, overdueFrom(sc, &a, q, collections.RowFilter{})), a.values...)
	if err != nil {
		return nil, dbError(err, "Failed to total overdue invoices")
	}
	counts := map[types.CollectionsColumnKey]int{}
	totals := map[types.CollectionsColumnKey]map[string]decimal.Decimal{}
	if err := scanRows(rows, func() error {
		var (
			status, currency string
			count            int
			total            decimal.Decimal
		)
		if err := rows.Scan(&status, &currency, &count, &total); err != nil {
			return err
		}
		column := columnOf(types.ReminderStatus(status))
		counts[column] += count
		if totals[column] == nil {
			totals[column] = map[string]decimal.Decimal{}
		}
		totals[column][currency] = totals[column][currency].Add(total)
		return nil
	}); err != nil {
		return nil, dbError(err, "Failed to total overdue invoices")
	}
	summaries := map[types.CollectionsColumnKey]collections.ColumnSummary{}
	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld} {
		summaries[column] = *collections.NewColumnSummaryBuilder(nil).WithCount(counts[column]).WithTotals(totalsOf(totals[column])).Build()
	}
	return summaries, nil
}

func (r *boardRepository) ListRows(ctx context.Context, q collections.Query, f collections.RowFilter, offset, limit int) ([]collections.Row, bool, error) {
	span := StartRepositorySpan(ctx, "board", "list_rows", map[string]interface{}{"column": f.Column})
	defer FinishSpan(span)
	rows, hasMore, err := listRows(ctx, r.client.Reader(ctx), q, f, offset, limit)
	if err != nil {
		SetSpanError(span, err)
	}
	return rows, hasMore, err
}

// Raw SQL: pages invoices through the same joined filter as the totals so counts and rows agree, with NULLS LAST ordering.
func listRows(ctx context.Context, db queryer, q collections.Query, f collections.RowFilter, offset, limit int) ([]collections.Row, bool, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	var from, order string
	joined := false
	switch f.Column {
	case types.CollectionsColumnNotDue:
		from, order = invoiceFrom(sc, &a, q, f.Column), "i.due_date ASC NULLS LAST, i.id ASC"
	case types.CollectionsColumnPaidThisWeek:
		from, order = invoiceFrom(sc, &a, q, f.Column), "i.paid_at DESC, i.id ASC"
	default:
		from, order, joined = overdueFrom(sc, &a, q, f), overdueOrder, true
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT %s %s ORDER BY %s LIMIT %s OFFSET %s`,
		rowColumns(joined), from, order, a.bind(limit+1), a.bind(offset)), a.values...)
	if err != nil {
		return nil, false, dbError(err, "Failed to list board rows")
	}
	list, err := scanRowList(ctx, rows)
	if err != nil {
		return nil, false, dbError(err, "Failed to list board rows")
	}
	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}

// Raw SQL: GROUPING SETS over a LEFT JOIN; ent has no grouping sets.
func (r *boardRepository) SummarizeReminders(ctx context.Context, asOf time.Time) (collections.ReminderSummary, error) {
	span := StartRepositorySpan(ctx, "board", "summarize_reminders", nil)
	defer FinishSpan(span)

	var a sqlArgs
	sc := newScope(ctx, &a)
	rows, err := r.client.Reader(ctx).QueryContext(ctx, fmt.Sprintf(
		`SELECT grouping(%[1]s, lower(i.currency)), %[1]s, lower(i.currency), count(*), sum(i.amount_remaining), count(DISTINCT i.customer_id)
		 %[2]s GROUP BY GROUPING SETS ((%[1]s, lower(i.currency)), ())`,
		effectiveStatus, overdueFrom(sc, &a, collections.Query{AsOf: asOf}, collections.RowFilter{})), a.values...)
	if err != nil {
		SetSpanError(span, err)
		return collections.ReminderSummary{}, dbError(err, "Failed to total overdue invoices")
	}
	statuses := map[types.ReminderStatus]int{}
	totals := map[string]decimal.Decimal{}
	invoices, customers := 0, 0
	if err := scanRows(rows, func() error {
		var (
			level            int
			status, currency sql.NullString
			count            int
			total            decimal.NullDecimal
			distinct         int
		)
		if err := rows.Scan(&level, &status, &currency, &count, &total, &distinct); err != nil {
			return err
		}
		if level != 0 {
			customers = distinct
			return nil
		}
		statuses[types.ReminderStatus(status.String)] += count
		invoices += count
		totals[currency.String] = totals[currency.String].Add(total.Decimal)
		return nil
	}); err != nil {
		SetSpanError(span, err)
		return collections.ReminderSummary{}, dbError(err, "Failed to total overdue invoices")
	}
	SetSpanSuccess(span)
	return *collections.NewReminderSummaryBuilder(nil).WithTotals(totalsOf(totals)).WithCounts(invoices, customers).WithStatuses(statuses).Build(), nil
}

// overdueInvoice is the part of an invoice a reminder row needs; selecting only these columns keeps GetRows light.
type overdueInvoice struct {
	ID              string          `json:"id"`
	InvoiceNumber   *string         `json:"invoice_number"`
	CustomerID      string          `json:"customer_id"`
	Currency        string          `json:"currency"`
	AmountDue       decimal.Decimal `json:"amount_due"`
	AmountRemaining decimal.Decimal `json:"amount_remaining"`
	DueDate         *time.Time      `json:"due_date"`
	PaidAt          *time.Time      `json:"paid_at"`
}

func (r *invoiceReminderRepository) GetRows(ctx context.Context, asOf time.Time, invoiceIDs []string) ([]collections.Row, error) {
	if len(invoiceIDs) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "invoice_reminder", "get_rows", map[string]interface{}{"invoices": len(invoiceIDs)})
	defer FinishSpan(span)

	tenantID, environmentID, live := types.GetTenantID(ctx), types.GetEnvironmentID(ctx), string(types.StatusPublished)
	db := r.client.Reader(ctx)
	var invoices []overdueInvoice
	err := db.Invoice.Query().
		Where(
			invoice.TenantID(tenantID),
			invoice.EnvironmentID(environmentID),
			invoice.Status(live),
			invoice.IDIn(invoiceIDs...),
			invoice.InvoiceStatus(types.InvoiceStatusFinalized),
			invoice.PaymentStatusNEQ(types.PaymentStatusSucceeded),
			invoice.AmountRemainingGT(decimal.Zero),
			invoice.DueDateLT(asOf),
		).
		Order(ent.Asc(invoice.FieldID)).
		Select(invoice.FieldID, invoice.FieldInvoiceNumber, invoice.FieldCustomerID, invoice.FieldCurrency,
			invoice.FieldAmountDue, invoice.FieldAmountRemaining, invoice.FieldDueDate, invoice.FieldPaidAt).
		Scan(ctx, &invoices)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to read overdue invoices")
	}
	stored, err := db.InvoiceReminder.Query().
		Where(
			invoicereminder.TenantID(tenantID),
			invoicereminder.EnvironmentID(environmentID),
			invoicereminder.Status(live),
			invoicereminder.InvoiceIDIn(invoiceIDs...),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to read reminders")
	}
	byInvoice := lo.SliceToMap(stored, func(e *ent.InvoiceReminder) (string, *collections.InvoiceReminder) {
		return e.InvoiceID, reminderFromEnt(e)
	})
	rows := lo.Map(invoices, func(i overdueInvoice, _ int) collections.Row {
		return *collections.NewRowBuilder(&collections.Row{InvoiceID: i.ID}).
			WithInvoice(lo.FromPtr(i.InvoiceNumber), i.CustomerID, strings.ToLower(i.Currency)).
			WithAmounts(i.AmountDue, i.AmountRemaining).
			WithDates(i.DueDate, i.PaidAt).
			WithReminder(byInvoice[i.ID]).
			Build()
	})
	SetSpanSuccess(span)
	return rows, nil
}

func (r *invoiceReminderRepository) createBuilder(ctx context.Context, d *collections.InvoiceReminder) *ent.InvoiceReminderCreate {
	if d.EnvironmentID == "" {
		d.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return r.client.Writer(ctx).InvoiceReminder.Create().
		SetID(d.ID).
		SetInvoiceID(d.InvoiceID).
		SetReminderStatus(string(d.ReminderStatus)).
		SetStatusReason(d.StatusReason).
		SetNillableApprovedAt(d.ApprovedAt).
		SetMetadata(d.Metadata).
		SetTone(string(d.Tone)).
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

// CreateMany inserts the reminders in one statement and returns the invoice ids it inserted. If a concurrent first action
// makes the batch violate the one-reminder-per-invoice index, the whole statement is rolled back by Postgres and each
// reminder is inserted on its own, so the invoices that already have a reminder are skipped.
func (r *invoiceReminderRepository) CreateMany(ctx context.Context, reminders []*collections.InvoiceReminder) ([]string, error) {
	if len(reminders) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "invoice_reminder", "create_many", map[string]interface{}{"reminders": len(reminders)})
	defer FinishSpan(span)

	builders := lo.Map(reminders, func(d *collections.InvoiceReminder, _ int) *ent.InvoiceReminderCreate { return r.createBuilder(ctx, d) })
	created, err := r.client.Writer(ctx).InvoiceReminder.CreateBulk(builders...).Save(ctx)
	if err == nil {
		SetSpanSuccess(span)
		return lo.Map(created, func(e *ent.InvoiceReminder, _ int) string { return e.InvoiceID }), nil
	}
	if !ent.IsConstraintError(err) {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to save the reminders")
	}
	inserted := []string{}
	for _, d := range reminders {
		_, err := r.createBuilder(ctx, d).Save(ctx)
		switch {
		case err == nil:
			inserted = append(inserted, d.InvoiceID)
		case ent.IsConstraintError(err):
		default:
			SetSpanError(span, err)
			return nil, dbError(err, "Failed to save the reminders")
		}
	}
	SetSpanSuccess(span)
	return inserted, nil
}

func (r *invoiceReminderRepository) Update(ctx context.Context, d *collections.InvoiceReminder, previous *collections.InvoiceReminder) error {
	span := StartRepositorySpan(ctx, "invoice_reminder", "update", map[string]interface{}{"invoice_id": d.InvoiceID})
	defer FinishSpan(span)

	update := r.client.Writer(ctx).InvoiceReminder.UpdateOneID(d.ID).
		Where(
			invoicereminder.TenantID(types.GetTenantID(ctx)),
			invoicereminder.EnvironmentID(types.GetEnvironmentID(ctx)),
			invoicereminder.Status(string(types.StatusPublished)),
			invoicereminder.ReminderStatus(string(previous.ReminderStatus)),
			invoicereminder.UpdatedAt(previous.UpdatedAt),
		).
		SetReminderStatus(string(d.ReminderStatus)).
		SetStatusReason(d.StatusReason).
		SetNillableApprovedAt(d.ApprovedAt).
		SetMetadata(d.Metadata).
		SetTone(string(d.Tone)).
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
		WithStatus(types.ReminderStatus(e.ReminderStatus), e.StatusReason, e.ApprovedAt).
		WithMetadata(e.Metadata).
		WithText(types.ReminderTone(e.Tone), e.Subject, e.Body).
		Build()
}
