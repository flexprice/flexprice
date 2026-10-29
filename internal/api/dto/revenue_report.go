package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// periodLayout is the YYYY-MM form of report periods.
const periodLayout = "2006-01"

// RollforwardRequest asks for the opening, movements and closing of accounts per ended month.
type RollforwardRequest struct {
	PeriodStart string                       `json:"period_start" validate:"required"` // YYYY-MM
	PeriodEnd   string                       `json:"period_end,omitempty"`             // YYYY-MM, an ended month; defaults to period_start
	Currency    string                       `json:"currency" validate:"required"`
	Accounts    []types.LedgerAccountKey     `json:"accounts,omitempty"` // defaults to [deferred_revenue]
	GroupBy     []types.RevenueReportGroupBy `json:"group_by,omitempty"`
	CustomerIDs []string                     `json:"customer_ids,omitempty"`
	ContractIDs []string                     `json:"contract_ids,omitempty"`
	POBIDs      []string                     `json:"pob_ids,omitempty"`
	PostedBy    *time.Time                   `json:"posted_by,omitempty"`                                 // only entries posted at or before it: the books as they were then
	Limit       int                          `json:"limit,omitempty" validate:"omitempty,min=0,max=1000"` // item rows per page; totals are never paged
	Offset      int                          `json:"offset,omitempty" validate:"omitempty,min=0"`
}

