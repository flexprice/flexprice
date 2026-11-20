package recognition

import (
	"context"
	"sort"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

type contractRecognizer struct {
	contractRepo contractRepo.Repository
	pobRepo      pobRepo.Repository
	pobService   pobsvc.PerformanceObligationService
	journalRepo  journalRepo.Repository
	invoiceRepo  invoice.Repository
	timezones    timezones
}

func NewContractRecognizer(
	contractRepo contractRepo.Repository,
	pobRepo pobRepo.Repository,
	pobService pobsvc.PerformanceObligationService,
	journalRepo journalRepo.Repository,
	invoiceRepo invoice.Repository,
	subRepo subscription.Repository,
	customerRepo customer.Repository,
) ContractRecognizer {
	return &contractRecognizer{
		contractRepo: contractRepo,
		pobRepo:      pobRepo,
		pobService:   pobService,
		journalRepo:  journalRepo,
		invoiceRepo:  invoiceRepo,
		timezones:    timezones{subRepo: subRepo, customerRepo: customerRepo},
	}
}

func (r *contractRecognizer) LockKey(id string) string {
	return "revrec:contract:" + id
}

// Due is the scoped contract, or every contract of the scoped customer; else every active contract, every contract
// ended in the last two months (it may still owe its last month), and every contract of a subscription with an
// invoice changed since `since`.
func (r *contractRecognizer) Due(ctx context.Context, since, asOf time.Time, scope Scope) ([]string, error) {
	if scope.ContractID != "" {
		return []string{scope.ContractID}, nil
	}
	if scope.CustomerID != "" {
		contracts, err := r.contractRepo.List(ctx, &contractRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), CustomerIDs: []string{scope.CustomerID}})
		if err != nil {
			return nil, err
		}
		ids := lo.Map(contracts, func(c *domainContract.Contract, _ int) string { return c.ID() })
		sort.Strings(ids)
		return ids, nil
	}

	active, err := r.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractStatuses: []types.ContractStatus{types.ContractStatusActive},
	})
	if err != nil {
		return nil, err
	}
	ended, err := r.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractStatuses: []types.ContractStatus{types.ContractStatusEnded},
		EndedAtGTE:       lo.ToPtr(asOf.AddDate(0, -2, 0)),
	})
	if err != nil {
		return nil, err
	}

	invoiceFilter := types.NewNoLimitInvoiceFilter()
	invoiceFilter.UpdatedAtGTE = &since
	invoices, err := r.invoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return nil, err
	}
	subscriptionIDs := lo.Uniq(lo.FilterMap(invoices, func(inv *invoice.Invoice, _ int) (string, bool) {
		return lo.FromPtr(inv.SubscriptionID), lo.FromPtr(inv.SubscriptionID) != ""
	}))

	var billed []*domainContract.Contract
	if len(subscriptionIDs) > 0 {
		if billed, err = r.contractRepo.List(ctx, &contractRepo.Filter{
			QueryFilter:     types.NewNoLimitQueryFilter(),
			SubscriptionIDs: subscriptionIDs,
		}); err != nil {
			return nil, err
		}
	}

	ids := lo.Uniq(lo.Map(append(append(active, ended...), billed...), func(c *domainContract.Contract, _ int) string { return c.ID() }))
	sort.Strings(ids)
	return ids, nil
}

