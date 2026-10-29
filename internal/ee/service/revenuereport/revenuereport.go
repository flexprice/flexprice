package revenuereport

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// RevenueReportService builds revenue reports from the journal; nothing is persisted.
type RevenueReportService interface {
	// GetRollforward returns the opening, movements by category and closing per ended month and account.
	GetRollforward(ctx context.Context, req *dto.RollforwardRequest) (*dto.RollforwardResponse, error)
	// GetWaterfall returns revenue per group and month: actual, pending and scheduled.
	GetWaterfall(ctx context.Context, req *dto.WaterfallRequest) (*dto.WaterfallResponse, error)
}

type revenueReportService struct {
	journalRepo    journalRepo.Repository
	journalService journalsvc.JournalService
	contractRepo   contractRepo.Repository
}

func NewRevenueReportService(journalRepo journalRepo.Repository, journalService journalsvc.JournalService, contractRepo contractRepo.Repository) RevenueReportService {
	return &revenueReportService{journalRepo: journalRepo, journalService: journalService, contractRepo: contractRepo}
}

// groupKey is one account's rollforward line across periods; a key that is not grouped by stays empty.
type groupKey struct {
	account    types.LedgerAccountKey
	customerID string
	contractID string
	pobID      string
}

const (
	// defaultLimit is the item rows per page when the request sets none.
	defaultLimit = 50
	periodLayout = "2006-01"
)

// GetRollforward carries each group's closing into the next month's opening, so opening(P) = closing(P−1).
// e.g. Feb deferred for acme: opening 1,100, earned −100, closing 1,000.
func (s *revenueReportService) GetRollforward(ctx context.Context, req *dto.RollforwardRequest) (*dto.RollforwardResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	start, end, err := req.PeriodRange()
	if err != nil {
		return nil, err
	}

	// e.g. [{Feb, deferred, acme, opening 1100, earned −100, closing 1000}, {Feb, deferred, beta, opening 0, invoiced 100, earned −100, closing 0}]
	rows, err := s.rollforwardRows(ctx, &journalRepo.SumFilter{
		Currency:     req.Currency,
		PeriodStart:  start,
		PeriodEnd:    end,
		Accounts:     req.ReportAccounts(),
		CustomerIDs:  req.CustomerIDs,
		ContractIDs:  req.ContractIDs,
		POBIDs:       req.POBIDs,
		CreatedAtLTE: req.PostedBy,
		GroupBy:      req.GroupBy,
	})
	if err != nil {
		return nil, err
	}

	// Totals e.g. [{Feb, deferred, opening 1100, invoiced 100, earned −200, closing 1000}]; items are the rows, paged.
	resp := &dto.RollforwardResponse{Totals: totalsOf(rows), Items: []*dto.RollforwardRow{}}
	if len(req.GroupBy) > 0 {
		limit := lo.Ternary(req.Limit > 0, req.Limit, defaultLimit)
		resp.Items = lo.Subset(rows, req.Offset, uint(limit))
		resp.Pagination = lo.ToPtr(types.NewPaginationResponse(len(rows), limit, req.Offset))
	}

	return resp, nil
}

// rollforwardRows returns every group's rows for the filter's months, ordered by customer, contract, POB and account.
func (s *revenueReportService) rollforwardRows(ctx context.Context, f *journalRepo.SumFilter) ([]*dto.RollforwardRow, error) {
	// e.g. [{opening, deferred, acme, 1100}, {Feb, deferred, revenue_offset, acme, −100}, {Feb, deferred, invoice_finalized, beta, 100}, ...]
	sums, err := s.journalRepo.SumLines(ctx, f)
	if err != nil {
		return nil, err
	}

	// e.g. {deferred, acme} → [opening 1100, Feb revenue_offset −100]; {deferred, beta} → [Feb invoice_finalized 100, ...]
	byGroup := lo.GroupBy(sums, func(sum *journalRepo.LineSum) groupKey {
		return groupKey{
			account:    sum.Account,
			customerID: lo.FromPtr(sum.CustomerID),
			contractID: lo.FromPtr(sum.ContractID),
			pobID:      lo.FromPtr(sum.POBID),
		}
	})
	keys := lo.Keys(byGroup)
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.customerID != b.customerID {
			return a.customerID < b.customerID
		}
		if a.contractID != b.contractID {
			return a.contractID < b.contractID
		}
		if a.pobID != b.pobID {
			return a.pobID < b.pobID
		}
		return a.account < b.account
	})

	var rows []*dto.RollforwardRow
	for _, key := range keys {
		rows = append(rows, rollGroup(key, byGroup[key], f.PeriodStart, f.PeriodEnd)...)
	}
	return rows, nil
}

