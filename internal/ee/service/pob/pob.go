package pob

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// PerformanceObligationService keeps each subscription line item's performance obligation in step with the subscription.
type PerformanceObligationService interface {
	SyncForSubscription(ctx context.Context, sub *subscription.Subscription, c *domainContract.Contract) ([]*domainPOB.PerformanceObligation, error)
	// EarnedToDate is, for each asOf, what each of the contract's POBs earned before asOf's local day, keyed by POB ID,
	// with every POB of the contract present. It is the one place recognition and reports read earned revenue from.
	EarnedToDate(ctx context.Context, contractID string, asOfs []time.Time) ([]map[string]decimal.Decimal, error)
	Reconcile(ctx context.Context, contractID string) error
	// POBIDForInvoiceLine is the line's POB, empty without a subscription line item; NotFound before the sync.
	POBIDForInvoiceLine(ctx context.Context, li *invoice.InvoiceLineItem) (string, error)
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
}

func NewPerformanceObligationService(
	pobRepo pobRepo.Repository,
	contractRepo contractRepo.Repository,
	subRepo subscription.Repository,
	invoiceRepo invoice.Repository,
	taxAppliedRepo taxapplied.Repository,
	revenueFactRepo revenuefact.Repository,
) PerformanceObligationService {
	return &performanceObligationService{
		pobRepo:         pobRepo,
		contractRepo:    contractRepo,
		subRepo:         subRepo,
		invoiceRepo:     invoiceRepo,
		taxAppliedRepo:  taxAppliedRepo,
		revenueFactRepo: revenueFactRepo,
	}
}

// SyncForSubscription resolves a POB for each of the subscription's current line items and derives its service end
// and status, then marks satisfied any other active POB of the contract whose service end has passed; it is idempotent.
// sub.LineItems holds only line items open in or after the current period; one that ended earlier set its POB's
// service end while it was still current.
func (s *performanceObligationService) SyncForSubscription(ctx context.Context, sub *subscription.Subscription, c *domainContract.Contract) ([]*domainPOB.PerformanceObligation, error) {
	// A predecessor starts before its successor, so it is resolved first.
	lineItems := append([]*subscription.SubscriptionLineItem(nil), sub.LineItems...)
	sort.SliceStable(lineItems, func(i, j int) bool { return lineItems[i].StartDate.Before(lineItems[j].StartDate) })

	pobs := make(map[string]*domainPOB.PerformanceObligation)
	lineItemsByPOB := make(map[string][]*subscription.SubscriptionLineItem)
	for _, li := range lineItems {
		p, err := s.resolveForLineItem(ctx, li, c)
		if err != nil {
			return nil, err
		}
		pobs[p.ID()] = p
		lineItemsByPOB[p.ID()] = append(lineItemsByPOB[p.ID()], li)
	}

	now := time.Now().UTC()
	synced := make([]*domainPOB.PerformanceObligation, 0, len(pobs))
	for id, existing := range pobs {
		next := derivePOB(existing, lineItemsByPOB[id], c, now)
		if next.POBStatus() != existing.POBStatus() || !lo.FromPtr(next.ServiceEnd()).Equal(lo.FromPtr(existing.ServiceEnd())) {
			if err := s.pobRepo.Update(ctx, next); err != nil {
				return nil, err
			}
		}
		synced = append(synced, next)
	}

	satisfied, err := s.satisfyEnded(ctx, c, pobs, now)
	if err != nil {
		return nil, err
	}
	return append(synced, satisfied...), nil
}

// satisfyEnded marks satisfied the contract's active POBs outside touched whose service end has passed.
// e.g. a line item removed Mar 20 drops out of sub.LineItems at the Apr 1 renewal; its POB, ending Mar 20, is satisfied then.
func (s *performanceObligationService) satisfyEnded(ctx context.Context, c *domainContract.Contract, touched map[string]*domainPOB.PerformanceObligation, now time.Time) ([]*domainPOB.PerformanceObligation, error) {
	filter := &pobRepo.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		ContractIDs: []string{c.ID()},
		POBStatuses: []types.PerformanceObligationStatus{types.PerformanceObligationStatusActive},
	}
	active, err := s.pobRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	var satisfied []*domainPOB.PerformanceObligation
	for _, p := range active {
		if _, ok := touched[p.ID()]; ok || p.ServiceEnd() == nil || p.ServiceEnd().After(now) {
			continue
		}

		next := domainPOB.NewBuilder(p).WithPOBStatus(types.PerformanceObligationStatusSatisfied).Build()
		if err := s.pobRepo.Update(ctx, next); err != nil {
			return nil, err
		}
		satisfied = append(satisfied, next)
	}
	return satisfied, nil
}

