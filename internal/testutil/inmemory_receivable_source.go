package testutil

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryReceivableSource is the reference implementation of collections.ReceivableSource over the in-memory stores.
// It computes everything in Go with the semantics the Postgres source expresses in SQL, and exists so services can be
// tested without a database and so the SQL can be checked against it.
type InMemoryReceivableSource struct {
	invoices  *InMemoryInvoiceStore
	customers *InMemoryCustomerStore
	decisions *InMemoryReminderDecisionStore
}

func NewInMemoryReceivableSource(invoices *InMemoryInvoiceStore, customers *InMemoryCustomerStore, decisions *InMemoryReminderDecisionStore) *InMemoryReceivableSource {
	return &InMemoryReceivableSource{invoices: invoices, customers: customers, decisions: decisions}
}

const referenceGroupNumbers = 25

type referenceInvoice struct {
	inv      *invoice.Invoice
	customer *customer.Customer
	currency string
}

func (s *InMemoryReceivableSource) finalized(ctx context.Context) ([]referenceInvoice, error) {
	queryFilter := types.NewNoLimitQueryFilter()
	queryFilter.Status = lo.ToPtr(types.StatusPublished)
	rows, err := s.invoices.List(ctx, &types.InvoiceFilter{QueryFilter: queryFilter, InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusFinalized}})
	if err != nil {
		return nil, err
	}
	customers, err := s.customers.ListAll(ctx, types.NewNoLimitCustomerFilter())
	if err != nil {
		return nil, err
	}
	byID := lo.KeyBy(customers, func(c *customer.Customer) string { return c.ID })
	return lo.Map(rows, func(inv *invoice.Invoice, _ int) referenceInvoice {
		return referenceInvoice{inv: inv, customer: byID[inv.CustomerID], currency: strings.ToLower(inv.Currency)}
	}), nil
}

func (r referenceInvoice) matches(q collections.Query) bool {
	if q.Currency != "" && r.currency != q.Currency {
		return false
	}
	return q.Text == "" || r.matchesText(q.Text)
}

func (r referenceInvoice) matchesText(text string) bool {
	text = strings.ToLower(text)
	fields := []string{lo.FromPtr(r.inv.InvoiceNumber)}
	if r.customer != nil {
		fields = append(fields, r.customer.Name, r.customer.Email)
	}
	return lo.SomeBy(fields, func(f string) bool { return strings.Contains(strings.ToLower(f), text) })
}

func (r referenceInvoice) open() bool {
	return r.inv.PaymentStatus != types.PaymentStatusSucceeded && r.inv.AmountRemaining.IsPositive()
}

func (r referenceInvoice) overdue(asOf time.Time) bool {
	return r.open() && collections.IsOverdue(r.inv.DueDate, asOf)
}

func (r referenceInvoice) notDue(asOf time.Time) bool {
	return r.open() && !collections.IsOverdue(r.inv.DueDate, asOf)
}

func (r referenceInvoice) paidInWindow(asOf time.Time) bool {
	return r.inv.PaymentStatus == types.PaymentStatusSucceeded && r.inv.PaidAt != nil &&
		!r.inv.PaidAt.Before(asOf.Add(-collections.PaidWindow)) && !r.inv.PaidAt.After(asOf)
}

func (r referenceInvoice) receivable(status types.ReceivableStatus) collections.Receivable {
	return collections.Receivable{
		Source:          types.ReceivableSourceFlexprice,
		ExternalID:      r.inv.ID,
		Number:          lo.FromPtr(r.inv.InvoiceNumber),
		Customer:        collections.CustomerRef{ID: r.inv.CustomerID},
		Currency:        r.currency,
		AmountTotal:     r.inv.AmountDue,
		AmountRemaining: r.inv.AmountRemaining,
		DueDate:         r.inv.DueDate,
		PaidAt:          r.inv.PaidAt,
		Status:          status,
	}
}

func (s *InMemoryReceivableSource) invoiceColumn(ctx context.Context, q collections.Query, column types.CollectionsColumnKey) ([]collections.Receivable, error) {
	all, err := s.finalized(ctx)
	if err != nil {
		return nil, err
	}
	paid := column == types.CollectionsColumnPaidThisWeek
	var rows []collections.Receivable
	for _, r := range all {
		switch {
		case !r.matches(q):
		case paid && r.paidInWindow(q.AsOf):
			rows = append(rows, r.receivable(types.ReceivableStatusPaid))
		case !paid && r.notDue(q.AsOf):
			rows = append(rows, r.receivable(types.ReceivableStatusOpen))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if paid {
			if !a.PaidAt.Equal(*b.PaidAt) {
				return a.PaidAt.After(*b.PaidAt)
			}
			return a.ExternalID < b.ExternalID
		}
		switch {
		case a.DueDate == nil && b.DueDate == nil:
		case a.DueDate == nil || b.DueDate == nil:
			return a.DueDate != nil
		case !a.DueDate.Equal(*b.DueDate):
			return a.DueDate.Before(*b.DueDate)
		}
		return a.ExternalID < b.ExternalID
	})
	return rows, nil
}