// rollGroup returns one group's rows from start to end, signed in the account's normal direction; months with no
// balance and no movement are skipped.
// e.g. unbilled with Dr 100 revenue_earned in Feb → Feb row: opening 0, earned +100, closing 100.
func rollGroup(key groupKey, sums []*journalRepo.LineSum, start, end time.Time) []*dto.RollforwardRow {
	// e.g. openings [opening 1100], moves [Feb revenue_offset −100]
	openings, moves := lo.FilterReject(sums, func(sum *journalRepo.LineSum, _ int) bool { return sum.PeriodStart == nil })

	opening := decimal.Zero
	for _, sum := range openings {
		opening = opening.Add(key.account.NormalBalance(sum.Net))
	}

	// Keyed by YYYY-MM, not time, so a database time zone cannot split a month.
	// e.g. {"2026-02": [revenue_offset −100]}
	byPeriod := lo.GroupBy(moves, func(sum *journalRepo.LineSum) string { return sum.PeriodStart.Format(periodLayout) })

	var rows []*dto.RollforwardRow
	for _, month := range utils.Months(start, end) {
		period := month.Format(periodLayout)

		// e.g. deferred in Feb: {invoiced: 100, earned: −200}
		movements := map[types.MovementCategory]decimal.Decimal{}
		for _, sum := range byPeriod[period] {
			category := movementCategory(sum.EventType, key.account)
			movements[category] = movements[category].Add(key.account.NormalBalance(sum.Net))
		}

		if opening.IsZero() && len(movements) == 0 {
			continue
		}

		closing := opening.Add(decimal.Sum(decimal.Zero, lo.Values(movements)...))
		rows = append(rows, &dto.RollforwardRow{
			Period:     period,
			Account:    key.account,
			CustomerID: lo.EmptyableToPtr(key.customerID),
			ContractID: lo.EmptyableToPtr(key.contractID),
			POBID:      lo.EmptyableToPtr(key.pobID),
			Opening:    opening,
			Movements:  movements,
			Closing:    closing,
		})
		opening = closing
	}

	return rows
}

// totalsOf sums rows per period and account across groups, ordered by period then account.
func totalsOf(rows []*dto.RollforwardRow) []*dto.RollforwardRow {
	type totalKey struct {
		period  string
		account types.LedgerAccountKey
	}

	// e.g. {Feb, deferred} → acme's row + beta's row: opening 1100 + 0, invoiced 0 + 100, earned −100 + −100
	totals := map[totalKey]*dto.RollforwardRow{}
	for _, row := range rows {
		key := totalKey{period: row.Period, account: row.Account}
		total, ok := totals[key]
		if !ok {
			total = &dto.RollforwardRow{Period: row.Period, Account: row.Account, Movements: map[types.MovementCategory]decimal.Decimal{}}
			totals[key] = total
		}
		total.Opening = total.Opening.Add(row.Opening)
		total.Closing = total.Closing.Add(row.Closing)
		for category, amount := range row.Movements {
			total.Movements[category] = total.Movements[category].Add(amount)
		}
	}

	result := lo.Values(totals)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Period != result[j].Period {
			return result[i].Period < result[j].Period
		}
		return result[i].Account < result[j].Account
	})

	return result
}

// movementCategory maps a line to its rollforward category: the offset is earned on deferred and invoiced on unbilled.
func movementCategory(eventType types.JournalEventType, account types.LedgerAccountKey) types.MovementCategory {
	switch eventType {
	case types.JournalEventInvoiceFinalized, types.JournalEventInvoiceVoided:
		return types.MovementCategoryInvoiced
	case types.JournalEventRevenueEarned:
		return types.MovementCategoryEarned
	case types.JournalEventRevenueOffset:
		return lo.Ternary(account == types.LedgerAccountUnbilledReceivable, types.MovementCategoryInvoiced, types.MovementCategoryEarned)
	case types.JournalEventPaymentSucceeded, types.JournalEventPaymentReversed:
		return types.MovementCategoryPayments
	case types.JournalEventRefundSucceeded:
		return types.MovementCategoryRefunds
	default: // credit notes, and any manual adjustment event added later
		return types.MovementCategoryAdjustments
	}
}

