package journal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/payment"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
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
	pobRepo         pobRepo.Repository
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
	Since      time.Time
	AsOf       time.Time
	Resume     *RecognitionCursor
	OnProgress func(RecognitionCursor)
}

func NewJournalService(
	db postgres.IClient,
	journalRepo journalRepo.Repository,
	periodService accountingperiodsvc.AccountingPeriodService,
	pobService pobsvc.PerformanceObligationService,
	pobRepo pobRepo.Repository,
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
		pobRepo:         pobRepo,
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
			WithBusinessDate(utils.LocalDay(at, loc)).
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
			WithBusinessDate(utils.LocalDay(at, loc)).
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
			WithBusinessDate(utils.LocalDay(at, loc)).
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
		WithBusinessDate(utils.LocalDay(*r.SucceededAt, loc)).
		WithCustomerID(inv.CustomerID).
		WithContractID(contractID).
		WithCurrency(r.Currency).
		WithLines(toLines(postings)).
		Build())
	return err
}

// e.g. a $1,200 annual prepaid from Jan 1, run Feb 1: Dr unbilled 100 / Cr revenue 100 and Dr deferred 100 / Cr unbilled
// 100, dated Jan 31; a backdated cut of January to 90 later posts −10 and −10 to January.
func (s *journalService) Recognize(ctx context.Context, req RecognizeRequest) ([]*domainJournal.Entry, error) {
	contractID := req.ContractID
	c, err := s.contractRepo.GetByID(ctx, contractID)
	if err != nil {
		return nil, err
	}
	pobs, err := s.pobRepo.List(ctx, &pobRepo.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		ContractIDs: []string{contractID},
		POBStatuses: []types.PerformanceObligationStatus{types.PerformanceObligationStatusActive, types.PerformanceObligationStatusSatisfied},
	})
	if err != nil {
		return nil, err
	}
	if len(pobs) == 0 {
		return nil, nil
	}

	sources, err := s.contractRepo.ListAssociations(ctx, contractID)
	if err != nil {
		return nil, err
	}
	sub, hasSub := lo.Find(sources, func(a *domainContract.Association) bool { return a.EntityType() == types.RevRecEntityTypeSubscription })
	loc, _, err := s.sourceContext(ctx, c.CustomerID(), lo.Ternary(hasSub, lo.ToPtr(sub.EntityID()), nil))
	if err != nil {
		return nil, err
	}

	// e.g. a POB from Jan 15 run on Apr 2 → Feb 1, Mar 1, Apr 1: the ends of January, February and March
	first := utils.LocalDay(lo.Earliest(lo.Map(pobs, func(p *domainPOB.PerformanceObligation, _ int) time.Time { return p.ServiceStart() })...), loc)
	today := utils.LocalDay(req.AsOf, loc)
	// Months ended in the customer's local time, not UTC: each month's start is the previous month's end.
	monthEnds := lo.Drop(utils.Months(first, today), 1)

	// A dry run ends the open month at today, e.g. Feb 15 → Feb 1–14; on the 1st the open month has no days yet.
	if req.DryRun && today.Day() != 1 && today.After(first) {
		monthEnds = append(monthEnds, today)
	}
	if len(monthEnds) == 0 {
		return nil, nil
	}

	// EarnedToDate reads local days, so each month end is local midnight.
	earned, err := s.pobService.EarnedToDate(ctx, contractID, lo.Map(monthEnds, func(m time.Time, _ int) time.Time {
		return time.Date(m.Year(), m.Month(), m.Day(), 0, 0, 0, 0, loc)
	}))
	if err != nil {
		return nil, err
	}

	precision := types.GetCurrencyPrecision(c.Currency())
	var recognized []*domainJournal.Entry
	var errs []error
	for _, p := range pobs {
		recognizePOB := func(ctx context.Context) error {
			entries, err := s.journalRepo.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), POBIDs: []string{p.ID()}})
			if err != nil {
				return err
			}

			entry := func(eventType types.JournalEventType, lastDay time.Time, postings []posting) *domainJournal.Entry {
				return domainJournal.NewBuilder(nil).
					WithEventType(eventType).
					WithSource(types.RevRecEntityTypePOB, p.ID()).
					WithBusinessDate(lastDay).
					WithCustomerID(c.CustomerID()).
					WithContractID(c.ID()).
					WithCurrency(c.Currency()).
					WithLines(toLines(postings)).
					Build()
			}

			for i, monthEnd := range monthEnds {
				lastDay := monthEnd.AddDate(0, 0, -1)
				pos := positionAt(entries, p.ID(), lastDay)
				toRecognize, toOffset := computeMonth(pos, earned[i][p.ID()].Round(precision))

				// Zero amounts leave no lines, so post skips them; posted entries join the in-memory totals.
				// e.g. arrears Feb 28: [revenue_earned Dr unbilled 250, revenue_offset Dr deferred 300 / Cr unbilled 300]
				month := []*domainJournal.Entry{
					entry(types.JournalEventRevenueEarned, lastDay, []posting{
						{account: types.LedgerAccountUnbilledReceivable, amount: toRecognize.Neg(), pobID: p.ID()},
						{account: types.LedgerAccountRevenue, amount: toRecognize, pobID: p.ID()},
					}),
					entry(types.JournalEventRevenueOffset, lastDay, []posting{
						{account: types.LedgerAccountDeferredRevenue, amount: toOffset.Neg(), pobID: p.ID()},
						{account: types.LedgerAccountUnbilledReceivable, amount: toOffset, pobID: p.ID()},
					}),
				}
				// Billed ahead and fully earned with nothing in unbilled: unbilled would rise and fall by the same amount, so
				// post one entry instead. e.g. prepaid Jan 31: revenue_earned Dr deferred 100 / Cr revenue 100
				if toOffset.Equal(toRecognize) && pos.revenue.Equal(pos.offset) {
					month = []*domainJournal.Entry{
						entry(types.JournalEventRevenueEarned, lastDay, []posting{
							{account: types.LedgerAccountDeferredRevenue, amount: toRecognize.Neg(), pobID: p.ID()},
							{account: types.LedgerAccountRevenue, amount: toRecognize, pobID: p.ID()},
						}),
					}
				}

				if !req.DryRun {
					for _, e := range month {
						if _, err := s.post(ctx, e); err != nil {
							return err
						}
					}
				}
				entries = append(entries, month...)
				recognized = append(recognized, lo.Filter(month, func(e *domainJournal.Entry, _ int) bool { return len(e.Lines()) > 0 })...)
			}
			return nil
		}

		if req.DryRun {
			errs = append(errs, sourceError("pob", p.ID(), recognizePOB(ctx)))
			continue
		}

		err := s.db.WithTx(ctx, func(txCtx context.Context) error {
			if err := s.db.LockWithWait(txCtx, postgres.LockRequest{Key: "revrec:pob:" + p.ID()}); err != nil {
				return err
			}
			return recognizePOB(txCtx)
		})
		errs = append(errs, sourceError("pob", p.ID(), err))
	}

	return recognized, errors.Join(errs...)
}