// Reconcile re-runs SyncForSubscription for every subscription of the contract against the contract as stored; it is
// idempotent. Callers sync the contract first when its status may be stale.
func (s *performanceObligationService) Reconcile(ctx context.Context, contractID string) error {
	c, err := s.contractRepo.GetByID(ctx, contractID)
	if err != nil {
		return err
	}
	sources, err := s.contractRepo.ListAssociations(ctx, contractID)
	if err != nil {
		return err
	}

	for _, source := range sources {
		if source.EntityType() != types.RevRecEntityTypeSubscription {
			continue
		}

		sub, _, err := s.subRepo.GetWithLineItems(ctx, source.EntityID())
		if err != nil {
			return err
		}
		if _, err := s.SyncForSubscription(ctx, sub, c); err != nil {
			return err
		}
	}
	return nil
}

// GetPerformanceObligation returns the POB with its associations.
func (s *performanceObligationService) GetPerformanceObligation(ctx context.Context, id string) (*dto.PerformanceObligationResponse, error) {
	p, err := s.pobRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	associations, err := s.pobRepo.ListAssociations(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewPerformanceObligationResponse(p).WithAssociations(associations), nil
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

	lineItemPOBs := make(map[string]*domainPOB.PerformanceObligation)
	for _, p := range pobs {
		associations, err := s.pobRepo.ListAssociations(ctx, p.ID())
		if err != nil {
			return nil, err
		}
		for _, a := range associations {
			lineItemPOBs[a.EntityID()] = p
		}
	}
	if len(lineItemPOBs) == 0 || len(asOfs) == 0 {
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

		for i, asOf := range asOfs {
			for pobID, amount := range earnedByPOB(lineItemPOBs, inputs, asOf, types.LoadTimezone(sub.Timezone)) {
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

// earnedByPOB is what each POB earned before asOf's local day, keyed by POB ID. lineItemPOBs maps each subscription line
// item to its POB. Each finalized invoice line earns its LineNets amount over its own service period with the POB's
// methods, usage daily (a line with no period earns at finalization); proration lines simply add. A PROVISIONAL fact earns on its
// day (a period_only fact evenly over its period), unless a SUBSCRIPTION invoice line already covers that day.
// e.g. a $90 April line, a −$30 and a +$40 proration line over [Apr 21, May 1), and a $5 fact for May 2: by May 3 → 105.
func earnedByPOB(lineItemPOBs map[string]*domainPOB.PerformanceObligation, inputs *earningInputs, asOf time.Time, loc *time.Location) map[string]decimal.Decimal {
	earned := make(map[string]decimal.Decimal)
	// e.g. sli_1 → [Apr 1, May 1) once its April SUBSCRIPTION invoice line is finalized
	invoiced := make(map[string][]dayRange)

	for _, inv := range inputs.invoices {
		nets := inv.LineNets(inputs.taxes)
		for _, line := range inv.LineItems {
			p, ok := lineItemPOBs[lo.FromPtr(line.SubscriptionLineItemID)]
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
		p, ok := lineItemPOBs[lineItemID]
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

func (s *performanceObligationService) POBIDForInvoiceLine(ctx context.Context, li *invoice.InvoiceLineItem) (string, error) {
	if li == nil || lo.FromPtr(li.SubscriptionLineItemID) == "" {
		return "", nil
	}
	p, err := s.pobRepo.GetByLineItemID(ctx, *li.SubscriptionLineItemID)
	if err != nil {
		return "", err
	}
	return p.ID(), nil
}

// resolveForLineItem returns the line item's POB: its own, else its predecessor's when the method matches, else a new one.
func (s *performanceObligationService) resolveForLineItem(ctx context.Context, li *subscription.SubscriptionLineItem, c *domainContract.Contract) (*domainPOB.PerformanceObligation, error) {
	p, err := s.pobRepo.GetByLineItemID(ctx, li.ID)
	if !ierr.IsNotFound(err) {
		return p, err
	}

	association := domainPOB.NewAssociationBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_POB_ASSOCIATION)).
		WithEntityType(types.RevRecEntityTypeSubscriptionLineItem).
		WithEntityID(li.ID).
		WithStatus(types.StatusPublished).
		WithCreatedAt(time.Now().UTC()).
		Build()

	predecessor, err := s.predecessorPOB(ctx, li, c)
	if err != nil {
		return nil, err
	}
	if predecessor != nil {
		err = s.pobRepo.CreateAssociation(ctx, domainPOB.NewAssociationBuilder(association).WithPOBID(predecessor.ID()).Build())
	} else {
		err = s.pobRepo.CreateWithAssociation(ctx, newPOB(li, c), association)
	}
	// A concurrent sync linked the line item first; use its POB.
	if err != nil && !ierr.IsAlreadyExists(err) {
		return nil, err
	}
	return s.pobRepo.GetByLineItemID(ctx, li.ID)
}

// predecessorPOB returns the POB of the line item this one replaced, if it is in the same contract and earns the same way.
func (s *performanceObligationService) predecessorPOB(ctx context.Context, li *subscription.SubscriptionLineItem, c *domainContract.Contract) (*domainPOB.PerformanceObligation, error) {
	// Line items versioned before the lineage columns existed hold the pointer in metadata.
	predecessorID := lo.FromPtr(li.PredecessorLineItemID)
	if predecessorID == "" {
		predecessorID = li.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID]
	}
	if predecessorID == "" {
		return nil, nil
	}

	p, err := s.pobRepo.GetByLineItemID(ctx, predecessorID)
	if ierr.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if p.ContractID() != c.ID() || p.RecognitionMethod() != deriveRecognitionMethod(li) {
		return nil, nil
	}
	return p, nil
}

func newPOB(li *subscription.SubscriptionLineItem, c *domainContract.Contract) *domainPOB.PerformanceObligation {
	now := time.Now().UTC()
	return domainPOB.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PERFORMANCE_OBLIGATION)).
		WithContractID(c.ID()).
		WithName(lo.CoalesceOrEmpty(li.DisplayName, li.PlanDisplayName, li.PriceID)).
		WithRecognitionMethod(deriveRecognitionMethod(li)).
		WithAmortizationMethod(types.AmortizationPeriodEvenProrated).
		WithServiceStart(li.StartDate).
		WithPOBStatus(types.PerformanceObligationStatusDraft).
		WithStatus(types.StatusPublished).
		WithCreatedAt(now).
		WithUpdatedAt(now).
		Build()
}

// derivePOB returns the POB after applying its line items and the contract's status.
// Recognition and amortization methods are never changed after creation.
func derivePOB(existing *domainPOB.PerformanceObligation, lineItems []*subscription.SubscriptionLineItem, c *domainContract.Contract, now time.Time) *domainPOB.PerformanceObligation {
	serviceEnd := deriveServiceEnd(existing.RecognitionMethod(), lineItems, c)

	status := types.PerformanceObligationStatusActive
	switch {
	case c.ContractStatus() == types.ContractStatusDraft:
		status = types.PerformanceObligationStatusDraft
	case c.ContractStatus() == types.ContractStatusCancelled:
		status = types.PerformanceObligationStatusCancelled
	case serviceEnd != nil && !serviceEnd.After(now):
		status = types.PerformanceObligationStatusSatisfied
	}

	return domainPOB.NewBuilder(existing).WithServiceEnd(serviceEnd).WithPOBStatus(status).Build()
}

// deriveServiceEnd is the latest end of the POB's line items, or nil while any is still open.
// A point_in_time POB is delivered at its line items' start; an open line item of an ended contract ends with it.
// e.g. line items ending Mar 31 and Jun 30 → Jun 30; one with no end on a contract ended May 10 → May 10.
func deriveServiceEnd(method types.RecognitionMethod, lineItems []*subscription.SubscriptionLineItem, c *domainContract.Contract) *time.Time {
	var serviceEnd time.Time
	for _, li := range lineItems {
		end := li.EndDate
		switch {
		case method == types.RecognitionMethodPointInTime:
			end = li.StartDate
		case end.IsZero() && c.EndedAt() != nil:
			end = *c.EndedAt()
		case end.IsZero():
			return nil
		}
		if end.After(serviceEnd) {
			serviceEnd = end
		}
	}
	return &serviceEnd
}

// deriveRecognitionMethod maps a line item's price to how its POB earns revenue.
func deriveRecognitionMethod(li *subscription.SubscriptionLineItem) types.RecognitionMethod {
	switch {
	case li.PriceType == types.PRICE_TYPE_USAGE:
		return types.RecognitionMethodAsConsumed
	case li.IsOneTime():
		return types.RecognitionMethodPointInTime
	default:
		return types.RecognitionMethodStraightLine
	}
}
