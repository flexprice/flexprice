package journal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	"github.com/flexprice/flexprice/internal/ee/service/recognition"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
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
	// Recognize posts, for each ended month of the contract's active and satisfied POBs, the recognition and offset
	// the journal is missing, and returns them; a rerun posts nothing. A dry run also counts the open month up to AsOf
	// and saves nothing.
	Recognize(ctx context.Context, req RecognizeRequest) ([]*domainJournal.Entry, error)
	// RunRecognition posts missed billing and recognizes the changed contracts of every environment with revenue
	// recognition on; failures are collected per environment and contract.
	RunRecognition(ctx context.Context, req RecognitionRunRequest) error
	// Backfill rebuilds the ctx environment from history: contracts and POBs for every subscription, all billing, then
	// recognition of every contract; safe to rerun, it only adds what is missing or wrong.
	Backfill(ctx context.Context, req BackfillRequest) error
	// Reconcile posts what hooks missed for every billing source changed since `since`; errors are collected per source.
	Reconcile(ctx context.Context, since time.Time) error
	GetJournalEntry(ctx context.Context, id string) (*dto.JournalEntryResponse, error)
	GetJournalEntries(ctx context.Context, filter *journalRepo.Filter) (*dto.ListJournalEntriesResponse, error)
}

type journalService struct {
	db              postgres.IClient
	journalRepo     journalRepo.Repository
	periodService   accountingperiodsvc.AccountingPeriodService
	pobService      pobsvc.PerformanceObligationService
	contracts       recognition.ContractRecognizer
	invoices        recognition.InvoiceRecognizer
	contractService contractsvc.ContractService
	contractRepo    contractRepo.Repository
	subRepo         subscription.Repository
	customerRepo    customer.Repository
	invoiceRepo     invoice.Repository
	creditNoteRepo  creditnote.Repository
	paymentRepo     payment.Repository
	refundRepo      refund.Repository
	taxAppliedRepo  taxapplied.Repository
	settingsRepo    settings.Repository
}

// RecognitionCursor is the last contract a recognition run finished; a retry resumes after it.
type RecognitionCursor struct {
	EnvironmentID string `json:"environment_id"`
	ContractID    string `json:"contract_id"`
}

// RecognizeRequest recognizes ContractID as of AsOf; DryRun returns the missing entries without the lock or saving.
type RecognizeRequest struct {
	ContractID string
	AsOf       time.Time
	DryRun     bool
}

// BackfillRequest recognizes as of AsOf, resuming after Resume when it is in the ctx environment; OnProgress receives
// the cursor after each contract.
type BackfillRequest struct {
	AsOf       time.Time
	Resume     *RecognitionCursor
	OnProgress func(RecognitionCursor)
}

// RecognitionRunRequest is one run: billing changed since Since is posted, contracts are recognized as of AsOf, and
// OnProgress receives the cursor after each contract.
type RecognitionRunRequest struct {
	Since time.Time
	AsOf  time.Time
	// TenantID and EnvironmentID, when set, limit the run to them; empty runs every enabled environment.
	TenantID      string
	EnvironmentID string
	Resume        *RecognitionCursor
	OnProgress    func(RecognitionCursor)
}

func NewJournalService(
	db postgres.IClient,
	journalRepo journalRepo.Repository,
	periodService accountingperiodsvc.AccountingPeriodService,
	pobService pobsvc.PerformanceObligationService,
	contracts recognition.ContractRecognizer,
	invoices recognition.InvoiceRecognizer,
	contractService contractsvc.ContractService,
	contractRepo contractRepo.Repository,
	subRepo subscription.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	creditNoteRepo creditnote.Repository,
	paymentRepo payment.Repository,
	refundRepo refund.Repository,
	taxAppliedRepo taxapplied.Repository,
	settingsRepo settings.Repository,
) JournalService {
	return &journalService{
		db:              db,
		journalRepo:     journalRepo,
		periodService:   periodService,
		pobService:      pobService,
		contracts:       contracts,
		invoices:        invoices,
		contractService: contractService,
		contractRepo:    contractRepo,
		subRepo:         subRepo,
		customerRepo:    customerRepo,
		invoiceRepo:     invoiceRepo,
		creditNoteRepo:  creditNoteRepo,
		paymentRepo:     paymentRepo,
		refundRepo:      refundRepo,
		taxAppliedRepo:  taxAppliedRepo,
		settingsRepo:    settingsRepo,
	}
}

