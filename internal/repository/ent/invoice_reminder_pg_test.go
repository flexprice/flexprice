//go:build pgintegration

package ent_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entrepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Run against the local Postgres only (the one docker compose starts), with the local env file loaded:
//
//	set -a; . ./.env.local; set +a; go test -tags pgintegration ./internal/repository/ent -run "Board|InvoiceReminder"
//
// Every test creates its own tenant and environment ids and removes its rows afterwards.

type invSpec struct {
	id, customer, currency, invoiceStatus, paymentStatus, number string
	due, paidAt                                                  *time.Time
	remaining, total                                             int64
	deleted                                                      bool
	reminder                                                     types.ReminderStatus
}

type customerSpec struct{ id, name, email string }

type reminderFixture struct {
	ctx, other, otherEnv context.Context
	client               postgres.IClient
	board                collections.CollectionsRepository
	reminders            collections.InvoiceReminderRepository
	asOf                 time.Time
	run                  string
	customers            []customerSpec
	invoices             []invSpec
	// approvedAt is when each approved reminder was approved, by invoice id; every second approval ties with the one before.
	approvedAt map[string]time.Time
	approvals  int
}

func newReminderFixture(t *testing.T) *reminderFixture {
	t.Helper()
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)
	t.Cleanup(func() { _ = client.Close() })

	run := strings.ReplaceAll(types.GenerateUUID(), "-", "")[:12]
	scoped := func(tenant, env string) context.Context {
		return types.SetUserID(types.SetEnvironmentID(types.SetTenantID(context.Background(), tenant), env), "user_test")
	}
	f := &reminderFixture{
		ctx: scoped("tenant_pg_"+run, "env_pg_"+run), other: scoped("tenant_pg_"+run+"_b", "env_pg_"+run), otherEnv: scoped("tenant_pg_"+run, "env_pg_"+run+"_b"),
		client: client, board: entrepo.NewCollectionsRepository(client, log), reminders: entrepo.NewInvoiceReminderRepository(client, log),
		asOf: time.Now().UTC().Truncate(time.Microsecond), run: run,
	}
	t.Cleanup(func() {
		for _, table := range []string{"email_logs", "invoice_reminders", "invoices", "customers"} {
			_, err := client.Writer(context.Background()).ExecContext(context.Background(),
				"DELETE FROM "+table+" WHERE tenant_id = ANY($1)", pq.Array([]string{types.GetTenantID(f.ctx), types.GetTenantID(f.other)}))
			require.NoError(t, err)
		}
	})
	return f
}

func (f *reminderFixture) at(offset time.Duration) *time.Time {
	t := f.asOf.Add(offset).Truncate(time.Microsecond)
	return &t
}

func (f *reminderFixture) insertCustomer(t *testing.T, ctx context.Context, c customerSpec) {
	t.Helper()
	_, err := f.client.Writer(ctx).ExecContext(ctx,
		`INSERT INTO customers (id, tenant_id, environment_id, status, created_at, updated_at, external_id, name, email, tax_treatment)
		 VALUES ($1,$2,$3,'published',now(),now(),$4,$5,$6,'taxable')`,
		c.id, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), "ext_"+c.id, c.name, c.email)
	require.NoError(t, err)
}

