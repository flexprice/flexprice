package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// evaluateEntitlementGrantsForCustomer refreshes usage and fires alerts for
// each live grant. Alert-log dedup makes it safe under Temporal retries.
func (s *alertService) evaluateEntitlementGrantsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	grants []*entitlementgrant.EntitlementGrant,
	at time.Time,
) error {
	if len(grants) == 0 {
		return nil
	}

	featureGrants := make([]*entitlementgrant.EntitlementGrant, 0, len(grants))
	for _, g := range grants {
		if g.IsFeatureScoped() {
			featureGrants = append(featureGrants, g)
			continue
		}
		s.Logger.Debug(ctx, "entitlement grant evaluation: skipping non-feature scope",
			"grant_id", g.ID,
			"scope_entity_type", g.ScopeEntityType,
		)
	}
	if len(featureGrants) == 0 {
		return nil
	}

	meta, err := s.loadEntitlementGrantMeta(ctx, featureGrants)
	if err != nil {
		return err
	}

	alertLogsSvc := NewAlertLogsService(s.ServiceParams)
	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	priceSvc := NewPriceService(s.ServiceParams)

	extIDsBySub := make(map[string][]string, len(meta.subsByID))

	for _, g := range featureGrants {
		f, ok := meta.featureByID[g.ScopeEntityID]
		if !ok || f.MeterID == "" {
			s.Logger.Debug(ctx, "entitlement grant evaluation: feature or meter missing", "grant_id", g.ID)
			continue
		}
		m, ok := meta.meterByID[f.MeterID]
		if !ok {
			s.Logger.Debug(ctx, "entitlement grant evaluation: meter missing", "grant_id", g.ID, "meter_id", f.MeterID)
			continue
		}
		sub, ok := meta.subsByID[g.SubscriptionID]
		if !ok {
			s.Logger.Debug(ctx, "entitlement grant evaluation: subscription missing", "grant_id", g.ID)
			continue
		}

		extIDs, ok := extIDsBySub[sub.ID]
		if !ok {
			extIDs, err = subscriptionSvc.ExternalCustomerIDsForSubscription(ctx, sub)
			if err != nil {
				s.Logger.Error(ctx, "entitlement grant evaluation: external customer id lookup failed",
					"grant_id", g.ID, "subscription_id", sub.ID, "error", err)
				continue
			}
			extIDsBySub[sub.ID] = extIDs
		}

		usage, err := s.refreshEntitlementGrantUsage(ctx, g, m, extIDs, priceSvc, meta.pricesByMeter, at)
		if err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: usage refresh failed",
				"grant_id", g.ID, "error", err)
			continue
		}

		if err := s.transitionEntitlementGrantAlert(ctx, alertLogsSvc, cust, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: alert transition failed",
				"grant_id", g.ID, "error", err)
			// fall through to snapshot: grant state is billing's source of truth.
		}

		if err := s.snapshotEntitlementGrant(ctx, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: snapshot write failed",
				"grant_id", g.ID, "error", err)
		}
	}
	return nil
}

// entitlementGrantEvalMeta is the per-tick lookup bundle for the grant loop.
type entitlementGrantEvalMeta struct {
	subsByID      map[string]*subscription.Subscription
	featureByID   map[string]*feature.Feature
	meterByID     map[string]*meter.Meter
	pricesByMeter map[string][]*price.Price
}

func (s *alertService) loadEntitlementGrantMeta(
	ctx context.Context,
	grants []*entitlementgrant.EntitlementGrant,
) (*entitlementGrantEvalMeta, error) {
	subIDs := lo.Uniq(lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string {
		return g.SubscriptionID
	}))
	subs := make(map[string]*subscription.Subscription, len(subIDs))
	for _, id := range subIDs {
		sub, err := s.SubRepo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		subs[id] = sub
	}

	featureIDs := lo.Uniq(lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string {
		return g.ScopeEntityID
	}))
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	featureByID := lo.KeyBy(features, func(f *feature.Feature) string { return f.ID })

	meterIDs := lo.Uniq(lo.FilterMap(features, func(f *feature.Feature, _ int) (string, bool) {
		return f.MeterID, f.MeterID != ""
	}))
	var meters []*meter.Meter
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err = s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return nil, err
		}
	}
	meterByID := lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID })

	pricesByMeter := make(map[string][]*price.Price)
	if len(meterIDs) > 0 {
		priceFilter := types.NewNoLimitPriceFilter()
		priceFilter.MeterIDs = meterIDs
		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			return nil, err
		}
		for _, p := range prices {
			pricesByMeter[p.MeterID] = append(pricesByMeter[p.MeterID], p)
		}
	}

	return &entitlementGrantEvalMeta{
		subsByID:      subs,
		featureByID:   featureByID,
		meterByID:     meterByID,
		pricesByMeter: pricesByMeter,
	}, nil
}

