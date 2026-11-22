package ent

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	groupNumbersLimit = 25
	openInvoice       = "i.invoice_status = 'FINALIZED' AND i.payment_status <> 'SUCCEEDED' AND i.amount_remaining > 0"
	likeEscape        = ` ESCAPE '\'`
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// receivableSource serves the collections board from Flexprice's own finalized invoices and reminder decisions.
type receivableSource struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewReceivableSource(client postgres.IClient, log *logger.Logger) collections.ReceivableSource {
	return &receivableSource{client: client, log: log}
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

// textMatch matches the customer name, customer email or invoice number, with LIKE wildcards taken literally.
func (s scope) textMatch(a *sqlArgs, text string) string {
	p := a.bind("%" + likeEscaper.Replace(text) + "%")
	return fmt.Sprintf(`(i.invoice_number ILIKE %[1]s%[2]s OR i.customer_id IN (SELECT c.id FROM customers c`+
		` WHERE c.tenant_id = %[3]s AND c.environment_id = %[4]s AND c.status = %[5]s`+
		` AND (c.name ILIKE %[1]s%[2]s OR c.email ILIKE %[1]s%[2]s)))`, p, likeEscape, s.tenant, s.environment, s.status)
}

func (s scope) currency(a *sqlArgs, currency string) string {
	return "lower(i.currency) = " + a.bind(currency)
}

func and(conditions ...string) string {
	return strings.Join(lo.Filter(conditions, func(c string, _ int) bool { return c != "" }), " AND ")
}

func (r *receivableSource) reader(ctx context.Context) queryer {
	return r.client.Reader(ctx)
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

func dbError(err error, hint string) error {
	return ierr.WithError(err).WithHint(hint).Mark(ierr.ErrDatabase)
}

func (r *receivableSource) Board(ctx context.Context, q collections.Query, limit int) (*collections.Board, error) {
	span := StartRepositorySpan(ctx, "receivable", "board", nil)
	defer FinishSpan(span)

	var db queryer = r.reader(ctx)
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

func (r *receivableSource) board(ctx context.Context, db queryer, q collections.Query, limit int) (*collections.Board, error) {
	board := &collections.Board{Columns: map[types.CollectionsColumnKey]collections.BoardColumn{}}

	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek} {
		summary, err := r.summarizeInvoices(ctx, db, q, column)
		if err != nil {
			return nil, err
		}
		var (
			rows    []collections.Receivable
			hasMore bool
		)
		if limit > 0 {
			if rows, hasMore, err = r.listInvoices(ctx, db, q, column, 0, limit); err != nil {
				return nil, err
			}
		}
		board.Columns[column] = collections.BoardColumn{Summary: summary, Invoices: rows, HasMore: hasMore}
	}

	if err := r.boardGroups(ctx, db, q, limit, board); err != nil {
		return nil, err
	}
	if q.Text == "" && q.Currency == "" {
		board.AvailableCurrencies = currenciesOf(board)
		return board, nil
	}
	currencies, err := r.availableCurrencies(ctx, db, q.AsOf)
	if err != nil {
		return nil, err
	}
	board.AvailableCurrencies = currencies
	return board, nil
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

func (r *receivableSource) availableCurrencies(ctx context.Context, db queryer, asOf time.Time) ([]string, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	asOfP, sinceP := a.bind(asOf), a.bind(asOf.Add(-collections.PaidWindow))
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

func paidWindow(asOfP, sinceP string) string {
	return fmt.Sprintf("i.invoice_status = 'FINALIZED' AND i.payment_status = 'SUCCEEDED' AND i.paid_at >= %s AND i.paid_at <= %s", sinceP, asOfP)
}

func notDue(asOfP string) string {
	return fmt.Sprintf("%s AND (i.due_date IS NULL OR i.due_date >= %s)", openInvoice, asOfP)
}

// invoiceFilters are the predicates shared by the summary and the pages of an invoice column.
func invoiceFilters(sc scope, a *sqlArgs, q collections.Query, column types.CollectionsColumnKey) string {
	asOfP := a.bind(q.AsOf)
	var predicate string
	switch column {
	case types.CollectionsColumnPaidThisWeek:
		predicate = paidWindow(asOfP, a.bind(q.AsOf.Add(-collections.PaidWindow)))
	default:
		predicate = notDue(asOfP)
	}
	conditions := []string{sc.invoices(), predicate}
	if q.Currency != "" {
		conditions = append(conditions, sc.currency(a, q.Currency))
	}
	if q.Text != "" {
		conditions = append(conditions, sc.textMatch(a, q.Text))
	}
	return and(conditions...)
}

func (r *receivableSource) summarizeInvoices(ctx context.Context, db queryer, q collections.Query, column types.CollectionsColumnKey) (collections.ColumnSummary, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	amount := "i.amount_remaining"
	if column == types.CollectionsColumnPaidThisWeek {
		amount = "i.amount_due"
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT lower(i.currency), count(*), sum(%s) FROM invoices i WHERE %s GROUP BY 1`,
		amount, invoiceFilters(sc, &a, q, column)), a.values...)
	if err != nil {
		return collections.ColumnSummary{}, dbError(err, "Failed to total a board column")
	}
	summary := collections.ColumnSummary{Totals: []collections.CurrencyTotal{}}
	if err := scanRows(rows, func() error {
		var (
			currency string
			count    int
			total    decimal.Decimal
		)
		if err := rows.Scan(&currency, &count, &total); err != nil {
			return err
		}
		summary.Count += count
		summary.InvoiceCount += count
		summary.Totals = append(summary.Totals, collections.CurrencyTotal{Currency: currency, Amount: total})
		return nil
	}); err != nil {
		return collections.ColumnSummary{}, dbError(err, "Failed to total a board column")
	}
	sort.Slice(summary.Totals, func(i, j int) bool { return summary.Totals[i].Currency < summary.Totals[j].Currency })
	return summary, nil
}

func (r *receivableSource) ListInvoices(ctx context.Context, q collections.Query, column types.CollectionsColumnKey, offset, limit int) ([]collections.Receivable, bool, error) {
	span := StartRepositorySpan(ctx, "receivable", "list_invoices", map[string]interface{}{"column": column})
	defer FinishSpan(span)
	rows, hasMore, err := r.listInvoices(ctx, r.reader(ctx), q, column, offset, limit)
	if err != nil {
		SetSpanError(span, err)
	}
	return rows, hasMore, err
}

func (r *receivableSource) listInvoices(ctx context.Context, db queryer, q collections.Query, column types.CollectionsColumnKey, offset, limit int) ([]collections.Receivable, bool, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	conditions := []string{invoiceFilters(sc, &a, q, column)}
	order := "i.due_date ASC NULLS LAST, i.id ASC"
	status := types.ReceivableStatusOpen

	if column == types.CollectionsColumnPaidThisWeek {
		order, status = "i.paid_at DESC, i.id ASC", types.ReceivableStatusPaid
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT i.id, i.customer_id, coalesce(i.invoice_number, ''), lower(i.currency), i.amount_due, i.amount_remaining, i.due_date, i.paid_at
		 FROM invoices i WHERE %s ORDER BY %s LIMIT %s OFFSET %s`,
		and(conditions...), order, a.bind(limit+1), a.bind(offset)), a.values...)
	if err != nil {
		return nil, false, dbError(err, "Failed to list board invoices")
	}
	receivables, err := scanReceivables(rows, status)
	if err != nil {
		return nil, false, dbError(err, "Failed to list board invoices")
	}
	if hasMore := len(receivables) > limit; hasMore {
		return receivables[:limit], true, nil
	}
	return receivables, false, nil
}

func scanReceivables(rows *sql.Rows, status types.ReceivableStatus) ([]collections.Receivable, error) {
	receivables := []collections.Receivable{}
	err := scanRows(rows, func() error {
		var (
			r      = collections.Receivable{Source: types.ReceivableSourceFlexprice, Status: status}
			due    sql.NullTime
			paidAt sql.NullTime
			custID string
		)
		if err := rows.Scan(&r.ExternalID, &custID, &r.Number, &r.Currency, &r.AmountTotal, &r.AmountRemaining, &due, &paidAt); err != nil {
			return err
		}
		r.Customer = collections.CustomerRef{ID: custID}
		if due.Valid {
			r.DueDate = &due.Time
		}
		if paidAt.Valid {
			r.PaidAt = &paidAt.Time
		}
		receivables = append(receivables, r)
		return nil
	})
	return receivables, err
}

// groupConditions are the predicates on invoices that make up overdue groups; it reports whether the text
// filter must be applied per group (whole groups) instead of per invoice.
func groupConditions(sc scope, a *sqlArgs, q collections.Query, f collections.GroupFilter) (conditions []string, matches string) {
	conditions = []string{sc.invoices(), openInvoice, "i.due_date < " + a.bind(q.AsOf)}
	if q.Currency != "" {
		conditions = append(conditions, sc.currency(a, q.Currency))
	}
	if f.CustomerID != "" {
		conditions = append(conditions, "i.customer_id = "+a.bind(f.CustomerID))
	}
	if q.Text != "" {
		if f.WholeGroups {
			matches = ", COALESCE(bool_or(" + sc.textMatch(a, q.Text) + "), false) AS matches"
		} else {
			conditions = append(conditions, sc.textMatch(a, q.Text))
		}
	}
	return conditions, matches
}

// groupsCTE aggregates overdue receivables per customer and currency and attaches the reminder decision
// that applies, which is one made on invoices at least one of which is still in the group.
func groupsCTE(sc scope, a *sqlArgs, q collections.Query, f collections.GroupFilter) (cte string, wholeGroupMatch bool) {
	conditions, matches := groupConditions(sc, a, q, f)
	cte = fmt.Sprintf(`WITH a AS (
  SELECT i.customer_id AS customer_id, lower(i.currency) AS currency,
         sum(i.amount_remaining) AS amount, count(*) AS invoice_count, min(i.due_date) AS oldest_due,
         array_agg(i.id) AS ids%s
  FROM invoices i
  WHERE %s
  GROUP BY i.customer_id, lower(i.currency)
), g AS (
  SELECT a.*, d.draft_status AS draft_status, d.hold_reason AS hold_reason, d.tone_override AS tone_override,
         d.frozen_subject AS frozen_subject, d.frozen_tone AS frozen_tone,
         COALESCE(d.id IS NOT NULL AND jsonb_exists_any(d.invoice_ids, a.ids), false) AS applies
  FROM a
  LEFT JOIN reminder_decisions d ON d.tenant_id = %s AND d.environment_id = %s AND d.status = %s
       AND d.customer_id = a.customer_id AND d.currency = a.currency
), e AS MATERIALIZED (
  SELECT g.*, CASE WHEN applies THEN draft_status ELSE 'pending' END AS eff_status FROM g
)`, matches, and(conditions...), sc.tenant, sc.environment, sc.status)
	return cte, matches != ""
}

// fillSamples adds the oldest receivable numbers and the first receivable id to page groups; they are read
// for the page only, so the aggregate over all groups never sorts invoice numbers.
func (r *receivableSource) fillSamples(ctx context.Context, db queryer, q collections.Query, f collections.GroupFilter, groups []collections.ReceivableGroup) error {
	if len(groups) == 0 {
		return nil
	}
	var a sqlArgs
	sc := newScope(ctx, &a)
	if f.WholeGroups {
		q.Text = ""
	}
	conditions, _ := groupConditions(sc, &a, q, f)
	customers := lo.Map(groups, func(g collections.ReceivableGroup, _ int) string { return g.CustomerID })
	currencies := lo.Map(groups, func(g collections.ReceivableGroup, _ int) string { return g.Currency })
	conditions = append(conditions, fmt.Sprintf("(i.customer_id, lower(i.currency)) IN (SELECT * FROM unnest(%s::text[], %s::text[]))", a.bind(pq.Array(customers)), a.bind(pq.Array(currencies))))
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT customer_id, currency, id, number FROM (
		   SELECT i.customer_id AS customer_id, lower(i.currency) AS currency, i.id AS id, coalesce(i.invoice_number, '') AS number,
		          row_number() OVER (PARTITION BY i.customer_id, lower(i.currency) ORDER BY i.due_date, i.id) AS rn
		   FROM invoices i WHERE %s) x
		 WHERE rn <= %d ORDER BY customer_id, currency, rn`, and(conditions...), groupNumbersLimit), a.values...)
	if err != nil {
		return dbError(err, "Failed to read overdue group invoices")
	}
	index := map[collections.GroupKey]int{}
	for i, g := range groups {
		index[collections.GroupKey{CustomerID: g.CustomerID, Currency: g.Currency}] = i
	}
	return dbErrorIf(scanRows(rows, func() error {
		var customerID, currency, id, number string
		if err := rows.Scan(&customerID, &currency, &id, &number); err != nil {
			return err
		}
		i, ok := index[collections.GroupKey{CustomerID: customerID, Currency: currency}]
		if !ok {
			return nil
		}
		if groups[i].FirstExternalID == "" {
			groups[i].FirstExternalID = id
		}
		groups[i].Numbers = append(groups[i].Numbers, number)
		return nil
	}), "Failed to read overdue group invoices")
}

func dbErrorIf(err error, hint string) error {
	if err == nil {
		return nil
	}
	return dbError(err, hint)
}

// groupBucket is the board column of an effective reminder status; it mirrors collections.StatusColumn.
const groupBucket = `CASE eff_status WHEN 'approved' THEN 'chasing' WHEN 'held' THEN 'held' WHEN 'skipped' THEN 'held' ELSE 'overdue' END`

// boardGroups reads the three overdue-type columns with one pass over the groups: each column's totals and its first page.
func (r *receivableSource) boardGroups(ctx context.Context, db queryer, q collections.Query, limit int, board *collections.Board) error {
	var a sqlArgs
	sc := newScope(ctx, &a)
	cte, _ := groupsCTE(sc, &a, q, collections.GroupFilter{})
	totalsSQL := fmt.Sprintf(`SELECT 'sum', %[1]s, NULL, currency, sum(amount), sum(invoice_count), NULL, NULL, NULL, count(*)
FROM e GROUP BY %[1]s, currency`, groupBucket)
	query := cte + " " + totalsSQL
	if limit > 0 {
		query = cte + fmt.Sprintf(`
SELECT 'row' AS kind, bucket, customer_id, currency, amount, invoice_count, oldest_due, eff_status, CASE WHEN applies THEN coalesce(hold_reason, '') ELSE '' END, 1
FROM (SELECT e.*, %[1]s AS bucket,
             row_number() OVER (PARTITION BY %[1]s ORDER BY amount DESC, currency ASC, customer_id ASC) AS rn FROM e) ranked
WHERE rn <= %[2]s
UNION ALL
`, groupBucket, a.bind(limit+1)) + totalsSQL
	}
	rows, err := db.QueryContext(ctx, query, a.values...)
	if err != nil {
		return dbError(err, "Failed to read overdue groups")
	}

	pages := map[types.CollectionsColumnKey][]collections.ReceivableGroup{}
	summaries := map[types.CollectionsColumnKey]collections.ColumnSummary{}
	totals := map[types.CollectionsColumnKey]map[string]decimal.Decimal{}
	if err := scanRows(rows, func() error {
		var (
			kind, bucket, currency string
			customerID, status     sql.NullString
			amount                 decimal.Decimal
			invoices               int
			oldest                 sql.NullTime
			hold                   sql.NullString
			count                  int
		)
		if err := rows.Scan(&kind, &bucket, &customerID, &currency, &amount, &invoices, &oldest, &status, &hold, &count); err != nil {
			return err
		}
		column := types.CollectionsColumnKey(bucket)
		if kind == "sum" {
			s := summaries[column]
			s.Count += count
			s.InvoiceCount += invoices
			summaries[column] = s
			if totals[column] == nil {
				totals[column] = map[string]decimal.Decimal{}
			}
			totals[column][currency] = totals[column][currency].Add(amount)
			return nil
		}
		pages[column] = append(pages[column], collections.ReceivableGroup{
			Source: types.ReceivableSourceFlexprice, CustomerID: customerID.String, Currency: currency, Amount: amount,
			InvoiceCount: invoices, OldestDue: oldest.Time, Status: types.ReminderStatus(status.String), HoldReason: hold.String,
		})
		return nil
	}); err != nil {
		return dbError(err, "Failed to read overdue groups")
	}

	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld} {
		groups := pages[column]
		sortGroups(groups)
		hasMore := len(groups) > limit
		if hasMore {
			groups = groups[:limit]
		}
		if err := r.fillSamples(ctx, db, q, collections.GroupFilter{}, groups); err != nil {
			return err
		}
		summary := summaries[column]
		summary.Totals = currencyTotals(totals[column])
		board.Columns[column] = collections.BoardColumn{Summary: summary, Groups: groups, HasMore: hasMore}
	}
	return nil
}

func sortGroups(groups []collections.ReceivableGroup) {
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if !a.Amount.Equal(b.Amount) {
			return a.Amount.GreaterThan(b.Amount)
		}
		if a.Currency != b.Currency {
			return a.Currency < b.Currency
		}
		return a.CustomerID < b.CustomerID
	})
}

func toneExpression(a *sqlArgs, c collections.ToneCutoffs) string {
	friendly, firm := a.bind(c.Friendly), a.bind(c.Firm)
	return fmt.Sprintf(`(CASE
  WHEN applies AND draft_status = 'approved' AND coalesce(frozen_subject, '') <> '' THEN frozen_tone
  WHEN applies AND coalesce(tone_override, '') <> '' THEN tone_override
  WHEN oldest_due >= %s THEN 'friendly'
  WHEN oldest_due >= %s THEN 'firm'
  ELSE 'final' END)`, friendly, firm)
}

func (r *receivableSource) ListGroups(ctx context.Context, q collections.Query, filter collections.GroupFilter, offset, limit int) ([]collections.ReceivableGroup, bool, error) {
	span := StartRepositorySpan(ctx, "receivable", "list_groups", nil)
	defer FinishSpan(span)
	rows, hasMore, err := r.listGroups(ctx, r.reader(ctx), q, filter, offset, limit)
	if err != nil {
		SetSpanError(span, err)
	}
	return rows, hasMore, err
}

func (r *receivableSource) listGroups(ctx context.Context, db queryer, q collections.Query, f collections.GroupFilter, offset, limit int) ([]collections.ReceivableGroup, bool, error) {
	var a sqlArgs
	sc := newScope(ctx, &a)
	cte, whole := groupsCTE(sc, &a, q, f)

	var conditions []string
	if len(f.Statuses) > 0 {
		conditions = append(conditions, "eff_status = ANY("+a.bind(pq.Array(lo.Map(f.Statuses, func(s types.ReminderStatus, _ int) string { return string(s) })))+"::text[])")
	}
	if f.Tone != "" {
		conditions = append(conditions, toneExpression(&a, f.ToneCutoff)+" = "+a.bind(string(f.Tone)))
	}
	if whole {
		conditions = append(conditions, "matches")
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + and(conditions...)
	}
	rows, err := db.QueryContext(ctx, cte+fmt.Sprintf(
		` SELECT customer_id, currency, amount, invoice_count, oldest_due, eff_status, CASE WHEN applies THEN coalesce(hold_reason, '') ELSE '' END
		  FROM e%s ORDER BY amount DESC, currency ASC, customer_id ASC LIMIT %s OFFSET %s`, where, a.bind(limit+1), a.bind(offset)), a.values...)
	if err != nil {
		return nil, false, dbError(err, "Failed to list overdue groups")
	}
	groups := []collections.ReceivableGroup{}
	if err := scanRows(rows, func() error {
		g := collections.ReceivableGroup{Source: types.ReceivableSourceFlexprice}
		var status string
		if err := rows.Scan(&g.CustomerID, &g.Currency, &g.Amount, &g.InvoiceCount, &g.OldestDue, &status, &g.HoldReason); err != nil {
			return err
		}
		g.Status = types.ReminderStatus(status)
		groups = append(groups, g)
		return nil
	}); err != nil {
		return nil, false, dbError(err, "Failed to list overdue groups")
	}
	hasMore := len(groups) > limit
	if hasMore {
		groups = groups[:limit]
	}
	if err := r.fillSamples(ctx, db, q, f, groups); err != nil {
		return nil, false, err
	}
	return groups, hasMore, nil
}

func currencyTotals(totals map[string]decimal.Decimal) []collections.CurrencyTotal {
	out := lo.MapToSlice(totals, func(currency string, amount decimal.Decimal) collections.CurrencyTotal {
		return collections.CurrencyTotal{Currency: currency, Amount: amount}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

func (r *receivableSource) SummarizeGroups(ctx context.Context, asOf time.Time) (collections.GroupSummary, error) {
	span := StartRepositorySpan(ctx, "receivable", "summarize_groups", nil)
	defer FinishSpan(span)

	var a sqlArgs
	sc := newScope(ctx, &a)
	cte, _ := groupsCTE(sc, &a, collections.Query{AsOf: asOf}, collections.GroupFilter{})
	rows, err := r.reader(ctx).QueryContext(ctx, cte+
		` SELECT grouping(eff_status, currency), eff_status, currency, count(*), sum(invoice_count), sum(amount), count(DISTINCT customer_id)
		  FROM e GROUP BY GROUPING SETS ((eff_status, currency), ())`, a.values...)
	if err != nil {
		SetSpanError(span, err)
		return collections.GroupSummary{}, dbError(err, "Failed to total overdue groups")
	}
	summary := collections.GroupSummary{Totals: []collections.CurrencyTotal{}, Statuses: map[types.ReminderStatus]int{}}
	totals := map[string]decimal.Decimal{}
	if err := scanRows(rows, func() error {
		var (
			level            int
			status, currency sql.NullString
			count, invoices  sql.NullInt64
			total            decimal.NullDecimal
			customers        int
		)
		if err := rows.Scan(&level, &status, &currency, &count, &invoices, &total, &customers); err != nil {
			return err
		}
		if level != 0 {
			summary.CustomerCount = customers
			return nil
		}
		summary.Statuses[types.ReminderStatus(status.String)] += int(count.Int64)
		summary.InvoiceCount += int(invoices.Int64)
		totals[currency.String] = totals[currency.String].Add(total.Decimal)
		return nil
	}); err != nil {
		SetSpanError(span, err)
		return collections.GroupSummary{}, dbError(err, "Failed to total overdue groups")
	}
	summary.Totals = currencyTotals(totals)
	SetSpanSuccess(span)
	return summary, nil
}

func (r *receivableSource) GroupInvoices(ctx context.Context, asOf time.Time, keys []collections.GroupKey) ([]collections.Receivable, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "receivable", "group_invoices", map[string]interface{}{"groups": len(keys)})
	defer FinishSpan(span)

	var a sqlArgs
	sc := newScope(ctx, &a)
	customers := lo.Map(keys, func(k collections.GroupKey, _ int) string { return k.CustomerID })
	currencies := lo.Map(keys, func(k collections.GroupKey, _ int) string { return k.Currency })
	rows, err := r.reader(ctx).QueryContext(ctx, fmt.Sprintf(
		`SELECT i.id, i.customer_id, coalesce(i.invoice_number, ''), lower(i.currency), i.amount_due, i.amount_remaining, i.due_date, i.paid_at
		 FROM invoices i WHERE %s AND %s AND i.due_date < %s
		   AND (i.customer_id, lower(i.currency)) IN (SELECT * FROM unnest(%s::text[], %s::text[]))
		 ORDER BY i.due_date ASC, i.id ASC`,
		sc.invoices(), openInvoice, a.bind(asOf), a.bind(pq.Array(customers)), a.bind(pq.Array(currencies))), a.values...)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to list group invoices")
	}
	receivables, err := scanReceivables(rows, types.ReceivableStatusOpen)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to list group invoices")
	}
	SetSpanSuccess(span)
	return receivables, nil
}
