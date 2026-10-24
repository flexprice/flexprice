package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// JournalService posts billing and earning events as balanced journal entries.
type JournalService interface {
	// PostInvoice posts a finalized invoice's entry and, once voided, its void; safe to repeat.
	PostInvoice(ctx context.Context, inv *invoice.Invoice) error
	// PostCreditNote posts a finalized credit note's entry and, once voided, its reversal; safe to repeat.
	PostCreditNote(ctx context.Context, cn *creditnote.CreditNote) error
	// PostPayment posts a succeeded invoice payment and, once refunded or voided, its reversal; safe to repeat.
	PostPayment(ctx context.Context, p *payment.Payment) error
	// PostRefund posts a settled refund; safe to repeat.
	PostRefund(ctx context.Context, r *refund.Refund) error
	// Reconcile posts what hooks missed for every billing source changed since `since`; errors are collected per source.
	Reconcile(ctx context.Context, since time.Time) error
	GetJournalEntry(ctx context.Context, id string) (*dto.JournalEntryResponse, error)
	GetJournalEntries(ctx context.Context, filter *journalRepo.Filter) (*dto.ListJournalEntriesResponse, error)
}

type journalService struct {
	journalRepo    journalRepo.Repository
	periodService  accountingperiodsvc.AccountingPeriodService
	pobService     pobsvc.PerformanceObligationService
	contractRepo   contractRepo.Repository
	subRepo        subscription.Repository
	customerRepo   customer.Repository
	invoiceRepo    invoice.Repository
	creditNoteRepo creditnote.Repository
	paymentRepo    payment.Repository
	refundRepo     refund.Repository
	taxAppliedRepo taxapplied.Repository
}

func NewJournalService(
	journalRepo journalRepo.Repository,
	periodService accountingperiodsvc.AccountingPeriodService,
	pobService pobsvc.PerformanceObligationService,
	contractRepo contractRepo.Repository,
	subRepo subscription.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	creditNoteRepo creditnote.Repository,
	paymentRepo payment.Repository,
	refundRepo refund.Repository,
	taxAppliedRepo taxapplied.Repository,
) JournalService {
	return &journalService{
		journalRepo:    journalRepo,
		periodService:  periodService,
		pobService:     pobService,
		contractRepo:   contractRepo,
		subRepo:        subRepo,
		customerRepo:   customerRepo,
		invoiceRepo:    invoiceRepo,
		creditNoteRepo: creditNoteRepo,
		paymentRepo:    paymentRepo,
		refundRepo:     refundRepo,
		taxAppliedRepo: taxAppliedRepo,
	}
}

// posting is a signed amount on an account: positive credits, negative debits.
type posting struct {
	account types.LedgerAccountKey
	amount  decimal.Decimal
	pobID   string
}

// e.g. 100 + 18 tax with 20 credits: Dr AR 98, Dr customer_credit 20 / Cr deferred 100, Cr tax 18; void Cr AR 118.
func (s *journalService) PostInvoice(ctx context.Context, inv *invoice.Invoice) error {
	if inv.FinalizedAt == nil {
		return nil
	}

	revenuePostings, err := s.invoiceRevenuePostings(ctx, inv)
	if err != nil {
		return err
	}

	loc, contractID, err := s.sourceContext(ctx, inv.CustomerID, inv.SubscriptionID)
	if err != nil {
		return err
	}

	entry := func(eventType types.JournalEventType, at time.Time, postings []posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeInvoice, inv.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(localDate(at, loc)).
			WithCustomerID(inv.CustomerID).
			WithContractID(contractID).
			WithCurrency(inv.Currency).
			WithLines(toLines(postings)).
			Build()
	}

	finalizePostings := append([]posting{
		{account: types.LedgerAccountReceivable, amount: inv.Total.Neg()},
		{account: types.LedgerAccountCustomerCredit, amount: inv.TotalPrepaidCreditsApplied.Neg()},
	}, revenuePostings...)

	finalizeID, err := s.post(ctx, entry(types.JournalEventInvoiceFinalized, *inv.FinalizedAt, finalizePostings, ""))
	if err != nil {
		return err
	}

	if inv.InvoiceStatus != types.InvoiceStatusVoided || inv.VoidedAt == nil {
		return nil
	}

	void := append([]posting{
		{account: types.LedgerAccountReceivable, amount: inv.Total.Add(inv.TotalPrepaidCreditsApplied)},
	}, reversed(revenuePostings)...)

	_, err = s.post(ctx, entry(types.JournalEventInvoiceVoided, *inv.VoidedAt, void, finalizeID))
	return err
}