// refreshEntitlementGrantUsage sums usage over [valid_from, min(at, valid_to))
// and, for amount lane, multiplies by the flat unit price.
func (s *alertService) refreshEntitlementGrantUsage(
	ctx context.Context,
	g *entitlementgrant.EntitlementGrant,
	m *meter.Meter,
	extCustomerIDs []string,
	priceSvc PriceService,
	pricesByMeter map[string][]*price.Price,
	at time.Time,
) (decimal.Decimal, error) {
	end := at
	if end.After(g.ValidTo) {
		end = g.ValidTo
	}
	if !end.After(g.ValidFrom) {
		return decimal.Zero, nil
	}

	result, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
		TenantID:            g.TenantID,
		EnvironmentID:       g.EnvironmentID,
		ExternalCustomerIDs: extCustomerIDs,
		MeterID:             m.ID,
		StartTime:           g.ValidFrom,
		EndTime:             end,
		AggregationType:     m.Aggregation.Type,
		UseFinal:            true,
	})
	if err != nil {
		return decimal.Zero, err
	}
	qty := result.TotalValue

	if g.Measure == types.EntitlementGrantMeasureQuantity {
		return qty, nil
	}

	// Amount lane: qty * flat unit price. Anything non-flat is a config bug; log and skip.
	unitPrice, ok := s.selectGrantUnitPrice(g, pricesByMeter[m.ID])
	if !ok {
		s.Logger.Warn(ctx, "entitlement grant evaluation: amount lane could not resolve flat unit price",
			"grant_id", g.ID, "meter_id", m.ID)
		return decimal.Zero, nil
	}
	amount := priceSvc.CalculateCost(ctx, unitPrice, qty)
	return amount, nil
}

// selectGrantUnitPrice picks the highest-sequence flat price on the meter.
func (s *alertService) selectGrantUnitPrice(
	g *entitlementgrant.EntitlementGrant,
	prices []*price.Price,
) (*price.Price, bool) {
	flatPrices := lo.Filter(prices, func(p *price.Price, _ int) bool {
		return p.BillingModel == types.BILLING_MODEL_FLAT_FEE
	})
	if len(flatPrices) == 0 {
		return nil, false
	}
	best := flatPrices[0]
	for _, p := range flatPrices[1:] {
		if p.Sequence > best.Sequence {
			best = p
		}
	}
	return best, true
}

// transitionEntitlementGrantAlert emits an alert-log row on state change.
func (s *alertService) transitionEntitlementGrantAlert(
	ctx context.Context,
	alertLogsSvc AlertLogsService,
	cust *customer.Customer,
	g *entitlementgrant.EntitlementGrant,
	usage decimal.Decimal,
	at time.Time,
) error {
	if !g.Quota.IsPositive() {
		return nil
	}
	ratio := usage.Div(g.Quota)
	state := entitlementGrantStateFromRatio(ratio)
	if state == types.AlertStateOk {
		return nil
	}

	parentEntityType := string(types.AlertEntityTypeSubscription)
	custID := cust.ID
	return alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
		EntityType:       types.AlertEntityTypeEntitlementGrant,
		EntityID:         g.ID,
		ParentEntityType: &parentEntityType,
		ParentEntityID:   &g.SubscriptionID,
		CustomerID:       &custID,
		AlertType:        types.AlertTypeEntitlementGrantExhausted,
		AlertStatus:      state,
		AlertInfo: types.AlertInfo{
			ValueAtTime: ratio,
			Timestamp:   at,
		},
	})
}

// snapshotEntitlementGrant writes usage and flips active→exhausted on crossing.
func (s *alertService) snapshotEntitlementGrant(
	ctx context.Context,
	g *entitlementgrant.EntitlementGrant,
	usage decimal.Decimal,
	at time.Time,
) error {
	g.Usage = usage
	g.LastComputedAt = &at
	if usage.GreaterThanOrEqual(g.Quota) && g.GrantStatus == types.EntitlementGrantStatusActive {
		g.GrantStatus = types.EntitlementGrantStatusExhausted
	}
	return s.EntitlementGrantRepo.UpdateSnapshot(ctx, g)
}

// entitlementGrantStateFromRatio fires only on exhaustion (ratio >= 1).
func entitlementGrantStateFromRatio(ratio decimal.Decimal) types.AlertState {
	if ratio.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return types.AlertStateInAlarm
	}
	return types.AlertStateOk
}