// Recognize is, for each month of the contract's active and satisfied POBs, the recognition and offset each POB is
// missing.
// e.g. a $1,200 annual prepaid from Jan 1, as of Feb 1: Dr unbilled 100 / Cr revenue 100 and Dr deferred 100 / Cr unbilled
// 100, dated Jan 31; a backdated cut of January to 90 later gives −10 and −10 for January.
func (r *contractRecognizer) Recognize(ctx context.Context, id string, asOf time.Time, openMonth bool) ([]*domainJournal.Entry, error) {
	c, err := r.contractRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	pobs, err := r.pobRepo.List(ctx, &pobRepo.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		ContractIDs: []string{id},
		POBStatuses: []types.PerformanceObligationStatus{types.PerformanceObligationStatusActive, types.PerformanceObligationStatusSatisfied},
	})
	if err != nil || len(pobs) == 0 {
		return nil, err
	}

	sources, err := r.contractRepo.ListAssociations(ctx, id)
	if err != nil {
		return nil, err
	}
	sub, _ := lo.Find(sources, func(a *domainContract.Association) bool { return a.EntityType() == types.RevRecEntityTypeSubscription })
	loc, err := r.timezones.of(ctx, c.CustomerID(), sub.EntityID())
	if err != nil {
		return nil, err
	}

	first := utils.LocalDay(lo.Earliest(lo.Map(pobs, func(p *domainPOB.PerformanceObligation, _ int) time.Time { return p.ServiceStart() })...), loc)
	ends := monthEnds(first, utils.LocalDay(asOf, loc), openMonth)
	if len(ends) == 0 {
		return nil, nil
	}

	earned, err := r.pobService.EarnedToDate(ctx, id, lo.Map(ends, func(m time.Time, _ int) time.Time { return localMidnight(m, loc) }))
	if err != nil {
		return nil, err
	}

	precision := types.GetCurrencyPrecision(c.Currency())
	var missing []*domainJournal.Entry
	for _, p := range pobs {
		entries, err := r.journalRepo.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), POBIDs: []string{p.ID()}})
		if err != nil {
			return nil, err
		}

		entry := func(eventType types.JournalEventType, lastDay time.Time, postings []domainJournal.Posting) *domainJournal.Entry {
			return domainJournal.NewBuilder(nil).
				WithEventType(eventType).
				WithSource(types.RevRecEntityTypePOB, p.ID()).
				WithBusinessDate(lastDay).
				WithCustomerID(c.CustomerID()).
				WithContractID(c.ID()).
				WithCurrency(c.Currency()).
				WithLines(domainJournal.LinesFrom(postings)).
				Build()
		}

		for i, monthEnd := range ends {
			lastDay := monthEnd.AddDate(0, 0, -1)
			pos := positionAt(entries, p.ID(), lastDay)
			toRecognize, toOffset := computeMonth(pos, earned[i][p.ID()].Round(precision))

			// e.g. arrears Feb 28: [revenue_earned Dr unbilled 250, revenue_offset Dr deferred 300 / Cr unbilled 300]
			month := []*domainJournal.Entry{
				entry(types.JournalEventRevenueEarned, lastDay, []domainJournal.Posting{
					{Account: types.LedgerAccountUnbilledReceivable, Amount: toRecognize.Neg(), POBID: p.ID()},
					{Account: types.LedgerAccountRevenue, Amount: toRecognize, POBID: p.ID()},
				}),
				entry(types.JournalEventRevenueOffset, lastDay, []domainJournal.Posting{
					{Account: types.LedgerAccountDeferredRevenue, Amount: toOffset.Neg(), POBID: p.ID()},
					{Account: types.LedgerAccountUnbilledReceivable, Amount: toOffset, POBID: p.ID()},
				}),
			}
			// Billed ahead and fully earned with nothing in unbilled: unbilled would rise and fall by the same amount, so
			// one entry instead. e.g. prepaid Jan 31: revenue_earned Dr deferred 100 / Cr revenue 100
			if toOffset.Equal(toRecognize) && pos.revenue.Equal(pos.offset) {
				month = []*domainJournal.Entry{
					entry(types.JournalEventRevenueEarned, lastDay, []domainJournal.Posting{
						{Account: types.LedgerAccountDeferredRevenue, Amount: toRecognize.Neg(), POBID: p.ID()},
						{Account: types.LedgerAccountRevenue, Amount: toRecognize, POBID: p.ID()},
					}),
				}
			}

			// Zero amounts leave no lines; the rest join the in-memory totals for the next month.
			month = lo.Filter(month, func(e *domainJournal.Entry, _ int) bool { return len(e.Lines()) > 0 })
			entries = append(entries, month...)
			missing = append(missing, month...)
		}
	}
	return missing, nil
}

// pobPosition is a POB's journal sums over entries dated on or before a month's last day.
type pobPosition struct {
	billed  decimal.Decimal // deferred credits − debits on billing entries
	revenue decimal.Decimal // revenue credits − debits on revenue_earned entries
	offset  decimal.Decimal // deferred debits − credits on recognition entries (offsets and prepaid single entries)
}

// positionAt sums the POB's lines on entries dated on or before monthEnd.
// e.g. a Jan 1 invoice crediting deferred 1,200 and a Jan 31 offset debiting it 100 → billed 1,200, offset 100.
func positionAt(entries []*domainJournal.Entry, pobID string, monthEnd time.Time) pobPosition {
	var pos pobPosition

	for _, e := range entries {
		if e.BusinessDate().After(monthEnd) {
			continue
		}

		for _, l := range e.Lines() {
			if l.POBID() != pobID {
				continue
			}

			credit := l.Net()
			switch {
			case e.EventType() == types.JournalEventRevenueEarned || e.EventType() == types.JournalEventRevenueOffset:
				// A prepaid month's single entry debits deferred directly, so it counts as offset too.
				switch l.Account() {
				case types.LedgerAccountRevenue:
					pos.revenue = pos.revenue.Add(credit)
				case types.LedgerAccountDeferredRevenue:
					pos.offset = pos.offset.Sub(credit)
				}
			case l.Account() == types.LedgerAccountDeferredRevenue:
				pos.billed = pos.billed.Add(credit)
			}
		}
	}

	return pos
}

// computeMonth is the month's recognition δ and offset given the position and the rounded earned-to-date at its end.
// e.g. Example 1 January: earned 100, position{billed: 1,200} → δ 100, offset 100; a rerun gives 0, 0.
func computeMonth(pos pobPosition, earned decimal.Decimal) (delta, offset decimal.Decimal) {
	// Recognition posts only what the journal is missing.
	delta = earned.Sub(pos.revenue)

	// What is both billed and earned sits in deferred and unbilled at once; earlier offsets removed part of it.
	billedAndEarned := decimal.Min(pos.billed, earned)
	offset = billedAndEarned.Sub(pos.offset)

	return delta, offset
}
