package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// entitlementGrantProrationSource labels the audit metadata written by this path.
const entitlementGrantProrationSource = "addon_attach"

// addonEntitlementGrantProrationKey is the per-association idempotency marker.
// persistAddonAttach also runs on the payment-gated replay, so a top-up that has
// already landed must be recognised and skipped. Keying by association (rather
// than by addon) also lets the same addon be attached twice, each attachment
// adding its own delta under its own key.
func addonEntitlementGrantProrationKey(associationID string) string {
	return "proration_addon_assoc_" + associationID
}

// addonEntitlementGrantProrationEntry is one feature's prorated allowance,
// resolved before the attach transaction and applied inside it.
type addonEntitlementGrantProrationEntry struct {
	// slotECID is the entitlement config whose slot the cycle's row lives on —
	// the row to top up, or the row to create when the slot is empty.
	slotECID  string
	featureID string
	measure   types.EntitlementGrantMeasure

	// proratedDelta is the addon's quota scaled to the part of the period it covers.
	proratedDelta decimal.Decimal
	// preAddonQuotaSum is what the slot's row carries without this addon. Only
	// used when the row has to be created from scratch.
	preAddonQuotaSum decimal.Decimal

	// coverageStart is where a newly created row begins: the cycle start when the
	// feature already had allowance, the attach date when it is new to the
	// subscription.
	coverageStart time.Time
	cycleStart    time.Time
	cycleEnd      time.Time

	metadata types.Metadata
}

