package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// entitlementGrantQuotaScale matches the numeric(25,15) precision of entitlement_grants.quota.
const entitlementGrantQuotaScale = 15

// grantProrationSource names the subscription change that wrote a grant segment.
// It lands in the segment's metadata as proration_source, so a window can always
// be traced back to what cut it.
type grantProrationSource string

const (
	grantProrationSourceAddonAttach grantProrationSource = "addon_attach"
	grantProrationSourceAddonDetach grantProrationSource = "addon_detach"
)

func (s grantProrationSource) String() string { return string(s) }

func (s *subscriptionService) resolveGrantProration(
	ctx context.Context,
	sub *subscription.Subscription,
	incomingECs []*entitlement.Entitlement,
	existingByFeature map[string][]*entitlement.Entitlement,
	effectiveDate time.Time,
	behavior types.ProrationBehavior,
	source grantProrationSource,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || s.EntitlementRepo == nil {
		return nil, nil
	}

	incoming := lo.Filter(incomingECs, func(ec *entitlement.Entitlement, _ int) bool {
		return ec != nil && ec.HasGrantConfig()
	})
	if len(incoming) == 0 {
		return nil, nil
	}

	p, err := types.FindPeriodForDate(&types.FindPeriodForDateParams{
		Target:           effectiveDate,
		KnownPeriodStart: sub.CurrentPeriodStart,
		KnownPeriodEnd:   sub.CurrentPeriodEnd,
		Anchor:           sub.BillingAnchor,
		PeriodCount:      sub.BillingPeriodCount,
		BillingPeriod:    sub.BillingPeriod,
		Timezone:         sub.Timezone,
	})
	if err != nil {
		s.Logger.Info(ctx, "skipping entitlement grant proration; could not resolve billing period",
			"subscription_id", sub.ID,
			"effective_date", effectiveDate,
			"error", err.Error())
		return nil, nil
	}

	if !p.Start.Before(sub.CurrentPeriodEnd) {
		s.Logger.Info(ctx, "skipping entitlement grant proration; the change lands in a later billing cycle",
			"subscription_id", sub.ID,
			"effective_date", effectiveDate,
			"resolved_period_start", p.Start,
			"current_period_end", sub.CurrentPeriodEnd)
		return nil, nil
	}

	grants := make([]*entitlementgrant.EntitlementGrant, 0, len(incoming))
	for featureID, featureECs := range lo.GroupBy(incoming, func(ec *entitlement.Entitlement) string {
		return ec.FeatureID
	}) {
		if !shouldOpenGrantManually(featureECs) {
			continue
		}

		prorationDate := p.Start
		if behavior == types.ProrationBehaviorCreateProrations && effectiveDate.After(p.Start) {
			prorationDate = effectiveDate
		}

		coefficient, err := proration.Coefficient(p.Start, p.End, prorationDate, types.StrategySecondBased)
		if err != nil {
			s.Logger.Info(ctx, "skipping entitlement grant proration; coefficient could not be computed",
				"subscription_id", sub.ID,
				"feature_id", featureID,
				"effective_date", effectiveDate,
				"error", err.Error())
			continue
		}

		originalQuota := decimal.Zero
		for _, ec := range featureECs {
			originalQuota = originalQuota.Add(lo.FromPtr(ec.GrantQuota))
		}

		delta := originalQuota.Mul(coefficient).Round(entitlementGrantQuotaScale)
		if !delta.IsPositive() {
			s.Logger.Info(ctx, "skipping entitlement grant proration; quota is not positive",
				"subscription_id", sub.ID,
				"feature_id", featureID,
				"coefficient", coefficient.String())
			continue
		}

		coverageStart := effectiveDate
		if len(existingByFeature[featureID]) > 0 {
			coverageStart = p.Start
		}

		grants = append(grants, entitlementgrant.NewEntitlementGrantBuilder(nil).
			WithCustomerID(sub.CustomerID).
			WithSubscriptionID(sub.ID).
			WithScope(types.EntitlementGrantScopeFeature, featureID).
			WithMeasure(featureECs[0].GrantMeasure).
			WithQuota(delta).
			WithWindow(coverageStart, p.End).
			WithMetadata(proration.AuditMetadata(proration.AuditParams{
				Source:        source.String(),
				Coefficient:   coefficient,
				OriginalKey:   "proration_original_quota",
				OriginalValue: originalQuota,
				PeriodStart:   p.Start,
				PeriodEnd:     p.End,
				ProrationDate: prorationDate,
				Strategy:      types.StrategySecondBased,
			})).
			Build())
	}

	return grants, nil
}