func (s *InMemoryReceivableSource) ListInvoices(ctx context.Context, q collections.Query, column types.CollectionsColumnKey, offset, limit int) ([]collections.Receivable, bool, error) {
	rows, err := s.invoiceColumn(ctx, q, column)
	if err != nil {
		return nil, false, err
	}
	page, hasMore := pageOf(rows, offset, limit)
	return page, hasMore, nil
}

// pageOf cuts one page out of an ordered list the way LIMIT limit+1 OFFSET offset does.
func pageOf[T any](rows []T, offset, limit int) ([]T, bool) {
	if offset >= len(rows) {
		return nil, false
	}
	rows = rows[offset:]
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

type referenceGroup struct {
	collections.ReceivableGroup
	applies  bool
	decision *collections.ReminderDecision
}

func (s *InMemoryReceivableSource) groups(ctx context.Context, q collections.Query, f collections.GroupFilter) ([]referenceGroup, error) {
	all, err := s.finalized(ctx)
	if err != nil {
		return nil, err
	}
	decisions, err := s.decisions.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	byKey := lo.KeyBy(decisions, func(d *collections.ReminderDecision) string { return d.CustomerID + "|" + d.Currency })

	members := map[string][]referenceInvoice{}
	var keys []string
	for _, r := range all {
		cq := collections.Query{AsOf: q.AsOf, Currency: q.Currency}
		if !f.WholeGroups {
			cq.Text = q.Text
		}
		if !r.overdue(q.AsOf) || !r.matches(cq) || (f.CustomerID != "" && r.inv.CustomerID != f.CustomerID) {
			continue
		}
		key := r.inv.CustomerID + "|" + r.currency
		if _, ok := members[key]; !ok {
			keys = append(keys, key)
		}
		members[key] = append(members[key], r)
	}

	var groups []referenceGroup
	for _, key := range keys {
		rows := members[key]
		if f.WholeGroups && q.Text != "" && !lo.SomeBy(rows, func(r referenceInvoice) bool { return r.matchesText(q.Text) }) {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			if !rows[i].inv.DueDate.Equal(*rows[j].inv.DueDate) {
				return rows[i].inv.DueDate.Before(*rows[j].inv.DueDate)
			}
			return rows[i].inv.ID < rows[j].inv.ID
		})
		g := referenceGroup{ReceivableGroup: collections.ReceivableGroup{
			Source:          types.ReceivableSourceFlexprice,
			CustomerID:      rows[0].inv.CustomerID,
			Currency:        rows[0].currency,
			InvoiceCount:    len(rows),
			FirstExternalID: rows[0].inv.ID,
			OldestDue:       *rows[0].inv.DueDate,
			Status:          types.ReminderStatusPending,
		}}
		for i, r := range rows {
			g.Amount = g.Amount.Add(r.inv.AmountRemaining)
			if i < referenceGroupNumbers {
				g.Numbers = append(g.Numbers, lo.FromPtr(r.inv.InvoiceNumber))
			}
		}
		if d := byKey[key]; d != nil && lo.SomeBy(rows, func(r referenceInvoice) bool { return lo.Contains(d.InvoiceIDs, r.inv.ID) }) {
			g.applies, g.decision = true, d
			g.Status, g.HoldReason = d.Status, d.HoldReason
		}
		groups = append(groups, g)
	}
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
	return groups, nil
}

func (g referenceGroup) tone(c collections.ToneCutoffs) types.ReminderTone {
	switch {
	case g.applies && g.decision.Status == types.ReminderStatusApproved && g.decision.FrozenSubject != "":
		return g.decision.FrozenTone
	case g.applies && g.decision.ToneOverride != "":
		return g.decision.ToneOverride
	case !g.OldestDue.Before(c.Friendly):
		return types.ReminderToneFriendly
	case !g.OldestDue.Before(c.Firm):
		return types.ReminderToneFirm
	}
	return types.ReminderToneFinal
}

func (s *InMemoryReceivableSource) ListGroups(ctx context.Context, q collections.Query, f collections.GroupFilter, offset, limit int) ([]collections.ReceivableGroup, bool, error) {
	groups, err := s.groups(ctx, q, f)
	if err != nil {
		return nil, false, err
	}
	var rows []collections.ReceivableGroup
	for _, g := range groups {
		switch {
		case len(f.Statuses) > 0 && !lo.Contains(f.Statuses, g.Status):
		case f.Tone != "" && g.tone(f.ToneCutoff) != f.Tone:
		default:
			rows = append(rows, g.ReceivableGroup)
		}
	}
	page, hasMore := pageOf(rows, offset, limit)
	return page, hasMore, nil
}

