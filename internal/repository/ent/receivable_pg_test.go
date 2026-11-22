//go:build pgintegration

package ent_test

import (
	"context"
	"fmt"
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
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entrepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

// Run against the local Postgres only (the one docker compose starts), with the local env file loaded:
//
//	set -a; . ./.env.local; set +a; go test -tags pgintegration ./internal/repository/ent -run Receivable
//
// Every test creates its own tenant and environment ids and removes its rows afterwards.

type receivableFixture struct {
	ctx       context.Context
	other     context.Context
	client    postgres.IClient
	source    collections.ReceivableSource
	decisions collections.ReminderDecisionRepository
	asOf      time.Time
	run       string

	refCtx    context.Context
	reference *testutil.InMemoryReceivableSource
	refInv    *testutil.InMemoryInvoiceStore
	refCust   *testutil.InMemoryCustomerStore
	refDec    *testutil.InMemoryReminderDecisionStore
}

func newReceivableFixture(t *testing.T) *receivableFixture {
	t.Helper()
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	run := strings.ReplaceAll(types.GenerateUUID(), "-", "")[:16]
	scopeCtx := func(tenant, env string) context.Context {
		return types.SetUserID(types.SetEnvironmentID(types.SetTenantID(context.Background(), tenant), env), "user_test")
	}
	f := &receivableFixture{
		ctx:       scopeCtx("tenant_pg_"+run, "env_pg_"+run),
		other:     scopeCtx("tenant_pg_"+run+"_b", "env_pg_"+run),
		client:    client,
		source:    entrepo.NewReceivableSource(client, log),
		decisions: entrepo.NewReminderDecisionRepository(client, log),
		asOf:      time.Now().UTC().Truncate(time.Microsecond),
		run:       run,
		refInv:    testutil.NewInMemoryInvoiceStore(),
		refCust:   testutil.NewInMemoryCustomerStore(),
		refDec:    testutil.NewInMemoryReminderDecisionStore(),
	}
	f.refCtx = f.ctx
	f.reference = testutil.NewInMemoryReceivableSource(f.refInv, f.refCust, f.refDec)

	t.Cleanup(func() {
		for _, table := range []string{"reminder_decisions", "invoices", "customers"} {
			_, err := client.Writer(context.Background()).ExecContext(context.Background(),
				"DELETE FROM "+table+" WHERE tenant_id = ANY($1)", pq.Array([]string{types.GetTenantID(f.ctx), types.GetTenantID(f.other)}))
			require.NoError(t, err)
		}
	})
	return f
}

type invoiceSpec struct {
	id, customerID, currency, invoiceStatus, paymentStatus string
	number                                                 string
	due, paidAt                                            *time.Time
	remaining, total                                       int64
	deleted                                                bool
}

func (f *receivableFixture) addCustomer(t *testing.T, ctx context.Context, id, name, email string, alsoReference bool) {
	t.Helper()
	_, err := f.client.Writer(ctx).ExecContext(ctx,
		`INSERT INTO customers (id, tenant_id, environment_id, status, created_at, updated_at, external_id, name, email, tax_treatment)
		 VALUES ($1,$2,$3,'published',now(),now(),$4,$5,$6,'taxable')`,
		id, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), "ext_"+id, name, email)
	require.NoError(t, err)
	if alsoReference {
		require.NoError(t, f.refCust.Create(ctx, &customer.Customer{ID: id, Name: name, Email: email, BaseModel: types.GetDefaultBaseModel(ctx)}))
	}
}