// invoiceRevenuePostings credits tax and each line's net: POB lines to deferred, top-ups to customer credit, else revenue.
func (s *journalService) invoiceRevenuePostings(ctx context.Context, inv *invoice.Invoice) ([]posting, error) {
	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeInvoice
	taxFilter.EntityIDs = []string{inv.ID}
	taxes, err := s.taxAppliedRepo.List(ctx, taxFilter)
	if err != nil {
		return nil, err
	}
	nets := inv.LineNets(taxes)

	postings := []posting{{account: types.LedgerAccountTaxPayable, amount: inv.TotalTax}}
	for _, li := range inv.LineItems {
		p := posting{account: types.LedgerAccountRevenue, amount: nets[li.ID]}

		switch {
		case inv.Metadata["wallet_transaction_id"] != "":
			p.account = types.LedgerAccountCustomerCredit
		case lo.FromPtr(li.SubscriptionLineItemID) != "":
			pobID, err := s.pobService.POBIDForInvoiceLine(ctx, li)
			if err != nil {
				return nil, err
			}

			p.account, p.pobID = types.LedgerAccountDeferredRevenue, pobID
		}

		postings = append(postings, p)
	}

	return postings, nil
}

// e.g. 118 with 18 tax: Dr contra_revenue 100, Dr tax 18 / Cr AR 118; a void reverses it.
func (s *journalService) PostCreditNote(ctx context.Context, cn *creditnote.CreditNote) error {
	if cn.FinalizedAt == nil {
		return nil
	}

	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeCreditNote
	taxFilter.EntityIDs = []string{cn.ID}
	taxes, err := s.taxAppliedRepo.List(ctx, taxFilter)
	if err != nil {
		return err
	}
	tax := decimal.Zero
	for _, t := range taxes {
		tax = tax.Add(t.TaxAmount)
	}

	contraPostings, err := s.creditNoteContraPostings(ctx, cn, cn.TotalAmount.Sub(tax))
	if err != nil {
		return err
	}

	loc, contractID, err := s.sourceContext(ctx, cn.CustomerID, cn.SubscriptionID)
	if err != nil {
		return err
	}

	entry := func(eventType types.JournalEventType, at time.Time, postings []posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeCreditNote, cn.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(localDate(at, loc)).
			WithCustomerID(cn.CustomerID).
			WithContractID(contractID).
			WithCurrency(cn.Currency).
			WithLines(toLines(postings)).
			Build()
	}

	finalizePostings := append([]posting{
		{account: types.LedgerAccountReceivable, amount: cn.TotalAmount},
		{account: types.LedgerAccountTaxPayable, amount: tax.Neg()},
	}, contraPostings...)

	finalizeID, err := s.post(ctx, entry(types.JournalEventCreditNoteFinalized, *cn.FinalizedAt, finalizePostings, ""))
	if err != nil {
		return err
	}

	if cn.CreditNoteStatus != types.CreditNoteStatusVoided || cn.VoidedAt == nil {
		return nil
	}

	_, err = s.post(ctx, entry(types.JournalEventCreditNoteVoided, *cn.VoidedAt, reversed(finalizePostings), finalizeID))
	return err
}

// creditNoteContraPostings debits contra revenue per line, with its POB, splitting net pro rata by line amount.
// e.g. lines 60 and 40 with net 88 → 52.80 and 35.20.
func (s *journalService) creditNoteContraPostings(ctx context.Context, cn *creditnote.CreditNote, net decimal.Decimal) ([]posting, error) {
	inv, err := s.invoiceRepo.Get(ctx, cn.InvoiceID)
	if err != nil {
		return nil, err
	}
	invoiceLines := lo.KeyBy(inv.LineItems, func(li *invoice.InvoiceLineItem) string { return li.ID })

	amounts := utils.AllocateProRata(lo.SliceToMap(cn.LineItems, func(li *creditnote.CreditNoteLineItem) (string, decimal.Decimal) {
		return li.ID, li.Amount
	}), net, types.GetCurrencyPrecision(cn.Currency))

	postings := make([]posting, 0, len(cn.LineItems))
	for _, li := range cn.LineItems {
		pobID, err := s.pobService.POBIDForInvoiceLine(ctx, invoiceLines[li.InvoiceLineItemID])
		if err != nil {
			return nil, err
		}
		postings = append(postings, posting{account: types.LedgerAccountContraRevenue, amount: amounts[li.ID].Neg(), pobID: pobID})
	}
	return postings, nil
}