// e.g. 100 + 18 tax with 20 credits: Dr AR 98, Dr customer_credit 20 / Cr deferred 100, Cr tax 18; void Cr AR 118.
func (s *journalService) PostInvoice(ctx context.Context, inv *invoice.Invoice) error {
	if inv.FinalizedAt == nil {
		return nil
	}

	loc, contractID, err := s.sourceContext(ctx, inv.CustomerID, inv.SubscriptionID)
	if err != nil {
		return err
	}

	revenuePostings, err := s.invoiceRevenuePostings(ctx, inv, s.pobFor(contractID))
	if err != nil {
		return err
	}

	entry := func(eventType types.JournalEventType, at time.Time, postings []domainJournal.Posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeInvoice, inv.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(utils.LocalDay(at, loc)).
			WithCustomerID(inv.CustomerID).
			WithContractID(contractID).
			WithCurrency(inv.Currency).
			WithLines(domainJournal.LinesFrom(postings)).
			Build()
	}

	finalizePostings := append([]domainJournal.Posting{
		{Account: types.LedgerAccountReceivable, Amount: inv.Total.Neg()},
		{Account: types.LedgerAccountCustomerCredit, Amount: inv.TotalPrepaidCreditsApplied.Neg()},
	}, revenuePostings...)

	finalizeID, err := s.post(ctx, entry(types.JournalEventInvoiceFinalized, lo.FromPtrOr(inv.IssueDate, *inv.FinalizedAt), finalizePostings, ""))
	if err != nil {
		return err
	}

	if inv.InvoiceStatus != types.InvoiceStatusVoided || inv.VoidedAt == nil {
		return nil
	}

	void := append([]domainJournal.Posting{
		{Account: types.LedgerAccountReceivable, Amount: inv.Total.Add(inv.TotalPrepaidCreditsApplied)},
	}, domainJournal.Reversed(revenuePostings)...)

	_, err = s.post(ctx, entry(types.JournalEventInvoiceVoided, *inv.VoidedAt, void, finalizeID))
	return err
}

// invoiceRevenuePostings credits tax and each line's net: subscription lines to deferred with the POB pobFor finds,
// top-ups to customer credit, else revenue.
func (s *journalService) invoiceRevenuePostings(ctx context.Context, inv *invoice.Invoice, pobFor pobFinder) ([]domainJournal.Posting, error) {
	nets, err := s.lineNets(ctx, inv)
	if err != nil {
		return nil, err
	}

	postings := []domainJournal.Posting{{Account: types.LedgerAccountTaxPayable, Amount: inv.TotalTax}}
	for _, li := range inv.LineItems {
		p := domainJournal.Posting{Account: types.LedgerAccountRevenue, Amount: nets[li.ID]}

		switch {
		case inv.Metadata["wallet_transaction_id"] != "":
			p.Account = types.LedgerAccountCustomerCredit
		case lo.FromPtr(li.SubscriptionLineItemID) != "":
			pobID, err := pobFor(ctx, li)
			if err != nil {
				return nil, err
			}

			p.Account, p.POBID = types.LedgerAccountDeferredRevenue, pobID
		}

		postings = append(postings, p)
	}

	return postings, nil
}

// lineNets is each invoice line's net of discounts and tax, by line ID.
func (s *journalService) lineNets(ctx context.Context, inv *invoice.Invoice) (map[string]decimal.Decimal, error) {
	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeInvoice
	taxFilter.EntityIDs = []string{inv.ID}
	taxes, err := s.taxAppliedRepo.List(ctx, taxFilter)
	if err != nil {
		return nil, err
	}
	return inv.LineNets(taxes), nil
}

