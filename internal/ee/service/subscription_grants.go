package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// SubscriptionGrantService owns credit grants and entitlement grants for any subscription
// change — addon attach/detach, plan change, future flows — so a change touching several
// sources at once resolves and writes them as one batch instead of one pass per source.
type SubscriptionGrantService interface {
	// Resolve computes every credit grant the change implies. Reads only, so it is safe
	// to call outside a transaction and from preview.
	Resolve(ctx context.Context, req GrantChangeRequest) (*GrantChangeConfig, error)

	// Apply writes the resolved config. Must run inside the caller's transaction.
	Apply(ctx context.Context, cfg *GrantChangeConfig) error
}

// GrantSource is one contributor to the change, with its own dates and provenance.
type GrantSource struct {
	// ChangeType says whether this source reaches the current cycle at all. A custom date is
	// scheduled and re-enters as immediate when it fires.
	ChangeType types.ScheduleType
	// EffectiveDate is when this source joins or leaves: it anchors the source's credit
	// grant chain and dates its entitlement grant proration.
	EffectiveDate time.Time
	// EndDate caps recurring grants at a time-bounded (onetime) addon's boundary.
	EndDate  *time.Time
	Behavior types.ProrationBehavior
	Origin   grantProrationSource
	// AddonID is the source's identity: credit grant templates and entitlement configs are
	// both read from it, and removal targets the grants it materialized.
	AddonID string
}

type GrantChangeRequest struct {
	Sub *subscription.Subscription

	Incoming []GrantSource
	Removed  []GrantSource
}

// GrantChangeConfig is a resolved GrantChangeRequest: what Apply will write, and nothing
// that still needs a read to determine.
type GrantChangeConfig struct {
	sub *subscription.Subscription

	creditGrantsToAdd    []dto.CreateSubscriptionGrantsRequest
	creditGrantsToCancel []dto.CancelFutureSubscriptionGrantsRequest

	// entitlementGrantsToAdd is one prorated segment per feature the incoming sources feed,
	// pooled so two addons landing on one feature share a successor.
	entitlementGrantsToAdd []*entitlementgrant.EntitlementGrant
	entitlementsToRemove   []*entitlement.Entitlement

	incomingECs           []*entitlement.Entitlement
	survivingECsByFeature map[string][]*entitlement.Entitlement

	// entitlementChangeAt is the instant every window of this change is cut at.
	entitlementChangeAt time.Time
}

type subscriptionGrantService struct {
	ServiceParams
}

func NewSubscriptionGrantService(params ServiceParams) SubscriptionGrantService {
	return newSubscriptionGrantService(params)
}

func newSubscriptionGrantService(params ServiceParams) *subscriptionGrantService {
	return &subscriptionGrantService{ServiceParams: params}
}

func (s *subscriptionGrantService) Resolve(ctx context.Context, req GrantChangeRequest) (*GrantChangeConfig, error) {
	if req.Sub == nil {
		return nil, ierr.NewError("subscription is required to resolve grant changes").
			Mark(ierr.ErrValidation)
	}

	cfg := &GrantChangeConfig{sub: req.Sub}

	creditGrantsToAdd, err := s.resolveCreditGrantsToAdd(ctx, req.Sub, req.Incoming)
	if err != nil {
		return nil, err
	}

	cfg.creditGrantsToAdd = creditGrantsToAdd
	cfg.creditGrantsToCancel = resolveCreditGrantCancellations(req.Sub, req.Removed)

	if cfg.entitlementsToRemove, err = s.resolveGrantECs(ctx, req.Removed); err != nil {
		return nil, err
	}

	if cfg.survivingECsByFeature, err = s.resolveSurvivingGrantECs(ctx, req.Sub, cfg.entitlementsToRemove); err != nil {
		return nil, err
	}

	cfg.incomingECs, cfg.entitlementGrantsToAdd, err = s.resolveIncomingGrants(
		ctx,
		req.Sub,
		req.Incoming,
		cfg.survivingECsByFeature,
	)
	if err != nil {
		return nil, err
	}

	cfg.entitlementChangeAt = entitlementChangeAt(req)
	return cfg, nil
}

