package pob

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// PerformanceObligationService creates a contract's performance obligations and keeps their status in step with the contract.
type PerformanceObligationService interface {
	CreateForContract(ctx context.Context, c *domainContract.Contract, reqs []dto.CreatePOBRequest) ([]*domainPOB.PerformanceObligation, error)
	SyncForContract(ctx context.Context, c *domainContract.Contract) ([]*domainPOB.PerformanceObligation, error)
	// EarnedToDate is, for each asOf, what each of the contract's POBs earned before asOf's local day, keyed by POB ID,
	// with every POB of the contract present. It is the one place recognition and reports read earned revenue from.
	EarnedToDate(ctx context.Context, contractID string, asOfs []time.Time) ([]map[string]decimal.Decimal, error)
	Reconcile(ctx context.Context, contractID string) error
	// POBIDForInvoiceLine is the contract's POB for the line's price, empty without a subscription line item; NotFound
	// when the contract has none for it.
	POBIDForInvoiceLine(ctx context.Context, contractID string, li *invoice.InvoiceLineItem) (string, error)
	GetPerformanceObligation(ctx context.Context, id string) (*dto.PerformanceObligationResponse, error)
	GetPerformanceObligations(ctx context.Context, filter *pobRepo.Filter) (*dto.ListPerformanceObligationsResponse, error)
}

type performanceObligationService struct {
	pobRepo         pobRepo.Repository
	contractRepo    contractRepo.Repository
	subRepo         subscription.Repository
	invoiceRepo     invoice.Repository
	taxAppliedRepo  taxapplied.Repository
	revenueFactRepo revenuefact.Repository
	priceRepo       price.Repository
}

func NewPerformanceObligationService(
	pobRepo pobRepo.Repository,
	contractRepo contractRepo.Repository,
	subRepo subscription.Repository,
	invoiceRepo invoice.Repository,
	taxAppliedRepo taxapplied.Repository,
	revenueFactRepo revenuefact.Repository,
	priceRepo price.Repository,
) PerformanceObligationService {
	return &performanceObligationService{
		pobRepo:         pobRepo,
		contractRepo:    contractRepo,
		subRepo:         subRepo,
		invoiceRepo:     invoiceRepo,
		taxAppliedRepo:  taxAppliedRepo,
		revenueFactRepo: revenueFactRepo,
		priceRepo:       priceRepo,
	}
}

// CreateForContract creates the requested POBs of the contract, each with its status derived from the contract.
func (s *performanceObligationService) CreateForContract(ctx context.Context, c *domainContract.Contract, reqs []dto.CreatePOBRequest) ([]*domainPOB.PerformanceObligation, error) {
	now := clock.Now()
	pobs := make([]*domainPOB.PerformanceObligation, 0, len(reqs))
	for _, req := range reqs {
		p := domainPOB.NewBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PERFORMANCE_OBLIGATION)).
			WithContractID(c.ID()).
			WithProductID(req.ProductID).
			WithAllocatedAmount(req.AllocatedAmount).
			WithName(req.Name).
			WithRecognitionMethod(req.RecognitionMethod).
			WithAmortizationMethod(types.AmortizationPeriodEvenProrated).
			WithServiceStart(req.ServiceStart).
			WithServiceEnd(req.ServiceEnd).
			WithStatus(types.StatusPublished).
			WithCreatedAt(now).
			WithUpdatedAt(now).
			Build()
		p = derivePOB(p, c, now)
		if err := s.pobRepo.Create(ctx, p); err != nil {
			return nil, err
		}
		pobs = append(pobs, p)
	}
	return pobs, nil
}

// SyncForContract derives the status of each of the contract's POBs from the contract and the POB's service end; it is
// idempotent.
func (s *performanceObligationService) SyncForContract(ctx context.Context, c *domainContract.Contract) ([]*domainPOB.PerformanceObligation, error) {
	pobs, err := s.pobRepo.List(ctx, &pobRepo.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		ContractIDs: []string{c.ID()},
	})
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	synced := make([]*domainPOB.PerformanceObligation, 0, len(pobs))
	for _, existing := range pobs {
		next := derivePOB(existing, c, now)
		if next.POBStatus() != existing.POBStatus() {
			if err := s.pobRepo.Update(ctx, next); err != nil {
				return nil, err
			}
		}
		synced = append(synced, next)
	}
	return synced, nil
}