// materialiseEntitlementGrants writes the resolved grants for the cycle: it closes every
// live window of the features the change touches, then builds the open requests for the
// ones this path owns.
func (s *subscriptionService) materialiseEntitlementGrants(
	ctx context.Context,
	sub *subscription.Subscription,
	newGrants []*entitlementgrant.EntitlementGrant,
	incomingECs []*entitlement.Entitlement,
	existingByFeature map[string][]*entitlement.Entitlement,
	effectiveDate time.Time,
) error {
	incomingByFeature := lo.GroupBy(
		lo.Filter(incomingECs, func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && ec.HasGrantConfig()
		}),
		func(ec *entitlement.Entitlement) string { return ec.FeatureID },
	)
	if len(incomingByFeature) == 0 {
		return nil
	}

	at := latestOf(effectiveDate, time.Now().UTC())
	liveByFeature, err := s.liveGrantsByFeature(ctx, sub, at)
	if err != nil {
		return err
	}

	grantSvc := NewEntitlementGrantService(s.ServiceParams)

	// Close every live window of every feature the change touches, not only the ones this
	// path reopens. A window this path does not own — a day cadence, or a parallel slot —
	// would otherwise hold its slot until it expires and hide the incoming quota until
	// then. Closing hands the slot back to the tick, which reopens it at the config's full quota.
	toClose := make([]*entitlementgrant.EntitlementGrant, 0, len(incomingByFeature))
	for featureID := range incomingByFeature {
		toClose = append(toClose, liveByFeature[featureID]...)
	}

	closedByID, err := grantSvc.CloseEntitlementGrants(ctx, toClose, at)
	if err != nil {
		return err
	}

	reqs := make([]OpenFeatureBasedEntitlementGrantsRequest, 0, len(newGrants))
	for _, g := range newGrants {
		featureID := g.FeatureID()
		incoming := incomingByFeature[featureID]
		existing := existingByFeature[featureID]

		if !shouldOpenGrantManually(append(append([]*entitlement.Entitlement{}, existing...), incoming...)) {
			continue
		}

		req := OpenFeatureBasedEntitlementGrantsRequest{
			FeatureID:   featureID,
			New:         g,
			ExistingECs: existing,
			IncomingECs: incoming,
		}

		// Stamp the successor onto the window that was actually written, so the segments
		// tile. A feature with no live row keeps the resolved window.
		if live := lo.FirstOrEmpty(liveByFeature[featureID]); live != nil {
			req.Closed = closedByID[live.ID]
			req.New = entitlementgrant.NewEntitlementGrantBuilder(g).
				WithWindow(req.Closed.ValidTo, g.ValidTo).
				Build()
		}

		reqs = append(reqs, req)
	}
	if len(reqs) == 0 {
		return nil
	}

	_, err = grantSvc.OpenFeatureBasedEntitlementGrants(ctx, reqs)
	return err
}

func shouldOpenGrantManually(featureECs []*entitlement.Entitlement) bool {
	if len(featureECs) == 0 {
		return false
	}

	return lo.EveryBy(featureECs, func(ec *entitlement.Entitlement) bool {
		return ec != nil &&
			defaultedMode(ec.AggregationMode) == types.EntitlementAggregationModeAdditive &&
			ec.GrantDurationUnit == types.EntitlementGrantDurationUnitSubscriptionPeriod
	})
}