// e.g. 80 by card: Dr cash 80 / Cr AR 80; by wallet credits: Dr customer_credit 80 / Cr AR 80.
func (s *journalService) PostPayment(ctx context.Context, p *payment.Payment) error {
	if p.DestinationType != types.PaymentDestinationTypeInvoice || p.SucceededAt == nil {
		return nil
	}

	inv, err := s.invoiceRepo.Get(ctx, p.DestinationID)
	if err != nil {
		return err
	}
	loc, contractID, err := s.sourceContext(ctx, inv.CustomerID, inv.SubscriptionID)
	if err != nil {
		return err
	}

	entry := func(eventType types.JournalEventType, at time.Time, postings []posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypePayment, p.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(localDate(at, loc)).
			WithCustomerID(inv.CustomerID).
			WithContractID(contractID).
			WithCurrency(p.Currency).
			WithLines(toLines(postings)).
			Build()
	}

	debit := lo.Ternary(p.PaymentMethodType == types.PaymentMethodTypeCredits, types.LedgerAccountCustomerCredit, types.LedgerAccountCash)
	succeededPostings := []posting{
		{account: debit, amount: p.Amount.Neg()},
		{account: types.LedgerAccountReceivable, amount: p.Amount},
	}

	succeededID, err := s.post(ctx, entry(types.JournalEventPaymentSucceeded, *p.SucceededAt, succeededPostings, ""))
	if err != nil {
		return err
	}

	reversedAt := lo.CoalesceOrEmpty(p.RefundedAt, p.VoidedAt)
	if (p.PaymentStatus != types.PaymentStatusRefunded && p.PaymentStatus != types.PaymentStatusVoided) || reversedAt == nil {
		return nil
	}

	_, err = s.post(ctx, entry(types.JournalEventPaymentReversed, *reversedAt, reversed(succeededPostings), succeededID))
	return err
}

// e.g. 100 to the wallet: Dr AR 100 / Cr customer_credit 100; to the gateway: Dr AR 100 / Cr cash 100.
func (s *journalService) PostRefund(ctx context.Context, r *refund.Refund) error {
	if !r.IsSettled() || r.SucceededAt == nil {
		return nil
	}

	inv, err := s.invoiceRepo.Get(ctx, r.InvoiceID)
	if err != nil {
		return err
	}
	loc, contractID, err := s.sourceContext(ctx, inv.CustomerID, inv.SubscriptionID)
	if err != nil {
		return err
	}

	credit := lo.Ternary(r.RefundDestination == types.RefundDestinationWallet, types.LedgerAccountCustomerCredit, types.LedgerAccountCash)
	postings := []posting{
		{account: types.LedgerAccountReceivable, amount: r.SettledAmount.Neg()},
		{account: credit, amount: r.SettledAmount},
	}

	_, err = s.post(ctx, domainJournal.NewBuilder(nil).
		WithEventType(types.JournalEventRefundSucceeded).
		WithSource(types.RevRecEntityTypeRefund, r.ID).
		WithBusinessDate(localDate(*r.SucceededAt, loc)).
		WithCustomerID(inv.CustomerID).
		WithContractID(contractID).
		WithCurrency(r.Currency).
		WithLines(toLines(postings)).
		Build())
	return err
}

// e.g. a finalize hook lost to a crash posts on the next run; posted entries are skipped by key.
func (s *journalService) Reconcile(ctx context.Context, since time.Time) error {
	return errors.Join(
		postChanged(func(page *types.QueryFilter) ([]*invoice.Invoice, error) {
			f := types.NewNoLimitInvoiceFilter()
			f.QueryFilter, f.UpdatedAtGTE = page, &since
			return s.invoiceRepo.List(ctx, f)
		}, func(inv *invoice.Invoice) error { return sourceError("invoice", inv.ID, s.PostInvoice(ctx, inv)) }),

		postChanged(func(page *types.QueryFilter) ([]*creditnote.CreditNote, error) {
			f := types.NewNoLimitCreditNoteFilter()
			f.QueryFilter, f.UpdatedAtGTE = page, &since
			return s.creditNoteRepo.List(ctx, f)
		}, func(cn *creditnote.CreditNote) error {
			return sourceError("credit note", cn.ID, s.PostCreditNote(ctx, cn))
		}),

		postChanged(func(page *types.QueryFilter) ([]*payment.Payment, error) {
			f := types.NewNoLimitPaymentFilter()
			f.QueryFilter, f.UpdatedAtGTE = page, &since
			return s.paymentRepo.List(ctx, f)
		}, func(p *payment.Payment) error { return sourceError("payment", p.ID, s.PostPayment(ctx, p)) }),

		postChanged(func(page *types.QueryFilter) ([]*refund.Refund, error) {
			f := types.NewNoLimitRefundFilter()
			f.QueryFilter, f.UpdatedAtGTE = page, &since
			return s.refundRepo.List(ctx, f)
		}, func(r *refund.Refund) error { return sourceError("refund", r.ID, s.PostRefund(ctx, r)) }),
	)
}

