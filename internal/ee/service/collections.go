package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const customerLookupBatch = 500

const (
	asOfMaxAge    = 24 * time.Hour
	asOfClockSkew = time.Minute
)

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
	return collections.Query{
		AsOf:     asOf,
		Text:     req.Q,
		Currency: normalizedCurrency(req.Currency),
		Range:    queryRange(req.CollectionsDateFilter),
	}
}

// queryRange turns inclusive UTC days into a half-open range of instants; nil when there is none.
func queryRange(filter dto.CollectionsDateFilter) *collections.DateRange {
	if filter.From == nil && filter.To == nil {
		return nil
	}

	rng := &collections.DateRange{}
	if filter.From != nil {
		from := types.UTCDay(*filter.From)
		rng.From = &from
	}
	if filter.To != nil {
		to := types.UTCDay(*filter.To).AddDate(0, 0, 1)
		rng.To = &to
	}

	return rng
}

// requireCollectionsEnabled refuses Collections requests in an environment where it is not turned on.
func requireCollectionsEnabled(ctx context.Context, params ServiceParams) error {
	cfg, err := GetSetting[types.CollectionsConfig](NewSettingsService(params).(*settingsService), ctx, types.SettingKeyCollectionsConfig)
	if err != nil {
		return err
	}

	if !cfg.Enabled {
		return ierr.NewError("collections is not enabled").
			WithHint("Collections is not enabled for this environment. Ask an administrator to turn it on in Settings.").
			Mark(ierr.ErrPermissionDenied)
	}

	return nil
}

func (s *collectionsService) GetBoard(ctx context.Context, req *dto.CollectionsBoardRequest) (*dto.CollectionsBoardResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	query := boardQuery(clock.Now(), req)
	limit := req.BoardCardLimit()

	board, err := s.CollectionsRepo.Board(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	var invoices []collections.InvoiceWithReminder
	for _, column := range board.Columns {
		invoices = append(invoices, column.Rows...)
	}

	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(invoices))
	if err != nil {
		return nil, err
	}

	resp := &dto.CollectionsBoardResponse{
		AsOf:                query.AsOf,
		Range:               req.DateRange(),
		AvailableCurrencies: board.AvailableCurrencies,
		Columns:             []dto.CollectionsBoardColumn{},
	}

	for _, meta := range boardColumns {
		column := board.Columns[meta.key]

		resp.Columns = append(resp.Columns, dto.CollectionsBoardColumn{
			Key:     meta.key,
			Title:   meta.title,
			Hint:    meta.hint,
			Count:   column.Summary.Count,
			Totals:  dtoTotals(column.Summary.Totals),
			Cards:   cardsOf(meta.key, query.AsOf, column.Rows, customers),
			HasMore: column.HasMore || (limit == 0 && column.Summary.Count > 0),
		})
	}

	return resp, nil
}

func isBoardColumn(key types.CollectionsColumnKey) bool {
	for _, meta := range boardColumns {
		if meta.key == key {
			return true
		}
	}

	return false
}

func (s *collectionsService) GetColumn(ctx context.Context, key string, req *dto.CollectionsBoardRequest) (*dto.CollectionsColumnPageResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}

	column := types.CollectionsColumnKey(key)
	if !isBoardColumn(column) {
		return nil, ierr.NewErrorf("unknown column %s", key).
			WithHintf("Column %s does not exist.", key).
			Mark(ierr.ErrNotFound)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	asOf, err := parsePageAsOf(req.AsOf, clock.Now())
	if err != nil {
		return nil, err
	}

	filter := collections.InvoiceFilter{
		Column:   column,
		Statuses: types.ColumnStatuses(column),
	}

	invoices, hasMore, err := s.CollectionsRepo.ListInvoicesWithReminders(ctx, boardQuery(asOf, req), filter, req.PageOffset(), req.PageLimit())
	if err != nil {
		return nil, err
	}

	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(invoices))
	if err != nil {
		return nil, err
	}

	return &dto.CollectionsColumnPageResponse{
		Key:     column,
		AsOf:    asOf,
		Range:   req.DateRange(),
		Page:    req.PageNumber(),
		Cards:   cardsOf(column, asOf, invoices, customers),
		HasMore: hasMore,
	}, nil
}

