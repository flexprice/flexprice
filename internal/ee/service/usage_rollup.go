package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// UsageRollupService keeps the per-customer flag that puts a customer's wallet reads on the usage rollup path,
// and serves that path its plan and addon prices and usage quantities.
type UsageRollupService interface {
	// ClearEligibility clears, in ctx's transaction, the flag of every customer whose billing a write to the entity
	// can change, so their next wallet read evaluates it again. Failures are logged, never returned: callers are
	// writes that must not fail on this.
	ClearEligibility(ctx context.Context, entity UsageRollupEntity, entityID string)
	// IsUsageRollupEligible reports whether this read of sub is on the usage rollup path: a wallet read, for a tenant
	// with the usage rollup enabled, and a customer whose flag is set.
	IsUsageRollupEligible(ctx context.Context, sub *subscription.Subscription, source types.UsageSource) bool
	// UsagePrices returns the usage prices of sub's plan and addons that match its currency and billing
	// period, and their meters, both by meter ID. Call it only for a subscription on the usage rollup path.
	UsagePrices(ctx context.Context, sub *subscription.Subscription) (map[string]*price.Price, map[string]*meter.Meter, error)
	// PeriodQuantities returns sub's current-period quantity of each meter in meters that has usage.
	// Call it only for a subscription on the usage rollup path.
	PeriodQuantities(ctx context.Context, sub *subscription.Subscription, meters map[string]*meter.Meter) (map[string]decimal.Decimal, error)
}

// UsageRollupEntity is the kind of entity whose write changes which customers are on the usage rollup path.
type UsageRollupEntity string

const (
	UsageRollupSubscription UsageRollupEntity = "subscription"
	UsageRollupPlan         UsageRollupEntity = "plan"
	UsageRollupAddon        UsageRollupEntity = "addon"
)

type usageRollupIneligibleReason string

const (
	usageRollupIneligibleNotStandalone usageRollupIneligibleReason = "not_standalone"
	usageRollupIneligibleCommitment    usageRollupIneligibleReason = "commitment"
	usageRollupIneligibleGrant         usageRollupIneligibleReason = "grant"
	usageRollupIneligibleEntitlement   usageRollupIneligibleReason = "entitlement"
	usageRollupIneligibleOverride      usageRollupIneligibleReason = "override"
	usageRollupIneligiblePriceChanged  usageRollupIneligibleReason = "price_changed"
	usageRollupIneligibleMeter         usageRollupIneligibleReason = "meter"
	usageRollupIneligiblePriceUnit     usageRollupIneligibleReason = "price_unit"
)

const (
	// usageRollupCacheTTL bounds how long a change no refresh caught can leave stale cached prices or addons.
	usageRollupCacheTTL = 10 * time.Minute
	// usageRollupEligibilityTTL bounds how long a cached flag outlives a write that cleared it.
	usageRollupEligibilityTTL = time.Hour
	// usageSnapshotTTL backs up the checkpoint job, which drops a period's snapshot after updating it.
	usageSnapshotTTL = time.Hour
)

// Outcomes of a wallet read for a tenant that reads from the usage rollup, recorded on usage_rollup.reads.
const (
	usageRollupReadServed     = "fast"
	usageRollupReadIneligible = "ineligible"
	usageRollupReadError      = "error"
)

// customerAndAddons is the subscription's external customer ID and the addons active in its current period,
// cached per subscription and period. Exported fields: it is stored as JSON.
type customerAndAddons struct {
	ExternalCustomerID string   `json:"external_customer_id"`
	AddonIDs           []string `json:"addon_ids"`
}

// usageRollupEntityPrices is a plan's or addon's active usage prices with their meters by ID. Exported fields: it
// is stored as JSON.
type usageRollupEntityPrices struct {
	Prices []*price.Price          `json:"prices"`
	Meters map[string]*meter.Meter `json:"meters"`
}

// usageSnapshot is a period's checkpoint: the time its totals run to and each meter's total. Exported fields: it is
// stored as JSON.
type usageSnapshot struct {
	AggregatedUntil time.Time                  `json:"aggregated_until"`
	Totals          map[string]decimal.Decimal `json:"totals"`
}

type usageRollupService struct {
	ServiceParams
}