func (r *RollforwardRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	_, end, err := r.PeriodRange()
	if err != nil {
		return err
	}
	if end.After(utils.LastEndedMonth(time.Now())) {
		return ierr.NewError("period_end must be an ended month").
			WithHint("Only ended months can be reported").
			Mark(ierr.ErrValidation)
	}
	for _, account := range r.Accounts {
		if account.Class() == "" {
			return ierr.NewErrorf("unknown account %q", account).
				WithHintf("Unknown account %q", account).
				Mark(ierr.ErrValidation)
		}
	}
	if err := validateGroupBy(r.GroupBy...); err != nil {
		return err
	}
	byPOB := len(r.POBIDs) > 0 || lo.Contains(r.GroupBy, types.RevenueReportGroupByPOB)
	if byPOB && lo.SomeBy(r.ReportAccounts(), func(a types.LedgerAccountKey) bool { return !a.CarriesPOB() }) {
		return ierr.NewError("pob_id applies only to deferred_revenue, unbilled_receivable, revenue and contra_revenue").
			WithHint("Group or filter by POB only for deferred_revenue, unbilled_receivable, revenue and contra_revenue").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PeriodRange returns the first days of the first and last requested months.
func (r *RollforwardRequest) PeriodRange() (time.Time, time.Time, error) {
	return parsePeriodRange(r.PeriodStart, lo.CoalesceOrEmpty(r.PeriodEnd, r.PeriodStart))
}

// parsePeriodRange parses YYYY-MM bounds into the first days of their months, e.g. "2026-01", "2026-03" → Jan 1, Mar 1.
func parsePeriodRange(periodStart, periodEnd string) (time.Time, time.Time, error) {
	start, err := time.Parse(periodLayout, periodStart)
	if err != nil {
		return time.Time{}, time.Time{}, ierr.WithError(err).
			WithHint("period_start must be YYYY-MM").
			Mark(ierr.ErrValidation)
	}
	end, err := time.Parse(periodLayout, periodEnd)
	if err != nil {
		return time.Time{}, time.Time{}, ierr.WithError(err).
			WithHint("period_end must be YYYY-MM").
			Mark(ierr.ErrValidation)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, ierr.NewError("period_end is before period_start").
			WithHint("period_end must not be before period_start").
			Mark(ierr.ErrValidation)
	}
	return start, end, nil
}

func validateGroupBy(keys ...types.RevenueReportGroupBy) error {
	for _, key := range keys {
		if !lo.Contains([]types.RevenueReportGroupBy{types.RevenueReportGroupByCustomer, types.RevenueReportGroupByContract, types.RevenueReportGroupByPOB}, key) {
			return ierr.NewErrorf("unknown group_by %q", key).
				WithHintf("Unknown group_by %q", key).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ReportAccounts returns the requested accounts, defaulting to deferred revenue.
func (r *RollforwardRequest) ReportAccounts() []types.LedgerAccountKey {
	if len(r.Accounts) == 0 {
		return []types.LedgerAccountKey{types.LedgerAccountDeferredRevenue}
	}
	return r.Accounts
}

type RollforwardResponse struct {
	Totals     []*RollforwardRow         `json:"totals"` // per period and account, all groups
	Items      []*RollforwardRow         `json:"items"`  // per period, account and group; empty without group_by
	Pagination *types.PaginationResponse `json:"pagination,omitempty"`
}

// RollforwardRow amounts are in the account's normal direction: positive increases the balance.
type RollforwardRow struct {
	Period     string                                     `json:"period"` // YYYY-MM
	Account    types.LedgerAccountKey                     `json:"account"`
	CustomerID *string                                    `json:"customer_id,omitempty"`
	ContractID *string                                    `json:"contract_id,omitempty"`
	POBID      *string                                    `json:"pob_id,omitempty"`
	Opening    decimal.Decimal                            `json:"opening" swaggertype:"string"`
	Movements  map[types.MovementCategory]decimal.Decimal `json:"movements" swaggertype:"object,string"` // a missing category is 0
	Closing    decimal.Decimal                            `json:"closing" swaggertype:"string"`
}

// maxWaterfallMonths caps a waterfall's range.
const maxWaterfallMonths = 36

// WaterfallRequest asks for revenue per group and month: actual, pending and scheduled.
type WaterfallRequest struct {
	PeriodStart string                     `json:"period_start" validate:"required"` // YYYY-MM
	PeriodEnd   string                     `json:"period_end" validate:"required"`   // YYYY-MM, at most 36 months from period_start
	Currency    string                     `json:"currency" validate:"required"`
	GroupBy     types.RevenueReportGroupBy `json:"group_by,omitempty"` // defaults to customer_id
	CustomerIDs []string                   `json:"customer_ids,omitempty"`
	ContractIDs []string                   `json:"contract_ids,omitempty"`
	POBIDs      []string                   `json:"pob_ids,omitempty"`
	Details     bool                       `json:"details,omitempty"`                                   // month-end deferred and unbilled balances
	AsOf        *time.Time                 `json:"as_of,omitempty"`                                     // report date: pending and scheduled split here; defaults to now
	Limit       int                        `json:"limit,omitempty" validate:"omitempty,min=0,max=1000"` // rows per page; totals are never paged
	Offset      int                        `json:"offset,omitempty" validate:"omitempty,min=0"`
}

func (r *WaterfallRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	start, end, err := r.PeriodRange()
	if err != nil {
		return err
	}
	if !end.Before(start.AddDate(0, maxWaterfallMonths, 0)) {
		return ierr.NewErrorf("a waterfall covers at most %d months", maxWaterfallMonths).
			WithHintf("Request at most %d months", maxWaterfallMonths).
			Mark(ierr.ErrValidation)
	}
	return validateGroupBy(r.ReportGroupBy())
}

// PeriodRange returns the first days of the first and last requested months.
func (r *WaterfallRequest) PeriodRange() (time.Time, time.Time, error) {
	return parsePeriodRange(r.PeriodStart, r.PeriodEnd)
}

// ReportGroupBy returns the requested group key, defaulting to customer.
func (r *WaterfallRequest) ReportGroupBy() types.RevenueReportGroupBy {
	return lo.CoalesceOrEmpty(r.GroupBy, types.RevenueReportGroupByCustomer)
}

type WaterfallResponse struct {
	Totals     []*WaterfallCell          `json:"totals"` // per period over all rows, never paged
	Items      []*WaterfallRow           `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination"`
}

// WaterfallRow is one group's cells; only the grouped key is set, and a null key collects lines without one.
type WaterfallRow struct {
	CustomerID *string          `json:"customer_id,omitempty"`
	ContractID *string          `json:"contract_id,omitempty"`
	POBID      *string          `json:"pob_id,omitempty"`
	Cells      []*WaterfallCell `json:"cells"` // one per requested period, in order
}

type WaterfallCell struct {
	Period   string           `json:"period"` // YYYY-MM
	Revenue  RevenueBreakdown `json:"revenue"`
	Deferred *decimal.Decimal `json:"deferred,omitempty" swaggertype:"string"` // details: closing deferred_revenue, ended months only
	Unbilled *decimal.Decimal `json:"unbilled,omitempty" swaggertype:"string"` // details: closing unbilled_receivable, ended months only
}

// RevenueBreakdown splits a month's revenue: actual is posted, pending is earned but not posted, scheduled is invoiced
// but not yet earned.
type RevenueBreakdown struct {
	Total     decimal.Decimal `json:"total" swaggertype:"string"` // actual + pending + scheduled
	Actual    decimal.Decimal `json:"actual" swaggertype:"string"`
	Pending   decimal.Decimal `json:"pending" swaggertype:"string"`
	Scheduled decimal.Decimal `json:"scheduled" swaggertype:"string"`
}