// e.g. a retry resuming after contract_7 of env_2 skips env_1 and env_2's contracts up to contract_7.
func (s *journalService) RunRecognition(ctx context.Context, req RecognitionRunRequest) error {
	configs, err := s.settingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	if err != nil {
		return err
	}
	envs := lo.Filter(configs, func(c *types.TenantEnvConfig, _ int) bool {
		cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](c.Config)
		return err == nil && cfg.Enabled && c.TenantID != "" && c.EnvironmentID != ""
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
		f.QueryFilter = page
		return s.subRepo.List(ctx, f)
	}, func(listed *subscription.Subscription) error {
		defer req.OnProgress(unchanged)
		sub, _, err := s.subRepo.GetWithLineItems(ctx, listed.ID)
		if err != nil {
			return sourceError("subscription", listed.ID, err)
		}

		c, err := s.contractService.SyncForSubscription(ctx, sub)
		if err == nil {
			_, err = s.pobService.SyncForSubscription(ctx, sub, c)
		}

		return sourceError("subscription", sub.ID, err)
	})

	return errors.Join(syncErr, s.recognizeEnvironment(ctx, RecognitionRunRequest{AsOf: req.AsOf, OnProgress: req.OnProgress}, after))
}

// recognizeEnvironment repairs the POBs of each selected contract with an ID after `after`, posts missed billing (which
// needs those POBs), then recognizes each contract.
func (s *journalService) recognizeEnvironment(ctx context.Context, req RecognitionRunRequest, after string) error {
	contractIDs, err := s.recognitionContracts(ctx, req.Since, req.AsOf)
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
		_, err := s.Recognize(ctx, RecognizeRequest{ContractID: id, AsOf: req.AsOf})
		errs = append(errs, sourceError("contract", id, err))
		req.OnProgress(RecognitionCursor{EnvironmentID: types.GetEnvironmentID(ctx), ContractID: id})
	}

	return errors.Join(errs...)
}

// recognitionContracts is, sorted by ID, every active contract, every contract ended in the last two months (it may
// still owe its last month), and every contract of a subscription with an invoice changed since `since`.
func (s *journalService) recognitionContracts(ctx context.Context, since, asOf time.Time) ([]string, error) {
	active, err := s.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractStatuses: []types.ContractStatus{types.ContractStatusActive},
	})
	if err != nil {
		return nil, err
	}
	ended, err := s.contractRepo.List(ctx, &contractRepo.Filter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ContractStatuses: []types.ContractStatus{types.ContractStatusEnded},
		EndedAtGTE:       lo.ToPtr(asOf.AddDate(0, -2, 0)),
	})
	if err != nil {
		return nil, err
	}

	invoiceFilter := types.NewNoLimitInvoiceFilter()
	invoiceFilter.UpdatedAtGTE = &since
	invoices, err := s.invoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return nil, err
	}
	subscriptionIDs := lo.Uniq(lo.FilterMap(invoices, func(inv *invoice.Invoice, _ int) (string, bool) {
		return lo.FromPtr(inv.SubscriptionID), lo.FromPtr(inv.SubscriptionID) != ""
	}))

	var billed []*domainContract.Contract
	if len(subscriptionIDs) > 0 {
		if billed, err = s.contractRepo.List(ctx, &contractRepo.Filter{
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