// NewUsageRollupService returns the service behind the usage rollup path.
func NewUsageRollupService(params ServiceParams) UsageRollupService {
	return &usageRollupService{ServiceParams: params}
}

func (s *usageRollupService) ClearEligibility(ctx context.Context, entity UsageRollupEntity, entityID string) {
	if !s.usageRollupEnabled(ctx) {
		return
	}
	// Callers clear after their write commits; a client gone mid-request must not leave the flag set.
	ctx = context.WithoutCancel(ctx)
	customerIDs, err := s.eligibilityCustomers(ctx, entity, entityID)
	if err != nil {
		s.Logger.Error(ctx, "usage rollup: failed to resolve customers to clear", "error", err, "entity", entity, "entity_id", entityID)
		return
	}
	if err := s.CustomerRepo.ClearUsageRollupEligible(ctx, customerIDs); err != nil {
		s.Logger.Error(ctx, "usage rollup: failed to clear eligibility", "error", err, "entity", entity, "entity_id", entityID)
		return
	}
	if s.RedisCache == nil {
		return
	}
	// Dropped after commit, so a read in between can't cache the old flag again.
	dropCached := func() {
		for _, customerID := range customerIDs {
			s.RedisCache.ForceCacheDelete(ctx, usageRollupEligibilityKey(ctx, customerID))
		}
	}
	if !types.RegisterPostCommit(ctx, dropCached) {
		dropCached()
	}
}

// eligibilityCustomers returns the customers whose billing a write to the entity can change.
func (s *usageRollupService) eligibilityCustomers(ctx context.Context, entity UsageRollupEntity, entityID string) ([]string, error) {
	switch entity {
	case UsageRollupSubscription:
		sub, err := s.SubRepo.Get(ctx, entityID)
		if err != nil {
			return nil, err
		}
		return []string{sub.CustomerID}, nil
	case UsageRollupPlan:
		subs, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
			QueryFilter:        types.NewNoLimitQueryFilter(),
			PlanID:             entityID,
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing},
		})
		if err != nil {
			return nil, err
		}
		return lo.Uniq(lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return sub.CustomerID })), nil
	case UsageRollupAddon:
		// An addon has few prices, so its live line items are few per subscription and carry the customer.
		filter := types.NewNoLimitSubscriptionLineItemFilter()
		filter.EntityIDs = []string{entityID}
		filter.ActiveFilter = true
		filter.CurrentPeriodStart = lo.ToPtr(clock.Now())
		lineItems, err := s.SubscriptionLineItemRepo.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		return lo.Uniq(lo.Map(lineItems, func(li *subscription.SubscriptionLineItem, _ int) string { return li.CustomerID })), nil
	default:
		return nil, fmt.Errorf("usage rollup: unknown eligibility entity %q", entity)
	}
}

// usageRollupEnabled reports whether the tenant in ctx writes usage rollup; eligibility is kept only for those.
func (s *usageRollupService) usageRollupEnabled(ctx context.Context) bool {
	return s.Config != nil && s.Config.UsageRollup.IsEnabledForTenant(types.GetTenantID(ctx))
}

func usageRollupEligibilityKey(ctx context.Context, customerID string) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageRollupEligibility, customerID)
}

func customerAndAddonsKey(ctx context.Context, sub *subscription.Subscription) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageRollupCustomerAddons, sub.ID, sub.CurrentPeriodStart.Unix())
}

func usageRollupEntityPricesKey(ctx context.Context, entityID string) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageRollupEntityPrices, entityID)
}

// ineligibleReason evaluates the customer's active subscriptions and returns the first reason the
// customer can't be on the usage rollup path, or "" when it can.
func (s *usageRollupService) ineligibleReason(ctx context.Context, customerID string) (usageRollupIneligibleReason, error) {
	subs, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CustomerID:         customerID,
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing},
	})
	if err != nil {
		return "", err
	}
	for _, sub := range subs {
		if reason, err := s.subscriptionIneligibleReason(ctx, sub); err != nil || reason != "" {
			return reason, err
		}
	}
	return "", nil
}