// pobFinder finds the POB a subscription invoice line credits.
type pobFinder func(ctx context.Context, li *invoice.InvoiceLineItem) (string, error)

// pobFor is how a source's lines find their POB: the contract's POB for the line's price, none for a standalone
// subscription's lines.
func (s *journalService) pobFor(contractID string) pobFinder {
	if contractID == "" {
		return func(context.Context, *invoice.InvoiceLineItem) (string, error) { return "", nil }
	}
	return func(ctx context.Context, li *invoice.InvoiceLineItem) (string, error) {
		return s.pobService.POBIDForInvoiceLine(ctx, contractID, li)
	}
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

	loc, contractID, err := s.sourceContext(ctx, cn.CustomerID, cn.SubscriptionID)
	if err != nil {
		return err
	}

	contraPostings, err := s.creditNoteContraPostings(ctx, cn, cn.TotalAmount.Sub(tax), s.pobFor(contractID))
	if err != nil {
		return err
	}

	entry := func(eventType types.JournalEventType, at time.Time, postings []domainJournal.Posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeCreditNote, cn.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(utils.LocalDay(at, loc)).
			WithCustomerID(cn.CustomerID).
			WithContractID(contractID).
			WithCurrency(cn.Currency).
			WithLines(domainJournal.LinesFrom(postings)).
			Build()
	}

	finalizePostings := append([]domainJournal.Posting{
		{Account: types.LedgerAccountReceivable, Amount: cn.TotalAmount},
		{Account: types.LedgerAccountTaxPayable, Amount: tax.Neg()},
	}, contraPostings...)

	finalizeID, err := s.post(ctx, entry(types.JournalEventCreditNoteFinalized, *cn.FinalizedAt, finalizePostings, ""))
	if err != nil {
		return err
	}

	if cn.CreditNoteStatus != types.CreditNoteStatusVoided || cn.VoidedAt == nil {
		return nil
	}

	_, err = s.post(ctx, entry(types.JournalEventCreditNoteVoided, *cn.VoidedAt, domainJournal.Reversed(finalizePostings), finalizeID))
	return err
}

// creditNoteContraPostings debits contra revenue per line, with the POB pobFor finds, splitting net pro rata by line
// amount.
// e.g. lines 60 and 40 with net 88 → 52.80 and 35.20.
func (s *journalService) creditNoteContraPostings(ctx context.Context, cn *creditnote.CreditNote, net decimal.Decimal, pobFor pobFinder) ([]domainJournal.Posting, error) {
	inv, err := s.invoiceRepo.Get(ctx, cn.InvoiceID)
	if err != nil {
		return nil, err
	}
	invoiceLines := lo.KeyBy(inv.LineItems, func(li *invoice.InvoiceLineItem) string { return li.ID })

	amounts := utils.AllocateProRata(lo.SliceToMap(cn.LineItems, func(li *creditnote.CreditNoteLineItem) (string, decimal.Decimal) {
		return li.ID, li.Amount
	}), net, types.GetCurrencyPrecision(cn.Currency))

	postings := make([]domainJournal.Posting, 0, len(cn.LineItems))
	for _, li := range cn.LineItems {
		pobID, err := pobFor(ctx, invoiceLines[li.InvoiceLineItemID])
		if err != nil {
			return nil, err
		}
		postings = append(postings, domainJournal.Posting{Account: types.LedgerAccountContraRevenue, Amount: amounts[li.ID].Neg(), POBID: pobID})
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

	entry := func(eventType types.JournalEventType, at time.Time, postings []domainJournal.Posting, reversalOf string) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypePayment, p.ID).
			WithReversalOf(reversalOf).
			WithBusinessDate(utils.LocalDay(at, loc)).
			WithCustomerID(inv.CustomerID).
			WithContractID(contractID).
			WithCurrency(p.Currency).
			WithLines(domainJournal.LinesFrom(postings)).
			Build()
	}

	debit := lo.Ternary(p.PaymentMethodType == types.PaymentMethodTypeCredits, types.LedgerAccountCustomerCredit, types.LedgerAccountCash)
	succeededPostings := []domainJournal.Posting{
		{Account: debit, Amount: p.Amount.Neg()},
		{Account: types.LedgerAccountReceivable, Amount: p.Amount},
	}

	succeededID, err := s.post(ctx, entry(types.JournalEventPaymentSucceeded, *p.SucceededAt, succeededPostings, ""))
	if err != nil {
		return err
	}

	reversedAt := lo.CoalesceOrEmpty(p.RefundedAt, p.VoidedAt)
	if (p.PaymentStatus != types.PaymentStatusRefunded && p.PaymentStatus != types.PaymentStatusVoided) || reversedAt == nil {
		return nil
	}

	_, err = s.post(ctx, entry(types.JournalEventPaymentReversed, *reversedAt, domainJournal.Reversed(succeededPostings), succeededID))
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
	postings := []domainJournal.Posting{
		{Account: types.LedgerAccountReceivable, Amount: r.SettledAmount.Neg()},
		{Account: credit, Amount: r.SettledAmount},
	}

	_, err = s.post(ctx, domainJournal.NewBuilder(nil).
		WithEventType(types.JournalEventRefundSucceeded).
		WithSource(types.RevRecEntityTypeRefund, r.ID).
		WithBusinessDate(utils.LocalDay(*r.SucceededAt, loc)).
		WithCustomerID(inv.CustomerID).
		WithContractID(contractID).
		WithCurrency(r.Currency).
		WithLines(domainJournal.LinesFrom(postings)).
		Build())
	return err
}