// Reconcile re-runs SyncForContract against the contract as stored; it is idempotent. Callers sync the contract first
// when its status may be stale.
func (s *performanceObligationService) Reconcile(ctx context.Context, contractID string) error {
	c, err := s.contractRepo.GetByID(ctx, contractID)
	if err != nil {
		return err
	}
	_, err = s.SyncForContract(ctx, c)
	return err
}

// GetPerformanceObligation returns the POB.
func (s *performanceObligationService) GetPerformanceObligation(ctx context.Context, id string) (*dto.PerformanceObligationResponse, error) {
	p, err := s.pobRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewPerformanceObligationResponse(p), nil
}

func (s *performanceObligationService) GetPerformanceObligations(ctx context.Context, filter *pobRepo.Filter) (*dto.ListPerformanceObligationsResponse, error) {
	if filter == nil {
		filter = pobRepo.NewFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	pobs, err := s.pobRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count := len(pobs)
	if !filter.IsUnlimited() {
		count, err = s.pobRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListPerformanceObligationsResponse{
		Items: lo.Map(pobs, func(p *domainPOB.PerformanceObligation, _ int) *dto.PerformanceObligationResponse {
			return dto.NewPerformanceObligationResponse(p)
		}),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// EarnedToDate earns every POB of the contract from its subscriptions' invoices and facts, one load per subscription
// for all asOfs.
func (s *performanceObligationService) EarnedToDate(ctx context.Context, contractID string, asOfs []time.Time) ([]map[string]decimal.Decimal, error) {
	pobs, err := s.pobRepo.List(ctx, &pobRepo.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		ContractIDs: []string{contractID},
	})
	if err != nil {
		return nil, err
	}

	earned := lo.Map(asOfs, func(time.Time, int) map[string]decimal.Decimal {
		return lo.SliceToMap(pobs, func(p *domainPOB.PerformanceObligation) (string, decimal.Decimal) { return p.ID(), decimal.Zero })
	})

	if len(pobs) == 0 || len(asOfs) == 0 {
		return earned, nil
	}

	sources, err := s.contractRepo.ListAssociations(ctx, contractID)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source.EntityType() != types.RevRecEntityTypeSubscription {
			continue
		}

		sub, err := s.subRepo.Get(ctx, source.EntityID())
		if err != nil {
			return nil, err
		}

		inputs, err := s.loadEarningInputs(ctx, sub, lo.Latest(asOfs...))
		if err != nil {
			return nil, err
		}
		pricePOBs, err := s.pobsByPrice(ctx, pobs, inputs.priceIDs())
		if err != nil {
			return nil, err
		}

		for i, asOf := range asOfs {
			for pobID, amount := range earnedByPOB(pricePOBs, inputs, asOf, types.LoadTimezone(sub.Timezone)) {
				earned[i][pobID] = earned[i][pobID].Add(amount)
			}
		}
	}

	return earned, nil
}

// earningInputs is what a subscription's POBs earn from: its finalized invoices, their applied taxes, and the
// PROVISIONAL revenue facts that accrue un-invoiced periods while revenue analytics is on.
type earningInputs struct {
	invoices []*invoice.Invoice
	taxes    []*taxapplied.TaxApplied
	facts    []*revenuefact.RevenueFact
}

// priceIDs is every price the inputs' invoice lines and facts bill.
func (in *earningInputs) priceIDs() []string {
	var ids []string
	for _, inv := range in.invoices {
		for _, line := range inv.LineItems {
			ids = append(ids, lo.FromPtr(line.PriceID))
		}
	}
	for _, fact := range in.facts {
		ids = append(ids, lo.FromPtr(fact.PriceID))
	}
	return ids
}

// loadEarningInputs reads the subscription's finalized invoices (voided and draft ones never earn) with all their lines,
// their taxes, and its PROVISIONAL facts up to asOf. With revenue analytics off there are no facts, so revenue is
// recognized only once invoiced.
func (s *performanceObligationService) loadEarningInputs(ctx context.Context, sub *subscription.Subscription, asOf time.Time) (*earningInputs, error) {
	invoiceFilter := types.NewNoLimitInvoiceFilter()
	invoiceFilter.SubscriptionID = sub.ID
	invoiceFilter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
	invoices, err := s.invoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return nil, err
	}

	var taxes []*taxapplied.TaxApplied
	if len(invoices) > 0 {
		taxFilter := types.NewNoLimitTaxAppliedFilter()
		taxFilter.EntityType = types.TaxRateEntityTypeInvoice
		taxFilter.EntityIDs = lo.Map(invoices, func(inv *invoice.Invoice, _ int) string { return inv.ID })
		if taxes, err = s.taxAppliedRepo.List(ctx, taxFilter); err != nil {
			return nil, err
		}
	}

	facts, err := s.revenueFactRepo.ListFacts(ctx, revenuefact.FactsFilter{
		DayEnd:          utils.LocalDay(asOf, types.LoadTimezone(sub.Timezone)),
		Status:          types.FactProvisional,
		SubscriptionIDs: []string{sub.ID},
	})
	if err != nil {
		return nil, err
	}

	return &earningInputs{invoices: invoices, taxes: taxes, facts: facts}, nil
}

// earnedByPOB is what each POB earned before asOf's local day, keyed by POB ID. pricePOBs maps each price to its POB.
// Each finalized invoice line earns its LineNets amount over its own service period with the POB's methods, usage daily
// (a line with no period earns at finalization); proration lines simply add. A PROVISIONAL fact earns on its day (a
// period_only fact evenly over its period), unless a SUBSCRIPTION invoice line already covers that day.
// e.g. a $90 April line, a −$30 and a +$40 proration line over [Apr 21, May 1), and a $5 fact for May 2: by May 3 → 105.
func earnedByPOB(pricePOBs map[string]*domainPOB.PerformanceObligation, inputs *earningInputs, asOf time.Time, loc *time.Location) map[string]decimal.Decimal {
	earned := make(map[string]decimal.Decimal)
	// e.g. sli_1 → [Apr 1, May 1) once its April SUBSCRIPTION invoice line is finalized
	invoiced := make(map[string][]dayRange)

	for _, inv := range inputs.invoices {
		nets := inv.LineNets(inputs.taxes)
		for _, line := range inv.LineItems {
			p, ok := pricePOBs[lo.FromPtr(line.PriceID)]
			if !ok {
				continue
			}

			// e.g. a one-time line with no period earns in full when the invoice is finalized
			start := lo.FromPtrOr(line.PeriodStart, lo.FromPtr(inv.FinalizedAt))
			end := lo.FromPtrOr(line.PeriodEnd, start)
			// Usage earns on the days its line covers.
			amortization := lo.Ternary(p.RecognitionMethod() == types.RecognitionMethodAsConsumed, types.AmortizationDaily, p.AmortizationMethod())
			earned[p.ID()] = earned[p.ID()].Add(Earned(nets[line.ID], start, end, p.RecognitionMethod(), amortization, asOf, loc))

			if inv.InvoiceType == types.InvoiceTypeSubscription {
				lineItemID := lo.FromPtr(line.SubscriptionLineItemID)
				invoiced[lineItemID] = append(invoiced[lineItemID], dayRange{start: utils.LocalDay(start, loc), end: utils.LocalDay(end, loc)})
			}
		}
	}

	// Fact days are already local dates stamped as UTC midnight.
	asOfDay := utils.LocalDay(asOf, loc)
	for _, fact := range inputs.facts {
		lineItemID := lo.FromPtr(fact.SubLineItemID)
		p, ok := pricePOBs[lo.FromPtr(fact.PriceID)]
		if !ok || lo.ContainsBy(invoiced[lineItemID], func(r dayRange) bool { return r.contains(fact.Day) }) {
			continue
		}

		// e.g. a marginal fact for May 2 earns on May 2; a period_only fact for [May 1, May 31] spreads over May
		start, end := fact.Day, fact.Day
		if fact.DecompositionMode == types.PeriodOnly {
			start, end = fact.PeriodStart, fact.PeriodEnd.AddDate(0, 0, 1)
		}
		earned[p.ID()] = earned[p.ID()].Add(Earned(fact.NetAmount, start, end, types.RecognitionMethodStraightLine, types.AmortizationDaily, asOfDay, time.UTC))
	}

	return earned
}

func (s *performanceObligationService) POBIDForInvoiceLine(ctx context.Context, contractID string, li *invoice.InvoiceLineItem) (string, error) {
	if li == nil || lo.FromPtr(li.SubscriptionLineItemID) == "" {
		return "", nil
	}
	pobs, err := s.pobRepo.List(ctx, &pobRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), ContractIDs: []string{contractID}})
	if err != nil {
		return "", err
	}
	pricePOBs, err := s.pobsByPrice(ctx, pobs, []string{lo.FromPtr(li.PriceID)})
	if err != nil {
		return "", err
	}
	p, ok := pricePOBs[lo.FromPtr(li.PriceID)]
	if !ok {
		return "", ierr.NewError("performance obligation not found").
			WithHint("The contract has no performance obligation for the line's product and recognition method").
			WithReportableDetails(map[string]any{"contract_id": contractID, "invoice_line_item_id": li.ID, "price_id": lo.FromPtr(li.PriceID)}).
			Mark(ierr.ErrNotFound)
	}
	return p.ID(), nil
}

