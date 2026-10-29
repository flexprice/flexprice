package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const commitmentAnalyticsScanLimit = 500

type commitmentAnalyticsQuery struct {
	params      *events.MeterUsageDetailedAnalyticsParams
	customerIDs []string
	limit       int
	afterID     string
}

func newCommitmentAnalyticsQuery(params *events.MeterUsageDetailedAnalyticsParams, customerIDs []string, limit int, afterID string) *commitmentAnalyticsQuery {
	if params == nil {
		return nil
	}
	copied := *params
	copied.MeterIDs = slices.Clone(params.MeterIDs)
	copied.FeatureIDs = slices.Clone(params.FeatureIDs)
	if limit == 0 {
		limit = 50
	}
	return &commitmentAnalyticsQuery{params: &copied, customerIDs: slices.Clone(customerIDs), limit: limit, afterID: afterID}
}

type commitmentAnalyticsEntry struct {
	owner       *commitment.Commitment
	usage       *dto.CommitmentUsageBreakdown
	usageReason string
	collection  *domain.CollectionSummary
}

// Owner returns a detached commitment snapshot.
func (e *commitmentAnalyticsEntry) Owner() *commitment.Commitment {
	if e == nil || e.owner == nil {
		return nil
	}
	return commitment.NewCommitmentBuilder(e.owner).Build()
}

// Usage returns the query-window allocation, or nil when attribution is unavailable.
func (e *commitmentAnalyticsEntry) Usage() *dto.CommitmentUsageBreakdown {
	if e == nil || e.usage == nil {
		return nil
	}
	copied := *e.usage
	return &copied
}

// UsageUnavailableReason explains a missing allocation without inventing zero usage.
func (e *commitmentAnalyticsEntry) UsageUnavailableReason() string {
	if e == nil {
		return ""
	}
	return e.usageReason
}

// Collection returns lifetime principal independently of the usage window.
func (e *commitmentAnalyticsEntry) Collection() *domain.CollectionSummary {
	if e == nil || e.collection == nil {
		return nil
	}
	copied := *e.collection
	if copied.DeferredReason != nil {
		copied.DeferredReason = lo.ToPtr(*copied.DeferredReason)
	}
	if copied.NextBillOn != nil {
		copied.NextBillOn = lo.ToPtr(*copied.NextBillOn)
	}
	return &copied
}

type commitmentAnalyticsPage struct {
	entries []*commitmentAnalyticsEntry
	nextID  string
}

// Entries returns the bounded commitment page.
func (p *commitmentAnalyticsPage) Entries() []*commitmentAnalyticsEntry {
	if p == nil {
		return nil
	}
	return slices.Clone(p.entries)
}

// NextID is the exclusive continuation boundary; empty means traversal is complete.
func (p *commitmentAnalyticsPage) NextID() string {
	if p == nil {
		return ""
	}
	return p.nextID
}