// postChanged posts every listed source, 500 per page; a post failure is collected, a list failure stops.
func postChanged[T any](list func(page *types.QueryFilter) ([]T, error), post func(T) error) error {
	const pageSize = 500
	var errs []error
	for offset := 0; ; offset += pageSize {
		page := types.NewDefaultQueryFilter()
		page.Limit, page.Offset = lo.ToPtr(pageSize), lo.ToPtr(offset)
		sources, err := list(page)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		for _, src := range sources {
			errs = append(errs, post(src))
		}
		if len(sources) < pageSize {
			return errors.Join(errs...)
		}
	}
}

func sourceError(kind, id string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s %s: %w", kind, id, err)
}

func (s *journalService) GetJournalEntry(ctx context.Context, id string) (*dto.JournalEntryResponse, error) {
	e, err := s.journalRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewJournalEntryResponse(e), nil
}

func (s *journalService) GetJournalEntries(ctx context.Context, filter *journalRepo.Filter) (*dto.ListJournalEntriesResponse, error) {
	if filter == nil {
		filter = journalRepo.NewFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	entries, err := s.journalRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count := len(entries)
	if !filter.IsUnlimited() {
		count, err = s.journalRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListJournalEntriesResponse{
		Items:      lo.Map(entries, func(e *domainJournal.Entry, _ int) *dto.JournalEntryResponse { return dto.NewJournalEntryResponse(e) }),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// sourceContext is the source's timezone (subscription, else customer) and contract ID, empty without a subscription.
func (s *journalService) sourceContext(ctx context.Context, customerID string, subscriptionID *string) (*time.Location, string, error) {
	if subscriptionID == nil {
		c, err := s.customerRepo.Get(ctx, customerID)
		if err != nil {
			return nil, "", err
		}
		return types.LoadTimezone(c.Timezone), "", nil
	}

	sub, err := s.subRepo.Get(ctx, *subscriptionID)
	if err != nil {
		return nil, "", err
	}
	c, err := s.contractRepo.GetBySubscriptionID(ctx, sub.ID)
	if err != nil {
		return nil, "", err
	}

	return types.LoadTimezone(sub.Timezone), c.ID(), nil
}

// post saves the entry with its period and, for billing events, its idempotency key, and returns its ID (the existing
// one when already posted; "" when there are no lines).
// e.g. invoice_finalized of inv_1 on 2026-10-17 posts once to October as invoice_finalized:inv_1.
func (s *journalService) post(ctx context.Context, e *domainJournal.Entry) (string, error) {
	if len(e.Lines()) == 0 {
		return "", nil
	}

	period, err := s.periodService.PeriodFor(ctx, e.BusinessDate())
	if err != nil {
		return "", err
	}

	b := domainJournal.NewBuilder(e).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_ENTRY)).
		WithAccountingPeriodID(period.ID()).
		WithLines(lo.Map(e.Lines(), func(l *domainJournal.Line, _ int) *domainJournal.Line {
			return domainJournal.NewLineBuilder(l).WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_LINE)).Build()
		})).
		WithCreatedAt(time.Now().UTC())
	if e.EventType() != types.JournalEventRevenueEarned && e.EventType() != types.JournalEventRevenueOffset {
		b = b.WithIdempotencyKey(string(e.EventType()) + ":" + e.SourceEntityID())
	}

	entry := b.Build()
	err = s.journalRepo.CreateWithLines(ctx, entry)
	if ierr.IsAlreadyExists(err) {
		return s.journalRepo.GetIDByIdempotencyKey(ctx, entry.IdempotencyKey())
	}
	if err != nil {
		return "", err
	}

	return entry.ID(), nil
}

// reversed negates every posting.
func reversed(postings []posting) []posting {
	return lo.Map(postings, func(p posting, _ int) posting {
		return posting{account: p.account, amount: p.amount.Neg(), pobID: p.pobID}
	})
}

// toLines turns postings into lines, dropping zeros.
func toLines(postings []posting) []*domainJournal.Line {
	var lines []*domainJournal.Line
	for _, p := range postings {
		if p.amount.IsZero() {
			continue
		}

		lines = append(lines, domainJournal.NewLineBuilder(nil).
			WithAccount(p.account).
			WithDirection(lo.Ternary(p.amount.IsPositive(), types.JournalDirectionCredit, types.JournalDirectionDebit)).
			WithAmount(p.amount.Abs()).
			WithPOBID(p.pobID).
			Build())
	}

	return lines
}

// localDate is t's date in loc at midnight UTC.
func localDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