func (s *subscriptionGrantService) Apply(ctx context.Context, cfg *GrantChangeConfig) error {
	if cfg == nil {
		return nil
	}

	creditGrantService := NewCreditGrantService(s.ServiceParams)
	for _, req := range cfg.creditGrantsToAdd {
		if err := creditGrantService.CreateSubscriptionGrants(ctx, req); err != nil {
			return err
		}
	}

	for _, req := range cfg.creditGrantsToCancel {
		if err := creditGrantService.CancelFutureSubscriptionGrants(ctx, req); err != nil {
			return err
		}
	}

	return s.applyEntitlementGrantChange(ctx, cfg)
}

// entitlementChangeAt is the instant the change cuts this cycle's windows: never in the past,
// since a window already measured cannot be re-cut.
func entitlementChangeAt(req GrantChangeRequest) time.Time {
	at := time.Now().UTC()
	for _, src := range append(append([]GrantSource{}, req.Incoming...), req.Removed...) {
		if src.ChangeType != types.ScheduleTypePeriodEnd {
			at = types.LatestOf(at, src.EffectiveDate)
		}
	}

	return at
}

// resolveCreditGrantsToAdd clones each incoming addon's ADDON-scoped credit grant templates
// into SUBSCRIPTION-scoped requests, then groups the sources that share an anchoring so the
// whole batch materializes in as few passes as there are distinct anchorings.
func (s *subscriptionGrantService) resolveCreditGrantsToAdd(
	ctx context.Context,
	sub *subscription.Subscription,
	incoming []GrantSource,
) ([]dto.CreateSubscriptionGrantsRequest, error) {
	addonIDs := lo.Compact(lo.Map(incoming, func(src GrantSource, _ int) string { return src.AddonID }))
	if len(addonIDs) == 0 {
		return nil, nil
	}

	templates, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByAddonIDs(ctx, lo.Uniq(addonIDs))
	if err != nil {
		return nil, err
	}

	addRequests := make([]dto.CreateSubscriptionGrantsRequest, 0, len(incoming))
	index := make(map[string]int, len(incoming))
	for _, src := range incoming {
		grants := templates[src.AddonID]
		if src.AddonID == "" || len(grants) == 0 {
			continue
		}

		s.Logger.Info(ctx, "addon has credit grants",
			"addon_id", src.AddonID,
			"subscription_id", sub.ID,
			"credit_grants_count", len(grants))

		proration := s.addonCreditGrantProration(ctx, sub, src.EffectiveDate, src.Behavior)
		key := creditGrantGroupKey(src, proration)
		if i, ok := index[key]; ok {
			addRequests[i].Grants = append(addRequests[i].Grants, creditGrantRequestsFromAddon(sub, src.AddonID, grants)...)
			continue
		}

		index[key] = len(addRequests)
		addRequests = append(addRequests, dto.CreateSubscriptionGrantsRequest{
			Subscription:         sub,
			Grants:               creditGrantRequestsFromAddon(sub, src.AddonID, grants),
			StartDate:            src.EffectiveDate,
			EndDate:              src.EndDate,
			FirstPeriodProration: proration,
		})
	}

	return addRequests, nil
}

// creditGrantGroupKey identifies the anchoring materializeCreditGrants pins for a source:
// two sources may share a pass only when all three agree.
func creditGrantGroupKey(src GrantSource, proration *dto.FirstPeriodProration) string {
	end := ""
	if src.EndDate != nil {
		end = src.EndDate.UTC().String()
	}
	prorationKey := ""
	if proration != nil {
		prorationKey = fmt.Sprintf("%v|%v|%v|%s|%s",
			proration.PeriodStart, proration.PeriodEnd, proration.ProrationDate,
			proration.Strategy, proration.Source)
	}

	return fmt.Sprintf("%v|%s|%s", src.EffectiveDate.UTC(), end, prorationKey)
}

