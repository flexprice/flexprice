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
	grantProrationSourceSubscriptionCreate grantProrationSource = "subscription_create"
	grantProrationSourceCreditGrantCreate  grantProrationSource = "credit_grant_create"
	grantProrationSourceAddonAttach        grantProrationSource = "addon_attach"
	grantProrationSourceAddonDetach        grantProrationSource = "addon_detach"
	grantProrationSourceAddonsModify       grantProrationSource = "addons_modify"
	grantProrationSourcePlanChange         grantProrationSource = "plan_change"
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

		// The allowance follows the charge's proration, never-reset meters included: a prorated
		// change grants the prorated quota, an unprorated one the full quota.
		prorate := behavior == types.ProrationBehaviorCreateProrations

		prorationDate := p.Start
		if prorate && effectiveDate.After(p.Start) {
			prorationDate = effectiveDate
		}

		// Same coefficient as the charge; e.g. 100 units attached Jan 20 in [Jan 1, Feb 1) → 38.71 (12/31).
		coefficient, full := decimal.NewFromInt(1), p
		if prorate {
			serviceablePeriod := types.Period{Start: prorationDate, End: p.End}
			coefficient, full, err = proration.CalculateProrationCoefficient(sub, sub.BillingPeriod, sub.BillingPeriodCount,
				serviceablePeriod, types.StrategySecondBased)
		}
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
				PeriodStart:   full.Start,
				PeriodEnd:     full.End,
				ProrationDate: prorationDate,
				Strategy:      types.StrategySecondBased,
			})).
			Build())
	}

	return grants, nil
}

// survivingParallelSlots is the set of surviving parallel config IDs the incoming configs do
// not share; a re-attached addon reuses its config ID, and so its slot, and must still close.
func survivingParallelSlots(surviving, incoming []*entitlement.Entitlement) map[string]bool {
	incomingIDs := lo.SliceToMap(incoming, func(ec *entitlement.Entitlement) (string, bool) { return ec.ID, true })
	keep := make(map[string]bool, len(surviving))
	for _, ec := range surviving {
		if ec != nil && !incomingIDs[ec.ID] && defaultedMode(ec.AggregationMode) == types.EntitlementAggregationModeParallel {
			keep[ec.ID] = true
		}
	}
	return keep
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
	// path reopens: a day-cadence window would otherwise hide the incoming quota until it
	// expires. A surviving parallel config owns its slot, so its window stays open with its balance.
	for featureID, incoming := range incomingByFeature {
		keep := survivingParallelSlots(cfg.survivingECsByFeature[featureID], incoming)
		toClose = append(toClose, lo.Reject(liveByFeature[featureID], func(g *entitlementgrant.EntitlementGrant, _ int) bool {
			return keep[g.EntitlementConfigID]
		})...)
	}

	grantSvc := &entitlementGrantService{ServiceParams: s.ServiceParams}

	// A feature with both directions names its pooled row twice.
	closedByID, err := grantSvc.CloseEntitlementGrants(ctx, lo.UniqBy(lo.Compact(toClose),
		func(g *entitlementgrant.EntitlementGrant) string { return g.ID }), at, false)
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

		neverReset, err := grantSvc.isNeverResetMeter(ctx, featureID)
		if err != nil {
			return err
		}

		predecessorGrant := lo.FirstOrEmpty(liveByFeature[featureID])
		closed := closedByID[predecessorGrant.GetID()]

		existingQuota := decimal.Zero
		for _, ec := range surviving {
			existingQuota = existingQuota.Add(lo.FromPtr(ec.GrantQuota))
		}
		switch {
		case !neverReset && closed != nil:
			existingQuota, _ = closed.Remaining()
		case neverReset && closed != nil && len(surviving) > 0 && !cfg.removesFeature(featureID):
			// Nothing leaves, so the cap carries as it stands, prorated segments included.
			// With no surviving config the live row is a detached addon's, so it adds nothing.
			existingQuota = closed.Quota
		}
		// E.g. incoming 150, old remaining 20, no surviving configs: normal gets 170; NEVER gets 150.
		quota := grantToAdd.Quota.Add(existingQuota)

		newGrant := entitlementgrant.NewEntitlementGrantBuilder(grantToAdd).WithQuota(quota)
		if closed != nil {
			req.Closed = closed
			newGrant.WithWindow(closed.ValidTo, grantToAdd.ValidTo)
		}
		req.New = newGrant.Build()

		reqs = append(reqs, req)
	}

	for featureID, pooled := range carryForward {
		closed := closedByID[pooled.ID]
		if closed == nil {
			continue
		}

		neverReset, err := grantSvc.isNeverResetMeter(ctx, featureID)
		if err != nil {
			return err
		}

		quota := decimal.Zero
		for _, ec := range cfg.survivingECsByFeature[featureID] {
			quota = quota.Add(lo.FromPtr(ec.GrantQuota))
		}
		if !neverReset {
			quota, _ = closed.Remaining()
		}
		reqs = append(reqs, OpenFeatureBasedEntitlementGrantsRequest{
			FeatureID: featureID,
			Closed:    closed,
			New: entitlementgrant.NewEntitlementGrantBuilder(pooled).
				WithQuota(quota).
				WithWindow(closed.ValidTo, pooled.ValidTo).
				WithMetadata(types.Metadata{
					"proration_source":   cfg.origin().String(),
					"carry_forward_from": pooled.ID,
				}).
				Build(),
			ExistingECs: cfg.survivingECsByFeature[featureID],
			// An unlimited incoming config has no prorated segment, so it joins here to set the slot.
			IncomingECs: incomingByFeature[featureID],
		})
	}
	if len(reqs) == 0 {
		return nil
	}

	_, err = grantSvc.OpenFeatureBasedEntitlementGrants(ctx, reqs)
	return err
}

// removesFeature reports whether a grant config on the feature leaves in this change.
func (c *GrantChangeConfig) removesFeature(featureID string) bool {
	return lo.SomeBy(c.entitlementsToRemove, func(ec *entitlement.Entitlement) bool {
		return ec != nil && ec.FeatureID == featureID && ec.HasGrantConfig()
	})
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
