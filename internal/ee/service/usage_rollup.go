package service

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
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
	UsageRollupCustomer     UsageRollupEntity = "customer"
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

// usageRollupSubscription is what the usage rollup path reads about a subscription besides its prices, cached per
// subscription and period. Exported fields: it is stored as JSON.
type usageRollupSubscription struct {
	ExternalCustomerID string   `json:"external_customer_id"`
	AddonIDs           []string `json:"addon_ids"`
}

// usageRollupEntityPrices is a plan's or addon's active usage prices with their meters by ID.
type usageRollupEntityPrices struct {
	Prices []*price.Price
	Meters map[string]*meter.Meter
}

// usageRollupPrice is the part of a usage price and its meter that the read path prices with, cached per plan or
// addon instead of the full records: a read decodes every price of the plan. Exported fields: it is stored as JSON.
type usageRollupPrice struct {
	ID                 string                       `json:"id"`
	MeterID            string                       `json:"meter_id"`
	Amount             decimal.Decimal              `json:"amount"`
	Currency           string                       `json:"currency"`
	BillingPeriod      types.BillingPeriod          `json:"billing_period"`
	BillingPeriodCount int                          `json:"billing_period_count"`
	BillingModel       types.BillingModel           `json:"billing_model"`
	TierMode           types.BillingTier            `json:"tier_mode,omitempty"`
	Tiers              price.JSONBTiers             `json:"tiers,omitempty"`
	TransformQuantity  price.JSONBTransformQuantity `json:"transform_quantity"`
	MeterName          string                       `json:"meter_name"`
	AggregationType    types.AggregationType        `json:"aggregation_type"`
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
	// Dropped after commit, so a read in between can't cache the old flag or prices again.
	dropCached := func() {
		if entity == UsageRollupPlan || entity == UsageRollupAddon {
			s.RedisCache.ForceCacheDelete(ctx, usageRollupEntityPricesKey(ctx, entityID))
		}
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
	case UsageRollupCustomer:
		return []string{entityID}, nil
	case UsageRollupSubscription:
		sub, err := s.SubRepo.Get(ctx, entityID)
		if err != nil {
			return nil, err
		}
		return []string{sub.CustomerID}, nil
	case UsageRollupPlan, UsageRollupAddon:
		filter := types.NewNoLimitSubscriptionLineItemFilter()
		filter.EntityIDs = []string{entityID}
		filter.ActiveFilter = true
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

func usageRollupSubscriptionKey(ctx context.Context, sub *subscription.Subscription) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageRollupSubscription, sub.ID, sub.CurrentPeriodStart.Unix())
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
	if err != nil || len(subs) == 0 {
		return "", err
	}
	if reason, err := s.customerLevelReason(ctx, customerID, subs); err != nil || reason != "" {
		return reason, err
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	for _, sub := range subs {
		if sub.SubscriptionType != types.SubscriptionTypeStandalone || lo.FromPtr(sub.ParentSubscriptionID) != "" {
			return usageRollupIneligibleNotStandalone, nil
		}
		if sub.HasCommitment() {
			return usageRollupIneligibleCommitment, nil
		}
		usageItems, err := s.usageLineItems(ctx, sub)
		if err != nil {
			return "", err
		}
		if reason := lineItemReason(sub, usageItems); reason != "" {
			return reason, nil
		}
		addonIDs, err := s.activeAddonIDs(ctx, sub)
		if err != nil {
			return "", err
		}
		pricesByMeter, meters, reason, err := s.priceSourceReason(ctx, sub, addonIDs)
		if err != nil || reason != "" {
			return reason, err
		}
		if reason := lineItemPriceReason(usageItems, pricesByMeter); reason != "" {
			return reason, nil
		}
		if reason, err := entitlementReason(ctx, subscriptionService, sub, meters); err != nil || reason != "" {
			return reason, err
		}
	}
	return "", nil
}

// usageLineItems returns sub's metered usage line items active in its current period.
func (s *usageRollupService) usageLineItems(ctx context.Context, sub *subscription.Subscription) ([]*subscription.SubscriptionLineItem, error) {
	lineItems, err := NewMeterUsageService(s.ServiceParams).ListUsageLineItems(ctx, sub, sub.CurrentPeriodStart, false)
	if err != nil {
		return nil, err
	}
	return lo.Filter(lineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != ""
	}), nil
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

// customerLevelReason checks what spans the customer's subscriptions: entitlement grants and commitments.
func (s *usageRollupService) customerLevelReason(ctx context.Context, customerID string, subs []*subscription.Subscription) (usageRollupIneligibleReason, error) {
	grants, err := s.EntitlementGrantRepo.Count(ctx, &types.EntitlementGrantFilter{
		QueryFilter:  types.NewNoLimitQueryFilter(),
		CustomerIDs:  []string{customerID},
		ValidToAfter: lo.ToPtr(clock.Now()),
	})
	if err != nil {
		return "", err
	}
	if grants > 0 {
		return usageRollupIneligibleGrant, nil
	}
	commitments, err := s.CommitmentRepo.Count(ctx, &commitment.Filter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return sub.ID }),
		Statuses:        []types.CommitmentStatus{types.CommitmentStatusActive},
	})
	if err != nil {
		return "", err
	}
	if commitments > 0 {
		return usageRollupIneligibleCommitment, nil
	}
	return "", nil
}