// pobsByPrice maps each price to the POB for its Product and the recognition method it earns by; a price with no such
// POB is left out.
// e.g. a usage price of prod_api → the (prod_api, as_consumed) POB.
func (s *performanceObligationService) pobsByPrice(ctx context.Context, pobs []*domainPOB.PerformanceObligation, priceIDs []string) (map[string]*domainPOB.PerformanceObligation, error) {
	priceIDs = lo.Compact(lo.Uniq(priceIDs))
	if len(pobs) == 0 || len(priceIDs) == 0 {
		return map[string]*domainPOB.PerformanceObligation{}, nil
	}
	prices, err := s.priceRepo.ListByIDs(ctx, priceIDs)
	if err != nil {
		return nil, err
	}

	type key struct {
		productID string
		method    types.RecognitionMethod
	}
	byKey := lo.SliceToMap(pobs, func(p *domainPOB.PerformanceObligation) (key, *domainPOB.PerformanceObligation) {
		return key{productID: p.ProductID(), method: p.RecognitionMethod()}, p
	})

	pricePOBs := make(map[string]*domainPOB.PerformanceObligation, len(prices))
	for _, pr := range prices {
		if p, ok := byKey[key{productID: pr.ProductID, method: recognitionMethod(pr)}]; ok && pr.ProductID != "" {
			pricePOBs[pr.ID] = p
		}
	}
	return pricePOBs, nil
}