// subscriptionIneligibleReason checks one subscription: its shape, then its plan and active addons' prices,
// then its line items against those prices, then the entitlements on all of them.
func (s *usageRollupService) subscriptionIneligibleReason(ctx context.Context, sub *subscription.Subscription) (usageRollupIneligibleReason, error) {
	if sub.SubscriptionType != types.SubscriptionTypeStandalone || lo.FromPtr(sub.ParentSubscriptionID) != "" {
		return usageRollupIneligibleNotStandalone, nil
	}
	if reason, err := s.commitmentReason(ctx, sub); err != nil || reason != "" {
		return reason, err
	}
	addonIDs, err := s.activeAddonIDs(ctx, sub)
	if err != nil {
		return "", err
	}
	sources := append([]string{sub.PlanID}, addonIDs...)
	pricesByMeter, meters, reason, err := s.priceReason(ctx, sub, sources)
	if err != nil || reason != "" {
		return reason, err
	}
	if reason, err := s.lineItemReason(ctx, sub, pricesByMeter); err != nil || reason != "" {
		return reason, err
	}
	return s.entitlementReason(ctx, sub, append(sources, sub.ID), meters)
}

// commitmentReason rejects a subscription with a commitment in its current period: billing nets usage against it,
// including a commitment terminated mid-period, up to its termination.
func (s *usageRollupService) commitmentReason(ctx context.Context, sub *subscription.Subscription) (usageRollupIneligibleReason, error) {
	if sub.HasCommitment() {
		return usageRollupIneligibleCommitment, nil
	}
	commitments, err := newCommitmentService(s.ServiceParams).ListOverlapping(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	if err != nil {
		return "", err
	}
	for _, c := range commitments {
		if c.TerminatedAt == nil || c.TerminatedAt.After(sub.CurrentPeriodStart) {
			return usageRollupIneligibleCommitment, nil
		}
	}
	return "", nil
}

// activeAddonIDs returns the addons associated with sub in its current period.
func (s *usageRollupService) activeAddonIDs(ctx context.Context, sub *subscription.Subscription) ([]string, error) {
	associations, err := NewAddonService(s.ServiceParams).GetActiveAddonAssociation(ctx, dto.GetActiveAddonAssociationRequest{
		EntityID:   sub.ID,
		EntityType: types.AddonAssociationEntityTypeSubscription,
		StartDate:  lo.ToPtr(sub.CurrentPeriodStart),
		EndDate:    lo.ToPtr(sub.CurrentPeriodEnd),
	})
	if err != nil {
		return nil, err
	}
	return lo.Uniq(lo.Map(associations.Items, func(a *dto.AddonAssociationResponse, _ int) string { return a.AddonID })), nil
}

// priceReason reads the plan's and addons' prices, ended ones included, and returns the usage prices the
// usage rollup path charges and their meters, both by meter ID. Any price added, edited or ended since the
// subscription started, or a usage price whose meter the usage rollup can't hold, makes it ineligible.
func (s *usageRollupService) priceReason(
	ctx context.Context,
	sub *subscription.Subscription,
	sources []string,
) (map[string]*price.Price, map[string]*meter.Meter, usageRollupIneligibleReason, error) {
	filter := types.NewNoLimitPriceFilter()
	filter.EntityIDs = sources
	filter.AllowExpiredPrices = true
	filter.Expand = lo.ToPtr(string(types.ExpandMeters))
	list, err := NewPriceService(s.ServiceParams).GetPrices(ctx, filter)
	if err != nil {
		return nil, nil, "", err
	}
	for _, p := range list.Items {
		if priceChangedSince(p.Price, sub.StartDate) {
			return nil, nil, usageRollupIneligiblePriceChanged, nil
		}
	}

	pricesByMeter := make(map[string]*price.Price)
	meters := make(map[string]*meter.Meter)
	for _, p := range list.Items {
		// A price with an end date left here ended before the subscription started.
		if p.Type != types.PRICE_TYPE_USAGE || p.EndDate != nil ||
			!p.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
			continue
		}
		_, duplicate := pricesByMeter[p.MeterID]
		if p.Meter == nil || duplicate {
			return nil, nil, usageRollupIneligibleMeter, nil
		}
		m := p.Meter.ToMeter()
		if !usagerollup.SupportsAggregation(m.Aggregation.Type) || price.IsBucketed(p.Price, m) || keepsRunningAggregate(m) {
			return nil, nil, usageRollupIneligibleMeter, nil
		}
		pricesByMeter[p.MeterID] = p.Price
		meters[p.MeterID] = m
	}
	return pricesByMeter, meters, "", nil
}