// lineItemReason rejects line items that change pricing within the period or price outside the plan and addons.
func lineItemReason(sub *subscription.Subscription, items []*subscription.SubscriptionLineItem) usageRollupIneligibleReason {
	for _, item := range items {
		switch {
		case item.EntityType == types.SubscriptionLineItemEntityTypeSubscription:
			return usageRollupIneligibleOverride
		case item.HasAnyCommitment():
			return usageRollupIneligibleCommitment
		case item.PriceUnit != nil:
			return usageRollupIneligiblePriceUnit
		case item.StartDate.After(sub.CurrentPeriodStart),
			!item.EndDate.IsZero() && item.EndDate.Before(sub.CurrentPeriodEnd):
			return usageRollupIneligiblePriceChanged
		}
	}
	return ""
}

// priceSourceReason checks the plan's and addons' prices and returns the usage prices the usage rollup path
// charges and their meters, both by meter ID. Any plan or addon price added, edited or ended since the subscription
// started keeps it ineligible, as does a usage price whose meter the usage rollup can't hold.
func (s *usageRollupService) priceSourceReason(
	ctx context.Context,
	sub *subscription.Subscription,
	addonIDs []string,
) (map[string]*price.Price, map[string]*meter.Meter, usageRollupIneligibleReason, error) {
	sourceFilter := types.NewNoLimitPriceFilter()
	sourceFilter.EntityIDs = append([]string{sub.PlanID}, addonIDs...)
	sourceFilter.AllowExpiredPrices = true
	sourcePrices, err := NewPriceService(s.ServiceParams).GetPrices(ctx, sourceFilter)
	if err != nil {
		return nil, nil, "", err
	}
	for _, p := range sourcePrices.Items {
		if priceChangedSince(p.Price, sub.StartDate) {
			return nil, nil, usageRollupIneligiblePriceChanged, nil
		}
	}

	prices, meters, err := s.subscriptionUsagePrices(ctx, sub, addonIDs)
	if err != nil {
		return nil, nil, "", err
	}
	pricesByMeter := make(map[string]*price.Price, len(prices))
	chargedMeters := make(map[string]*meter.Meter, len(prices))
	for _, p := range prices {
		m := meters[p.MeterID]
		_, duplicate := pricesByMeter[p.MeterID]
		if m == nil || duplicate || !usagerollup.SupportsAggregation(m.Aggregation.Type) || price.IsBucketed(p, m) || keepsRunningAggregate(m) {
			return nil, nil, usageRollupIneligibleMeter, nil
		}
		pricesByMeter[p.MeterID] = p
		chargedMeters[p.MeterID] = m
	}
	return pricesByMeter, chargedMeters, "", nil
}

// lineItemPriceReason requires the plan and addon prices the usage rollup path charges to be exactly the
// prices on the subscription's usage line items: an override, or a price the subscription doesn't
// carry, would be charged wrongly.
func lineItemPriceReason(items []*subscription.SubscriptionLineItem, pricesByMeter map[string]*price.Price) usageRollupIneligibleReason {
	carried := make(map[string]bool, len(items))
	for _, item := range items {
		p, charged := pricesByMeter[item.MeterID]
		if !charged || p.ID != item.PriceID {
			return usageRollupIneligibleOverride
		}
		carried[item.PriceID] = true
	}
	for _, p := range pricesByMeter {
		if !carried[p.ID] {
			return usageRollupIneligiblePriceChanged
		}
	}
	return ""
}

// priceChangedSince reports whether a plan or addon price was added, edited or ended after start.
func priceChangedSince(p *price.Price, start time.Time) bool {
	return p.CreatedAt.After(start) || p.UpdatedAt.After(start) || (p.EndDate != nil && p.EndDate.After(start))
}

// entitlementReason rejects a subscription whose enabled metered entitlements cover a charged meter:
// billing nets their allowance off the usage.
func entitlementReason(
	ctx context.Context,
	subscriptionService SubscriptionService,
	sub *subscription.Subscription,
	meters map[string]*meter.Meter,
) (usageRollupIneligibleReason, error) {
	if len(meters) == 0 {
		return "", nil
	}
	entitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlementsForSubscription(ctx, sub, nil)
	if err != nil {
		return "", err
	}
	for _, f := range entitlements.Features {
		if f.Feature == nil || f.Entitlement == nil || !f.Entitlement.IsEnabled {
			continue
		}
		if _, charged := meters[f.Feature.MeterID]; charged && types.FeatureType(f.Feature.Type) == types.FeatureTypeMetered {
			return usageRollupIneligibleEntitlement, nil
		}
	}
	return "", nil
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
	cached, err := s.usageRollupSubscription(ctx, sub)
	if err != nil {
		return nil, nil, err
	}
	pricesByMeter := make(map[string]*price.Price)
	meters := make(map[string]*meter.Meter)
	for _, entityID := range append([]string{sub.PlanID}, cached.AddonIDs...) {
		entityPrices, err := s.cachedEntityUsagePrices(ctx, entityID)
		if err != nil {
			return nil, nil, err
		}
		for _, cp := range entityPrices {
			p := cp.price()
			if p.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
				pricesByMeter[p.MeterID] = p
				meters[p.MeterID] = cp.meter()
			}
		}
	}
	return pricesByMeter, meters, nil
}