// commitmentAnalytics joins a bounded owner page independently of whether usage produced rows.
func (s *meterUsageService) commitmentAnalytics(ctx context.Context, query *commitmentAnalyticsQuery) (*commitmentAnalyticsPage, error) {
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return nil, ierr.Validation("commitment analytics requires tenant and environment scope")
	}
	if query == nil || query.params == nil || !query.params.StartTime.Before(query.params.EndTime) || query.params.StartTime.IsZero() || query.params.EndTime.IsZero() {
		return nil, ierr.Validation("commitment analytics requires an ordered time range")
	}
	if query.params.TenantID != "" && query.params.TenantID != types.GetTenantID(ctx) || query.params.EnvironmentID != "" && query.params.EnvironmentID != types.GetEnvironmentID(ctx) {
		return nil, ierr.Validation("commitment analytics scope must match the request context")
	}
	if len(query.customerIDs) == 0 || len(query.customerIDs) > commitmentAnalyticsScanLimit || query.limit < 1 || query.limit > 100 || len(query.afterID) > 50 {
		return nil, ierr.Validation("commitment analytics requires scoped customers and a page limit between 1 and 100")
	}
	for _, id := range query.customerIDs {
		if strings.TrimSpace(id) == "" || len(id) > 50 {
			return nil, ierr.Validation("commitment analytics requires valid customer IDs")
		}
	}
	if s == nil || s.CommitmentRepo == nil {
		return nil, ierr.NewError("commitment analytics is unavailable").Mark(ierr.ErrServiceUnavailable)
	}

	params := *query.params
	if len(params.FeatureIDs) > 0 && len(params.MeterIDs) == 0 {
		meters, err := featureMeterIDs(ctx, s.FeatureRepo, params.FeatureIDs)
		if err != nil {
			return nil, err
		}
		params.MeterIDs = meters
		if len(meters) == 0 {
			return &commitmentAnalyticsPage{entries: []*commitmentAnalyticsEntry{}}, nil
		}
	}
	page := &commitmentAnalyticsPage{entries: []*commitmentAnalyticsEntry{}}
	contexts := map[string]*commitmentContext{}
	usageBySubscription := map[string]map[string]*dto.CommitmentUsageBreakdown{}
	remainingPeriods := commitmentAnalyticsPeriodLimit
	ownerService := newCommitmentService(s.ServiceParams)
	cursor := query.afterID
	for scanned := 0; scanned < commitmentAnalyticsScanLimit; {
		batchSize := min(100, commitmentAnalyticsScanLimit-scanned)
		owners, err := s.CommitmentRepo.List(ctx, &commitment.Filter{
			QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(batchSize), Status: lo.ToPtr(types.StatusPublished)},
			CustomerIDs: query.customerIDs, AfterID: cursor,
			ActiveFrom: &params.StartTime, ActiveTo: &params.EndTime,
			Sort: []*types.SortCondition{{Field: "id", Direction: types.SortDirectionAsc}},
		})
		if err != nil {
			return nil, err
		}
		if len(owners) > batchSize {
			return nil, ierr.NewError("commitment analytics exceeded its repository bound").Mark(ierr.ErrDatabase)
		}
		for _, owner := range owners {
			if owner == nil || owner.ID == "" || owner.ID <= cursor || owner.SubscriptionID == "" || owner.Status != types.StatusPublished || owner.TenantID != types.GetTenantID(ctx) || owner.EnvironmentID != types.GetEnvironmentID(ctx) || !slices.Contains(query.customerIDs, owner.CustomerID) {
				return nil, ierr.NewError("commitment analytics returned invalid owner scope").Mark(ierr.ErrDatabase)
			}
			cursor = owner.ID
			scanned++
			if !owner.EffectiveEnd().After(params.StartTime) {
				continue
			}
			if len(params.MeterIDs) > 0 {
				loaded := contexts[owner.SubscriptionID]
				if loaded == nil {
					loaded, err = ownerService.loadCommitmentContext(ctx, owner.SubscriptionID)
					if err != nil {
						return nil, err
					}
					contexts[owner.SubscriptionID] = loaded
				}
				if loaded == nil || loaded.Sub == nil || loaded.Sub.CustomerID != owner.CustomerID || loaded.Sub.TenantID != owner.TenantID || loaded.Sub.EnvironmentID != owner.EnvironmentID {
					return nil, ierr.NewError("commitment analytics returned invalid subscription scope").Mark(ierr.ErrDatabase)
				}
				if !analyticsCommitmentMatches(owner, loaded, &params) {
					continue
				}
			}
			if len(page.entries) == query.limit {
				page.nextID = page.entries[len(page.entries)-1].owner.ID
				return page, nil
			}
			entry := &commitmentAnalyticsEntry{owner: commitment.NewCommitmentBuilder(owner).Build()}
			if len(params.PropertyFilters) > 0 || len(params.Sources) > 0 {
				entry.usageReason = "filtered_usage"
			} else {
				breakdown, ok := usageBySubscription[owner.SubscriptionID]
				if !ok {
					breakdown, err = s.analyticsCommitmentUsage(ctx, owner, &params, &remainingPeriods)
					if err != nil {
						return nil, err
					}
					usageBySubscription[owner.SubscriptionID] = breakdown
				}
				entry.usage = breakdown[owner.ID]
				if entry.usage == nil {
					entry.usageReason = "no_applicable_usage_scope"
				}
			}
			if commitmentCollectionMethod(owner) == types.CommitmentCollectionBillingTerms {
				entry.collection, err = s.analyticsCommitmentCollection(ctx, owner)
				if err != nil {
					return nil, err
				}
			}
			page.entries = append(page.entries, entry)
		}
		if len(owners) < batchSize {
			return page, nil
		}
	}
	page.nextID = cursor
	return page, nil
}