// priceChangedSince reports whether a plan or addon price was added, edited or ended after start.
func priceChangedSince(p *price.Price, start time.Time) bool {
	return p.CreatedAt.After(start) || p.UpdatedAt.After(start) || (p.EndDate != nil && p.EndDate.After(start))
}

// lineItemReason requires the subscription's usage line items to carry exactly the charged plan and addon
// prices for the whole current period, with no commitment or price unit.
func (s *usageRollupService) lineItemReason(
	ctx context.Context,
	sub *subscription.Subscription,
	pricesByMeter map[string]*price.Price,
) (usageRollupIneligibleReason, error) {
	filter := types.NewNoLimitSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{sub.ID}
	filter.ActiveFilter = true
	filter.CurrentPeriodStart = lo.ToPtr(sub.CurrentPeriodStart)
	items, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}
	carried := make(map[string]bool, len(items))
	for _, item := range items {
		if item.PriceType != types.PRICE_TYPE_USAGE || item.MeterID == "" {
			continue
		}
		p, charged := pricesByMeter[item.MeterID]
		switch {
		case item.EntityType == types.SubscriptionLineItemEntityTypeSubscription:
			return usageRollupIneligibleOverride, nil
		case item.HasAnyCommitment():
			return usageRollupIneligibleCommitment, nil
		case item.PriceUnit != nil:
			return usageRollupIneligiblePriceUnit, nil
		case item.StartDate.After(sub.CurrentPeriodStart),
			!item.EndDate.IsZero() && item.EndDate.Before(sub.CurrentPeriodEnd):
			return usageRollupIneligiblePriceChanged, nil
		case !charged || p.ID != item.PriceID:
			return usageRollupIneligibleOverride, nil
		}
		carried[item.PriceID] = true
	}
	for _, p := range pricesByMeter {
		if !carried[p.ID] {
			return usageRollupIneligiblePriceChanged, nil
		}
	}
	return "", nil
}

// entitlementReason rejects a subscription whose usage an entitlement changes: a subscription or group grant, or a
// metered feature on a charged meter. It checks the enabled entitlements of entityIDs and the grants billing
// applies to the current period, which outlive an entitlement disabled or deleted mid-period.
func (s *usageRollupService) entitlementReason(
	ctx context.Context,
	sub *subscription.Subscription,
	entityIDs []string,
	meters map[string]*meter.Meter,
) (usageRollupIneligibleReason, error) {
	filter := types.NewNoLimitEntitlementFilter()
	filter.EntityIDs = entityIDs
	filter.IsEnabled = lo.ToPtr(true)
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{
		types.EntitlementGrantScopeFeature, types.EntitlementGrantScopeSubscription, types.EntitlementGrantScopeGroup,
	}
	entitlements, err := s.EntitlementRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}
	var meteredFeatureIDs []string
	for _, e := range entitlements {
		if scope := e.EffectiveGrantScope(); scope == types.EntitlementGrantScopeSubscription || scope == types.EntitlementGrantScopeGroup {
			return usageRollupIneligibleEntitlement, nil
		}
		if e.FeatureType == types.FeatureTypeMetered && e.FeatureID != "" {
			meteredFeatureIDs = append(meteredFeatureIDs, e.FeatureID)
		}
	}
	grantFeatureIDs, reason, err := s.periodGrantFeatureIDs(ctx, sub)
	if err != nil || reason != "" {
		return reason, err
	}
	meteredFeatureIDs = append(meteredFeatureIDs, grantFeatureIDs...)
	if len(meteredFeatureIDs) == 0 {
		return "", nil
	}
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = lo.Uniq(meteredFeatureIDs)
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return "", err
	}
	for _, f := range features {
		if _, charged := meters[f.MeterID]; charged {
			return usageRollupIneligibleEntitlement, nil
		}
	}
	return "", nil
}