// cellKey is one waterfall cell: the group (customer, contract or POB; "" when the line has none) and a YYYY-MM month.
type cellKey struct {
	group  string
	period string
}

// balance is a group's closing deferred and unbilled at a month end.
type balance struct {
	deferred decimal.Decimal
	unbilled decimal.Decimal
}

// GetWaterfall builds each cell from four sources: actual from the journal, pending and scheduled from recognition dry
// runs, and month-end balances from the rollforward. Lines without the grouped key collect under a null-key row, so rows
// add up to totals.
// e.g. by customer on Mar 15, Jan–Apr: acme [Jan actual 200, Feb actual 200, Mar pending 90.32 + scheduled 109.68, Apr scheduled 100].
func (s *revenueReportService) GetWaterfall(ctx context.Context, req *dto.WaterfallRequest) (*dto.WaterfallResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	start, end, err := req.PeriodRange()
	if err != nil {
		return nil, err
	}
	groupBy := req.ReportGroupBy()
	now := lo.FromPtrOr(req.AsOf, time.Now()).UTC()

	// e.g. ["2026-01", "2026-02"]
	periods := lo.Map(utils.Months(start, end), func(month time.Time, _ int) string { return month.Format(periodLayout) })

	actual, err := s.actualRevenue(ctx, req, groupBy, start, end)
	if err != nil {
		return nil, err
	}
	pending, scheduled, err := s.forecastRevenue(ctx, req, groupBy, periods, end, now)
	if err != nil {
		return nil, err
	}
	balances, detailsThrough, err := s.monthEndBalances(ctx, req, groupBy, start, end, now)
	if err != nil {
		return nil, err
	}

	rows := waterfallRows(groupBy, periods, actual, pending, scheduled, balances, detailsThrough)
	limit := lo.Ternary(req.Limit > 0, req.Limit, defaultLimit)
	return &dto.WaterfallResponse{
		Totals:     waterfallTotals(periods, rows, detailsThrough),
		Items:      lo.Subset(rows, req.Offset, uint(limit)),
		Pagination: lo.ToPtr(types.NewPaginationResponse(len(rows), limit, req.Offset)),
	}, nil
}

// actualRevenue is the revenue posted in each month of the range, per group.
// e.g. {acme, 2026-01}: 200, {acme, 2026-02}: 200
func (s *revenueReportService) actualRevenue(ctx context.Context, req *dto.WaterfallRequest, groupBy types.RevenueReportGroupBy, start, end time.Time) (map[cellKey]decimal.Decimal, error) {
	sums, err := s.journalRepo.SumLines(ctx, &journalRepo.SumFilter{
		Currency:       req.Currency,
		PeriodStart:    start,
		PeriodEnd:      end,
		Accounts:       []types.LedgerAccountKey{types.LedgerAccountRevenue},
		CustomerIDs:    req.CustomerIDs,
		ContractIDs:    req.ContractIDs,
		POBIDs:         req.POBIDs,
		GroupBy:        []types.RevenueReportGroupBy{groupBy},
		ExcludeOpening: true,
	})
	if err != nil {
		return nil, err
	}

	actual := map[cellKey]decimal.Decimal{}
	for _, sum := range sums {
		key := cellKey{group: groupValue(groupBy, sum.CustomerID, sum.ContractID, sum.POBID), period: sum.PeriodStart.Format(periodLayout)}
		actual[key] = actual[key].Add(sum.Net)
	}
	return actual, nil
}