// price rebuilds the fields of the usage price that pricing and the eligibility filter read.
func (cp *usageRollupPrice) price() *price.Price {
	return &price.Price{
		ID:                 cp.ID,
		MeterID:            cp.MeterID,
		Type:               types.PRICE_TYPE_USAGE,
		Amount:             cp.Amount,
		Currency:           cp.Currency,
		BillingPeriod:      cp.BillingPeriod,
		BillingPeriodCount: cp.BillingPeriodCount,
		BillingModel:       cp.BillingModel,
		TierMode:           cp.TierMode,
		Tiers:              cp.Tiers,
		TransformQuantity:  cp.TransformQuantity,
	}
}

// meter rebuilds the fields of the price's meter that the quantity read and the charge read.
func (cp *usageRollupPrice) meter() *meter.Meter {
	return &meter.Meter{ID: cp.MeterID, Name: cp.MeterName, Aggregation: meter.Aggregation{Type: cp.AggregationType}}
}

// usageRollupSubscription reads the subscription's external customer ID and active addons through the cache.
// The key holds the period, so a rollover reads them afresh; within a period an addon change makes the
// customer ineligible.
func (s *usageRollupService) usageRollupSubscription(ctx context.Context, sub *subscription.Subscription) (*usageRollupSubscription, error) {
	key := usageRollupSubscriptionKey(ctx, sub)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if value, ok := cache.UnmarshalCacheValue[usageRollupSubscription](cached); ok {
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
	value := &usageRollupSubscription{ExternalCustomerID: cust.ExternalID, AddonIDs: addonIDs}
	s.RedisCache.ForceCacheSet(ctx, key, value, usageRollupCacheTTL)
	return value, nil
}

// subscriptionUsagePrices returns the active usage prices of sub's plan and addons that match its
// currency and billing period, and the meters of all their usage prices by ID.
func (s *usageRollupService) subscriptionUsagePrices(ctx context.Context, sub *subscription.Subscription, addonIDs []string) ([]*price.Price, map[string]*meter.Meter, error) {
	var prices []*price.Price
	meters := make(map[string]*meter.Meter)
	for _, entityID := range append([]string{sub.PlanID}, addonIDs...) {
		entityPrices, err := s.entityUsagePrices(ctx, entityID)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range entityPrices.Prices {
			if p.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
				prices = append(prices, p)
			}
		}
		maps.Copy(meters, entityPrices.Meters)
	}
	return prices, meters, nil
}

// cachedEntityUsagePrices reads the compact form of a plan's or addon's active usage prices through the cache.
func (s *usageRollupService) cachedEntityUsagePrices(ctx context.Context, entityID string) ([]*usageRollupPrice, error) {
	key := usageRollupEntityPricesKey(ctx, entityID)
	if cached, found := s.RedisCache.ForceCacheGet(ctx, key); found {
		if entityPrices, ok := cache.UnmarshalCacheValue[[]*usageRollupPrice](cached); ok {
			return *entityPrices, nil
		}
	}
	entityPrices, err := s.entityUsagePrices(ctx, entityID)
	if err != nil {
		return nil, err
	}
	compact := make([]*usageRollupPrice, 0, len(entityPrices.Prices))
	for _, p := range entityPrices.Prices {
		m := entityPrices.Meters[p.MeterID]
		if m == nil {
			continue
		}
		compact = append(compact, &usageRollupPrice{
			ID:                 p.ID,
			MeterID:            p.MeterID,
			Amount:             p.Amount,
			Currency:           p.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: p.BillingPeriodCount,
			BillingModel:       p.BillingModel,
			TierMode:           p.TierMode,
			Tiers:              p.Tiers,
			TransformQuantity:  p.TransformQuantity,
			MeterName:          m.Name,
			AggregationType:    m.Aggregation.Type,
		})
	}
	s.RedisCache.ForceCacheSet(ctx, key, compact, usageRollupCacheTTL)
	return compact, nil
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
	cached, err := s.usageRollupSubscription(ctx, sub)
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
			ExternalCustomerIDs: []string{cached.ExternalCustomerID},
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
	hourlyUsage, err := s.HourlyUsageRepo.ListCustomerHourlyUsage(ctx, cached.ExternalCustomerID, hours)
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
	// A watermark from before this period means nothing of it is counted yet: totals start at the period start.
	if len(watermarks) == 1 {
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