// addonEntitlementGrantProration resolves, per feature, how much prorated quota a
// mid-cycle addon attach should add to this cycle's grant. It reads only — the
// writes happen in materializeAddonEntitlementGrantProration inside the attach
// transaction.
//
// Only subscription_period grants participate.
func (s *subscriptionService) addonEntitlementGrantProration(
	ctx context.Context,
	sub *subscription.Subscription,
	startDate time.Time,
	behavior types.ProrationBehavior,
	addonID string,
	associationID string,
) []addonEntitlementGrantProrationEntry {
	// Dep guard, matching the pre-invoice refresh: partially-wired services stay
	// on the old path rather than panicking.
	if s.EntitlementGrantRepo == nil || s.EntitlementRepo == nil {
		return nil
	}

	p, err := types.FindPeriodForDate(&types.FindPeriodForDateParams{
		Target:           startDate,
		KnownPeriodStart: sub.CurrentPeriodStart,
		KnownPeriodEnd:   sub.CurrentPeriodEnd,
		Anchor:           sub.BillingAnchor,
		PeriodCount:      sub.BillingPeriodCount,
		BillingPeriod:    sub.BillingPeriod,
		Timezone:         sub.Timezone,
	})
	if err != nil {
		s.Logger.Info(ctx, "skipping entitlement grant proration; could not resolve billing period for addon start",
			"subscription_id", sub.ID,
			"addon_id", addonID,
			"start_date", startDate,
			"error", err.Error())
		return nil
	}

	shouldProrate := behavior == types.ProrationBehaviorCreateProrations && startDate.After(p.Start)

	addonECs := s.grantConfigECsForAddon(ctx, addonID)
	if len(addonECs) == 0 {
		return nil
	}

	preAddonByFeature := s.preAddonGrantECsByFeature(ctx, sub)
	liveByFeature := s.liveCycleGrantsByFeature(ctx, sub, p.Start, p.End)

	entries := make([]addonEntitlementGrantProrationEntry, 0, len(addonECs))
	for _, ec := range addonECs {
		entry, ok := s.resolveAddonGrantProrationEntry(
			ctx, sub, ec, preAddonByFeature[ec.FeatureID], liveByFeature[ec.FeatureID],
			p.Start, p.End, startDate, associationID, shouldProrate)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// resolveAddonGrantProrationEntry turns one addon EC into a prorated entry, or
// reports that there is nothing to do for it.
func (s *subscriptionService) resolveAddonGrantProrationEntry(
	ctx context.Context,
	sub *subscription.Subscription,
	addonEC *entitlement.Entitlement,
	preAddonECs []*entitlement.Entitlement,
	liveGrants []*entitlementgrant.EntitlementGrant,
	cycleStart, cycleEnd, startDate time.Time,
	associationID string,
	shouldProrate bool,
) (addonEntitlementGrantProrationEntry, bool) {
	prorationDate := cycleStart
	if shouldProrate {
		prorationDate = startDate
	}

	res, err := proration.CalculateEntitlementGrantProration(proration.EntitlementGrantProrationParams{
		PeriodStart:   cycleStart,
		PeriodEnd:     cycleEnd,
		ProrationDate: prorationDate,
		Strategy:      types.StrategySecondBased,
		OriginalQuota: lo.FromPtr(addonEC.GrantQuota),
	})
	if err != nil {
		s.Logger.Info(ctx, "skipping entitlement grant proration; coefficient could not be computed",
			"subscription_id", sub.ID,
			"entitlement_id", addonEC.ID,
			"error", err.Error())
		return addonEntitlementGrantProrationEntry{}, false
	}

	if !res.ProratedQuota.IsPositive() {
		s.Logger.Info(ctx, "skipping addon entitlement grant allocation; quota is not positive",
			"subscription_id", sub.ID,
			"entitlement_id", addonEC.ID,
			"coefficient", res.Coefficient.String())
		return addonEntitlementGrantProrationEntry{}, false
	}

	entry := addonEntitlementGrantProrationEntry{
		featureID:     addonEC.FeatureID,
		measure:       addonEC.GrantMeasure,
		proratedDelta: res.ProratedQuota,
		cycleStart:    cycleStart,
		cycleEnd:      cycleEnd,
		coverageStart: cycleStart,
		metadata:      res.AuditMetadata(entitlementGrantProrationSource),
	}

	entry.metadata["proration_applied"] = lo.Ternary(shouldProrate, "true", "false")
	entry.metadata[addonEntitlementGrantProrationKey(associationID)] = res.Coefficient.String()

	// Parallel: every EC owns its own slot, so the addon's grant is a new budget
	// on its own row and there is nothing to pool with. Its window starts at the
	// attach so it meters only post-attach usage.
	if isParallelGrantFeature(preAddonECs, addonEC) {
		entry.slotECID = addonEC.ID
		entry.preAddonQuotaSum = decimal.Zero
		entry.coverageStart = startDate
		return entry, true
	}

	// Additive: one pooled row per feature per cycle.
	for _, ec := range preAddonECs {
		entry.preAddonQuotaSum = entry.preAddonQuotaSum.Add(lo.FromPtr(ec.GrantQuota))
	}

	if incumbent := lowestIDGrant(liveGrants); incumbent != nil {
		// The row already exists: top it up in place, window untouched. Its EC
		// keeps the slot (grantCandidatesForFeature prefers the incumbent), so
		// no second row opens for the rest of the cycle.
		entry.slotECID = incumbent.EntitlementConfigID
		return entry, true
	}

	// No row yet this cycle — the attach materializes it, on the same slot the
	// evaluator would have chosen.
	entry.slotECID = lowestIDEC(append(append([]*entitlement.Entitlement{}, preAddonECs...), addonEC))
	if entry.preAddonQuotaSum.IsZero() {
		// The feature is new to the subscription: the customer held no allowance
		// before, so the window starts at the attach and counts only post-attach
		// usage — the same window the addon's own prorated charge covers.
		entry.coverageStart = startDate
	}

	return entry, true
}

// materializeAddonEntitlementGrantProration applies the resolved entries. Runs
// inside the attach transaction so the quota lands atomically with the addon.
//
// The attach writes the row itself rather than waiting for the evaluator: grants
// are opened lazily by a usage-driven tick, and there is no request in scope at
// open time to carry the proration.
func (s *subscriptionService) materializeAddonEntitlementGrantProration(
	ctx context.Context,
	sub *subscription.Subscription,
	entries []addonEntitlementGrantProrationEntry,
	associationID string,
) error {
	if len(entries) == 0 {
		return nil
	}

	for _, entry := range entries {
		existing, err := s.EntitlementGrantRepo.FindLastBySlot(ctx, entry.slotECID, sub.CustomerID, sub.ID)
		if err != nil {
			return err
		}

		// Only a row covering THIS cycle can be topped up; an older window on the
		// same slot is history.
		if existing != nil && existing.ValidTo.After(entry.cycleStart) {
			if existing.HasMetadataKey(addonEntitlementGrantProrationKey(associationID)) {
				s.Logger.Info(ctx, "entitlement grant proration already applied for this association, skipping",
					"subscription_id", sub.ID,
					"grant_id", existing.ID,
					"association_id", associationID)
				continue
			}

			if _, err := s.EntitlementGrantRepo.TopUpQuota(
				ctx, existing.ID, entry.proratedDelta, time.Now().UTC(), entry.metadata,
			); err != nil {
				return err
			}

			s.Logger.Info(ctx, "topped up entitlement grant quota for addon attach",
				"subscription_id", sub.ID,
				"grant_id", existing.ID,
				"feature_id", entry.featureID,
				"delta", entry.proratedDelta.String())
			continue
		}

		quota := entry.preAddonQuotaSum.Add(entry.proratedDelta)
		if !quota.IsPositive() {
			s.Logger.Info(ctx, "skipping entitlement grant proration; resulting quota is not positive",
				"subscription_id", sub.ID,
				"feature_id", entry.featureID,
				"quota", quota.String())
			continue
		}

		grant := entitlementgrant.NewEntitlementGrantBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
			WithEntitlementConfigID(entry.slotECID).
			WithCustomerID(sub.CustomerID).
			WithSubscriptionID(sub.ID).
			WithScope(types.EntitlementGrantScopeFeature, entry.featureID).
			WithMeasure(entry.measure).
			WithQuota(quota).
			WithWindow(entry.coverageStart, entry.cycleEnd).
			WithGrantStatus(types.EntitlementGrantStatusActive).
			WithMetadata(entry.metadata).
			WithEnvironmentID(types.GetEnvironmentID(ctx)).
			WithBaseModel(types.GetDefaultBaseModel(ctx)).
			Build()
		if err := grant.Validate(); err != nil {
			s.Logger.Info(ctx, "skipping entitlement grant proration; grant failed validation",
				"subscription_id", sub.ID,
				"feature_id", entry.featureID,
				"error", err.Error())
			continue
		}
		if _, err := s.EntitlementGrantRepo.Create(ctx, grant); err != nil {
			return err
		}
		s.Logger.Info(ctx, "created prorated entitlement grant for addon attach",
			"subscription_id", sub.ID,
			"grant_id", grant.ID,
			"feature_id", entry.featureID,
			"quota", quota.String())
	}
	return nil
}

// -----------------------------------------------------------------------------
// read helpers
// -----------------------------------------------------------------------------

// grantConfigECsForAddon returns the addon's subscription_period grant ECs.
func (s *subscriptionService) grantConfigECsForAddon(ctx context.Context, addonID string) []*entitlement.Entitlement {
	resp, err := NewEntitlementService(s.ServiceParams).GetAddonEntitlements(ctx, addonID)
	if err != nil {
		s.Logger.Info(ctx, "skipping entitlement grant proration; addon entitlements could not be loaded",
			"addon_id", addonID, "error", err.Error())
		return nil
	}

	out := make([]*entitlement.Entitlement, 0, len(resp.Items))
	for _, e := range resp.Items {
		if e == nil || e.Entitlement == nil {
			continue
		}
		if !isProratableGrantEC(e.Entitlement) {
			continue
		}
		out = append(out, e.Entitlement)
	}

	return out
}

// preAddonGrantECsByFeature is the subscription's grant ECs BEFORE this addon's
// association exists — the set that decides the pool's slot and its unprorated
// quota. Called before the association is written, so the addon is absent.
func (s *subscriptionService) preAddonGrantECsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
) map[string][]*entitlement.Entitlement {
	ents, err := s.GetSubscriptionEntitlementsForSubscription(ctx, sub)
	if err != nil {
		s.Logger.Info(ctx, "entitlement grant proration; subscription entitlements could not be loaded",
			"subscription_id", sub.ID, "error", err.Error())
		return nil
	}

	byFeature := make(map[string][]*entitlement.Entitlement)
	for _, e := range ents {
		if e == nil || e.Entitlement == nil || !isProratableGrantEC(e.Entitlement) {
			continue
		}
		byFeature[e.Entitlement.FeatureID] = append(byFeature[e.Entitlement.FeatureID], e.Entitlement)
	}
	return byFeature
}

// liveCycleGrantsByFeature returns the subscription's grant rows overlapping
// [cycleStart, cycleEnd), grouped by feature.
func (s *subscriptionService) liveCycleGrantsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
	cycleStart, cycleEnd time.Time,
) map[string][]*entitlementgrant.EntitlementGrant {
	filter := types.NewNoLimitEntitlementGrantFilter()
	filter.CustomerIDs = []string{sub.CustomerID}
	filter.SubscriptionIDs = []string{sub.ID}
	filter.ValidFromBefore = &cycleEnd
	filter.ValidToAfter = &cycleStart

	rows, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Info(ctx, "entitlement grant proration; live grants could not be loaded",
			"subscription_id", sub.ID, "error", err.Error())
		return nil
	}

	byFeature := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range rows {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		byFeature[g.FeatureID()] = append(byFeature[g.FeatureID()], g)
	}
	return byFeature
}