// e.g. a $1,200 annual prepaid from Jan 1, run Feb 1: Dr unbilled 100 / Cr revenue 100 and Dr deferred 100 / Cr unbilled
// 100, dated Jan 31; a backdated cut of January to 90 later posts −10 and −10 to January.
func (s *journalService) Recognize(ctx context.Context, req RecognizeRequest) ([]*domainJournal.Entry, error) {
	if req.DryRun {
		return s.contracts.Recognize(ctx, req.ContractID, req.AsOf, true)
	}

	return s.recognize(ctx, s.contracts, req.ContractID, req.AsOf)
}

// recognize posts the entries the unit is missing as of asOf under its lock, and returns them.
func (s *journalService) recognize(ctx context.Context, r recognition.Recognizer, id string, asOf time.Time) ([]*domainJournal.Entry, error) {
	var missing []*domainJournal.Entry
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.db.LockWithWait(ctx, postgres.LockRequest{Key: r.LockKey(id)}); err != nil {
			return err
		}
		var err error
		if missing, err = r.Recognize(ctx, id, asOf, false); err != nil {
			return err
		}
		for _, e := range missing {
			if _, err := s.post(ctx, e); err != nil {
				return err
			}
		}
		return nil
	})
	return missing, err
}

// e.g. a retry resuming after contract_7 of env_2 skips env_1 and env_2's contracts up to contract_7.
func (s *journalService) RunRecognition(ctx context.Context, req RecognitionRunRequest) error {
	configs, err := s.settingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	if err != nil {
		return err
	}
	envs := lo.Filter(configs, func(c *types.TenantEnvConfig, _ int) bool {
		cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](c.Config)
		return err == nil && cfg.Enabled && c.TenantID != "" && c.EnvironmentID != "" &&
			(req.TenantID == "" || c.TenantID == req.TenantID) && (req.EnvironmentID == "" || c.EnvironmentID == req.EnvironmentID)
	})
	// Stable order, so a cursor means the same thing on a retry.
	sort.Slice(envs, func(i, j int) bool {
		return envs[i].TenantID+envs[i].EnvironmentID < envs[j].TenantID+envs[j].EnvironmentID
	})

	// A cursor whose environment is no longer enabled restarts from the top.
	resume := req.Resume
	if resume != nil && !lo.ContainsBy(envs, func(c *types.TenantEnvConfig) bool { return c.EnvironmentID == resume.EnvironmentID }) {
		resume = nil
	}

	var errs []error
	for _, env := range envs {
		after := ""
		if resume != nil {
			if env.EnvironmentID != resume.EnvironmentID {
				continue
			}
			after, resume = resume.ContractID, nil
		}

		envCtx := types.SetEnvironmentID(types.SetTenantID(ctx, env.TenantID), env.EnvironmentID)
		errs = append(errs, sourceError("environment", env.EnvironmentID, s.recognizeEnvironment(envCtx, req, after)))
	}

	return errors.Join(errs...)
}

