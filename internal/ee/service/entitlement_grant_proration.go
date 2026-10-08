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
	grantProrationSourceAddonAttach  grantProrationSource = "addon_attach"
	grantProrationSourceAddonDetach  grantProrationSource = "addon_detach"
	grantProrationSourceAddonsModify grantProrationSource = "addons_modify"
	// Not an addon change: an entitlement was deleted outright.
	grantProrationSourceEntitlementGone grantProrationSource = "entitlement_deleted"
)

func (s grantProrationSource) String() string { return string(s) }

func (s *subscriptionGrantService) resolveGrantProration(
	ctx context.Context,
	sub *subscription.Subscription,
	incomingECs []*entitlement.Entitlement,
	survivingByFeature map[string][]*entitlement.Entitlement,
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

		if lo.SomeBy(featureECs, func(ec *entitlement.Entitlement) bool { return ec.IsUnlimitedGrant() }) {
			s.Logger.Info(ctx, "skipping entitlement grant proration; allowance is unlimited",
				"subscription_id", sub.ID,
				"feature_id", featureID)
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
		if len(survivingByFeature[featureID]) > 0 {
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

// applyEntitlementGrantChange settles the whole change in one pass: every window the batch
// ends is closed together, and every successor is opened together. A feature touched by both
// a removal and an addition is one decision, so it closes once and opens once.
func (s *subscriptionGrantService) applyEntitlementGrantChange(
	ctx context.Context,
	cfg *GrantChangeConfig,
) error {
	if cfg == nil || s.EntitlementGrantRepo == nil {
		return nil
	}

	incomingByFeature := lo.GroupBy(
		lo.Filter(cfg.incomingECs, func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && ec.HasGrantConfig()
		}),
		func(ec *entitlement.Entitlement) string { return ec.FeatureID },
	)
	if len(incomingByFeature) == 0 && len(cfg.entitlementsToRemove) == 0 {
		return nil
	}

	at := cfg.entitlementChangeAt
	liveByFeature, err := NewEntitlementGrantService(s.ServiceParams).LiveGrantsByFeature(ctx, cfg.sub, at)
	if err != nil {
		return err
	}

	toClose, carryForward := s.removalClosures(ctx, cfg, liveByFeature)

	// Close every live window of every feature an addition touches, not only the ones this
	// path reopens. A window this path does not own — a day cadence, or a parallel slot —
	// would otherwise hold its slot until it expires and hide the incoming quota until then.
	for featureID := range incomingByFeature {
		toClose = append(toClose, liveByFeature[featureID]...)
	}

	grantSvc := NewEntitlementGrantService(s.ServiceParams)

	// A feature with both directions names its pooled row twice.
	closedByID, err := grantSvc.CloseEntitlementGrants(ctx, lo.UniqBy(lo.Compact(toClose),
		func(g *entitlementgrant.EntitlementGrant) string { return g.ID }), at)
	if err != nil {
		return err
	}

	reqs := make([]OpenFeatureBasedEntitlementGrantsRequest, 0, len(cfg.entitlementGrantsToAdd)+len(carryForward))
	for _, grantToAdd := range cfg.entitlementGrantsToAdd {
		featureID := grantToAdd.FeatureID()

		// The addition owns this feature's successor: it carries the balance itself, so the
		// removal must not open a second row for the same slot.
		delete(carryForward, featureID)

		incoming := incomingByFeature[featureID]
		surviving := cfg.survivingECsByFeature[featureID]
		if !shouldOpenGrantManually(append(append([]*entitlement.Entitlement{}, surviving...), incoming...)) {
			continue
		}

		req := OpenFeatureBasedEntitlementGrantsRequest{
			FeatureID:   featureID,
			New:         grantToAdd,
			ExistingECs: surviving,
			IncomingECs: incoming,
		}

		// Stamp the successor onto the window that was actually written, so the segments tile
		// and the predecessor's unspent balance carries. Whether a config still funds the
		// feature is irrelevant: quota already granted belongs to the customer for the rest of
		// the cycle, so it carries even when the config that bought it is the one leaving.
		predecessorGrant := lo.FirstOrEmpty(liveByFeature[featureID])
		if closed := closedByID[predecessorGrant.GetID()]; closed != nil {
			req.Closed = closed
			req.New = entitlementgrant.NewEntitlementGrantBuilder(grantToAdd).
				WithWindow(closed.ValidTo, grantToAdd.ValidTo).
				Build()
		}

		reqs = append(reqs, req)
	}

	for featureID, pooled := range carryForward {
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
					"proration_source":   cfg.origin().String(),
					"carry_forward_from": pooled.ID,
				}).
				Build(),
			ExistingECs: cfg.survivingECsByFeature[featureID],
		})
	}
	if len(reqs) == 0 {
		return nil
	}

	_, err = grantSvc.OpenFeatureBasedEntitlementGrants(ctx, reqs)
	return err
}