func creditGrantRequestsFromAddon(
	sub *subscription.Subscription,
	addonID string,
	templates []*dto.CreditGrantResponse,
) []dto.CreateCreditGrantRequest {
	return lo.Map(templates, func(cg *dto.CreditGrantResponse, _ int) dto.CreateCreditGrantRequest {
		return dto.CreateCreditGrantRequest{
			Name:                   cg.Name,
			Scope:                  types.CreditGrantScopeSubscription,
			Credits:                cg.Credits,
			Cadence:                cg.Cadence,
			ExpirationType:         cg.ExpirationType,
			Priority:               cg.Priority,
			SubscriptionID:         lo.ToPtr(sub.ID),
			AddonID:                lo.ToPtr(addonID), // provenance for targeted removal
			Period:                 cg.Period,
			ExpirationDuration:     cg.ExpirationDuration,
			ExpirationDurationUnit: cg.ExpirationDurationUnit,
			Metadata:               cg.Metadata,
			PeriodCount:            cg.PeriodCount,
			ConversionRate:         cg.ConversionRate,
			TopupConversionRate:    cg.TopupConversionRate,
		}
	})
}

// resolveCreditGrantCancellations collapses the removed sources into one cancellation per
// effective date, since CancelFutureSubscriptionGrants already scopes by a slice of addons.
func resolveCreditGrantCancellations(
	sub *subscription.Subscription,
	removed []GrantSource,
) []dto.CancelFutureSubscriptionGrantsRequest {
	cancellations := make([]dto.CancelFutureSubscriptionGrantsRequest, 0, len(removed))
	index := make(map[int64]int, len(removed))

	for _, src := range removed {
		if src.AddonID == "" {
			continue
		}

		key := src.EffectiveDate.UTC().UnixNano()
		if i, ok := index[key]; ok {
			cancellations[i].AddonIDs = append(cancellations[i].AddonIDs, src.AddonID)
			continue
		}

		index[key] = len(cancellations)
		cancellations = append(cancellations, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: sub.ID,
			AddonIDs:       []string{src.AddonID},
			EffectiveDate:  lo.ToPtr(src.EffectiveDate),
		})
	}

	return cancellations
}

// resolveGrantECs is the flat set of grant configs the sources contribute this cycle.
func (s *subscriptionGrantService) resolveGrantECs(
	ctx context.Context,
	sources []GrantSource,
) ([]*entitlement.Entitlement, error) {
	byAddon := make(map[string][]*entitlement.Entitlement, len(sources))
	resolved := make([]*entitlement.Entitlement, 0, len(sources))

	for _, src := range sources {
		ecs, err := s.sourceGrantECs(ctx, byAddon, src)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ecs...)
	}

	return resolved, nil
}

// sourceGrantECs reads one source's grant-carrying entitlement configs, memoised per addon.
// A period-end source yields nothing: the tick re-derives its window at renewal, so it neither
// cuts this cycle's windows nor feeds their quota.
func (s *subscriptionGrantService) sourceGrantECs(
	ctx context.Context,
	byAddon map[string][]*entitlement.Entitlement,
	src GrantSource,
) ([]*entitlement.Entitlement, error) {
	if src.AddonID == "" || src.ChangeType == types.ScheduleTypePeriodEnd {
		return nil, nil
	}

	if ecs, read := byAddon[src.AddonID]; read {
		return ecs, nil
	}

	ents, err := NewEntitlementService(s.ServiceParams).GetAddonEntitlements(ctx, src.AddonID)
	if err != nil {
		return nil, err
	}

	ecs := lo.Filter(dto.ToEntitlements(ents), func(ec *entitlement.Entitlement, _ int) bool {
		return ec != nil && ec.HasGrantConfig()
	})
	byAddon[src.AddonID] = ecs

	return ecs, nil
}

// resolveSurvivingGrantECs is the subscription's grant configs less the ones leaving in this
// cycle: what decides slot ownership, the cold-start quota and survivorship.
func (s *subscriptionGrantService) resolveSurvivingGrantECs(
	ctx context.Context,
	sub *subscription.Subscription,
	removedECs []*entitlement.Entitlement,
) (map[string][]*entitlement.Entitlement, error) {
	byFeature, err := s.GetSubscriptionGrantECsByFeature(ctx, sub)
	if err != nil {
		return nil, err
	}
	if len(removedECs) == 0 {
		return byFeature, nil
	}

	removedIDs := lo.SliceToMap(removedECs, func(ec *entitlement.Entitlement) (string, bool) {
		return ec.ID, true
	})

	surviving := make(map[string][]*entitlement.Entitlement, len(byFeature))
	for featureID, ecs := range byFeature {
		kept := lo.Filter(ecs, func(ec *entitlement.Entitlement, _ int) bool {
			return ec != nil && !removedIDs[ec.ID]
		})
		if len(kept) > 0 {
			surviving[featureID] = kept
		}
	}

	return surviving, nil
}