// e.g. a subscription created before enabling gets its contract and POBs, its 2025 invoices post to their 2025 months,
// and its months are recognized from its start; a rerun posts nothing.
func (s *journalService) Backfill(ctx context.Context, req BackfillRequest) error {
	after := ""
	if req.Resume != nil && req.Resume.EnvironmentID == types.GetEnvironmentID(ctx) {
		after = req.Resume.ContractID
	}
	unchanged := RecognitionCursor{EnvironmentID: types.GetEnvironmentID(ctx), ContractID: after}

	syncErr := postChanged(func(page *types.QueryFilter) ([]*subscription.Subscription, error) {
		f := types.NewNoLimitSubscriptionFilter()
		f.QueryFilter, f.SubscriptionStatus = page, types.AllSubscriptionStatuses()
		return s.subRepo.List(ctx, f)
	}, func(listed *subscription.Subscription) error {
		defer req.OnProgress(unchanged)
		sub, _, err := s.subRepo.GetWithLineItems(ctx, listed.ID)
		if err != nil {
			return sourceError("subscription", listed.ID, err)
		}

		c, err := s.contractService.SyncForSubscription(ctx, sub)
		if err == nil && c != nil {
			_, err = s.pobService.SyncForContract(ctx, c)
		}

		return sourceError("subscription", sub.ID, err)
	})

	return errors.Join(syncErr, s.recognizeEnvironment(ctx, RecognitionRunRequest{AsOf: req.AsOf, OnProgress: req.OnProgress}, after))
}

// recognizeEnvironment repairs the POBs of each due contract with an ID after `after`, posts missed billing (which needs
// those POBs), then recognizes each contract and each due standalone invoice.
func (s *journalService) recognizeEnvironment(ctx context.Context, req RecognitionRunRequest, after string) error {
	contractIDs, err := s.contracts.Due(ctx, req.Since, req.AsOf)
	if err != nil {
		return err
	}
	contractIDs = lo.Filter(contractIDs, func(id string, _ int) bool { return id > after })

	// The repair pass reports the unchanged cursor: it shows progress without moving the resume point.
	unchanged := RecognitionCursor{EnvironmentID: types.GetEnvironmentID(ctx), ContractID: after}
	var errs []error
	for _, id := range contractIDs {
		errs = append(errs, sourceError("contract", id, s.pobService.Reconcile(ctx, id)))
		req.OnProgress(unchanged)
	}
	errs = append(errs, s.Reconcile(ctx, req.Since))

	for _, id := range contractIDs {
		_, err := s.recognize(ctx, s.contracts, id, req.AsOf)
		errs = append(errs, sourceError("contract", id, err))
		req.OnProgress(RecognitionCursor{EnvironmentID: types.GetEnvironmentID(ctx), ContractID: id})
	}

	invoiceIDs, err := s.invoices.Due(ctx, req.Since, req.AsOf)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, id := range invoiceIDs {
		_, err := s.recognize(ctx, s.invoices, id, req.AsOf)
		errs = append(errs, sourceError("invoice", id, err))
	}

	return errors.Join(errs...)
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

// sourceContext is the source's timezone (subscription, else customer) and contract ID, empty without a contract.
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
	// A standalone subscription has no contract.
	c, err := s.contractRepo.GetBySubscriptionID(ctx, sub.ID)
	if err != nil && !ierr.IsNotFound(err) {
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
		WithCreatedAt(clock.Now())
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