func (f *receivableFixture) addInvoice(t *testing.T, ctx context.Context, s invoiceSpec, alsoReference bool) {
	t.Helper()
	status := string(types.StatusPublished)
	if s.deleted {
		status = string(types.StatusDeleted)
	}
	_, err := f.client.Writer(ctx).ExecContext(ctx,
		`INSERT INTO invoices (id, tenant_id, environment_id, status, created_at, updated_at, customer_id, invoice_type, invoice_status, payment_status,
		   currency, amount_due, amount_paid, amount_remaining, invoice_number, due_date, paid_at)
		 VALUES ($1,$2,$3,$4,now(),now(),$5,'ONE_OFF',$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		s.id, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), status, s.customerID, s.invoiceStatus, s.paymentStatus,
		s.currency, s.total, s.total-s.remaining, s.remaining, s.number, s.due, s.paidAt)
	require.NoError(t, err)
	if !alsoReference {
		return
	}
	inv := &invoice.Invoice{
		ID: s.id, CustomerID: s.customerID, InvoiceStatus: types.InvoiceStatus(s.invoiceStatus), PaymentStatus: types.PaymentStatus(s.paymentStatus),
		Currency: s.currency, AmountDue: decimal.NewFromInt(s.total), AmountRemaining: decimal.NewFromInt(s.remaining),
		InvoiceNumber: lo.ToPtr(s.number), DueDate: s.due, PaidAt: s.paidAt, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	inv.BaseModel.Status = types.Status(status)
	require.NoError(t, f.refInv.Create(ctx, inv))
}

func (f *receivableFixture) decide(t *testing.T, ctx context.Context, d *collections.ReminderDecision, alsoReference bool) {
	t.Helper()
	d.BaseModel = types.GetDefaultBaseModel(ctx)
	d.InvoiceSetHash = "hash"
	copyOf := *d
	require.NoError(t, f.decisions.Create(ctx, d))
	if alsoReference {
		require.NoError(t, f.refDec.Create(ctx, &copyOf))
	}
}

func (f *receivableFixture) at(offset time.Duration) *time.Time {
	t := f.asOf.Add(offset).Truncate(time.Microsecond)
	return &t
}

// seed builds a deterministic mix that covers every rule: both columns, ties, null due dates, mixed-case and
// several currencies, partial payments, excluded statuses and decisions that do and do not apply.
func (f *receivableFixture) seed(t *testing.T) {
	t.Helper()
	names := []string{"Acme Robotics", "Beta 100% Labs", "Gamma_Co", "Delta Ltd", "Epsilon Analytics", "Zeta Health", "Eta Freight", "Theta Foods"}
	for i, name := range names {
		slug := strings.ToLower(strings.Fields(strings.ReplaceAll(name, "_", " "))[0])
		f.addCustomer(t, f.ctx, fmt.Sprintf("cust_%d", i), name, "ap@"+slug+".example", true)
	}
	currencies := []string{"usd", "USD", "eur", "gbp"}
	for i := 0; i < 160; i++ {
		s := invoiceSpec{
			id:            fmt.Sprintf("inv_%03d", i),
			customerID:    fmt.Sprintf("cust_%d", i%len(names)),
			currency:      currencies[(i/3)%len(currencies)],
			invoiceStatus: string(types.InvoiceStatusFinalized),
			paymentStatus: string(types.PaymentStatusPending),
			number:        fmt.Sprintf("INV-%03d", i),
			remaining:     int64(100 * (1 + i%4)),
			total:         int64(100 * (1 + i%4)),
		}
		switch i % 10 {
		case 0, 1, 2, 3:
			s.due = f.at(-time.Duration(1+i%40) * 24 * time.Hour)
		case 4, 5:
			s.due = f.at(time.Duration(1+i%30) * 24 * time.Hour)
		case 6:
			s.paymentStatus, s.remaining = string(types.PaymentStatusSucceeded), 0
			s.paidAt = f.at(-time.Duration(1+i%160) * time.Hour)
		case 7:
			s.invoiceStatus = []string{"DRAFT", "VOIDED", "SKIPPED"}[i%3]
			s.due = f.at(-24 * time.Hour)
		case 8:
			s.remaining, s.due = 0, f.at(-48*time.Hour)
		case 9:
			if i%20 == 9 {
				s.due = nil
			} else {
				s.due, s.deleted = f.at(-72*time.Hour), true
			}
		}
		if i == 6 {
			s.paidAt = f.at(-7 * 24 * time.Hour)
		}
		if i == 16 {
			s.paidAt = f.at(-7*24*time.Hour - time.Microsecond)
		}
		f.addInvoice(t, f.ctx, s, true)
	}
	f.addInvoice(t, f.ctx, invoiceSpec{id: "inv_due_asof", customerID: "cust_0", currency: "usd", invoiceStatus: "FINALIZED", paymentStatus: "PENDING", number: "INV-ASOF", due: &f.asOf, remaining: 50, total: 50}, true)

	groups, _, err := f.reference.ListGroups(f.ctx, collections.Query{AsOf: f.asOf}, collections.GroupFilter{}, 0, 1000)
	require.NoError(t, err)
	statuses := []types.ReminderStatus{types.ReminderStatusPending, types.ReminderStatusApproved, types.ReminderStatusHeld, types.ReminderStatusSkipped}
	for i, g := range groups {
		if i%2 == 1 {
			continue
		}
		rows, err := f.reference.GroupInvoices(f.ctx, f.asOf, []collections.GroupKey{{CustomerID: g.CustomerID, Currency: g.Currency}})
		require.NoError(t, err)
		ids := []string{rows[0].ExternalID}
		if i%4 == 2 {
			ids = []string{"inv_gone"}
		}
		d := &collections.ReminderDecision{
			ID: fmt.Sprintf("rd_%s_%d", f.run, i), CustomerID: g.CustomerID, Currency: g.Currency,
			Status: statuses[i%4], InvoiceIDs: ids,
		}
		switch d.Status {
		case types.ReminderStatusHeld:
			d.HoldReason = "Promised to pay"
		case types.ReminderStatusApproved:
			d.FrozenSubject, d.FrozenBody, d.FrozenTone = "s", "b", types.ReminderTone([]string{"friendly", "firm", "final"}[i%3])
		case types.ReminderStatusPending:
			if i%3 == 0 {
				d.ToneOverride, d.SubjectOverride = types.ReminderToneFinal, "mine"
			}
		}
		f.decide(t, f.ctx, d, true)
	}

	// Another tenant and another environment hold look-alike rows that must never show.
	otherEnv := types.SetEnvironmentID(f.ctx, "env_other_"+types.GetEnvironmentID(f.ctx))
	for k, ctx := range []context.Context{f.other, otherEnv} {
		f.addCustomer(t, ctx, fmt.Sprintf("cust_iso_%s_%d", f.run, k), "Acme Robotics", "ap@acme.example", false)
		f.addInvoice(t, ctx, invoiceSpec{id: fmt.Sprintf("iso_%s_%d", f.run, k), customerID: "cust_0", currency: "usd", invoiceStatus: "FINALIZED",
			paymentStatus: "PENDING", number: "INV-ISO", due: f.at(-24 * time.Hour), remaining: 999999, total: 999999}, false)
		f.decide(t, ctx, &collections.ReminderDecision{ID: fmt.Sprintf("rd_iso_%s_%d", f.run, k), CustomerID: "cust_0", Currency: "usd",
			Status: types.ReminderStatusApproved, InvoiceIDs: []string{"inv_000"}}, false)
	}
	t.Cleanup(func() {
		_, err := f.client.Writer(context.Background()).ExecContext(context.Background(), "DELETE FROM invoices WHERE environment_id = $1", types.GetEnvironmentID(otherEnv))
		require.NoError(t, err)
		_, err = f.client.Writer(context.Background()).ExecContext(context.Background(), "DELETE FROM customers WHERE environment_id = $1", types.GetEnvironmentID(otherEnv))
		require.NoError(t, err)
		_, err = f.client.Writer(context.Background()).ExecContext(context.Background(), "DELETE FROM reminder_decisions WHERE environment_id = $1", types.GetEnvironmentID(otherEnv))
		require.NoError(t, err)
	})
}

func micro(t time.Time) int64 { return t.UnixMicro() }

func normReceivables(rows []collections.Receivable) []string {
	return lo.Map(rows, func(r collections.Receivable, _ int) string {
		due, paid := int64(-1), int64(-1)
		if r.DueDate != nil {
			due = micro(*r.DueDate)
		}
		if r.PaidAt != nil {
			paid = micro(*r.PaidAt)
		}
		return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%d|%s", r.ExternalID, r.Customer.ID, r.Number, r.Currency, r.AmountTotal, r.AmountRemaining, due, paid, r.Status)
	})
}

func normGroups(rows []collections.ReceivableGroup) []string {
	return lo.Map(rows, func(g collections.ReceivableGroup, _ int) string {
		return fmt.Sprintf("%s|%s|%s|%d|%s|%v|%d|%s|%s", g.CustomerID, g.Currency, g.Amount, g.InvoiceCount, g.FirstExternalID, g.Numbers, micro(g.OldestDue), g.Status, g.HoldReason)
	})
}

func normSummary(s collections.ColumnSummary) string {
	return fmt.Sprintf("%d|%d|%v", s.Count, s.InvoiceCount, lo.Map(s.Totals, func(t collections.CurrencyTotal, _ int) string { return t.Currency + "=" + t.Amount.String() }))
}

func queries(asOf time.Time) []collections.Query {
	return []collections.Query{
		{AsOf: asOf},
		{AsOf: asOf, Text: "acme"},
		{AsOf: asOf, Text: "ROBOTICS"},
		{AsOf: asOf, Text: "ap@beta"},
		{AsOf: asOf, Text: "INV-01"},
		{AsOf: asOf, Text: "100%"},
		{AsOf: asOf, Text: "%"},
		{AsOf: asOf, Text: "_"},
		{AsOf: asOf, Text: "gamma_"},
		{AsOf: asOf, Currency: "usd"},
		{AsOf: asOf, Currency: "eur"},
		{AsOf: asOf, Currency: "xyz"},
		{AsOf: asOf, Text: "acme", Currency: "usd"},
		{AsOf: asOf, Text: "nothing matches this"},
	}
}

func TestReceivableSource_BoardMatchesTheReferenceForEveryFilter(t *testing.T) {
	f := newReceivableFixture(t)
	f.seed(t)
	for _, q := range queries(f.asOf) {
		for _, limit := range []int{0, 1, 3, 7, 500} {
			got, err := f.source.Board(f.ctx, q, limit)
			require.NoError(t, err)
			want, err := f.reference.Board(f.refCtx, q, limit)
			require.NoError(t, err)

			label := fmt.Sprintf("q=%+v limit=%d", q, limit)
			require.Equal(t, want.AvailableCurrencies, got.AvailableCurrencies, label)
			for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld, types.CollectionsColumnPaidThisWeek} {
				g, w := got.Columns[column], want.Columns[column]
				require.Equal(t, normSummary(w.Summary), normSummary(g.Summary), "%s %s summary", label, column)
				require.Equal(t, normReceivables(w.Invoices), normReceivables(g.Invoices), "%s %s invoices", label, column)
				require.Equal(t, normGroups(w.Groups), normGroups(g.Groups), "%s %s groups", label, column)
				require.Equal(t, w.HasMore, g.HasMore, "%s %s has_more", label, column)
			}
		}
	}
}

func TestReceivableSource_InvoicePagesWalkLikeTheReference(t *testing.T) {
	f := newReceivableFixture(t)
	f.seed(t)
	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek} {
		for _, q := range queries(f.asOf) {
			for _, limit := range []int{1, 2, 3, 4, 7, 15} {
				var got, want []collections.Receivable
				for _, side := range []struct {
					src  collections.ReceivableSource
					ctx  context.Context
					into *[]collections.Receivable
				}{{f.source, f.ctx, &got}, {f.reference, f.refCtx, &want}} {
					for page := 0; page < 500; page++ {
						rows, hasMore, err := side.src.ListInvoices(side.ctx, q, column, page*limit, limit)
						require.NoError(t, err)
						*side.into = append(*side.into, rows...)
						if !hasMore {
							break
						}
					}
				}
				require.Equal(t, normReceivables(want), normReceivables(got), "%s q=%+v limit=%d", column, q, limit)
				require.Len(t, lo.Uniq(lo.Map(got, func(r collections.Receivable, _ int) string { return r.ExternalID })), len(got), "no repeats")
			}
		}
	}
}

func TestReceivableSource_GroupPagesWalkLikeTheReference(t *testing.T) {
	f := newReceivableFixture(t)
	f.seed(t)
	cutoffs := collections.ToneCutoffs{Friendly: *f.at(-30 * 24 * time.Hour), Firm: *f.at(-60 * 24 * time.Hour)}
	filters := []collections.GroupFilter{
		{},
		{Statuses: []types.ReminderStatus{types.ReminderStatusPending}},
		{Statuses: []types.ReminderStatus{types.ReminderStatusHeld, types.ReminderStatusSkipped}},
		{Statuses: []types.ReminderStatus{types.ReminderStatusApproved}},
		{Tone: types.ReminderToneFriendly, ToneCutoff: cutoffs},
		{Tone: types.ReminderToneFirm, ToneCutoff: cutoffs},
		{Tone: types.ReminderToneFinal, ToneCutoff: cutoffs},
		{CustomerID: "cust_0"},
		{WholeGroups: true},
	}
	for _, filter := range filters {
		for _, q := range queries(f.asOf) {
			for _, limit := range []int{1, 2, 3, 4, 7, 15} {
				walk := func(src collections.ReceivableSource, ctx context.Context) []collections.ReceivableGroup {
					var all []collections.ReceivableGroup
					for page := 0; page < 500; page++ {
						rows, hasMore, err := src.ListGroups(ctx, q, filter, page*limit, limit)
						require.NoError(t, err)
						all = append(all, rows...)
						if !hasMore {
							break
						}
					}
					return all
				}
				got, want := walk(f.source, f.ctx), walk(f.reference, f.refCtx)
				require.Equal(t, normGroups(want), normGroups(got), "filter=%+v q=%+v limit=%d", filter, q, limit)
			}
		}
	}
}

func TestReceivableSource_GroupSummaryAndInvoicesMatchTheReference(t *testing.T) {
	f := newReceivableFixture(t)
	f.seed(t)
	got, err := f.source.SummarizeGroups(f.ctx, f.asOf)
	require.NoError(t, err)
	want, err := f.reference.SummarizeGroups(f.refCtx, f.asOf)
	require.NoError(t, err)
	require.Equal(t, normSummary(collections.ColumnSummary{Count: 0, InvoiceCount: want.InvoiceCount, Totals: want.Totals}),
		normSummary(collections.ColumnSummary{Count: 0, InvoiceCount: got.InvoiceCount, Totals: got.Totals}))
	require.Equal(t, want.CustomerCount, got.CustomerCount)
	require.Equal(t, want.Statuses, got.Statuses)

	keys := []collections.GroupKey{{CustomerID: "cust_0", Currency: "usd"}, {CustomerID: "cust_1", Currency: "eur"}, {CustomerID: "cust_3", Currency: "gbp"}, {CustomerID: "cust_missing", Currency: "usd"}}
	gotRows, err := f.source.GroupInvoices(f.ctx, f.asOf, keys)
	require.NoError(t, err)
	wantRows, err := f.reference.GroupInvoices(f.refCtx, f.asOf, keys)
	require.NoError(t, err)
	require.Equal(t, normReceivables(wantRows), normReceivables(gotRows))
	require.NotEmpty(t, gotRows)

	none, err := f.source.GroupInvoices(f.ctx, f.asOf, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestReceivableSource_EmptyTenantAndIsolation(t *testing.T) {
	f := newReceivableFixture(t)
	empty, err := f.source.Board(f.ctx, collections.Query{AsOf: f.asOf}, 5)
	require.NoError(t, err)
	require.Empty(t, empty.AvailableCurrencies)
	for _, column := range empty.Columns {
		require.Zero(t, column.Summary.Count)
		require.Empty(t, column.Invoices)
		require.Empty(t, column.Groups)
		require.False(t, column.HasMore)
	}

	f.seed(t)
	mine, err := f.source.Board(f.ctx, collections.Query{AsOf: f.asOf}, 500)
	require.NoError(t, err)
	all := lo.Flatten(lo.Map(lo.Values(mine.Columns), func(c collections.BoardColumn, _ int) []string {
		return append(lo.Map(c.Invoices, func(r collections.Receivable, _ int) string { return r.ExternalID }),
			lo.Map(c.Groups, func(g collections.ReceivableGroup, _ int) string { return g.FirstExternalID })...)
	}))
	for _, id := range all {
		require.False(t, strings.HasPrefix(id, "iso_"), "no row of another tenant or environment: %s", id)
	}
	for _, g := range mine.Columns[types.CollectionsColumnChasing].Groups {
		require.NotEqual(t, "cust_0|usd", g.CustomerID+"|"+g.Currency+"|isolated", "decisions of other scopes never apply")
	}

	other, err := f.source.Board(f.other, collections.Query{AsOf: f.asOf}, 500)
	require.NoError(t, err)
	require.Equal(t, 1, other.Columns[types.CollectionsColumnOverdue].Summary.Count+other.Columns[types.CollectionsColumnChasing].Summary.Count, "the other tenant sees only its own one invoice")
	require.Equal(t, "999999", other.Columns[types.CollectionsColumnOverdue].Summary.Totals[0].Amount.String()+"", "and its own amount")

	rows, err := f.source.GroupInvoices(f.other, f.asOf, []collections.GroupKey{{CustomerID: "cust_0", Currency: "usd"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "999999", rows[0].AmountRemaining.String())

	decisions, err := f.decisions.ListByKeys(f.ctx, []collections.GroupKey{{CustomerID: "cust_0", Currency: "usd"}})
	require.NoError(t, err)
	for _, d := range decisions {
		require.Equal(t, types.GetTenantID(f.ctx), d.TenantID)
		require.Equal(t, types.GetEnvironmentID(f.ctx), d.EnvironmentID)
	}
}

func TestReceivableSource_BoardRunsInsideATransactionWhenOneIsInContext(t *testing.T) {
	f := newReceivableFixture(t)
	f.seed(t)
	err := f.client.WithTx(f.ctx, func(txCtx context.Context) error {
		board, err := f.source.Board(txCtx, collections.Query{AsOf: f.asOf}, 5)
		require.NoError(t, err)
		require.NotEmpty(t, board.AvailableCurrencies)
		return nil
	})
	require.NoError(t, err)
}

func TestReminderDecisionRepository_VersionedWrites(t *testing.T) {
	f := newReceivableFixture(t)
	f.addCustomer(t, f.ctx, "cust_v", "V", "v@example.com", false)
	d := &collections.ReminderDecision{ID: "rd_v_" + f.run, CustomerID: "cust_v", Currency: "usd", Status: types.ReminderStatusApproved, InvoiceIDs: []string{"i1"}, FrozenSubject: "s"}
	f.decide(t, f.ctx, d, false)
	require.Equal(t, 1, d.Version)

	stale := *d
	d.Status, d.HoldReason = types.ReminderStatusHeld, "wait"
	require.NoError(t, f.decisions.Update(f.ctx, d))
	require.Equal(t, 2, d.Version)

	stale.Status = types.ReminderStatusSkipped
	require.True(t, errors.Is(f.decisions.Update(f.ctx, &stale), ierr.ErrVersionConflict), "an update from an older read is refused")
	require.True(t, errors.Is(f.decisions.Delete(f.ctx, &stale), ierr.ErrVersionConflict), "so is a delete")

	dup := &collections.ReminderDecision{ID: "rd_v2_" + f.run, CustomerID: "cust_v", Currency: "usd", Status: types.ReminderStatusPending, InvoiceIDs: []string{"i2"}, InvoiceSetHash: "h", BaseModel: types.GetDefaultBaseModel(f.ctx)}
	require.True(t, errors.Is(f.decisions.Create(f.ctx, dup), ierr.ErrAlreadyExists), "one decision per customer and currency")

	got, err := f.decisions.ListByKeys(f.ctx, []collections.GroupKey{{CustomerID: "cust_v", Currency: "usd"}, {CustomerID: "cust_v", Currency: "eur"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, types.ReminderStatusHeld, got[0].Status)
	require.Equal(t, "wait", got[0].HoldReason)

	require.NoError(t, f.decisions.Delete(f.ctx, d))
	again, err := f.decisions.ListByKeys(f.ctx, []collections.GroupKey{{CustomerID: "cust_v", Currency: "usd"}})
	require.NoError(t, err)
	require.Empty(t, again)
	replacement := &collections.ReminderDecision{ID: "rd_v3_" + f.run, CustomerID: "cust_v", Currency: "usd", Status: types.ReminderStatusPending, InvoiceIDs: []string{"i3"}, InvoiceSetHash: "h", BaseModel: types.GetDefaultBaseModel(f.ctx)}
	require.NoError(t, f.decisions.Create(f.ctx, replacement), "a deleted decision frees the key")
}
