package service

import (
	"context"
	"slices"
	"strings"

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
					breakdown, err = s.analyticsCommitmentUsage(ctx, owner, &params)
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

func (s *meterUsageService) analyticsCommitmentUsage(ctx context.Context, owner *commitment.Commitment, params *events.MeterUsageDetailedAnalyticsParams) (map[string]*dto.CommitmentUsageBreakdown, error) {
	usage, err := s.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: owner.SubscriptionID, StartTime: params.StartTime, EndTime: params.EndTime,
		UseFinal: params.UseFinal, IncludeChildren: params.IncludeChildren,
	})
	if err != nil {
		return nil, err
	}
	if usage == nil || usage.Subscription == nil || usage.Subscription.ID != owner.SubscriptionID || usage.Subscription.CustomerID != owner.CustomerID || usage.Subscription.TenantID != owner.TenantID || usage.Subscription.EnvironmentID != owner.EnvironmentID {
		return nil, ierr.NewError("commitment usage returned invalid subscription scope").Mark(ierr.ErrDatabase)
	}
	unfiltered := *params
	unfiltered.MeterIDs, unfiltered.FeatureIDs, unfiltered.GroupBy = nil, nil, nil
	end := params.EndTime
	if usage.Subscription.CancelledAt != nil {
		end = minTime(end, *usage.Subscription.CancelledAt)
	}
	if !params.StartTime.Before(end) {
		return map[string]*dto.CommitmentUsageBreakdown{}, nil
	}
	charges, err := s.wholeLineCharges(ctx, usage, &unfiltered, &usage.Subscription.BillingAnchor, end)
	if err != nil {
		return nil, err
	}
	projectedEnd := projectedCommitmentEnd(usage.Subscription, params.StartTime, end)
	loader, err := newBillingService(s.ServiceParams).newCommitmentLoader(ctx, usage.Subscription, params.StartTime, projectedEnd, projectedEnd, charges, usage.MeterMap, types.UsageSourceAnalytics)
	if err != nil {
		return nil, err
	}
	loader.usageEnd = end
	breakdown, err := loader.UsageBreakdown(ctx)
	if err != nil {
		return nil, err
	}
	return lo.KeyBy(breakdown, func(row *dto.CommitmentUsageBreakdown) string { return row.CommitmentID }), nil
}