// periodGrantFeatureIDs reads the grants billing applies to sub's current period, with billing's filter. A
// subscription or group grant rejects sub; a feature grant returns its entitlement's feature, whatever that
// entitlement's state now.
func (s *usageRollupService) periodGrantFeatureIDs(ctx context.Context, sub *subscription.Subscription) ([]string, usageRollupIneligibleReason, error) {
	grants, err := s.EntitlementGrantRepo.List(ctx, types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithCycleOverlap(sub.CurrentPeriodStart, sub.CurrentPeriodEnd))
	if err != nil {
		return nil, "", err
	}
	var featureIDs []string
	checked := make(map[string]bool, len(grants))
	for _, grant := range grants {
		if grant.ScopeEntityType != types.EntitlementGrantScopeFeature {
			return nil, usageRollupIneligibleEntitlement, nil
		}
		if checked[grant.EntitlementConfigID] {
			continue
		}
		checked[grant.EntitlementConfigID] = true
		ent, err := s.EntitlementRepo.Get(ctx, grant.EntitlementConfigID)
		if ierr.IsNotFound(err) {
			return nil, usageRollupIneligibleEntitlement, nil // can't tell which meter the grant discounts
		}
		if err != nil {
			return nil, "", err
		}
		if ent.FeatureID != "" {
			featureIDs = append(featureIDs, ent.FeatureID)
		}
	}
	return featureIDs, "", nil
}

func (s *usageRollupService) IsUsageRollupEligible(ctx context.Context, sub *subscription.Subscription, source types.UsageSource) bool {
	if source != types.UsageSourceWallet || !s.usageRollupEnabled(ctx) {
		return false
	}
	eligible, err := s.isEligible(ctx, sub.CustomerID)
	if err != nil {
		s.Logger.Error(ctx, "usage rollup: failed to read eligibility, reading Postgres and ClickHouse", "error", err, "customer_id", sub.CustomerID)
		return false
	}
	return eligible
}

// isEligible reads the flag from the cache, then Postgres, and evaluates it when a write has cleared it.
func (s *usageRollupService) isEligible(ctx context.Context, customerID string) (bool, error) {
	key := usageRollupEligibilityKey(ctx, customerID)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if eligible, ok := cache.UnmarshalCacheValue[bool](cached); ok {
			return *eligible, nil
		}
	}
	saved, err := s.CustomerRepo.GetUsageRollupEligible(ctx, customerID)
	if err != nil {
		return false, err
	}
	if saved == nil {
		reason, err := s.ineligibleReason(ctx, customerID)
		if err != nil {
			return false, err
		}
		saved = lo.ToPtr(reason == "")
		if err := s.CustomerRepo.SetUsageRollupEligible(ctx, customerID, *saved); err != nil {
			return false, err
		}
	}
	s.RedisCache.ForceCacheSet(ctx, key, *saved, usageRollupEligibilityTTL)
	return *saved, nil
}

func (s *usageRollupService) UsagePrices(ctx context.Context, sub *subscription.Subscription) (map[string]*price.Price, map[string]*meter.Meter, error) {
	customerAndAddons, err := s.cachedCustomerAndAddons(ctx, sub)
	if err != nil {
		return nil, nil, err
	}
	pricesByMeter := make(map[string]*price.Price)
	meters := make(map[string]*meter.Meter)
	for _, entityID := range append([]string{sub.PlanID}, customerAndAddons.AddonIDs...) {
		entityPrices, err := s.cachedEntityUsagePrices(ctx, entityID)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range entityPrices.Prices {
			m := entityPrices.Meters[p.MeterID]
			if m == nil || !p.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
				continue
			}
			pricesByMeter[p.MeterID] = p
			meters[p.MeterID] = m
		}
	}
	return pricesByMeter, meters, nil
}