// forecastRevenue is pending, what a recognition run would post now (the open month to date and ended months not posted
// yet), and scheduled, what a run at the range's end would add on top (invoiced but not yet earned). Scheduled is empty
// when the range has no open month.
// e.g. on Mar 15, annual 1,200 from Jan 1: now → Mar 45.16; at the range end → Mar–Dec 100 each; scheduled Mar 54.84.
func (s *revenueReportService) forecastRevenue(ctx context.Context, req *dto.WaterfallRequest, groupBy types.RevenueReportGroupBy, periods []string, end, now time.Time) (pending, scheduled map[cellKey]decimal.Decimal, err error) {
	contracts, err := s.waterfallContracts(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	nowEntries, err := s.dryRun(ctx, contracts, now)
	if err != nil {
		return nil, nil, err
	}
	pending = revenueByCell(nowEntries, groupBy, periods, req.POBIDs)

	scheduled = map[cellKey]decimal.Decimal{}
	if !end.After(utils.LastEndedMonth(now)) {
		return pending, scheduled, nil
	}

	// Noon UTC on the 1st after the range is the 1st in every timezone, so no partial month is added.
	endEntries, err := s.dryRun(ctx, contracts, end.AddDate(0, 1, 0).Add(12*time.Hour))
	if err != nil {
		return nil, nil, err
	}
	scheduled = revenueByCell(endEntries, groupBy, periods, req.POBIDs)
	for key, amount := range pending {
		scheduled[key] = scheduled[key].Sub(amount)
	}
	return pending, scheduled, nil
}

// monthEndBalances is each group's closing deferred and unbilled for the months in range ended by now, with the last
// such month; empty without details or when no month in range has ended.
// e.g. {acme, 2026-01}: {deferred 1100, unbilled 0}, {acme, 2026-02}: {deferred 1000, unbilled 0}; through "2026-02"
func (s *revenueReportService) monthEndBalances(ctx context.Context, req *dto.WaterfallRequest, groupBy types.RevenueReportGroupBy, start, end, now time.Time) (map[cellKey]balance, string, error) {
	lastEnded := utils.LastEndedMonth(now)
	if !req.Details || start.After(lastEnded) {
		return nil, "", nil
	}
	detailsEnd := lo.Ternary(end.After(lastEnded), lastEnded, end)

	rows, err := s.rollforwardRows(ctx, &journalRepo.SumFilter{
		Currency:    req.Currency,
		PeriodStart: start,
		PeriodEnd:   detailsEnd,
		Accounts:    []types.LedgerAccountKey{types.LedgerAccountDeferredRevenue, types.LedgerAccountUnbilledReceivable},
		CustomerIDs: req.CustomerIDs,
		ContractIDs: req.ContractIDs,
		POBIDs:      req.POBIDs,
		GroupBy:     []types.RevenueReportGroupBy{groupBy},
	})
	if err != nil {
		return nil, "", err
	}

	balances := map[cellKey]balance{}
	for _, row := range rows {
		key := cellKey{group: groupValue(groupBy, row.CustomerID, row.ContractID, row.POBID), period: row.Period}
		b := balances[key]
		if row.Account == types.LedgerAccountDeferredRevenue {
			b.deferred = row.Closing
		} else {
			b.unbilled = row.Closing
		}
		balances[key] = b
	}
	return balances, detailsEnd.Format(periodLayout), nil
}

// waterfallContracts is the request's active and ended contracts in its currency, the ones a dry run can recognize.
func (s *revenueReportService) waterfallContracts(ctx context.Context, req *dto.WaterfallRequest) ([]string, error) {
	contracts, err := s.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractIDs:      req.ContractIDs,
		CustomerIDs:      req.CustomerIDs,
		ContractStatuses: []types.ContractStatus{types.ContractStatusActive, types.ContractStatusEnded},
	})
	if err != nil {
		return nil, err
	}
	return lo.FilterMap(contracts, func(c *domainContract.Contract, _ int) (string, bool) {
		return c.ID(), c.Currency() == req.Currency
	}), nil
}

// dryRun is what recognition would post for the contracts as of asOf, without saving anything.
func (s *revenueReportService) dryRun(ctx context.Context, contractIDs []string, asOf time.Time) ([]*domainJournal.Entry, error) {
	var entries []*domainJournal.Entry
	for _, id := range contractIDs {
		recognized, err := s.journalService.Recognize(ctx, journalsvc.RecognizeRequest{ContractID: id, AsOf: asOf, DryRun: true})
		if err != nil {
			return nil, err
		}
		entries = append(entries, recognized...)
	}
	return entries, nil
}

// revenueByCell sums the revenue lines of earned entries per cell, for months in range and the POB filter.
// e.g. a Mar 14 revenue_earned entry crediting revenue 45.16 for acme → {acme, 2026-03}: 45.16
func revenueByCell(entries []*domainJournal.Entry, groupBy types.RevenueReportGroupBy, periods []string, pobIDs []string) map[cellKey]decimal.Decimal {
	revenue := map[cellKey]decimal.Decimal{}
	for _, e := range entries {
		period := e.BusinessDate().Format(periodLayout)
		if e.EventType() != types.JournalEventRevenueEarned || period < periods[0] || period > periods[len(periods)-1] {
			continue
		}
		for _, l := range e.Lines() {
			if l.Account() != types.LedgerAccountRevenue || (len(pobIDs) > 0 && !lo.Contains(pobIDs, l.POBID())) {
				continue
			}
			key := cellKey{group: groupValue(groupBy, lo.ToPtr(e.CustomerID()), lo.EmptyableToPtr(e.ContractID()), lo.EmptyableToPtr(l.POBID())), period: period}
			revenue[key] = revenue[key].Add(l.Net())
		}
	}
	return revenue
}

