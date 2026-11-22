package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const customerLookupBatch = 500

// CollectionsService builds the collections board.
type CollectionsService interface {
	GetBoard(ctx context.Context, req *dto.CollectionsBoardRequest) (*dto.CollectionsBoardResponse, error)
	GetColumn(ctx context.Context, key string, req *dto.CollectionsBoardRequest) (*dto.CollectionsColumnPageResponse, error)
}

type collectionsService struct {
	ServiceParams
}

func NewCollectionsService(params ServiceParams) CollectionsService {
	return &collectionsService{ServiceParams: params}
}

type boardColumnMeta struct {
	key         types.CollectionsColumnKey
	title, hint string
}

var boardColumns = []boardColumnMeta{
	{types.CollectionsColumnNotDue, "Sent, not due", "Invoices out, still within terms"},
	{types.CollectionsColumnOverdue, "Overdue, waiting on you", "Reminder drafted, needs your approval"},
	{types.CollectionsColumnChasing, "Reminder approved", "Approved, waiting to be sent"},
	{types.CollectionsColumnHeld, "Held", "Promised to pay, disputed or on hold"},
	{types.CollectionsColumnPaidThisWeek, "Paid this week", "Matched to a payment in the last 7 days"},
}

func boardQuery(asOf time.Time, req *dto.CollectionsBoardRequest) collections.Query {
	return collections.Query{AsOf: asOf, Text: req.Q, Currency: normalizedCurrency(req.Currency)}
}