func (f *reminderFixture) insertInvoice(t *testing.T, ctx context.Context, s invSpec) {
	t.Helper()
	status := string(types.StatusPublished)
	if s.deleted {
		status = string(types.StatusDeleted)
	}
	_, err := f.client.Writer(ctx).ExecContext(ctx,
		`INSERT INTO invoices (id, tenant_id, environment_id, status, created_at, updated_at, customer_id, invoice_type, invoice_status, payment_status,
		   currency, amount_due, amount_paid, amount_remaining, invoice_number, due_date, paid_at)
		 VALUES ($1,$2,$3,$4,now(),now(),$5,'ONE_OFF',$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		s.id, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), status, s.customer, s.invoiceStatus, s.paymentStatus,
		s.currency, s.total, s.total-s.remaining, s.remaining, s.number, s.due, s.paidAt)
	require.NoError(t, err)
}

func (f *reminderFixture) remind(t *testing.T, ctx context.Context, invoiceID string, status types.ReminderStatus, reason string) *collections.InvoiceReminder {
	t.Helper()
	var approvedAt *time.Time
	if status == types.ReminderStatusApproved {
		f.approvals++
		at := f.asOf.Add(-time.Duration(f.approvals/2) * time.Minute)
		approvedAt = &at
		if f.approvedAt == nil {
			f.approvedAt = map[string]time.Time{}
		}
		f.approvedAt[invoiceID] = at
	}

	r := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID: "irem_" + f.run + "_" + invoiceID, InvoiceID: invoiceID, BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithOriginal(types.ReminderToneFriendly, "subject", "body").WithText(types.ReminderToneFriendly, "subject", "body").
		WithStatus(status, reason, approvedAt).Build()
	require.NoError(t, f.reminders.Create(ctx, r))
	return r
}

// seed builds a deterministic mix: overdue invoices with amount ties, mixed-case currencies, reminders in every status, not due
// invoices (one without a due date), paid invoices around the window edge, excluded invoices, and look-alike rows of other scopes.
func (f *reminderFixture) seed(t *testing.T) {
	t.Helper()
	f.customers = []customerSpec{
		{"cust_a", "Acme Robotics", "ap@acme.example"}, {"cust_b", "Beta 100% Labs", "billing@beta.example"},
		{"cust_c", "Gamma_Co", "ap@gamma.example"}, {"cust_d", "Delta Ltd", "ap@delta.example"},
	}
	for _, c := range f.customers {
		f.insertCustomer(t, f.ctx, c)
	}
	currencies := []string{"usd", "USD", "eur", "gbp"}
	statuses := []types.ReminderStatus{"", "", types.ReminderStatusApproved, types.ReminderStatusHeld, types.ReminderStatusSkipped, types.ReminderStatusPending, ""}
	for i := 0; i < 44; i++ {
		f.invoices = append(f.invoices, invSpec{
			id: fmt.Sprintf("%s_od%02d", f.run, i), customer: f.customers[i%4].id, currency: currencies[(i/2)%4],
			invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: fmt.Sprintf("INV-%03d", i),
			remaining: int64(100 * (1 + i%3)), total: int64(100*(1+i%3)) + 10,
			due: f.at(-time.Duration(1+i%40) * 24 * time.Hour), reminder: statuses[i%len(statuses)],
		})
	}
	for i := 0; i < 12; i++ {
		due := f.at(time.Duration(1+i%6) * 24 * time.Hour)
		if i == 11 {
			due = nil
		}
		f.invoices = append(f.invoices, invSpec{
			id: fmt.Sprintf("%s_nd%02d", f.run, i), customer: f.customers[i%4].id, currency: currencies[i%4], invoiceStatus: "FINALIZED", paymentStatus: "PENDING",
			number: fmt.Sprintf("INV-ND%02d", i), remaining: 50, total: 50, due: due,
		})
	}
	f.invoices = append(f.invoices, invSpec{id: f.run + "_nd_asof", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-ASOF", remaining: 5, total: 5, due: &f.asOf})
	for i := 0; i < 10; i++ {
		f.invoices = append(f.invoices, invSpec{
			id: fmt.Sprintf("%s_pd%02d", f.run, i), customer: f.customers[i%4].id, currency: currencies[i%4], invoiceStatus: "FINALIZED", paymentStatus: "SUCCEEDED",
			number: fmt.Sprintf("INV-PD%02d", i), remaining: 0, total: int64(70 + i), due: f.at(-30 * 24 * time.Hour), paidAt: f.at(-time.Duration(1+i%3) * time.Hour),
		})
	}
	f.invoices = append(f.invoices,
		invSpec{id: f.run + "_pd_edge_in", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "SUCCEEDED", number: "INV-EDGEIN", total: 9, paidAt: f.at(-types.CollectionsPaidWindow)},
		invSpec{id: f.run + "_pd_edge_out", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "SUCCEEDED", number: "INV-EDGEOUT", total: 9, paidAt: f.at(-types.CollectionsPaidWindow - time.Microsecond)},
		invSpec{id: f.run + "_pd_over", customer: "cust_b", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "OVERPAID", number: "INV-OVER", total: 25, paidAt: f.at(-time.Hour)},
		invSpec{id: f.run + "_od_edge_from", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-EFROM", remaining: 11, total: 11, due: f.dayAt(-5)},
		invSpec{id: f.run + "_od_edge_before", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-EBEFORE", remaining: 12, total: 12, due: f.dayAtMinusMicro(-5)},
		invSpec{id: f.run + "_od_edge_to", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-ETO", remaining: 13, total: 13, due: f.dayAt(-2)},
		invSpec{id: f.run + "_x_draft", customer: "cust_a", currency: "usd", invoiceStatus: "DRAFT", paymentStatus: "PENDING", number: "INV-X1", remaining: 10, total: 10, due: f.at(-48 * time.Hour)},
		invSpec{id: f.run + "_x_void", customer: "cust_a", currency: "usd", invoiceStatus: "VOIDED", paymentStatus: "PENDING", number: "INV-X2", remaining: 10, total: 10, due: f.at(-48 * time.Hour)},
		invSpec{id: f.run + "_x_zero", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-X3", total: 10, due: f.at(-48 * time.Hour)},
		invSpec{id: f.run + "_x_deleted", customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-X4", remaining: 10, total: 10, due: f.at(-48 * time.Hour), deleted: true},
	)
	for _, s := range f.invoices {
		f.insertInvoice(t, f.ctx, s)
		if s.reminder != "" {
			reason := ""
			if s.reminder == types.ReminderStatusHeld {
				reason = "Promised to pay"
			}
			f.remind(t, f.ctx, s.id, s.reminder, reason)
		}
	}

	// Look-alike rows of another tenant and another environment must never show.
	for k, ctx := range []context.Context{f.other, f.otherEnv} {
		f.insertCustomer(t, ctx, customerSpec{fmt.Sprintf("cust_iso_%s_%d", f.run, k), "Acme Robotics", "ap@acme.example"})
		f.insertInvoice(t, ctx, invSpec{id: fmt.Sprintf("iso_%s_%d", f.run, k), customer: "cust_a", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING",
			number: "INV-ISO", remaining: 999999, total: 999999, due: f.at(-24 * time.Hour)})
	}
	t.Cleanup(func() {
		for _, table := range []string{"email_logs", "invoice_reminders", "invoices", "customers"} {
			_, err := f.client.Writer(context.Background()).ExecContext(context.Background(), "DELETE FROM "+table+" WHERE environment_id = $1", types.GetEnvironmentID(f.otherEnv))
			require.NoError(t, err)
		}
	})
}

// expectation, in plain Go.

func (f *reminderFixture) customerOf(id string) customerSpec {
	c, _ := lo.Find(f.customers, func(c customerSpec) bool { return c.id == id })
	return c
}

func (f *reminderFixture) matches(s invSpec, q collections.Query) bool {
	if q.Currency != "" && strings.ToLower(s.currency) != q.Currency {
		return false
	}
	if q.Text == "" {
		return true
	}
	c := f.customerOf(s.customer)
	return lo.SomeBy([]string{s.number, c.name, c.email}, func(field string) bool { return strings.Contains(strings.ToLower(field), strings.ToLower(q.Text)) })
}

func (f *reminderFixture) open(s invSpec) bool {
	return !s.deleted && s.invoiceStatus == "FINALIZED" && s.paymentStatus != "SUCCEEDED" && s.remaining > 0
}

func (f *reminderFixture) status(s invSpec) types.ReminderStatus {
	if s.reminder == "" {
		return types.ReminderStatusPending
	}
	return s.reminder
}

// expected returns the ids of a column's rows in order for a query.
func (f *reminderFixture) expected(q collections.Query, filter collections.InvoiceFilter) []invSpec {
	byApproval := filter.Column != types.CollectionsColumnNotDue && filter.Column != types.CollectionsColumnPaidThisWeek &&
		len(filter.Statuses) == 1 && filter.Statuses[0] == types.ReminderStatusApproved
	var rows []invSpec
	for _, s := range f.invoices {
		if !f.matches(s, q) || (filter.CustomerID != "" && s.customer != filter.CustomerID) {
			continue
		}
		switch filter.Column {
		case types.CollectionsColumnNotDue:
			if f.open(s) && (s.due == nil || !s.due.Before(q.AsOf)) && inRange(s.due, q.Range) {
				rows = append(rows, s)
			}
		case types.CollectionsColumnPaidThisWeek:
			paid := !s.deleted && s.invoiceStatus == "FINALIZED" && (s.paymentStatus == "SUCCEEDED" || s.paymentStatus == "OVERPAID") && s.paidAt != nil && !s.paidAt.After(q.AsOf)
			if paid && ((q.Range == nil && !s.paidAt.Before(q.AsOf.Add(-types.CollectionsPaidWindow))) || (q.Range != nil && inRange(s.paidAt, q.Range))) {
				rows = append(rows, s)
			}
		default:
			if f.open(s) && s.due != nil && s.due.Before(q.AsOf) && inRange(s.due, q.Range) && (len(filter.Statuses) == 0 || lo.Contains(filter.Statuses, f.status(s))) {
				if f.status(s) == types.ReminderStatusApproved && f.approvedAt[s.id].After(q.AsOf) {
					continue
				}
				rows = append(rows, s)
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch filter.Column {
		case types.CollectionsColumnNotDue:
			switch {
			case a.due == nil || b.due == nil:
				if a.due == nil && b.due == nil {
					return a.id < b.id
				}
				return a.due != nil
			case !a.due.Equal(*b.due):
				return a.due.Before(*b.due)
			}
			return a.id < b.id
		case types.CollectionsColumnPaidThisWeek:
			if !a.paidAt.Equal(*b.paidAt) {
				return a.paidAt.After(*b.paidAt)
			}
			return a.id < b.id
		}
		if byApproval {
			if !f.approvedAt[a.id].Equal(f.approvedAt[b.id]) {
				return f.approvedAt[a.id].After(f.approvedAt[b.id])
			}
			return a.id < b.id
		}
		if a.remaining != b.remaining {
			return a.remaining > b.remaining
		}
		return a.id < b.id
	})
	return rows
}

// untouched lists the overdue invoices nobody has a reminder for.
func (f *reminderFixture) untouched() []string {
	overdue := f.expected(collections.Query{AsOf: f.asOf}, collections.InvoiceFilter{Column: types.CollectionsColumnOverdue})
	return ids(lo.Filter(overdue, func(s invSpec, _ int) bool { return s.reminder == "" }))
}

func ids(rows []invSpec) []string { return lo.Map(rows, func(s invSpec, _ int) string { return s.id }) }
func rowIDs(rows []collections.InvoiceWithReminder) []string {
	return lo.Map(rows, func(r collections.InvoiceWithReminder, _ int) string { return r.InvoiceID })
}

func totalsOf(rows []invSpec, paid bool) map[string]string {
	sums := map[string]decimal.Decimal{}
	for _, s := range rows {
		amount := s.remaining
		if paid {
			amount = s.total
		}
		sums[strings.ToLower(s.currency)] = sums[strings.ToLower(s.currency)].Add(decimal.NewFromInt(amount))
	}
	return lo.MapValues(sums, func(d decimal.Decimal, _ string) string { return d.String() })
}

func totalsMap(totals []collections.CurrencyTotal) map[string]string {
	return lo.SliceToMap(totals, func(t collections.CurrencyTotal) (string, string) { return t.Currency, t.Amount.String() })
}

var boardColumnKeys = []types.CollectionsColumnKey{
	types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek,
}

func filterOfColumn(c types.CollectionsColumnKey) collections.InvoiceFilter {
	return collections.InvoiceFilter{Column: c, Statuses: types.ColumnStatuses(c)}
}

// dayAt is the start of the UTC day n days from the as_of day.
func (f *reminderFixture) dayAt(n int) *time.Time {
	d := types.UTCDay(f.asOf).AddDate(0, 0, n)
	return &d
}

// dayAtMinusMicro is the last microsecond before dayAt(n).
func (f *reminderFixture) dayAtMinusMicro(n int) *time.Time {
	d := f.dayAt(n).Add(-time.Microsecond)
	return &d
}

// ranges are date ranges of whole UTC days, as the service builds them: From inclusive, To exclusive.
func (f *reminderFixture) ranges() []*collections.DateRange {
	return []*collections.DateRange{
		{From: f.dayAt(-10), To: f.dayAt(1)},
		{From: f.dayAt(-30)},
		{To: f.dayAt(2)},
		{From: f.dayAt(1), To: f.dayAt(3)},
		{From: f.dayAt(-1), To: f.dayAt(1)},
		{From: f.dayAt(-5), To: f.dayAt(-2)},
		{From: f.dayAt(-400), To: f.dayAt(-300)},
	}
}

func (f *reminderFixture) queries() []collections.Query {
	asOf := f.asOf
	queries := []collections.Query{
		{AsOf: asOf}, {AsOf: asOf, Text: "acme"}, {AsOf: asOf, Text: "ROBOTICS"}, {AsOf: asOf, Text: "ap@beta"}, {AsOf: asOf, Text: "INV-01"},
		{AsOf: asOf, Text: "100%"}, {AsOf: asOf, Text: "%"}, {AsOf: asOf, Text: "_"}, {AsOf: asOf, Text: "gamma_"},
		{AsOf: asOf, Currency: "usd"}, {AsOf: asOf, Currency: "eur"}, {AsOf: asOf, Currency: "xyz"},
		{AsOf: asOf, Text: "acme", Currency: "usd"}, {AsOf: asOf, Text: "nothing matches this"},
	}
	for _, rng := range f.ranges() {
		queries = append(queries, collections.Query{AsOf: asOf, Range: rng}, collections.Query{AsOf: asOf, Range: rng, Text: "acme", Currency: "usd"})
	}
	return queries
}

// inRange reports whether an instant is inside the range; no range means anything, and a missing instant is outside any range.
func inRange(t *time.Time, r *collections.DateRange) bool {
	if r == nil {
		return true
	}
	if t == nil {
		return false
	}
	return (r.From == nil || !t.Before(*r.From)) && (r.To == nil || t.Before(*r.To))
}

func TestCollectionsRepository_BoardMatchesThePlainGoExpectation(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	for _, q := range f.queries() {
		for _, limit := range []int{0, 1, 3, 7, 500} {
			board, err := f.board.Board(f.ctx, q, limit)
			require.NoError(t, err)
			label := fmt.Sprintf("q=%+v limit=%d", q, limit)
			for _, key := range boardColumnKeys {
				want := f.expected(q, filterOfColumn(key))
				col := board.Columns[key]
				require.Equal(t, len(want), col.Summary.Count, "%s %s count", label, key)
				require.Equal(t, totalsOf(want, key == types.CollectionsColumnPaidThisWeek), totalsMap(col.Summary.Totals), "%s %s totals", label, key)
				if limit == 0 {
					require.Empty(t, col.Rows, label)
					continue
				}
				shown := want
				if len(shown) > limit {
					shown = shown[:limit]
				}
				require.Equal(t, ids(shown), rowIDs(col.Rows), "%s %s rows", label, key)
				require.Equal(t, len(want) > limit, col.HasMore, "%s %s has_more", label, key)
			}
		}
	}
}

func TestCollectionsRepository_AvailableCurrenciesIgnoreTheFilters(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	for _, q := range f.queries() {
		board, err := f.board.Board(f.ctx, q, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"eur", "gbp", "usd"}, board.AvailableCurrencies, "q=%+v", q)
	}
}

func TestCollectionsRepository_PagesWalkToTheEndInTheSameOrder(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	filters := []collections.InvoiceFilter{
		filterOfColumn(types.CollectionsColumnNotDue), filterOfColumn(types.CollectionsColumnOverdue), filterOfColumn(types.CollectionsColumnChasing),
		filterOfColumn(types.CollectionsColumnHeld), filterOfColumn(types.CollectionsColumnPaidThisWeek),
		{Column: types.CollectionsColumnOverdue}, {Column: types.CollectionsColumnOverdue, CustomerID: "cust_b"},
		{Column: types.CollectionsColumnOverdue, Statuses: []types.ReminderStatus{types.ReminderStatusPending}},
	}
	for _, filter := range filters {
		for _, q := range f.queries() {
			want := ids(f.expected(q, filter))
			for _, size := range []int{1, 2, 3, 4, 7, 15} {
				got := []string{}
				pages := 0
				for page := 0; page < 500; page++ {
					rows, hasMore, err := f.board.ListInvoicesWithReminders(f.ctx, q, filter, page*size, size)
					require.NoError(t, err)
					got = append(got, rowIDs(rows)...)
					pages++
					if !hasMore {
						break
					}
					require.Len(t, rows, size, "a page that has more is full")
				}
				require.Equal(t, want, got, "filter=%+v q=%+v size=%d", filter, q, size)
				require.Equal(t, max(1, (len(want)+size-1)/size), pages, "pages read: filter=%+v size=%d", filter, size)
			}
		}
	}
	past, hasMore, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, filterOfColumn(types.CollectionsColumnNotDue), 1000, 15)
	require.NoError(t, err)
	require.Empty(t, past)
	require.False(t, hasMore)
}

func TestCollectionsRepository_RowsCarryTheirReminder(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	rows, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, filterOfColumn(types.CollectionsColumnHeld), 0, 50)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, r := range rows {
		require.NotNil(t, r.Reminder)
		require.Contains(t, []types.ReminderStatus{types.ReminderStatusHeld, types.ReminderStatusSkipped}, r.Reminder.ReminderStatus)
		require.Equal(t, r.InvoiceID, r.Reminder.InvoiceID)
		require.False(t, r.Reminder.UpdatedAt.IsZero())
		if r.Reminder.ReminderStatus == types.ReminderStatusHeld {
			require.Equal(t, "Promised to pay", r.Reminder.StatusReason)
		}
	}
	pending, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, filterOfColumn(types.CollectionsColumnOverdue), 0, 100)
	require.NoError(t, err)
	require.True(t, lo.SomeBy(pending, func(r collections.InvoiceWithReminder) bool { return r.Reminder == nil }), "an invoice nobody acted on has no reminder")
	require.True(t, lo.SomeBy(pending, func(r collections.InvoiceWithReminder) bool { return r.Reminder != nil }), "a reopened reminder is pending with a row")
	notDue, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, filterOfColumn(types.CollectionsColumnNotDue), 0, 50)
	require.NoError(t, err)
	for _, r := range notDue {
		require.Nil(t, r.Reminder)
	}
}

func TestCollectionsRepository_SummarizeRemindersMatchesTheGoExpectation(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	for _, q := range f.queries() {
		got, err := f.board.SummarizeReminders(f.ctx, q)
		require.NoError(t, err)
		overdue := f.expected(q, collections.InvoiceFilter{Column: types.CollectionsColumnOverdue})
		require.Equal(t, len(overdue), got.InvoiceCount, "query %+v", q)
		require.Equal(t, totalsOf(overdue, false), totalsMap(got.Totals), "query %+v", q)
		require.Equal(t, len(lo.Uniq(lo.Map(overdue, func(s invSpec, _ int) string { return s.customer }))), got.CustomerCount, "query %+v", q)
		want := map[types.ReminderStatus]int{}
		for _, s := range overdue {
			want[f.status(s)]++
		}
		require.Equal(t, want, got.Statuses, "query %+v", q)
	}
}

// The reminder page and the board must show the same numbers for the same range, currency and search: board columns
// are the reminder statuses.
func TestCollectionsRepository_ReminderSummaryAgreesWithTheBoardForTheSameFilters(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	queries := append(f.queries(),
		collections.Query{AsOf: f.asOf, Currency: "eur", Range: f.ranges()[0]},
		collections.Query{AsOf: f.asOf, Currency: "usd", Range: f.ranges()[1]},
	)
	for _, q := range queries {
		summary, err := f.board.SummarizeReminders(f.ctx, q)
		require.NoError(t, err)
		board, err := f.board.Board(f.ctx, q, 0)
		require.NoError(t, err)

		require.Equal(t, board.Columns[types.CollectionsColumnOverdue].Summary.Count, summary.Statuses[types.ReminderStatusPending], "to review, query %+v", q)
		require.Equal(t, board.Columns[types.CollectionsColumnChasing].Summary.Count, summary.Statuses[types.ReminderStatusApproved], "approved, query %+v", q)
		require.Equal(t, board.Columns[types.CollectionsColumnHeld].Summary.Count, summary.Statuses[types.ReminderStatusHeld]+summary.Statuses[types.ReminderStatusSkipped], "held and skipped, query %+v", q)

		boardTotals := map[string]string{}
		for _, key := range []types.CollectionsColumnKey{types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld} {
			for _, total := range board.Columns[key].Summary.Totals {
				previous, _ := decimal.NewFromString(boardTotals[total.Currency])
				boardTotals[total.Currency] = previous.Add(total.Amount).String()
			}
		}
		summaryTotals := map[string]string{}
		for currency, amount := range totalsMap(summary.Totals) {
			summaryTotals[currency] = amount
		}
		require.Equal(t, boardTotals, summaryTotals, "overdue covered per currency, query %+v", q)
	}
}

func TestCollectionsRepository_ReminderSummaryOfOneCurrencyHasNoOthers(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	for _, currency := range []string{"usd", "eur", "xyz"} {
		got, err := f.board.SummarizeReminders(f.ctx, collections.Query{AsOf: f.asOf, Currency: currency})
		require.NoError(t, err)
		for _, total := range got.Totals {
			require.Equal(t, currency, total.Currency)
		}
	}
}

func TestCollectionsRepository_ReminderListPagesWithTheRange(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	for _, rng := range f.ranges() {
		q := collections.Query{AsOf: f.asOf, Range: rng}
		for _, statuses := range [][]types.ReminderStatus{nil, {types.ReminderStatusPending}, {types.ReminderStatusHeld, types.ReminderStatusSkipped}} {
			filter := collections.InvoiceFilter{Column: types.CollectionsColumnOverdue, Statuses: statuses}
			want := ids(f.expected(q, filter))

			var got []string
			for offset := 0; ; offset += 4 {
				page, hasMore, err := f.board.ListInvoicesWithReminders(f.ctx, q, filter, offset, 4)
				require.NoError(t, err)
				got = append(got, rowIDs(page)...)
				if !hasMore {
					break
				}
			}
			if len(want) == 0 {
				require.Empty(t, got, "range %+v statuses %v", rng, statuses)
				continue
			}
			require.Equal(t, want, got, "range %+v statuses %v", rng, statuses)
		}
	}
}

// A currency seen only on an older paid invoice is offered once the range reaches back to it.
func TestCollectionsRepository_BoardCurrenciesFollowThePaidColumn(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	f.insertInvoice(t, f.ctx, invSpec{id: f.run + "_pd_chf", customer: "cust_a", currency: "CHF", invoiceStatus: "FINALIZED", paymentStatus: "SUCCEEDED",
		number: "INV-CHF", total: 40, paidAt: f.at(-30 * 24 * time.Hour)})

	reaching, err := f.board.Board(f.ctx, collections.Query{AsOf: f.asOf, Range: &collections.DateRange{From: f.dayAt(-40)}}, 0)
	require.NoError(t, err)
	require.Equal(t, "40", totalsMap(reaching.Columns[types.CollectionsColumnPaidThisWeek].Summary.Totals)["chf"], "the paid column shows the CHF invoice")
	require.Equal(t, []string{"chf", "eur", "gbp", "usd"}, reaching.AvailableCurrencies)

	week, err := f.board.Board(f.ctx, collections.Query{AsOf: f.asOf, Currency: "usd"}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"eur", "gbp", "usd"}, week.AvailableCurrencies, "without a range the paid column is the last week")

	reminders, err := f.board.AvailableCurrencies(f.ctx, f.asOf)
	require.NoError(t, err)
	require.Equal(t, []string{"eur", "gbp", "usd"}, reminders, "the reminder list keeps open and recently paid invoices")
}

func TestCollectionsRepository_AvailableCurrenciesMatchTheBoard(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	board, err := f.board.Board(f.ctx, collections.Query{AsOf: f.asOf}, 0)
	require.NoError(t, err)
	got, err := f.board.AvailableCurrencies(f.ctx, f.asOf)
	require.NoError(t, err)
	require.Equal(t, board.AvailableCurrencies, got)
}

func TestCollectionsRepository_EmptyEnvironmentAndIsolation(t *testing.T) {
	f := newReminderFixture(t)
	empty, err := f.board.Board(f.ctx, collections.Query{AsOf: f.asOf}, 5)
	require.NoError(t, err)
	require.Empty(t, empty.AvailableCurrencies)
	for _, col := range empty.Columns {
		require.Zero(t, col.Summary.Count)
		require.Empty(t, col.Rows)
		require.False(t, col.HasMore)
	}

	f.seed(t)
	for _, q := range f.queries() {
		for _, key := range boardColumnKeys {
			rows, _, err := f.board.ListInvoicesWithReminders(f.ctx, q, filterOfColumn(key), 0, 500)
			require.NoError(t, err)
			for _, id := range rowIDs(rows) {
				require.False(t, strings.HasPrefix(id, "iso_"), "no row of another tenant or environment: %s", id)
			}
		}
	}
	other, err := f.board.Board(f.other, collections.Query{AsOf: f.asOf}, 500)
	require.NoError(t, err)
	require.Equal(t, 1, other.Columns[types.CollectionsColumnOverdue].Summary.Count)
	require.Equal(t, map[string]string{"usd": "999999"}, totalsMap(other.Columns[types.CollectionsColumnOverdue].Summary.Totals))

	otherEnv, err := f.board.Board(f.otherEnv, collections.Query{AsOf: f.asOf}, 500)
	require.NoError(t, err)
	require.Equal(t, 1, otherEnv.Columns[types.CollectionsColumnOverdue].Summary.Count)
	summary, err := f.board.SummarizeReminders(f.other, collections.Query{AsOf: f.asOf})
	require.NoError(t, err)
	require.Equal(t, 1, summary.InvoiceCount)
}

func TestCollectionsRepository_BoardRunsInsideATransactionWhenOneIsInContext(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	require.NoError(t, f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		_, err := f.client.Writer(txCtx).ExecContext(txCtx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
		require.NoError(t, err)
		board, err := f.board.Board(txCtx, collections.Query{AsOf: f.asOf}, 5)
		require.NoError(t, err)
		require.NotEmpty(t, board.AvailableCurrencies)
		return nil
	}))
}

func TestInvoiceReminderRepository_GetInvoicesWithRemindersReturnsOnlyOpenOverdueInvoices(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	overdue := f.expected(collections.Query{AsOf: f.asOf}, collections.InvoiceFilter{Column: types.CollectionsColumnOverdue})
	ask := append(ids(overdue[:5]), f.run+"_nd00", f.run+"_pd00", f.run+"_x_draft", f.run+"_x_void", f.run+"_x_zero", f.run+"_x_deleted", "missing", fmt.Sprintf("iso_%s_0", f.run))
	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, ask)
	require.NoError(t, err)
	require.ElementsMatch(t, ids(overdue[:5]), rowIDs(rows))
	for _, r := range rows {
		s, _ := lo.Find(f.invoices, func(s invSpec) bool { return s.id == r.InvoiceID })
		require.Equal(t, s.reminder != "", r.Reminder != nil)
		require.Equal(t, strings.ToLower(s.currency), r.Currency)
		require.Equal(t, decimal.NewFromInt(s.remaining).String(), r.AmountRemaining.String())
	}
	none, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestInvoiceReminderRepository_OneLiveReminderPerInvoiceAndSoftDeleteFreesIt(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	id := f.untouched()[0]
	first := f.remind(t, f.ctx, id, types.ReminderStatusApproved, "")
	require.False(t, first.CreatedAt.IsZero())

	dup := collections.NewInvoiceReminderBuilder(first).Build()
	dup.ID = "irem_dup_" + f.run
	require.True(t, errors.Is(f.reminders.Create(f.ctx, dup), ierr.ErrAlreadyExists), "a second live reminder for the invoice is refused")

	_, err := f.client.Writer(f.ctx).ExecContext(f.ctx, "UPDATE invoice_reminders SET status = 'deleted' WHERE id = $1", first.ID)
	require.NoError(t, err)
	require.NoError(t, f.reminders.Create(f.ctx, dup), "a soft-deleted reminder frees the invoice")
	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, []string{id})
	require.NoError(t, err)
	require.Equal(t, dup.ID, rows[0].Reminder.ID, "only the live reminder is read")
}

func TestInvoiceReminderRepository_UpdateComparesStatusAndTimestamp(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	id := f.untouched()[0]
	created := f.remind(t, f.ctx, id, types.ReminderStatusPending, "")

	stale := collections.NewInvoiceReminderBuilder(created).Build()
	held := collections.NewInvoiceReminderBuilder(created).WithStatus(types.ReminderStatusHeld, "wait", nil).Build()
	require.NoError(t, f.reminders.Update(f.ctx, held, created))
	require.Equal(t, types.ReminderStatusHeld, held.ReminderStatus)
	require.True(t, held.UpdatedAt.After(created.UpdatedAt), "updated_at moves on every write")

	approved := collections.NewInvoiceReminderBuilder(stale).WithStatus(types.ReminderStatusApproved, "", lo.ToPtr(f.asOf)).Build()
	require.True(t, errors.Is(f.reminders.Update(f.ctx, approved, stale), ierr.ErrVersionConflict), "an update from an older read is refused")

	sameStatusStale := collections.NewInvoiceReminderBuilder(held).WithText(types.ReminderToneFirm, "s", "b").Build()
	previous := collections.NewInvoiceReminderBuilder(held).Build()
	previous.UpdatedAt = previous.UpdatedAt.Add(-time.Second)
	require.True(t, errors.Is(f.reminders.Update(f.ctx, sameStatusStale, previous), ierr.ErrVersionConflict), "the timestamp is compared as well as the status")

	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, []string{id})
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusHeld, rows[0].Reminder.ReminderStatus)
	require.Equal(t, "wait", rows[0].Reminder.StatusReason)
	require.Nil(t, rows[0].Reminder.ApprovedAt)
}

func TestInvoiceReminderRepository_CreateManyIsAllOrNothing(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	pending := f.untouched()
	require.GreaterOrEqual(t, len(pending), 4)
	f.remind(t, f.ctx, pending[1], types.ReminderStatusHeld, "wait")

	now := f.asOf
	build := func(id string) *collections.InvoiceReminder {
		return collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{ID: "irem_bulk_" + f.run + "_" + id, InvoiceID: id, BaseModel: types.GetDefaultBaseModel(f.ctx)}).
			WithOriginal(types.ReminderToneFirm, "s\nwith newline", "b, with \"quotes\"").WithText(types.ReminderToneFirm, "s\nwith newline", "b, with \"quotes\"").
			WithStatus(types.ReminderStatusApproved, "", &now).Build()
	}

	err := f.reminders.CreateMany(f.ctx, lo.Map(pending[:3], func(id string, _ int) *collections.InvoiceReminder { return build(id) }))
	require.True(t, ierr.IsAlreadyExists(err), "got %v", err)
	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, pending[:3])
	require.NoError(t, err)
	byID := lo.KeyBy(rows, func(r collections.InvoiceWithReminder) string { return r.InvoiceID })
	require.Nil(t, byID[pending[0]].Reminder, "one duplicate inserts nothing")
	require.Nil(t, byID[pending[2]].Reminder)
	require.Equal(t, types.ReminderStatusHeld, byID[pending[1]].Reminder.ReminderStatus, "an existing reminder is untouched")

	fresh := []string{pending[0], pending[2]}
	require.NoError(t, f.reminders.CreateMany(f.ctx, lo.Map(fresh, func(id string, _ int) *collections.InvoiceReminder { return build(id) })))
	rows, err = f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, fresh)
	require.NoError(t, err)
	for _, row := range rows {
		r := row.Reminder
		require.Equal(t, types.ReminderStatusApproved, r.ReminderStatus)
		require.NotNil(t, r.ApprovedAt)
		require.True(t, r.ApprovedAt.Equal(now))
		require.Equal(t, "s\nwith newline", r.Subject)
		require.Equal(t, "b, with \"quotes\"", r.Original().OriginalBody)
		require.False(t, r.Edited())
	}
	require.NoError(t, f.reminders.CreateMany(f.ctx, nil))
}

func TestInvoiceReminderRepository_StoresMetadataAndText(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	id := f.untouched()[0]
	created := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID: "irem_meta_" + f.run, InvoiceID: id, BaseModel: types.GetDefaultBaseModel(f.ctx),
	}).
		WithStatus(types.ReminderStatusApproved, "", lo.ToPtr(f.asOf)).
		WithOriginal(types.ReminderToneFirm, "orig subject", "orig body").
		WithText(types.ReminderToneFinal, "new subject", "new body").
		Build()
	require.NoError(t, f.reminders.Create(f.ctx, created))

	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, []string{id})
	require.NoError(t, err)
	got := rows[0].Reminder
	require.Equal(t, collections.Metadata{OriginalTone: types.ReminderToneFirm, OriginalSubject: "orig subject", OriginalBody: "orig body"}, got.Original())
	require.Equal(t, "new subject", got.Subject)
	require.True(t, got.Edited())

	listed, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, filterOfColumn(types.CollectionsColumnChasing), 0, 500)
	require.NoError(t, err)
	row, ok := lo.Find(listed, func(r collections.InvoiceWithReminder) bool { return r.InvoiceID == id })
	require.True(t, ok)
	require.Equal(t, got.Original(), row.Reminder.Original(), "the joined board query decodes the same metadata")
	require.Equal(t, "new body", row.Reminder.Body)
}

// A reminder write and its email share one transaction: when the email insert fails, the reminder is rolled back too.
func TestInvoiceReminderRepository_ReminderAndEmailShareATransaction(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	emails := entrepo.NewEmailLogRepository(f.client, log)

	key := "reminder:" + f.run
	email := func(id string) *emaillog.EmailLog {
		return &emaillog.EmailLog{
			ID: id, EnvironmentID: types.GetEnvironmentID(f.ctx), EntityType: types.SystemEntityTypeInvoice, EntityID: "inv", CustomerID: "cust",
			TemplateKey: types.EmailTemplateInvoicePaymentReminder, Trigger: types.EmailTriggerManual, FromAddress: "a@b.test", ReplyTo: "r@b.test",
			ToAddresses: []string{"to@b.test"}, Subject: "s", Provider: types.EmailProviderLocal, DeliveryStatus: types.EmailDeliveryStatusQueued,
			IdempotencyKey: &key, Metadata: types.Metadata{"reminder_body": "frozen"}, BaseModel: types.GetDefaultBaseModel(f.ctx),
		}
	}
	require.NoError(t, emails.Create(f.ctx, email("email_a_"+f.run)))

	ids := f.untouched()
	err = f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		f.remind(t, txCtx, ids[0], types.ReminderStatusApproved, "")
		return emails.Create(txCtx, email("email_b_"+f.run))
	})
	require.Error(t, err, "the second email repeats the idempotency key")
	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, []string{ids[0]})
	require.NoError(t, err)
	require.Nil(t, rows[0].Reminder, "the reminder was rolled back with the email")

	err = f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		f.remind(t, txCtx, ids[1], types.ReminderStatusApproved, "")
		other := "other:" + f.run
		e := email("email_c_" + f.run)
		e.IdempotencyKey = &other
		return emails.Create(txCtx, e)
	})
	require.NoError(t, err)
	rows, err = f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, []string{ids[1]})
	require.NoError(t, err)
	require.NotNil(t, rows[0].Reminder)
	stored, err := emails.GetByIdempotencyKey(f.ctx, "other:"+f.run)
	require.NoError(t, err)
	require.Equal(t, "frozen", stored.Metadata["reminder_body"])
}

func TestCollectionsRepository_OverpaidInvoicesCountAsPaidThisWeek(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	rows, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, collections.InvoiceFilter{Column: types.CollectionsColumnPaidThisWeek}, 0, 200)
	require.NoError(t, err)
	require.Contains(t, rowIDs(rows), f.run+"_pd_over")
	over, _ := lo.Find(rows, func(r collections.InvoiceWithReminder) bool { return r.InvoiceID == f.run+"_pd_over" })
	require.Equal(t, "25", over.AmountTotal.String(), "a paid card shows the invoice total")
	require.NotNil(t, over.PaidAt)
}

func TestCollectionsRepository_BoardRefusesATransactionWithoutASnapshot(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	err := f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		_, err := f.board.Board(txCtx, collections.Query{AsOf: f.asOf}, 0)
		return err
	})
	require.Error(t, err, "a READ COMMITTED transaction cannot give the board one snapshot")
}

func TestInvoiceReminderRepository_CreateManyRollsBackWithItsTransaction(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	pending := f.untouched()
	require.GreaterOrEqual(t, len(pending), 2)
	f.remind(t, f.ctx, pending[1], types.ReminderStatusHeld, "wait")

	build := func(id string) *collections.InvoiceReminder {
		return collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
			ID: "irem_" + f.run + "_tx_" + id, InvoiceID: id, BaseModel: types.GetDefaultBaseModel(f.ctx),
		}).WithText(types.ReminderToneFriendly, "s", "b").Build()
	}

	err := f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		if err := f.reminders.Create(txCtx, build(pending[0])); err != nil {
			return err
		}
		return f.reminders.CreateMany(txCtx, []*collections.InvoiceReminder{build(pending[1])})
	})
	require.True(t, ierr.IsAlreadyExists(err), "got %v", err)

	rows, err := f.reminders.GetInvoicesWithReminders(f.ctx, f.asOf, pending[:1])
	require.NoError(t, err)
	require.Nil(t, rows[0].Reminder, "the write before the duplicate is rolled back with it")
}

func TestCollectionsRepository_ApprovedRemindersComeNewestApprovalFirst(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	approved := collections.InvoiceFilter{Column: types.CollectionsColumnOverdue, Statuses: []types.ReminderStatus{types.ReminderStatusApproved}}
	chasing := collections.InvoiceFilter{Column: types.CollectionsColumnChasing, Statuses: types.ColumnStatuses(types.CollectionsColumnChasing)}

	for _, q := range append(f.queries(), collections.Query{AsOf: f.asOf, Range: f.ranges()[1], Currency: "usd"}) {
		want := ids(f.expected(q, approved))
		for _, filter := range []collections.InvoiceFilter{approved, chasing} {
			var got []string
			for offset := 0; ; offset += 3 {
				page, hasMore, err := f.board.ListInvoicesWithReminders(f.ctx, q, filter, offset, 3)
				require.NoError(t, err)
				got = append(got, rowIDs(page)...)
				if !hasMore {
					break
				}
			}
			if len(want) == 0 {
				require.Empty(t, got)
				continue
			}
			require.Equal(t, want, got, "column %s, query %+v", filter.Column, q)
		}
	}

	all, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: f.asOf}, approved, 0, 200)
	require.NoError(t, err)
	for i := 1; i < len(all); i++ {
		prev, cur := f.approvedAt[all[i-1].InvoiceID], f.approvedAt[all[i].InvoiceID]
		require.False(t, cur.After(prev), "newest approval first")
		if cur.Equal(prev) {
			require.Less(t, all[i-1].InvoiceID, all[i].InvoiceID, "equal approval times fall back to the invoice id")
		}
	}
	require.NotEmpty(t, all)
	var first, last time.Time
	first, last = f.approvedAt[all[0].InvoiceID], f.approvedAt[all[len(all)-1].InvoiceID]
	require.True(t, first.After(last), "the seed holds distinct approval times")
}

func TestCollectionsRepository_ApprovalsAfterAsOfStayOutOfAPagedSession(t *testing.T) {
	f := newReminderFixture(t)
	f.seed(t)
	approved := collections.InvoiceFilter{Column: types.CollectionsColumnOverdue, Statuses: []types.ReminderStatus{types.ReminderStatusApproved}}
	q := collections.Query{AsOf: f.asOf}
	before, _, err := f.board.ListInvoicesWithReminders(f.ctx, q, approved, 0, 200)
	require.NoError(t, err)

	pending := f.untouched()[0]
	late := f.asOf.Add(time.Hour)
	r := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID: "irem_" + f.run + "_late", InvoiceID: pending, BaseModel: types.GetDefaultBaseModel(f.ctx),
	}).WithText(types.ReminderToneFriendly, "s", "b").WithStatus(types.ReminderStatusApproved, "", &late).Build()
	require.NoError(t, f.reminders.Create(f.ctx, r))

	after, _, err := f.board.ListInvoicesWithReminders(f.ctx, q, approved, 0, 200)
	require.NoError(t, err)
	require.Equal(t, rowIDs(before), rowIDs(after), "an approval made after as_of does not enter the session")

	board, err := f.board.Board(f.ctx, q, 0)
	require.NoError(t, err)
	require.Equal(t, len(before), board.Columns[types.CollectionsColumnChasing].Summary.Count, "nor the board's approved count")
	summary, err := f.board.SummarizeReminders(f.ctx, q)
	require.NoError(t, err)
	require.Equal(t, len(before), summary.Statuses[types.ReminderStatusApproved], "nor the reminder summary's")

	f.insertInvoice(t, f.ctx, invSpec{id: f.run + "_od_jpy", customer: "cust_a", currency: "JPY", invoiceStatus: "FINALIZED", paymentStatus: "PENDING",
		number: "INV-JPY", remaining: 10, total: 10, due: f.at(-48 * time.Hour)})
	jpy := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
		ID: "irem_" + f.run + "_jpy", InvoiceID: f.run + "_od_jpy", BaseModel: types.GetDefaultBaseModel(f.ctx),
	}).WithText(types.ReminderToneFriendly, "s", "b").WithStatus(types.ReminderStatusApproved, "", &late).Build()
	require.NoError(t, f.reminders.Create(f.ctx, jpy))
	board, err = f.board.Board(f.ctx, q, 0)
	require.NoError(t, err)
	require.Contains(t, board.AvailableCurrencies, "jpy", "an open invoice stays a currency choice while its late approval keeps it off the columns")

	fresh, _, err := f.board.ListInvoicesWithReminders(f.ctx, collections.Query{AsOf: late.Add(time.Minute)}, approved, 0, 200)
	require.NoError(t, err)
	require.Equal(t, pending, rowIDs(fresh)[0], "a fresh read starts with the newest approval")
}
