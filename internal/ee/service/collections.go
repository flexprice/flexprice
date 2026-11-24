package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"

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

	board, err := s.CollectionsRepo.Board(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	rows := lo.FlatMap(lo.Values(board.Columns), func(c collections.BoardColumn, _ int) []collections.Row { return c.Rows })
	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(rows))
	if err != nil {
		return nil, err
	}

	resp := &dto.CollectionsBoardResponse{AsOf: q.AsOf, AvailableCurrencies: board.AvailableCurrencies, Columns: []dto.CollectionsBoardColumn{}}
	for _, meta := range boardColumns {
		column := board.Columns[meta.key]
		boardColumn := dto.CollectionsBoardColumn{
			Key:     meta.key,
			Title:   meta.title,
			Hint:    meta.hint,
			Count:   column.Summary.Count,
			Totals:  dtoTotals(column.Summary.Totals),
			Cards:   cardsOf(meta.key, q.AsOf, column.Rows, customers),
			HasMore: column.HasMore || (limit == 0 && column.Summary.Count > 0),
		}
		resp.Columns = append(resp.Columns, boardColumn)
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
	rows, hasMore, err := s.CollectionsRepo.ListRows(
		ctx,
		boardQuery(asOf, req),
		collections.RowFilter{
			Column:   column,
			Statuses: types.ColumnStatuses(column),
		},
		req.PageOffset(),
		req.PageLimit(),
	)
	if err != nil {
		return nil, err
	}
	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(rows))
	if err != nil {
		return nil, err
	}
	return &dto.CollectionsColumnPageResponse{
		Key:     column,
		AsOf:    asOf,
		Page:    req.PageNumber(),
		Cards:   cardsOf(column, asOf, rows, customers),
		HasMore: hasMore,
	}, nil
}

func customerIDs(rows []collections.Row) []string {
	return lo.Uniq(lo.Map(rows, func(r collections.Row, _ int) string { return r.CustomerID }))
}

func cardsOf(column types.CollectionsColumnKey, asOf time.Time, rows []collections.Row, customers map[string]*customer.Customer) []dto.CollectionsBoardCard {
	return lo.Map(rows, func(r collections.Row, _ int) dto.CollectionsBoardCard {
		return cardOf(column, asOf, r, customers[r.CustomerID])
	})
}

func cardOf(column types.CollectionsColumnKey, asOf time.Time, r collections.Row, c *customer.Customer) dto.CollectionsBoardCard {
	card := dto.CollectionsBoardCard{
		InvoiceID: r.InvoiceID, InvoiceNumber: r.InvoiceNumber, CustomerID: r.CustomerID,
		Amount: r.AmountRemaining, Currency: r.Currency, DueDate: r.DueDate,
	}
	if c != nil {
		card.CustomerName = c.Name
	}
	switch column {
	case types.CollectionsColumnPaidThisWeek:
		card.Amount, card.PaidAt, card.DueDate = r.AmountTotal, r.PaidAt, nil
	case types.CollectionsColumnNotDue:
		if r.DueDate != nil {
			card.DueInDays = lo.ToPtr(calendarDaysBetween(asOf, *r.DueDate))
		}
	default:
		days := calendarDaysBetween(*r.DueDate, asOf)
		card.DaysOverdue = lo.ToPtr(days)
		card.ReminderStatus = reminderStatusOf(r)
		card.ReminderTone = shownTone(r, days)
		if r.Reminder != nil {
			card.ReminderStatusReason = r.Reminder.StatusReason
		}
	}
	return card
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