func (s *meterUsageService) analyticsCommitmentCollection(ctx context.Context, owner *commitment.Commitment) (*domain.CollectionSummary, error) {
	if s.BillingTerms == nil {
		return nil, ierr.NewError("billing terms summaries are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	ref := domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID}
	state, err := s.BillingTerms.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, ierr.NewError("billing terms summary is missing").Mark(ierr.ErrDatabase)
	}
	snapshot, summary := state.Owner(), state.Summary()
	if snapshot == nil || snapshot.Owner != ref || snapshot.TenantID != owner.TenantID || snapshot.EnvironmentID != owner.EnvironmentID || snapshot.CustomerID == "" || lo.FromPtr(snapshot.SubscriptionID) != owner.SubscriptionID || summary == nil {
		return nil, ierr.NewError("billing terms summary returned invalid owner scope").Mark(ierr.ErrDatabase)
	}
	sub, err := s.SubRepo.Get(types.WithForceWriter(ctx), owner.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if sub == nil || sub.ID != owner.SubscriptionID || sub.CustomerID != owner.CustomerID || sub.TenantID != owner.TenantID || sub.EnvironmentID != owner.EnvironmentID {
		return nil, ierr.NewError("billing terms summary returned invalid invoice recipient").Mark(ierr.ErrDatabase)
	}
	destination := sub
	if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
		adapter := &commitmentTermsOwner{subscriptions: s.SubRepo}
		destination, err = adapter.subscription(types.WithForceWriter(ctx), lo.FromPtr(sub.ParentSubscriptionID))
		if err != nil {
			return nil, err
		}
		if destination.SubscriptionType != types.SubscriptionTypeParent || destination.ParentSubscriptionID != nil || !strings.EqualFold(destination.Currency, owner.Currency) {
			return nil, ierr.NewError("billing terms summary returned invalid parent destination").Mark(ierr.ErrDatabase)
		}
	}
	if snapshot.CustomerID != destination.GetInvoicingCustomerID() {
		return nil, ierr.NewError("billing terms summary returned invalid invoice recipient").Mark(ierr.ErrDatabase)
	}
	return summary, nil
}

func analyticsCommitmentMatches(owner *commitment.Commitment, loaded *commitmentContext, params *events.MeterUsageDetailedAnalyticsParams) bool {
	if loaded == nil || loaded.Sub == nil || loaded.Sub.ID != owner.SubscriptionID || loaded.Sub.CustomerID != owner.CustomerID || loaded.Sub.TenantID != owner.TenantID || loaded.Sub.EnvironmentID != owner.EnvironmentID {
		return false
	}
	lines := scopeLinesInPeriod(owner.ScopeEntityType, owner.ScopeEntityID, loaded, maxTime(params.StartTime, owner.StartDate), minTime(params.EndTime, owner.EffectiveEnd()))
	return lo.SomeBy(lines, func(line *subscription.SubscriptionLineItem) bool {
		return slices.Contains(params.MeterIDs, line.MeterID)
	})
}

func (s *meterUsageService) analyticsCommitmentUsage(ctx context.Context, owner *commitment.Commitment, params *events.MeterUsageDetailedAnalyticsParams, remainingPeriods *int) (map[string]*dto.CommitmentUsageBreakdown, error) {
	sub, err := s.SubRepo.Get(ctx, owner.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if sub == nil || sub.ID != owner.SubscriptionID || sub.CustomerID != owner.CustomerID || sub.TenantID != owner.TenantID || sub.EnvironmentID != owner.EnvironmentID {
		return nil, ierr.NewError("commitment usage returned invalid subscription scope").Mark(ierr.ErrDatabase)
	}
	owners, err := newCommitmentService(s.ServiceParams).ActiveForPeriod(ctx, sub.ID, params.StartTime, params.EndTime)
	if err != nil {
		return nil, err
	}
	periods, err := analyticsCommitmentPeriods(sub, owners, params.StartTime, params.EndTime, *remainingPeriods)
	if err != nil {
		return nil, err
	}
	*remainingPeriods -= len(periods)
	combined := make(map[string]*dto.CommitmentUsageBreakdown)
	for _, period := range periods {
		window := *params
		window.StartTime, window.EndTime = period.Start, period.End
		window.MeterIDs, window.FeatureIDs, window.GroupBy = nil, nil, nil
		breakdown, err := s.analyticsCommitmentPeriodUsage(ctx, sub, &window)
		if err != nil {
			return nil, err
		}
		for _, row := range breakdown {
			if existing := combined[row.CommitmentID]; existing != nil {
				addCommitmentUsage(existing, row)
			} else {
				combined[row.CommitmentID] = row
			}
		}
	}
	return combined, nil
}

const commitmentAnalyticsPeriodLimit = 500

func analyticsCommitmentPeriods(sub *subscription.Subscription, owners []*commitment.Commitment, start, end time.Time, limit int) ([]types.Period, error) {
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor, sub.BillingPeriod, sub.BillingPeriodCount, sub.Timezone)
	if err != nil {
		return nil, err
	}
	start = maxTime(start, sub.StartDate)
	if sub.CancelledAt != nil {
		end = minTime(end, *sub.CancelledAt)
	}
	byStart := make(map[time.Time]types.Period)
	for _, owner := range owners {
		until := minTime(end, owner.EffectiveEnd())
		for at := maxTime(start, owner.StartDate); at.Before(until); {
			period := types.FullBillingPeriod(at, grid)
			period.Start = maxTime(start, period.Start)
			period.End = minTime(end, period.End)
			if _, exists := byStart[period.Start]; !exists {
				if len(byStart) == limit {
					return nil, ierr.Validation("commitment analytics supports at most 500 subscription billing periods per request; narrow the time range, customer selection or commitment page")
				}
				byStart[period.Start] = period
			}
			at = period.End
		}
	}
	periods := lo.Values(byStart)
	slices.SortFunc(periods, func(a, b types.Period) int { return a.Start.Compare(b.Start) })
	return periods, nil
}

func (s *meterUsageService) analyticsCommitmentPeriodUsage(ctx context.Context, sub *subscription.Subscription, params *events.MeterUsageDetailedAnalyticsParams) ([]*dto.CommitmentUsageBreakdown, error) {
	usage, err := s.GetSubscriptionMeterUsageWithSub(ctx, sub, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: sub.ID, StartTime: params.StartTime, EndTime: params.EndTime,
		UseFinal: params.UseFinal, IncludeChildren: params.IncludeChildren,
	})
	if err != nil {
		return nil, err
	}
	charges, err := s.wholeLineCharges(ctx, usage, params, &sub.BillingAnchor, params.EndTime)
	if err != nil {
		return nil, err
	}
	projectedEnd := projectedCommitmentEnd(sub, params.StartTime, params.EndTime)
	loader, err := newBillingService(s.ServiceParams).newCommitmentLoader(ctx, sub, params.StartTime, projectedEnd, projectedEnd, charges, usage.MeterMap, types.UsageSourceAnalytics)
	if err != nil {
		return nil, err
	}
	loader.usageEnd = params.EndTime
	return loader.UsageBreakdown(ctx)
}

func addCommitmentUsage(target, addition *dto.CommitmentUsageBreakdown) {
	target.AppliesFrom = minTime(target.AppliesFrom, addition.AppliesFrom)
	target.AppliesTo = maxTime(target.AppliesTo, addition.AppliesTo)
	target.Committed.Quantity = target.Committed.Quantity.Add(addition.Committed.Quantity)
	target.Committed.Amount = target.Committed.Amount.Add(addition.Committed.Amount)
	target.Used.Quantity = target.Used.Quantity.Add(addition.Used.Quantity)
	target.Used.Amount = target.Used.Amount.Add(addition.Used.Amount)
	target.Unused.Quantity = target.Unused.Quantity.Add(addition.Unused.Quantity)
	target.Unused.Amount = target.Unused.Amount.Add(addition.Unused.Amount)
	target.TrueUpAmount = target.TrueUpAmount.Add(addition.TrueUpAmount)
	target.BilledAmount = target.BilledAmount.Add(addition.BilledAmount)
}
