package revenue

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// eventSourceShares holds per-day and request-window usage quantities per
// event source (meter_usage.source), keyed by subscription and meter. The
// "source" group_by allocates each usage fact's metrics across these shares —
// revenue facts themselves carry no event source, so the split is derived
// from the same usage reads the analytics export uses.
type eventSourceShares struct {
	day    map[string]map[string]decimal.Decimal
	window map[string]map[string]decimal.Decimal
}

// unattributedSource labels the metrics no event source can claim: non-usage
// facts (fixed, true-up) and usage days with no recorded events.
const unattributedSource = ""

// maxSourceGroupingSubscriptions caps the per-subscription usage-read fan-out
// of a group_by "source" request.
const maxSourceGroupingSubscriptions = 50

// splitFactsBySource returns the per-fact source split for the request: the
// identity split (everything under one unlabeled source) unless "source" is a
// group dimension, in which case usage shares are loaded once for the whole
// fact set and each fact splits across the sources behind it.
func (s *revenueService) splitFactsBySource(ctx context.Context, req *dto.RevenueAnalyticsRequest, facts []*revenuefact.RevenueFact) (func(*revenuefact.RevenueFact) []sourceShare, error) {
	whole := []sourceShare{{source: unattributedSource, fraction: decimal.NewFromInt(1)}}
	if !lo.Contains(req.GroupBy, "source") {
		return func(*revenuefact.RevenueFact) []sourceShare { return whole }, nil
	}
	shares, err := s.buildEventSourceShares(ctx, req, facts)
	if err != nil {
		return nil, err
	}
	return shares.split, nil
}

// dayFloorUTC is the UTC midnight starting t's calendar day.
func dayFloorUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func dayShareKey(subscriptionID, meterID string, day time.Time) string {
	return subscriptionID + "|" + meterID + "|" + day.UTC().Format("2006-01-02")
}

func windowShareKey(subscriptionID, meterID string) string {
	return subscriptionID + "|" + meterID
}

// buildEventSourceShares queries daily usage grouped by (meter, source) once
// per distinct subscription in the fact set, scoped to that subscription's
// external customers like the rollup's own usage reads.
func (s *revenueService) buildEventSourceShares(ctx context.Context, req *dto.RevenueAnalyticsRequest, facts []*revenuefact.RevenueFact) (*eventSourceShares, error) {
	metersBySub := map[string]map[string]struct{}{}
	for _, f := range facts {
		if !sourceAllocatable(f) {
			continue
		}
		if metersBySub[f.SubscriptionID] == nil {
			metersBySub[f.SubscriptionID] = map[string]struct{}{}
		}
		metersBySub[f.SubscriptionID][lo.FromPtr(f.MeterID)] = struct{}{}
	}

	if len(metersBySub) > maxSourceGroupingSubscriptions {
		return nil, ierr.NewErrorf("group_by source spans %d subscriptions, more than the %d allowed", len(metersBySub), maxSourceGroupingSubscriptions).
			WithHint("Narrow the customer/subscription filter or time range").
			Mark(ierr.ErrValidation)
	}

	// Facts are day-grained: a fact dated D covers all of D's usage. Reading
	// the shares over the request's raw bounds would drop the events on either
	// edge day that fall outside the clock time, leaving those facts
	// unattributed — so the share window spans whole UTC days.
	start := dayFloorUTC(req.StartTime)
	end := dayFloorUTC(req.EndTime).AddDate(0, 0, 1)

	shares := &eventSourceShares{
		day:    map[string]map[string]decimal.Decimal{},
		window: map[string]map[string]decimal.Decimal{},
	}
	subscriptionService := service.NewSubscriptionService(s.ServiceParams)
	for subID, meterSet := range metersBySub {
		sub, err := s.SubRepo.Get(ctx, subID)
		if err != nil {
			return nil, err
		}
		extCustomerIDs, err := subscriptionService.ExternalCustomerIDsForSubscription(ctx, sub)
		if err != nil {
			return nil, err
		}
		results, err := s.MeterUsageRepo.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterIDs:            lo.Keys(meterSet),
			StartTime:           start,
			EndTime:             end,
			GroupBy:             []string{"meter_id", "source"},
			// Shares weigh by summed event quantity regardless of the meter's
			// own billing aggregation — the weight is event volume, not money.
			AggregationTypes: []types.AggregationType{types.AggregationSum},
			WindowSize:       types.WindowSizeDay,
			UseFinal:         true,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			for _, pt := range r.Points {
				addShare(shares.day, dayShareKey(subID, r.MeterID, pt.WindowStart), r.Source, pt.TotalUsage)
			}
			addShare(shares.window, windowShareKey(subID, r.MeterID), r.Source, r.TotalUsage)
		}
	}
	return shares, nil
}

func addShare(m map[string]map[string]decimal.Decimal, key, source string, qty decimal.Decimal) {
	if !qty.IsPositive() {
		return
	}
	if m[key] == nil {
		m[key] = map[string]decimal.Decimal{}
	}
	m[key][source] = m[key][source].Add(qty)
}

// sourceAllocatable reports whether f's metrics can be attributed to event
// sources: only usage-derived facts (usage and overage rows, reverts
// included) with a real meter carry event usage behind them.
func sourceAllocatable(f *revenuefact.RevenueFact) bool {
	return lo.FromPtr(f.MeterID) != "" &&
		(f.RevenueSource == types.RevenueSourceUsage || f.RevenueSource == types.RevenueSourceOverage)
}

type sourceShare struct {
	source   string
	fraction decimal.Decimal
}

// split returns the source fractions for one fact, summing exactly to 1.
// Marginal facts follow their day's source mix; whole-period facts follow the
// request window's mix for their (subscription, meter). Facts with no usage
// behind them land whole under the unattributed source.
func (es *eventSourceShares) split(f *revenuefact.RevenueFact) []sourceShare {
	one := decimal.NewFromInt(1)
	if !sourceAllocatable(f) {
		return []sourceShare{{source: unattributedSource, fraction: one}}
	}

	var bySource map[string]decimal.Decimal
	if f.DecompositionMode == types.Marginal {
		bySource = es.day[dayShareKey(f.SubscriptionID, lo.FromPtr(f.MeterID), f.Day)]
	}
	if len(bySource) == 0 {
		bySource = es.window[windowShareKey(f.SubscriptionID, lo.FromPtr(f.MeterID))]
	}
	if len(bySource) == 0 {
		return []sourceShare{{source: unattributedSource, fraction: one}}
	}

	sources := lo.Keys(bySource)
	sort.Strings(sources)
	weights := make([]decimal.Decimal, len(sources))
	for i, src := range sources {
		weights[i] = bySource[src]
	}
	fractions := service.SpreadAmount(one, weights)
	shares := make([]sourceShare, 0, len(sources))
	for i, src := range sources {
		if fractions[i].IsZero() {
			continue
		}
		shares = append(shares, sourceShare{source: src, fraction: fractions[i]})
	}
	return shares
}