// groupValue picks the grouped key of a line sum; empty when the line has none.
func groupValue(groupBy types.RevenueReportGroupBy, customerID, contractID, pobID *string) string {
	switch groupBy {
	case types.RevenueReportGroupByContract:
		return lo.FromPtr(contractID)
	case types.RevenueReportGroupByPOB:
		return lo.FromPtr(pobID)
	default:
		return lo.FromPtr(customerID)
	}
}

// waterfallRows gives each group with any amount one cell per period, ordered by group with the null-key row first;
// balances through detailsThrough default to zero, later ones stay empty.
// e.g. actual {beta, 2026-02}: 100 → [{customer_id: cust_beta, cells: [Jan 0, Feb actual 100 total 100]}]
func waterfallRows(groupBy types.RevenueReportGroupBy, periods []string, actual, pending, scheduled map[cellKey]decimal.Decimal, balances map[cellKey]balance, detailsThrough string) []*dto.WaterfallRow {
	keys := append(append(append(lo.Keys(actual), lo.Keys(pending)...), lo.Keys(scheduled)...), lo.Keys(balances)...)
	groups := lo.Uniq(lo.Map(keys, func(k cellKey, _ int) string { return k.group }))
	sort.Strings(groups)

	rows := []*dto.WaterfallRow{}
	for _, group := range groups {
		row := &dto.WaterfallRow{}
		groupPtr := lo.EmptyableToPtr(group)
		switch groupBy {
		case types.RevenueReportGroupByContract:
			row.ContractID = groupPtr
		case types.RevenueReportGroupByPOB:
			row.POBID = groupPtr
		default:
			row.CustomerID = groupPtr
		}

		empty := true
		for _, period := range periods {
			key := cellKey{group: group, period: period}
			c := &dto.WaterfallCell{Period: period, Revenue: dto.RevenueBreakdown{
				Actual:    actual[key],
				Pending:   pending[key],
				Scheduled: scheduled[key],
			}}
			c.Revenue.Total = c.Revenue.Actual.Add(c.Revenue.Pending).Add(c.Revenue.Scheduled)
			if period <= detailsThrough {
				c.Deferred, c.Unbilled = lo.ToPtr(balances[key].deferred), lo.ToPtr(balances[key].unbilled)
			}
			empty = empty && c.Revenue.Total.IsZero() && lo.FromPtr(c.Deferred).IsZero() && lo.FromPtr(c.Unbilled).IsZero()
			row.Cells = append(row.Cells, c)
		}
		if !empty {
			rows = append(rows, row)
		}
	}
	return rows
}

// waterfallTotals sums every row's cells per period, before paging; balances only through detailsThrough.
// e.g. acme [Feb actual 100] + beta [Feb actual 100] → [Feb actual 200]
func waterfallTotals(periods []string, rows []*dto.WaterfallRow, detailsThrough string) []*dto.WaterfallCell {
	totals := lo.Map(periods, func(period string, _ int) *dto.WaterfallCell {
		total := &dto.WaterfallCell{Period: period}
		if period <= detailsThrough {
			total.Deferred, total.Unbilled = lo.ToPtr(decimal.Zero), lo.ToPtr(decimal.Zero)
		}
		return total
	})

	for _, row := range rows {
		for i, c := range row.Cells {
			if c.Deferred != nil {
				totals[i].Deferred = lo.ToPtr(totals[i].Deferred.Add(*c.Deferred))
				totals[i].Unbilled = lo.ToPtr(totals[i].Unbilled.Add(*c.Unbilled))
			}
			totals[i].Revenue.Total = totals[i].Revenue.Total.Add(c.Revenue.Total)
			totals[i].Revenue.Actual = totals[i].Revenue.Actual.Add(c.Revenue.Actual)
			totals[i].Revenue.Pending = totals[i].Revenue.Pending.Add(c.Revenue.Pending)
			totals[i].Revenue.Scheduled = totals[i].Revenue.Scheduled.Add(c.Revenue.Scheduled)
		}
	}
	return totals
}