// isProratableGrantEC: a grant config whose window is the whole billing period.
// hour/day/week windows are usage-anchored and already open post-attach.
func isProratableGrantEC(ec *entitlement.Entitlement) bool {
	return ec != nil &&
		ec.HasGrantConfig() &&
		ec.GrantDurationUnit == types.EntitlementGrantDurationUnitSubscriptionPeriod
}

// isParallelGrantFeature mirrors grantCandidatesForFeature: one parallel EC
// makes the whole feature parallel.
func isParallelGrantFeature(preAddonECs []*entitlement.Entitlement, addonEC *entitlement.Entitlement) bool {
	if addonEC != nil && addonEC.AggregationMode == types.EntitlementAggregationModeParallel {
		return true
	}

	return lo.SomeBy(preAddonECs, func(ec *entitlement.Entitlement) bool {
		return ec.AggregationMode == types.EntitlementAggregationModeParallel
	})
}

// lowestIDEC mirrors the evaluator's additive tie-break.
func lowestIDEC(ecs []*entitlement.Entitlement) string {
	var lowest *entitlement.Entitlement
	for _, ec := range ecs {
		if ec == nil {
			continue
		}
		if lowest == nil || ec.ID < lowest.ID {
			lowest = ec
		}
	}
	if lowest == nil {
		return ""
	}
	return lowest.ID
}

// lowestIDGrant picks the pool's row deterministically. Additive yields exactly
// one row per feature per cycle; the tie-break only matters for rows left behind
// by an older evaluator, and matching the evaluator's rule keeps both agreeing.
func lowestIDGrant(grants []*entitlementgrant.EntitlementGrant) *entitlementgrant.EntitlementGrant {
	var lowest *entitlementgrant.EntitlementGrant
	for _, g := range grants {
		if g == nil {
			continue
		}
		if lowest == nil || g.EntitlementConfigID < lowest.EntitlementConfigID {
			lowest = g
		}
	}
	return lowest
}
