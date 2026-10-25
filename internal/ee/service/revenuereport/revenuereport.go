package revenuereport

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
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
		CreatedAtLTE: req.AsOf,
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
	// e.g. −1 for unbilled (debit-normal): SumLines' credit − debit of −100 becomes +100.
	sign := lo.Ternary(key.account.NormalSide() == types.JournalDirectionDebit, decimal.NewFromInt(-1), decimal.NewFromInt(1))

	// e.g. openings [opening 1100], moves [Feb revenue_offset −100]
	openings, moves := lo.FilterReject(sums, func(sum *journalRepo.LineSum, _ int) bool { return sum.PeriodStart == nil })

	opening := decimal.Zero
	for _, sum := range openings {
		opening = opening.Add(sum.Net.Mul(sign))
	}

	// Keyed by YYYY-MM, not time, so a database time zone cannot split a month.
	// e.g. {"2026-02": [revenue_offset −100]}
	byPeriod := lo.GroupBy(moves, func(sum *journalRepo.LineSum) string { return sum.PeriodStart.Format(periodLayout) })

	var rows []*dto.RollforwardRow
	for month := start; !month.After(end); month = month.AddDate(0, 1, 0) {
		period := month.Format(periodLayout)

		// e.g. deferred in Feb: {invoiced: 100, earned: −200}
		movements := map[types.MovementCategory]decimal.Decimal{}
		for _, sum := range byPeriod[period] {
			category := movementCategory(sum.EventType, key.account)
			movements[category] = movements[category].Add(sum.Net.Mul(sign))
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

// GetWaterfall fills each group's monthly cells; revenue lines without the grouped key collect under a null-key row, so
// rows add up to totals.
// e.g. by customer, Jan–Feb: acme [Jan actual 200, Feb actual 100], beta [Jan 0, Feb actual 100].
func (s *revenueReportService) GetWaterfall(ctx context.Context, req *dto.WaterfallRequest) (*dto.WaterfallResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	start, end, err := req.PeriodRange()
	if err != nil {
		return nil, err
	}
	groupBy := req.ReportGroupBy()

	// e.g. ["2026-01", "2026-02"]
	var periods []string
	for month := start; !month.After(end); month = month.AddDate(0, 1, 0) {
		periods = append(periods, month.Format(periodLayout))
	}

	// e.g. {"cust_acme": {"2026-01": cell}}; "" is the null-key row.
	cells := map[string]map[string]*dto.WaterfallCell{}
	cell := func(groupID, period string) *dto.WaterfallCell {
		if cells[groupID] == nil {
			cells[groupID] = map[string]*dto.WaterfallCell{}
		}
		if cells[groupID][period] == nil {
			cells[groupID][period] = &dto.WaterfallCell{Period: period}
		}
		return cells[groupID][period]
	}

	// Actual: posted revenue per month; the opening row (before the range) is not a waterfall cell.
	// e.g. [{Jan, revenue_earned, acme, 200}, {Feb, revenue_earned, acme, 100}]
	sums, err := s.journalRepo.SumLines(ctx, &journalRepo.SumFilter{
		Currency:    req.Currency,
		PeriodStart: start,
		PeriodEnd:   end,
		Accounts:    []types.LedgerAccountKey{types.LedgerAccountRevenue},
		CustomerIDs: req.CustomerIDs,
		ContractIDs: req.ContractIDs,
		POBIDs:      req.POBIDs,
		GroupBy:     []types.RevenueReportGroupBy{groupBy},
	})
	if err != nil {
		return nil, err
	}
	for _, sum := range sums {
		if sum.PeriodStart == nil {
			continue
		}
		c := cell(groupValue(groupBy, sum.CustomerID, sum.ContractID, sum.POBID), sum.PeriodStart.Format(periodLayout))
		c.Revenue.Actual = c.Revenue.Actual.Add(sum.Net)
	}

	// addRevenue adds, times sign, the in-range revenue lines of earned entries to the breakdown field it picks.
	// e.g. a Mar 14 entry crediting revenue 45.16 for acme → acme's March field += 45.16 × sign
	addRevenue := func(entries []*domainJournal.Entry, sign decimal.Decimal, field func(*dto.RevenueBreakdown) *decimal.Decimal) {
		for _, e := range entries {
			period := e.BusinessDate().Format(periodLayout)
			if e.EventType() != types.JournalEventRevenueEarned || period < periods[0] || period > periods[len(periods)-1] {
				continue
			}
			for _, l := range e.Lines() {
				if l.Account() != types.LedgerAccountRevenue || (len(req.POBIDs) > 0 && !lo.Contains(req.POBIDs, l.POBID())) {
					continue
				}
				c := cell(groupValue(groupBy, lo.ToPtr(e.CustomerID()), lo.EmptyableToPtr(e.ContractID()), lo.EmptyableToPtr(l.POBID())), period)
				amount := lo.Ternary(l.Direction() == types.JournalDirectionCredit, l.Amount(), l.Amount().Neg())
				*field(&c.Revenue) = field(&c.Revenue).Add(amount.Mul(sign))
			}
		}
	}

	// Pending is what a recognition run would post now: the open month to date and ended months not posted yet.
	// Scheduled is what a run at the range's end would add on top: invoiced revenue not yet earned, open and future
	// months only. The run is skipped when the range has no open month.
	// e.g. on Mar 15, annual 1,200 from Jan 1: now → Mar 45.16; Jan 1 next year → Mar–Dec 100 each; scheduled Mar 54.84.
	now := lo.FromPtrOr(req.AsOf, time.Now()).UTC()
	asOfs := []time.Time{now}
	if !end.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		// Noon UTC on the 1st after the range is the 1st in every timezone, so no partial month is added.
		asOfs = append(asOfs, end.AddDate(0, 1, 0).Add(12*time.Hour))
	}
	previews, err := s.previewRecognition(ctx, req, asOfs...)
	if err != nil {
		return nil, err
	}
	pending := func(b *dto.RevenueBreakdown) *decimal.Decimal { return &b.Pending }
	scheduled := func(b *dto.RevenueBreakdown) *decimal.Decimal { return &b.Scheduled }
	addRevenue(previews[0], decimal.NewFromInt(1), pending)
	if len(previews) > 1 {
		addRevenue(previews[1], decimal.NewFromInt(1), scheduled)
		addRevenue(previews[0], decimal.NewFromInt(-1), scheduled)
	}

	// Details: each group's closing deferred and unbilled for the ended months in range.
	detailsThrough := ""
	lastEnded := now.AddDate(0, -1, 0)
	lastEnded = time.Date(lastEnded.Year(), lastEnded.Month(), 1, 0, 0, 0, 0, time.UTC)

	if req.Details && !start.After(lastEnded) {
		detailsEnd := lo.Ternary(end.After(lastEnded), lastEnded, end)
		detailsThrough = detailsEnd.Format(periodLayout)
		// e.g. [{Jan, deferred, acme, closing 1100}, {Feb, deferred, acme, closing 1000}, {Feb, unbilled, beta, closing 0}]
		balances, err := s.rollforwardRows(ctx, &journalRepo.SumFilter{
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
			return nil, err
		}
		for _, balance := range balances {
			c := cell(groupValue(groupBy, balance.CustomerID, balance.ContractID, balance.POBID), balance.Period)
			if balance.Account == types.LedgerAccountDeferredRevenue {
				c.Deferred = lo.ToPtr(balance.Closing)
			} else {
				c.Unbilled = lo.ToPtr(balance.Closing)
			}
		}
	}

	rows := waterfallRows(groupBy, periods, cells, detailsThrough)
	limit := lo.Ternary(req.Limit > 0, req.Limit, defaultLimit)
	return &dto.WaterfallResponse{
		Totals:     waterfallTotals(periods, rows, detailsThrough),
		Items:      lo.Subset(rows, req.Offset, uint(limit)),
		Pagination: lo.ToPtr(types.NewPaginationResponse(len(rows), limit, req.Offset)),
	}, nil
}

// previewRecognition dry-runs recognition at each asOf for the request's active and ended contracts in its currency,
// returning one entry list per asOf.
func (s *revenueReportService) previewRecognition(ctx context.Context, req *dto.WaterfallRequest, asOfs ...time.Time) ([][]*domainJournal.Entry, error) {
	contracts, err := s.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractIDs:      req.ContractIDs,
		CustomerIDs:      req.CustomerIDs,
		ContractStatuses: []types.ContractStatus{types.ContractStatusActive, types.ContractStatusEnded},
	})
	if err != nil {
		return nil, err
	}

	previews := make([][]*domainJournal.Entry, len(asOfs))
	for _, c := range contracts {
		if c.Currency() != req.Currency {
			continue
		}
		for i, asOf := range asOfs {
			entries, err := s.journalService.Recognize(ctx, journalsvc.RecognizeRequest{ContractID: c.ID(), AsOf: asOf, DryRun: true})
			if err != nil {
				return nil, err
			}
			previews[i] = append(previews[i], entries...)
		}
	}
	return previews, nil
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

// waterfallRows gives each group with any amount one cell per period, totals filled, ordered by group ID with the
// null-key row first; balances through detailsThrough default to zero, later ones stay empty.
// e.g. {"cust_beta": {"2026-02": actual 100}} → [{customer_id: cust_beta, cells: [Jan 0, Feb actual 100 total 100]}]
func waterfallRows(groupBy types.RevenueReportGroupBy, periods []string, cells map[string]map[string]*dto.WaterfallCell, detailsThrough string) []*dto.WaterfallRow {
	groupIDs := lo.Keys(cells)
	sort.Strings(groupIDs)

	rows := []*dto.WaterfallRow{}
	for _, groupID := range groupIDs {
		row := &dto.WaterfallRow{}
		groupPtr := lo.EmptyableToPtr(groupID)
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
			c := lo.FromPtrOr(cells[groupID][period], dto.WaterfallCell{Period: period})
			c.Revenue.Total = c.Revenue.Actual.Add(c.Revenue.Pending).Add(c.Revenue.Scheduled)
			if period <= detailsThrough {
				c.Deferred = lo.CoalesceOrEmpty(c.Deferred, lo.ToPtr(decimal.Zero))
				c.Unbilled = lo.CoalesceOrEmpty(c.Unbilled, lo.ToPtr(decimal.Zero))
			}
			empty = empty && c.Revenue.Total.IsZero() && lo.FromPtr(c.Deferred).IsZero() && lo.FromPtr(c.Unbilled).IsZero()
			row.Cells = append(row.Cells, &c)
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