// cachedCustomerAndAddons reads the subscription's external customer ID and active addons through the cache.
// The key holds the period, so a rollover reads them afresh; within a period an addon change makes the
// customer ineligible.
func (s *usageRollupService) cachedCustomerAndAddons(ctx context.Context, sub *subscription.Subscription) (*customerAndAddons, error) {
	key := customerAndAddonsKey(ctx, sub)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if value, ok := cache.UnmarshalCacheValue[customerAndAddons](cached); ok {
			return value, nil
		}
	}
	cust, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
	if err != nil {
		return nil, err
	}
	addonIDs, err := s.activeAddonIDs(ctx, sub)
	if err != nil {
		return nil, err
	}
	value := &customerAndAddons{ExternalCustomerID: cust.ExternalID, AddonIDs: addonIDs}
	s.RedisCache.ForceCacheSet(ctx, key, value, usageRollupCacheTTL)
	return value, nil
}

// cachedEntityUsagePrices reads a plan's or addon's active usage prices and their meters through the cache.
func (s *usageRollupService) cachedEntityUsagePrices(ctx context.Context, entityID string) (*usageRollupEntityPrices, error) {
	key := usageRollupEntityPricesKey(ctx, entityID)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if entityPrices, ok := cache.UnmarshalCacheValue[usageRollupEntityPrices](cached); ok {
			return entityPrices, nil
		}
	}
	entityPrices, err := s.entityUsagePrices(ctx, entityID)
	if err != nil {
		return nil, err
	}
	s.RedisCache.ForceCacheSet(ctx, key, entityPrices, usageRollupCacheTTL)
	return entityPrices, nil
}

// entityUsagePrices loads a plan's or addon's active usage prices and their meters from Postgres.
func (s *usageRollupService) entityUsagePrices(ctx context.Context, entityID string) (*usageRollupEntityPrices, error) {
	filter := types.NewNoLimitPriceFilter()
	filter.EntityIDs = []string{entityID}
	filter.Expand = lo.ToPtr(string(types.ExpandMeters))
	list, err := NewPriceService(s.ServiceParams).GetPrices(ctx, filter)
	if err != nil {
		return nil, err
	}
	entityPrices := &usageRollupEntityPrices{Meters: make(map[string]*meter.Meter)}
	for _, p := range list.Items {
		if p.Type != types.PRICE_TYPE_USAGE {
			continue
		}
		entityPrices.Prices = append(entityPrices.Prices, p.Price)
		if p.Meter != nil {
			entityPrices.Meters[p.MeterID] = p.Meter.ToMeter()
		}
	}
	return entityPrices, nil
}

// PeriodQuantities reads the checkpoint total, then ClickHouse for any range neither the checkpoint nor
// Redis holds, then Redis.
func (s *usageRollupService) PeriodQuantities(ctx context.Context, sub *subscription.Subscription, meters map[string]*meter.Meter) (map[string]decimal.Decimal, error) {
	customerAndAddons, err := s.cachedCustomerAndAddons(ctx, sub)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.periodSnapshot(ctx, sub.CustomerID, sub.CurrentPeriodStart)
	if err != nil {
		return nil, err
	}
	from := sub.CurrentPeriodStart
	if !snapshot.AggregatedUntil.IsZero() {
		from = snapshot.AggregatedUntil
	}
	now := clock.Now()
	end, ongoing := now, sub.CurrentPeriodEnd.After(now)
	if !ongoing {
		end = sub.CurrentPeriodEnd
	}

	quantities := make(map[string]decimal.Decimal)
	for meterID, total := range snapshot.Totals {
		if _, charged := meters[meterID]; charged {
			quantities[meterID] = total
		}
	}
	// Hours past the checkpoint but before the cutoff (the job hasn't reached this customer yet, or
	// missed a run) are still in Redis; read them there instead of ClickHouse. The checkpoint replaces
	// them with ClickHouse's totals when it lands.
	redisFrom := usagerollup.Cutoff(now)
	if !snapshot.AggregatedUntil.IsZero() && !snapshot.AggregatedUntil.Before(usagerollup.OldestRedisHour(now)) {
		redisFrom = lo.Earliest(redisFrom, snapshot.AggregatedUntil)
	}
	clickhouseWindows, hours := planUsageReads(from, end, redisFrom, ongoing)
	for _, window := range clickhouseWindows {
		metrics.RecordCounter(ctx, metrics.UsageRollupGapQueries, 1)
		totals, err := s.MeterUsageRepo.GetMeterTotals(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: []string{customerAndAddons.ExternalCustomerID},
			UseFinal:            true,
			StartTime:           window.start,
			EndTime:             window.end,
		})
		if err != nil {
			return nil, err
		}
		for _, total := range totals {
			addMeterTotal(quantities, meters, total)
		}
	}
	hourlyUsage, err := s.HourlyUsageRepo.ListCustomerHourlyUsage(ctx, customerAndAddons.ExternalCustomerID, hours)
	if err != nil {
		return nil, err
	}
	for _, b := range hourlyUsage {
		addMeterTotal(quantities, meters, &events.MeterTotals{MeterID: b.MeterID, Sum: b.Quantity, Count: uint64(b.Quantity.IntPart()), Max: b.Quantity})
	}
	return quantities, nil
}