func totalsOf(totals map[string]decimal.Decimal) []collections.CurrencyTotal {
	out := lo.MapToSlice(totals, func(c string, a decimal.Decimal) collections.CurrencyTotal {
		return collections.CurrencyTotal{Currency: c, Amount: a}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

func (s *InMemoryReceivableSource) invoiceSummary(ctx context.Context, q collections.Query, column types.CollectionsColumnKey) (collections.ColumnSummary, error) {
	rows, err := s.invoiceColumn(ctx, q, column)
	if err != nil {
		return collections.ColumnSummary{}, err
	}
	totals := map[string]decimal.Decimal{}
	for _, r := range rows {
		amount := r.AmountRemaining
		if column == types.CollectionsColumnPaidThisWeek {
			amount = r.AmountTotal
		}
		totals[r.Currency] = totals[r.Currency].Add(amount)
	}
	return collections.ColumnSummary{Count: len(rows), InvoiceCount: len(rows), Totals: totalsOf(totals)}, nil
}

func (s *InMemoryReceivableSource) Board(ctx context.Context, q collections.Query, limit int) (*collections.Board, error) {
	all, err := s.finalized(ctx)
	if err != nil {
		return nil, err
	}
	currencies := lo.Uniq(lo.FilterMap(all, func(r referenceInvoice, _ int) (string, bool) {
		return r.currency, r.open() || r.paidInWindow(q.AsOf)
	}))
	sort.Strings(currencies)
	board := &collections.Board{AvailableCurrencies: currencies, Columns: map[types.CollectionsColumnKey]collections.BoardColumn{}}

	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek} {
		summary, err := s.invoiceSummary(ctx, q, column)
		if err != nil {
			return nil, err
		}
		var (
			rows    []collections.Receivable
			hasMore bool
		)
		if limit > 0 {
			if rows, hasMore, err = s.ListInvoices(ctx, q, column, 0, limit); err != nil {
				return nil, err
			}
		}
		board.Columns[column] = collections.BoardColumn{Summary: summary, Invoices: rows, HasMore: hasMore}
	}
	for _, column := range []types.CollectionsColumnKey{types.CollectionsColumnOverdue, types.CollectionsColumnChasing, types.CollectionsColumnHeld} {
		filter := collections.GroupFilter{Statuses: collections.ColumnStatuses(column)}
		everything, _, err := s.ListGroups(ctx, q, filter, 0, int(^uint(0)>>1))
		if err != nil {
			return nil, err
		}
		totals := map[string]decimal.Decimal{}
		summary := collections.ColumnSummary{Count: len(everything)}
		for _, g := range everything {
			summary.InvoiceCount += g.InvoiceCount
			totals[g.Currency] = totals[g.Currency].Add(g.Amount)
		}
		summary.Totals = totalsOf(totals)
		var (
			rows    []collections.ReceivableGroup
			hasMore bool
		)
		if limit > 0 {
			if rows, hasMore, err = s.ListGroups(ctx, q, filter, 0, limit); err != nil {
				return nil, err
			}
		}
		board.Columns[column] = collections.BoardColumn{Summary: summary, Groups: rows, HasMore: hasMore}
	}
	return board, nil
}

func (s *InMemoryReceivableSource) SummarizeGroups(ctx context.Context, asOf time.Time) (collections.GroupSummary, error) {
	groups, err := s.groups(ctx, collections.Query{AsOf: asOf}, collections.GroupFilter{})
	if err != nil {
		return collections.GroupSummary{}, err
	}
	summary := collections.GroupSummary{Statuses: map[types.ReminderStatus]int{}}
	totals := map[string]decimal.Decimal{}
	customers := map[string]bool{}
	for _, g := range groups {
		summary.Statuses[g.Status]++
		summary.InvoiceCount += g.InvoiceCount
		totals[g.Currency] = totals[g.Currency].Add(g.Amount)
		customers[g.CustomerID] = true
	}
	summary.CustomerCount = len(customers)
	summary.Totals = totalsOf(totals)
	return summary, nil
}

func (s *InMemoryReceivableSource) GroupInvoices(ctx context.Context, asOf time.Time, keys []collections.GroupKey) ([]collections.Receivable, error) {
	all, err := s.finalized(ctx)
	if err != nil {
		return nil, err
	}
	var rows []collections.Receivable
	for _, r := range all {
		if r.overdue(asOf) && lo.Contains(keys, collections.GroupKey{CustomerID: r.inv.CustomerID, Currency: r.currency}) {
			rows = append(rows, r.receivable(types.ReceivableStatusOpen))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].DueDate.Equal(*rows[j].DueDate) {
			return rows[i].DueDate.Before(*rows[j].DueDate)
		}
		return rows[i].ExternalID < rows[j].ExternalID
	})
	return rows, nil
}