func (c *GrantChangeConfig) origin() grantProrationSource {
	if c == nil || c.entitlementChangeOrigin == "" {
		return grantProrationSourceAddonsModify
	}
	return c.entitlementChangeOrigin
}

// removalClosures decides what the leaving configs settle, writing nothing: the windows to end,
// and per feature the pooled row a successor must carry forward. A parallel EC owns its slot
// outright, so its row ends with no successor. An additive feature pools: its row is re-keyed
// onto the surviving configs, carrying the balance — and when nothing survives, left alone,
// because granted quota is never taken back.
func (s *subscriptionGrantService) removalClosures(
	ctx context.Context,
	cfg *GrantChangeConfig,
	liveByFeature map[string][]*entitlementgrant.EntitlementGrant,
) ([]*entitlementgrant.EntitlementGrant, map[string]*entitlementgrant.EntitlementGrant) {
	removedByFeature := lo.GroupBy(
		lo.Filter(cfg.entitlementsToRemove, func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && ec.HasGrantConfig()
		}),
		func(ec *entitlement.Entitlement) string { return ec.FeatureID },
	)

	toClose := make([]*entitlementgrant.EntitlementGrant, 0, len(removedByFeature))
	carryForward := make(map[string]*entitlementgrant.EntitlementGrant, len(removedByFeature))

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

		// No config still funds the feature, so there is no live EC to re-key the slot onto.
		// Leave the window to run out: granted quota is never taken back, and the tick only
		// considers features that still have live configs, so nothing will open beside it.
		// Basically, do nothing and leave the grant alone.
		if len(cfg.survivingECsByFeature[featureID]) == 0 {
			s.Logger.Info(ctx, "leaving the entitlement grant window open; the last config on the feature left",
				"subscription_id", cfg.sub.ID,
				"grant_id", pooled.ID,
				"feature_id", featureID,
				"quota", pooled.Quota.String(),
				"usage", pooled.Usage.String())
			continue
		}

		// Survivors remain, so the row has to be re-keyed onto the lowest-id EC that is still
		// live — the slot the evaluator will look under. Closing and reopening is what moves
		// it; leaving the row on the departing EC would strand the slot and let the tick open
		// a second window beside it. The successor carries the remaining balance, which for a
		// spent pool is zero — the row still has to exist to hold the slot.
		toClose = append(toClose, pooled)
		carryForward[featureID] = pooled
	}

	return toClose, carryForward
}

// -----------------------------------------------------------------------------
// read helpers
// -----------------------------------------------------------------------------

// GetSubscriptionGrantECsByFeature is the subscription's grant ECs grouped by feature —
// the set that decides slot ownership and the cold-start quota. Called before the incoming
// ECs are persisted, so they are absent from the result.
func (s *subscriptionGrantService) GetSubscriptionGrantECsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
) (map[string][]*entitlement.Entitlement, error) {
	ents, err := NewSubscriptionService(s.ServiceParams).GetSubscriptionEntitlementsForSubscription(ctx, sub)
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