func (s *collectionsService) GetBoard(ctx context.Context, req *dto.CollectionsBoardRequest) (*dto.CollectionsBoardResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	q := boardQuery(clock.Now(), req)
	limit := req.BoardCardLimit()

	board, err := s.ReceivableSource.Board(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	cards := buildCardSets(q.AsOf, board)
	customers, err := lookupCustomers(ctx, s.CustomerRepo, cardCustomerIDs(cards))
	if err != nil {
		return nil, err
	}

	resp := &dto.CollectionsBoardResponse{AsOf: q.AsOf, AvailableCurrencies: board.AvailableCurrencies, Columns: []dto.CollectionsBoardColumn{}}
	for _, meta := range boardColumns {
		column := board.Columns[meta.key]
		page := cards[meta.key]
		fillCustomers(page.cards, customers)
		resp.Columns = append(resp.Columns, dto.CollectionsBoardColumn{
			Key:          meta.key,
			Title:        meta.title,
			Hint:         meta.hint,
			Count:        column.Summary.Count,
			InvoiceCount: column.Summary.InvoiceCount,
			Totals:       dtoTotals(column.Summary.Totals),
			Cards:        page.cards,
			HasMore:      column.HasMore || (limit == 0 && column.Summary.Count > 0),
		})
	}
	return resp, nil
}

func (s *collectionsService) GetColumn(ctx context.Context, key string, req *dto.CollectionsBoardRequest) (*dto.CollectionsColumnPageResponse, error) {
	column := types.CollectionsColumnKey(key)
	if !lo.ContainsBy(boardColumns, func(m boardColumnMeta) bool { return m.key == column }) {
		return nil, ierr.NewErrorf("unknown column %s", key).WithHintf("Column %s does not exist.", key).Mark(ierr.ErrNotFound)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	asOf, err := parsePageAsOf(req.AsOf, clock.Now())
	if err != nil {
		return nil, err
	}
	q := boardQuery(asOf, req)
	limit, offset := req.PageLimit(), req.PageOffset()

	var sets cardSets
	var hasMore bool
	switch column {
	case types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek:
		var rows []collections.Receivable
		if rows, hasMore, err = s.ReceivableSource.ListInvoices(ctx, q, column, offset, limit); err != nil {
			return nil, err
		}
		sets = cardSets{column: invoiceCards(column, asOf, rows)}
	default:
		var rows []collections.ReceivableGroup
		if rows, hasMore, err = s.ReceivableSource.ListGroups(ctx, q, collections.GroupFilter{Statuses: collections.ColumnStatuses(column)}, offset, limit); err != nil {
			return nil, err
		}
		sets = cardSets{column: groupCards(asOf, rows)}
	}
	page := sets[column]
	customers, err := lookupCustomers(ctx, s.CustomerRepo, lo.Uniq(lo.Map(page.cards, func(c dto.CollectionsBoardCard, _ int) string { return c.CustomerID })))
	if err != nil {
		return nil, err
	}
	fillCustomers(page.cards, customers)
	return &dto.CollectionsColumnPageResponse{
		Key:     column,
		AsOf:    asOf,
		Page:    req.PageNumber(),
		Cards:   page.cards,
		HasMore: hasMore,
	}, nil
}

// cardPage is one page of cards.
type cardPage struct {
	cards []dto.CollectionsBoardCard
}

type cardSets map[types.CollectionsColumnKey]cardPage

func buildCardSets(asOf time.Time, board *collections.Board) cardSets {
	sets := cardSets{}
	for key, column := range board.Columns {
		switch key {
		case types.CollectionsColumnNotDue, types.CollectionsColumnPaidThisWeek:
			sets[key] = invoiceCards(key, asOf, column.Invoices)
		default:
			sets[key] = groupCards(asOf, column.Groups)
		}
	}
	return sets
}

func cardCustomerIDs(sets cardSets) []string {
	var ids []string
	for _, page := range sets {
		ids = append(ids, lo.Map(page.cards, func(c dto.CollectionsBoardCard, _ int) string { return c.CustomerID })...)
	}
	return lo.Uniq(ids)
}

func fillCustomers(cards []dto.CollectionsBoardCard, customers map[string]*customer.Customer) {
	for i := range cards {
		if c := customers[cards[i].CustomerID]; c != nil {
			cards[i].CustomerName = c.Name
		}
	}
}

func invoiceCards(column types.CollectionsColumnKey, asOf time.Time, rows []collections.Receivable) cardPage {
	page := cardPage{cards: []dto.CollectionsBoardCard{}}
	for _, r := range rows {
		card := dto.CollectionsBoardCard{
			CustomerID:     r.Customer.ID,
			Source:         r.Source,
			ExternalID:     r.ExternalID,
			InvoiceCount:   1,
			InvoiceNumbers: []string{r.Number},
			Amount:         r.AmountRemaining,
			Currency:       normalizedCurrency(r.Currency),
		}
		if column == types.CollectionsColumnPaidThisWeek {
			card.Amount, card.PaidAt = r.AmountTotal, r.PaidAt
		} else {
			card.DueDate = r.DueDate
			if r.DueDate != nil {
				card.DueInDays = lo.ToPtr(calendarDaysBetween(asOf, *r.DueDate))
			}
		}
		page.cards = append(page.cards, card)
	}
	return page
}

func groupCards(asOf time.Time, rows []collections.ReceivableGroup) cardPage {
	page := cardPage{cards: []dto.CollectionsBoardCard{}}
	for _, g := range rows {
		card := dto.CollectionsBoardCard{
			CustomerID:     g.CustomerID,
			Source:         g.Source,
			InvoiceCount:   g.InvoiceCount,
			InvoiceNumbers: g.Numbers,
			Amount:         g.Amount,
			Currency:       normalizedCurrency(g.Currency),
			DaysOverdue:    lo.ToPtr(calendarDaysBetween(g.OldestDue, asOf)),
			ReminderID:     formatReminderID(normalizedCurrency(g.Currency), g.CustomerID),
			ReminderStatus: g.Status,
		}
		if g.InvoiceCount == 1 {
			card.ExternalID = g.FirstExternalID
		}
		if g.Status == types.ReminderStatusHeld {
			card.ReminderHoldReason = g.HoldReason
		}
		page.cards = append(page.cards, card)
	}
	return page
}

func dtoTotals(totals []collections.CurrencyTotal) []dto.CollectionsCurrencyTotal {
	out := lo.Map(totals, func(t collections.CurrencyTotal, _ int) dto.CollectionsCurrencyTotal {
		return dto.CollectionsCurrencyTotal{Currency: t.Currency, Amount: t.Amount}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

func lookupCustomers(ctx context.Context, repo customer.Repository, ids []string) (map[string]*customer.Customer, error) {
	found := make(map[string]*customer.Customer, len(ids))
	for _, batch := range lo.Chunk(ids, customerLookupBatch) {
		filter := types.NewNoLimitCustomerFilter()
		filter.CustomerIDs = batch
		customers, err := repo.ListAll(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, c := range customers {
			found[c.ID] = c
		}
	}
	return found, nil
}

func normalizedCurrency(currency string) string {
	return strings.ToLower(strings.TrimSpace(currency))
}

// calendarDaysBetween counts UTC calendar days from one instant to another; negative when to is earlier.
func calendarDaysBetween(from, to time.Time) int {
	return int(utcDay(to).Sub(utcDay(from)).Hours() / 24)
}

func utcDay(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// overdueGroup is one customer's overdue receivables in one currency; amounts in different currencies are never summed.
type overdueGroup struct {
	customerID string
	currency   string
	rows       []collections.Receivable
}

func (g overdueGroup) amount() decimal.Decimal {
	return lo.Reduce(g.rows, func(sum decimal.Decimal, r collections.Receivable, _ int) decimal.Decimal {
		return sum.Add(r.AmountRemaining)
	}, decimal.Zero)
}

func (g overdueGroup) oldestDaysOverdue(now time.Time) int {
	return lo.Max(lo.Map(g.rows, func(r collections.Receivable, _ int) int {
		return calendarDaysBetween(*r.DueDate, now)
	}))
}

// groupReceivables groups overdue receivables by customer and currency, keeping the order they came in.
func groupReceivables(rows []collections.Receivable) []overdueGroup {
	index := map[collections.GroupKey]int{}
	var groups []overdueGroup
	for _, r := range rows {
		key := collections.GroupKey{CustomerID: r.Customer.ID, Currency: normalizedCurrency(r.Currency)}
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, overdueGroup{customerID: key.CustomerID, currency: key.Currency})
		}
		groups[i].rows = append(groups[i].rows, r)
	}
	return groups
}