// handleGrantsForRemovedECs settles the grant windows the removed ECs fed. A parallel EC owns
// its slot outright, so its row is closed and nothing succeeds it. An additive feature pools
// the removed quota with whatever else feeds it: the live row is closed and, while any EC
// survives, a successor carries the remaining balance through the rest of the window.
// No-op when the removal is future-dated, since no window is live at that instant.
func (s *subscriptionService) handleGrantsForRemovedECs(
	ctx context.Context,
	sub *subscription.Subscription,
	removedECs []*entitlement.Entitlement,
	effectiveDate time.Time,
	source grantProrationSource,
) error {
	if s.EntitlementGrantRepo == nil || len(removedECs) == 0 {
		return nil
	}

	removedByFeature := lo.GroupBy(
		lo.Filter(removedECs, func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && ec.HasGrantConfig()
		}),
		func(ec *entitlement.Entitlement) string { return ec.FeatureID },
	)
	if len(removedByFeature) == 0 {
		return nil
	}

	// See materialiseEntitlementGrants: a removal dated in the past cannot re-cut windows
	// that have already been measured and succeeded.
	at := latestOf(effectiveDate, time.Now().UTC())
	liveByFeature, err := s.liveGrantsByFeature(ctx, sub, at)
	if err != nil {
		return err
	}

	ecsByFeature, err := s.GetSubscriptionGrantECsByFeature(ctx, sub)
	if err != nil {
		return err
	}

	toClose := make([]*entitlementgrant.EntitlementGrant, 0, len(removedByFeature))
	pooledByFeature := make(map[string]*entitlementgrant.EntitlementGrant, len(removedByFeature))
	survivingByFeature := make(map[string][]*entitlement.Entitlement, len(removedByFeature))

	for featureID, removed := range removedByFeature {
		live := liveByFeature[featureID]
		if len(live) == 0 {
			continue
		}

		removedIDs := lo.SliceToMap(removed, func(ec *entitlement.Entitlement) (string, bool) {
			return ec.ID, true
		})

		if hasParallelECs(removed) {
			toClose = append(toClose, lo.Filter(live, func(g *entitlementgrant.EntitlementGrant, _ int) bool {
				return removedIDs[g.EntitlementConfigID]
			})...)
			continue
		}

		pooled := lo.FirstOrEmpty(live)

		// Nothing left to hand forward, and the successor would have to carry a zero
		// quota — which the grant model rejects. Leaving the spent window open keeps the
		// slot covered, so the tick cannot re-derive a fresh allowance from the surviving
		// configs and hand back quota the pool already consumed.
		if pooled.Remaining().IsZero() {
			s.Logger.Info(ctx, "keeping the spent entitlement grant window open; nothing to carry forward",
				"subscription_id", sub.ID,
				"grant_id", pooled.ID,
				"feature_id", featureID,
				"quota", pooled.Quota.String(),
				"usage", pooled.Usage.String())
			continue
		}

		toClose = append(toClose, pooled)

		// all ECs that are not removed
		surviving := lo.Filter(ecsByFeature[featureID], func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && !removedIDs[ec.ID]
		})
		if len(surviving) == 0 {
			continue
		}

		pooledByFeature[featureID] = pooled
		survivingByFeature[featureID] = surviving
	}
	if len(toClose) == 0 {
		return nil
	}

	grantSvc := NewEntitlementGrantService(s.ServiceParams)

	closedByID, err := grantSvc.CloseEntitlementGrants(ctx, toClose, at)
	if err != nil {
		return err
	}

	reqs := make([]OpenFeatureBasedEntitlementGrantsRequest, 0, len(pooledByFeature))
	for featureID, pooled := range pooledByFeature {
		closed := closedByID[pooled.ID]
		if closed == nil {
			continue
		}

		// Zero delta: the successor's quota is whatever the closed window had left.
		reqs = append(reqs, OpenFeatureBasedEntitlementGrantsRequest{
			FeatureID: featureID,
			Closed:    closed,
			New: entitlementgrant.NewEntitlementGrantBuilder(pooled).
				WithQuota(decimal.Zero).
				WithWindow(closed.ValidTo, pooled.ValidTo).
				WithMetadata(types.Metadata{
					"proration_source":   source.String(),
					"carry_forward_from": pooled.ID,
				}).
				Build(),
			ExistingECs: survivingByFeature[featureID],
		})
	}
	if len(reqs) == 0 {
		return nil
	}

	_, err = grantSvc.OpenFeatureBasedEntitlementGrants(ctx, reqs)
	return err
}

// -----------------------------------------------------------------------------
// read helpers
// -----------------------------------------------------------------------------

// GetSubscriptionGrantECsByFeature is the subscription's grant ECs grouped by feature —
// the set that decides slot ownership and the cold-start quota. Called before the incoming
// ECs are persisted, so they are absent from the result.
func (s *subscriptionService) GetSubscriptionGrantECsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
) (map[string][]*entitlement.Entitlement, error) {
	ents, err := s.GetSubscriptionEntitlementsForSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}

	grantECs := lo.FilterMap(ents, func(e *dto.EntitlementResponse, _ int) (*entitlement.Entitlement, bool) {
		if e == nil || e.Entitlement == nil || !e.Entitlement.HasGrantConfig() {
			return nil, false
		}
		return e.Entitlement, true
	})

	return lo.GroupBy(grantECs, func(ec *entitlement.Entitlement) string {
		return ec.FeatureID
	}), nil
}

// liveGrantsByFeature returns the subscription's feature-scoped grant rows whose window
// contains `at`, grouped by feature. Windows already closed before `at` are excluded by the
// query, so each slot yields the one segment that is actually live.
func (s *subscriptionService) liveGrantsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
	at time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithLiveOnly(at)

	rows, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	byFeature := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range rows {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		byFeature[g.FeatureID()] = append(byFeature[g.FeatureID()], g)
	}
	return byFeature, nil
}