// parsePageAsOf reads the as_of of a board response for a page request (already microsecond-truncated by the request
// binding): none means now, otherwise it must be within the last 24 hours and not later than now plus a little clock skew.
func parsePageAsOf(raw *time.Time, now time.Time) (time.Time, error) {
	if raw == nil {
		return now, nil
	}

	asOf := *raw
	if now.Sub(asOf) > asOfMaxAge {
		return time.Time{}, ierr.NewError("as_of expired").
			WithHint("as_of is older than 24 hours. Reload the board.").
			Mark(ierr.ErrValidation)
	}

	if asOf.After(now.Add(asOfClockSkew)) {
		return time.Time{}, ierr.NewError("as_of in the future").
			WithHint("as_of cannot be later than now.").
			Mark(ierr.ErrValidation)
	}

	return asOf.UTC(), nil
}

func customerIDs(invoices []collections.InvoiceWithReminder) []string {
	seen := map[string]bool{}
	ids := []string{}

	for _, row := range invoices {
		if seen[row.CustomerID] {
			continue
		}

		seen[row.CustomerID] = true
		ids = append(ids, row.CustomerID)
	}

	return ids
}

func cardsOf(column types.CollectionsColumnKey, asOf time.Time, invoices []collections.InvoiceWithReminder, customers map[string]*customer.Customer) []dto.CollectionsItem {
	cards := make([]dto.CollectionsItem, 0, len(invoices))

	for _, row := range invoices {
		cards = append(cards, cardOf(column, asOf, row, customers[row.CustomerID]))
	}

	return cards
}

func cardOf(column types.CollectionsColumnKey, asOf time.Time, r collections.InvoiceWithReminder, c *customer.Customer) dto.CollectionsItem {
	card := dto.CollectionsItem{
		InvoiceID:     r.InvoiceID,
		InvoiceNumber: r.InvoiceNumber,
		CustomerID:    r.CustomerID,
		Amount:        r.AmountRemaining,
		Currency:      r.Currency,
		DueDate:       r.DueDate,
	}
	if c != nil {
		card.CustomerName = c.Name
	}

	switch column {
	case types.CollectionsColumnPaidThisWeek:
		card.Amount = r.AmountTotal
		card.PaidAt = r.PaidAt
		card.DueDate = nil

	case types.CollectionsColumnNotDue:
		if r.DueDate != nil {
			days := types.CalendarDaysBetween(asOf, *r.DueDate)
			card.DueInDays = &days
		}

	default:
		days := types.CalendarDaysBetween(*r.DueDate, asOf)
		card.DaysOverdue = &days
		card.ReminderStatus = reminderStatusOf(r)
		card.ReminderTone = shownTone(r, days)
		if r.Reminder != nil {
			card.ReminderStatusReason = r.Reminder.StatusReason
		}
	}

	return card
}

func dtoTotals(totals []collections.CurrencyTotal) []dto.CollectionsCurrencyTotal {
	out := make([]dto.CollectionsCurrencyTotal, 0, len(totals))

	for _, total := range totals {
		out = append(out, dto.CollectionsCurrencyTotal{Currency: total.Currency, Amount: total.Amount})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })

	return out
}

func lookupCustomers(ctx context.Context, repo customer.Repository, ids []string) (map[string]*customer.Customer, error) {
	found := make(map[string]*customer.Customer, len(ids))

	for start := 0; start < len(ids); start += customerLookupBatch {
		end := min(start+customerLookupBatch, len(ids))

		filter := types.NewNoLimitCustomerFilter()
		filter.CustomerIDs = ids[start:end]

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
