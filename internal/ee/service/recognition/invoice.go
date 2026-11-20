package recognition

import (
	"context"
	"sort"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

type invoiceRecognizer struct {
	invoiceRepo    invoice.Repository
	taxAppliedRepo taxapplied.Repository
	contractRepo   contractRepo.Repository
	journalRepo    journalRepo.Repository
	timezones      timezones
}

func NewInvoiceRecognizer(
	invoiceRepo invoice.Repository,
	taxAppliedRepo taxapplied.Repository,
	contractRepo contractRepo.Repository,
	journalRepo journalRepo.Repository,
	subRepo subscription.Repository,
	customerRepo customer.Repository,
) InvoiceRecognizer {
	return &invoiceRecognizer{
		invoiceRepo:    invoiceRepo,
		taxAppliedRepo: taxAppliedRepo,
		contractRepo:   contractRepo,
		journalRepo:    journalRepo,
		timezones:      timezones{subRepo: subRepo, customerRepo: customerRepo},
	}
}

func (r *invoiceRecognizer) LockKey(id string) string {
	return "revrec:invoice:" + id
}

// Due is every finalized or voided invoice of a subscription with no contract: all of the scoped customer's, none for a
// scoped contract, else those that changed since `since` or whose period ended in the last two months (they may still
// owe their last month).
func (r *invoiceRecognizer) Due(ctx context.Context, since, asOf time.Time, scope Scope) ([]string, error) {
	if scope.ContractID != "" {
		return nil, nil
	}
	list := func(f *types.InvoiceFilter) ([]*invoice.Invoice, error) {
		f.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized, types.InvoiceStatusVoided}
		f.CustomerID = scope.CustomerID
		return r.invoiceRepo.List(ctx, f)
	}

	var candidates []*invoice.Invoice
	if scope.CustomerID != "" {
		all, err := list(types.NewNoLimitInvoiceFilter())
		if err != nil {
			return nil, err
		}
		candidates = all
	} else {
		changed := types.NewNoLimitInvoiceFilter()
		changed.UpdatedAtGTE = &since
		changedInvoices, err := list(changed)
		if err != nil {
			return nil, err
		}
		recent := types.NewNoLimitInvoiceFilter()
		recent.PeriodEndGTE = lo.ToPtr(asOf.AddDate(0, -2, 0))
		recentInvoices, err := list(recent)
		if err != nil {
			return nil, err
		}
		candidates = lo.UniqBy(append(changedInvoices, recentInvoices...), func(inv *invoice.Invoice) string { return inv.ID })
	}

	standalone := make(map[string]bool)
	var ids []string
	var err error
	for _, inv := range candidates {
		subID := lo.FromPtr(inv.SubscriptionID)
		if subID == "" {
			continue
		}
		if _, ok := standalone[subID]; !ok {
			if standalone[subID], err = r.isStandalone(ctx, subID); err != nil {
				return nil, err
			}
		}
		if standalone[subID] {
			ids = append(ids, inv.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// isStandalone reports whether the subscription has no contract. An archived subscription is not, since its invoices
// never post billing either.
func (r *invoiceRecognizer) isStandalone(ctx context.Context, subscriptionID string) (bool, error) {
	if _, err := r.timezones.subRepo.Get(ctx, subscriptionID); err != nil {
		if ierr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	_, err := r.contractRepo.GetBySubscriptionID(ctx, subscriptionID)
	if ierr.IsNotFound(err) {
		return true, nil
	}
	return false, err
}

// Recognize is, for each month from the month the invoice was billed, the change in its earned-to-date as Dr deferred /
// Cr revenue on the month's last day. Nothing earns before it is billed, so arrears catch up in that month.
// e.g. a $1,200 annual invoice billed Jan 1: 101.92 on Jan 31; a Mar 1 arrears invoice for Jan–Feb: all of it on Mar 31.
func (r *invoiceRecognizer) Recognize(ctx context.Context, id string, asOf time.Time, openMonth bool) ([]*domainJournal.Entry, error) {
	inv, err := r.invoiceRepo.Get(ctx, id)
	if err != nil || inv.FinalizedAt == nil {
		return nil, err
	}
	loc, err := r.timezones.of(ctx, inv.CustomerID, lo.FromPtr(inv.SubscriptionID))
	if err != nil {
		return nil, err
	}
	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeInvoice
	taxFilter.EntityIDs = []string{inv.ID}
	taxes, err := r.taxAppliedRepo.List(ctx, taxFilter)
	if err != nil {
		return nil, err
	}
	nets := inv.LineNets(taxes)

	ends := monthEnds(utils.LocalDay(lo.FromPtrOr(inv.IssueDate, *inv.FinalizedAt), loc), utils.LocalDay(asOf, loc), openMonth)
	if len(ends) == 0 {
		return nil, nil
	}

	entries, err := r.journalRepo.List(ctx, &journalRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EventTypes:       []types.JournalEventType{types.JournalEventRevenueEarned},
		SourceEntityType: types.RevRecEntityTypeInvoice,
		SourceEntityIDs:  []string{inv.ID},
	})
	if err != nil {
		return nil, err
	}

	precision := types.GetCurrencyPrecision(inv.Currency)
	var missing []*domainJournal.Entry
	for _, monthEnd := range ends {
		lastDay := monthEnd.AddDate(0, 0, -1)
		delta := invoiceEarned(inv, nets, localMidnight(monthEnd, loc), loc).Round(precision).Sub(recognizedBy(entries, lastDay))

		e := domainJournal.NewBuilder(nil).
			WithEventType(types.JournalEventRevenueEarned).
			WithSource(types.RevRecEntityTypeInvoice, inv.ID).
			WithBusinessDate(lastDay).
			WithCustomerID(inv.CustomerID).
			WithCurrency(inv.Currency).
			WithLines(domainJournal.LinesFrom([]domainJournal.Posting{
				{Account: types.LedgerAccountDeferredRevenue, Amount: delta.Neg()},
				{Account: types.LedgerAccountRevenue, Amount: delta},
			})).
			Build()
		if len(e.Lines()) == 0 {
			continue
		}
		entries = append(entries, e)
		missing = append(missing, e)
	}
	return missing, nil
}

// invoiceEarned is what the invoice's subscription lines earned before asOf's local day, each line's net ratably over
// its service period; a top-up earns nothing here, and a voided invoice nothing from its void on.
// e.g. a $300 line over [Jan 1, Apr 1) before Feb 1 → 103.33; voided Jan 20 → 0 at Jan 31.
func invoiceEarned(inv *invoice.Invoice, nets map[string]decimal.Decimal, asOf time.Time, loc *time.Location) decimal.Decimal {
	if inv.Metadata["wallet_transaction_id"] != "" {
		return decimal.Zero
	}
	if inv.InvoiceStatus == types.InvoiceStatusVoided && inv.VoidedAt != nil && inv.VoidedAt.Before(asOf) {
		return decimal.Zero
	}

	earned := decimal.Zero
	for _, li := range inv.LineItems {
		if lo.FromPtr(li.SubscriptionLineItemID) == "" {
			continue
		}
		// e.g. a line with no period earns in full when the invoice is finalized
		start := lo.FromPtrOr(li.PeriodStart, *inv.FinalizedAt)
		end := lo.FromPtrOr(li.PeriodEnd, start)
		earned = earned.Add(pobsvc.Earned(nets[li.ID], start, end, types.RecognitionMethodStraightLine, types.AmortizationDaily, asOf, loc))
	}
	return earned
}

// recognizedBy is the revenue credited by the entries dated on or before lastDay.
func recognizedBy(entries []*domainJournal.Entry, lastDay time.Time) decimal.Decimal {
	recognized := decimal.Zero
	for _, e := range entries {
		if e.BusinessDate().After(lastDay) {
			continue
		}
		for _, l := range e.Lines() {
			if l.Account() == types.LedgerAccountRevenue {
				recognized = recognized.Add(l.Net())
			}
		}
	}
	return recognized
}