// addMeterTotal folds one window's total into the quantity of its meter, skipping meters not in meters.
func addMeterTotal(quantities map[string]decimal.Decimal, meters map[string]*meter.Meter, total *events.MeterTotals) {
	m, charged := meters[total.MeterID]
	if !charged {
		return
	}
	stored, hasStored := quantities[total.MeterID]
	quantities[total.MeterID] = addWindowTotal(m.Aggregation.Type, stored, hasStored, total)
}

// planUsageReads splits [from, end) into ranges read from ClickHouse and whole hours read from Redis.
// Redis holds hours from the cutoff on; ClickHouse reads what is before the cutoff and any partial hour
// at either edge, whose Redis hour also holds usage outside the period. An ongoing period's current hour is
// read from Redis, as it holds nothing after now.
func planUsageReads(from, end, cutoff time.Time, ongoing bool) ([]usageWindow, []time.Time) {
	if !end.After(from) {
		return nil, nil
	}
	firstHour := ceilHour(lo.Latest(from, cutoff))
	afterLastHour := end.Truncate(time.Hour)
	if ongoing {
		afterLastHour = ceilHour(end)
	}
	if !afterLastHour.After(firstHour) {
		return []usageWindow{{start: from, end: end}}, nil
	}

	var clickhouse []usageWindow
	if from.Before(firstHour) {
		clickhouse = append(clickhouse, usageWindow{start: from, end: firstHour})
	}
	if afterLastHour.Before(end) {
		clickhouse = append(clickhouse, usageWindow{start: afterLastHour, end: end})
	}
	var hours []time.Time
	for hour := firstHour; hour.Before(afterLastHour); hour = hour.Add(time.Hour) {
		hours = append(hours, hour)
	}
	return clickhouse, hours
}

func ceilHour(t time.Time) time.Time {
	floor := t.Truncate(time.Hour)
	if floor.Equal(t) {
		return t
	}
	return floor.Add(time.Hour)
}

func usageSnapshotKey(ctx context.Context, customerID string, periodStart time.Time) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageSnapshot, customerID, periodStart.Unix())
}

// periodSnapshot reads a period's checkpoint through the snapshot cache. A period with no checkpoint has a zero
// AggregatedUntil.
func (s *usageRollupService) periodSnapshot(ctx context.Context, customerID string, periodStart time.Time) (*usageSnapshot, error) {
	key := usageSnapshotKey(ctx, customerID, periodStart)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if snapshot, ok := cache.UnmarshalCacheValue[usageSnapshot](cached); ok {
			return snapshot, nil
		}
	}
	watermarks, err := s.MeterUsageCheckpointRepo.ListMeterUsageCheckpointWatermarks(ctx, []string{customerID})
	if err != nil {
		return nil, err
	}
	snapshot := &usageSnapshot{}
	// At most one row (unique per customer); none means never checkpointed. A watermark from before this period
	// means nothing of it is counted yet: totals start at the period start.
	if len(watermarks) > 0 {
		snapshot.AggregatedUntil = types.LatestOf(watermarks[0].AggregatedUntil, periodStart)
	}
	rows, err := s.MeterUsageCheckpointRepo.ListPeriodTotals(ctx, customerID, periodStart)
	if err != nil {
		return nil, err
	}
	snapshot.Totals = make(map[string]decimal.Decimal, len(rows))
	for _, row := range rows {
		snapshot.Totals[row.MeterID] = row.Total
	}
	s.RedisCache.ForceCacheSet(ctx, key, snapshot, usageSnapshotTTL)
	return snapshot, nil
}