// recognitionMethod is how a price's charge earns: usage as consumed, a one-time charge at a point in time, else
// straight line.
func recognitionMethod(p *price.Price) types.RecognitionMethod {
	switch {
	case p.Type == types.PRICE_TYPE_USAGE:
		return types.RecognitionMethodAsConsumed
	case p.BillingPeriod == types.BILLING_PERIOD_ONETIME:
		return types.RecognitionMethodPointInTime
	default:
		return types.RecognitionMethodStraightLine
	}
}

// derivePOB returns the POB with its status derived from the contract and its service end.
// e.g. a draft contract's POB is a draft; an active contract's POB whose service end has passed is satisfied.
func derivePOB(existing *domainPOB.PerformanceObligation, c *domainContract.Contract, now time.Time) *domainPOB.PerformanceObligation {
	status := types.PerformanceObligationStatusActive
	switch {
	case c.ContractStatus() == types.ContractStatusDraft:
		status = types.PerformanceObligationStatusDraft
	case c.ContractStatus() == types.ContractStatusCancelled:
		status = types.PerformanceObligationStatusCancelled
	case c.ContractStatus() == types.ContractStatusEnded:
		status = types.PerformanceObligationStatusSatisfied
	case existing.ServiceEnd() != nil && !existing.ServiceEnd().After(now):
		status = types.PerformanceObligationStatusSatisfied
	}

	return domainPOB.NewBuilder(existing).WithPOBStatus(status).Build()
}
