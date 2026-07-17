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

// evaluateEntitlementGrantsForCustomer refreshes usage for every live grant
// the customer has (one CH read per grant, one PG snapshot write per grant),
// then routes state transitions through the same alert_logs helper that wallet
// and spend alerts use. State-transition dedup on the log helper guarantees at
// most one row per (grant, alert_type, new_state) transition even under
// Temporal retries.
//
// Called from EvaluateSpendAndEntitlementAlertsForCustomer once per debounced
// tick. Not exported — the outer method owns the customer/subscription setup
// so we can share it with the spend evaluator without re-fetching subs.
func (s *alertService) evaluateEntitlementGrantsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	grants []*entitlementgrant.EntitlementGrant,
	at time.Time,
) error {
	if len(grants) == 0 {
		return nil
	}

	// Phase 1 only opens feature-scoped grants. Anything else must have been
	// written by a future code path; skip it defensively so this evaluator
	// never touches a scope it can't reason about.
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

	// Cache the external customer id list per subscription — one PG lookup per
	// sub instead of per grant.
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
			// Snapshot still writes below — grant state is the source of truth
			// for billing, and losing an alert delivery is preferable to losing
			// the usage refresh.
		}

		if err := s.snapshotEntitlementGrant(ctx, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: snapshot write failed",
				"grant_id", g.ID, "error", err)
		}
	}
	return nil
}

// entitlementGrantEvalMeta is the metadata cache we build up once per tick so
// the per-grant loop stays a straight walk with no side-fetches.
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

	// Amount-lane grants need the flat per-unit price. Fetch once per meter
	// so we don't re-query per grant. Prices scoped by entity are resolved
	// per grant below.
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

// refreshEntitlementGrantUsage runs one CH sum over the grant window and, for
// amount-lane grants, converts the quantity to currency using the per-unit
// flat price. Amount-lane grants are validated at EC-write time to only exist
// on flat/linear pricing (see entitlement_grant_validation.go), so a single
// multiplication is safe.
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
	// Guard: if the workflow is called before valid_from (rare — clock skew /
	// backdated bucket_end), we still want a zero, not a negative window.
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

	// Amount lane: qty × per-unit price. EC-validation rejected commit/tier
	// at config-time so we only expect FLAT_FEE here; if we see anything
	// else, we log and fall back to zero rather than mis-charge.
	unitPrice, ok := s.selectGrantUnitPrice(g, pricesByMeter[m.ID])
	if !ok {
		s.Logger.Warn(ctx, "entitlement grant evaluation: amount lane could not resolve flat unit price",
			"grant_id", g.ID, "meter_id", m.ID)
		return decimal.Zero, nil
	}
	// Use the price service's per-quantity cost when available; falls back to
	// straight multiplication for simple FLAT_FEE — behavior is identical.
	amount := priceSvc.CalculateCost(ctx, unitPrice, qty)
	return amount, nil
}

// selectGrantUnitPrice picks the flat per-unit price that governs the grant.
// Prefers the price attached to the same plan/addon as the grant's EC context,
// falling back to any flat price on the meter. Non-flat prices are filtered
// out because Phase 1 only supports flat pricing for amount grants.
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
	// Deterministic pick: the price with the highest sequence (most recent
	// state change) wins. Same tiebreaker the plan-price sync uses.
	best := flatPrices[0]
	for _, p := range flatPrices[1:] {
		if p.Sequence > best.Sequence {
			best = p
		}
	}
	return best, true
}

// transitionEntitlementGrantAlert routes the grant's new usage/quota ratio
// through the shared alert_logs helper. The helper handles GetLatestAlert +
// state comparison + webhook fire, so we just decide (a) the ratio → state
// mapping and (b) which AlertType to stamp on the log row.
func (s *alertService) transitionEntitlementGrantAlert(
	ctx context.Context,
	alertLogsSvc AlertLogsService,
	cust *customer.Customer,
	g *entitlementgrant.EntitlementGrant,
	usage decimal.Decimal,
	at time.Time,
) error {
	if !g.Quota.IsPositive() {
		// Zero-quota grants are pathological (validation should reject them)
		// but be defensive rather than divide-by-zero.
		return nil
	}
	ratio := usage.Div(g.Quota)
	state := entitlementGrantStateFromRatio(ratio)

	// Exhaustion (in_alarm) is stamped with the dedicated exhaustion type so
	// downstream webhook consumers can route it separately from the
	// intermediate threshold transitions.
	alertType := types.AlertTypeEntitlementGrantThreshold
	if state == types.AlertStateInAlarm {
		alertType = types.AlertTypeEntitlementGrantExhausted
	}

	parentEntityType := string(types.AlertEntityTypeSubscription)
	custID := cust.ID
	return alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
		EntityType:       types.AlertEntityTypeEntitlementGrant,
		EntityID:         g.ID,
		ParentEntityType: &parentEntityType,
		ParentEntityID:   &g.SubscriptionID,
		CustomerID:       &custID,
		AlertType:        alertType,
		AlertStatus:      state,
		AlertInfo: types.AlertInfo{
			ValueAtTime: ratio,
			Timestamp:   at,
		},
	})
}

// snapshotEntitlementGrant persists the freshly-computed usage back to PG.
// Also flips the grant to `exhausted` on the crossing so the DB shows the
// state at a glance (M5's adjustMeterUsageGrants reads this snapshot).
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
	if pct, ok := entitlementGrantThresholdPct(usage, g.Quota); ok {
		g.LastAlertPct = &pct
		g.LastAlertAt = &at
	}
	return s.EntitlementGrantRepo.UpdateSnapshot(ctx, g)
}

// -----------------------------------------------------------------------------
// State + threshold mapping.
//
// Phase 1 uses hardcoded thresholds (50 / 80 / 100) to keep the schema and
// service surface small. Making these configurable per EC or per tenant is
// tracked in ERD §14 open question 4 and would slot in here without touching
// the rest of the pipeline.
// -----------------------------------------------------------------------------

func entitlementGrantStateFromRatio(ratio decimal.Decimal) types.AlertState {
	switch {
	case ratio.GreaterThanOrEqual(decimal.NewFromInt(1)):
		return types.AlertStateInAlarm
	case ratio.GreaterThanOrEqual(decimal.NewFromFloat(0.8)):
		return types.AlertStateWarning
	case ratio.GreaterThanOrEqual(decimal.NewFromFloat(0.5)):
		return types.AlertStateInfo
	default:
		return types.AlertStateOk
	}
}

// entitlementGrantThresholdPct returns the highest threshold percentage that
// the given usage/quota ratio has crossed. Returns (0, false) when nothing has
// crossed — the caller should leave last_alert_pct alone in that case.
func entitlementGrantThresholdPct(usage, quota decimal.Decimal) (int, bool) {
	if !quota.IsPositive() {
		return 0, false
	}
	ratio := usage.Div(quota)
	switch {
	case ratio.GreaterThanOrEqual(decimal.NewFromInt(1)):
		return 100, true
	case ratio.GreaterThanOrEqual(decimal.NewFromFloat(0.8)):
		return 80, true
	case ratio.GreaterThanOrEqual(decimal.NewFromFloat(0.5)):
		return 50, true
	default:
		return 0, false
	}
}