// resolveIncomingGrants prorates each incoming source at its own date and behavior, then pools
// the results into one segment per feature.
func (s *subscriptionGrantService) resolveIncomingGrants(
	ctx context.Context,
	sub *subscription.Subscription,
	incoming []GrantSource,
	survivingByFeature map[string][]*entitlement.Entitlement,
) ([]*entitlement.Entitlement, []*entitlementgrant.EntitlementGrant, error) {
	byAddon := make(map[string][]*entitlement.Entitlement, len(incoming))
	ecs := make([]*entitlement.Entitlement, 0, len(incoming))

	pooled := make(map[string]*entitlementgrant.EntitlementGrant, len(incoming))
	features := make([]string, 0, len(incoming))

	for _, src := range incoming {
		srcECs, err := s.sourceGrantECs(ctx, byAddon, src)
		if err != nil {
			return nil, nil, err
		}
		if len(srcECs) == 0 {
			continue
		}
		ecs = append(ecs, srcECs...)

		grants, err := s.resolveGrantProration(
			ctx, sub, srcECs, survivingByFeature, src.EffectiveDate, src.Behavior, src.Origin)
		if err != nil {
			return nil, nil, err
		}

		for _, grant := range grants {
			featureID := grant.FeatureID()
			prior, seen := pooled[featureID]
			if !seen {
				pooled[featureID] = grant
				features = append(features, featureID)
				continue
			}

			// The audit metadata stays the first source's: the pooled row has one coefficient
			// only while the sources share a date, which is what the cycle filter guarantees.
			pooled[featureID] = entitlementgrant.NewEntitlementGrantBuilder(prior).
				WithQuota(prior.Quota.Add(grant.Quota)).
				WithWindow(types.EarliestOf(prior.ValidFrom, grant.ValidFrom), prior.ValidTo).
				Build()
		}
	}

	return ecs, lo.Map(features, func(featureID string, _ int) *entitlementgrant.EntitlementGrant {
		return pooled[featureID]
	}), nil
}

// grantChangeTypeFor classifies a date against the cycle boundary.
func grantChangeTypeFor(sub *subscription.Subscription, effectiveDate time.Time) types.ScheduleType {
	if effectiveDate.Before(sub.CurrentPeriodEnd) {
		return types.ScheduleTypeImmediate
	}

	return types.ScheduleTypePeriodEnd
}

// addonCreditGrantProration resolves the billing period containing effectiveDate so a
// mid-cycle grant can be scaled to the part of that period it actually covers.
// Returns nil when proration does not apply, in which case the grant keeps its full
// credits and its natural anchoring.
//
// Never returns an error: proration is an enhancement to the attach, so an
// unresolvable period downgrades to today's behaviour instead of rejecting the addon.
func (s *subscriptionGrantService) addonCreditGrantProration(
	ctx context.Context,
	sub *subscription.Subscription,
	effectiveDate time.Time,
	behavior types.ProrationBehavior,
) *dto.FirstPeriodProration {
	if behavior != types.ProrationBehaviorCreateProrations {
		return nil
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
		// FindPeriodForDate only walks forward, so a start date in an already-closed
		// period cannot be resolved. Grant in full rather than blocking the attach.
		s.Logger.Info(ctx, "skipping credit grant proration; could not resolve billing period for addon start",
			"subscription_id", sub.ID,
			"effective_date", effectiveDate,
			"current_period_start", sub.CurrentPeriodStart,
			"error", err.Error())
		return nil
	}

	if !effectiveDate.After(p.Start) {
		return nil
	}

	// A grant anchored past the subscription end fails CreateCreditGrant validation,
	// and the grant would be capped to that end anyway.
	if sub.EndDate != nil && p.End.After(lo.FromPtr(sub.EndDate)) {
		return nil
	}

	return &dto.FirstPeriodProration{
		PeriodStart:   p.Start,
		PeriodEnd:     p.End,
		ProrationDate: effectiveDate,
		Strategy:      types.StrategySecondBased,
		Source:        grantProrationSourceAddonAttach.String(),
	}
}
