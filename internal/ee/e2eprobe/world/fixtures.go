package world

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

const (
	basePriceLookupKey = "e2eprobe_base_price"
	usagePricePrefix   = "e2eprobe_usage_"
	softLimitUnits     = int64(100)

	grantMeterKey        = "sum_multiplier"
	grantQuota           = "1000"
	grantDurationValue   = 1
	grantDurationUnit    = "hour"
	grantMeasure         = "quantity"
	grantAggregationMode = "additive"
)

// Fixtures are the shared, tenant-wide entities every persona builds on.
type Fixtures struct {
	mu           sync.RWMutex
	planID       string
	meterIDs     map[string]string
	featureIDs   map[string]string
	priceIDs     map[string]string
	softLimitIDs map[string]string
	grantID      string
	couponID     string
	couponCode   string
	taxRateID    string
	taxRateCode  string
}

func newFixtures() *Fixtures {
	return &Fixtures{meterIDs: map[string]string{}, featureIDs: map[string]string{}, priceIDs: map[string]string{}, softLimitIDs: map[string]string{}}
}

func (f *Fixtures) PlanID() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.planID
}

func (f *Fixtures) MeterID(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.meterIDs[key]
}

func (f *Fixtures) FeatureID(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.featureIDs[key]
}

// PriceID is the plan price for a meter key, or for "base".
func (f *Fixtures) PriceID(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.priceIDs[key]
}

func (f *Fixtures) SoftLimitEntitlementID(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.softLimitIDs[key]
}

func (f *Fixtures) GrantEntitlementID() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.grantID
}

func (f *Fixtures) CouponID() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.couponID
}

func (f *Fixtures) CouponCode() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.couponCode
}

func (f *Fixtures) TaxRateID() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.taxRateID
}

func (f *Fixtures) TaxRateCode() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.taxRateCode
}

// Ready means the plan and every meter exist.
func (f *Fixtures) Ready() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.planID != "" && len(f.meterIDs) == len(e2eprobe.Meters())
}

func (f *Fixtures) set(fn func(*Fixtures)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// setTaxRateID backfills the rate id from an association when the rate pre-existed.
func (f *Fixtures) setTaxRateID(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.taxRateID == "" && id != "" {
		f.taxRateID = id
	}
}

func (w *World) provisionFixtures(ctx context.Context) error {
	fx := w.Fixtures()
	if err := w.ensureFeatures(ctx, fx); err != nil {
		return err
	}
	if err := w.ensureCoupon(ctx, fx); err != nil {
		return err
	}
	if err := w.ensureTaxRate(ctx, fx); err != nil {
		return err
	}
	if err := w.ensurePlan(ctx, fx); err != nil {
		return err
	}
	if err := w.ensurePrices(ctx, fx); err != nil {
		return err
	}
	if err := w.ensureSoftLimits(ctx, fx); err != nil {
		return err
	}
	// Grant coverage is one reserved feature; its failure must not take the
	// plan, prices and personas down with it.
	if err := w.ensureGrant(ctx, fx); err != nil {
		w.info(ctx, "world: grant entitlement unavailable; granted persona will record a gap", "error", err.Error())
		fx.set(func(f *Fixtures) { f.grantID = "" })
	}
	return nil
}

func (w *World) ensureFeatures(ctx context.Context, fx *Fixtures) error {
	meters := e2eprobe.Meters()
	lookupKeys := make([]string, 0, len(meters))
	for _, m := range meters {
		lookupKeys = append(lookupKeys, m.LookupKey)
	}
	resp, err := w.client.Features().Query(ctx, types.FeatureFilter{LookupKeys: lookupKeys})
	if err != nil {
		return fmt.Errorf("query features: %w", err)
	}
	byLookup := map[string]types.FeatureResponse{}
	if resp.ListFeaturesResponse != nil {
		for _, f := range resp.ListFeaturesResponse.Items {
			if f.LookupKey != nil {
				byLookup[*f.LookupKey] = f
			}
		}
	}
	for _, m := range meters {
		feat, ok := byLookup[m.LookupKey]
		if !ok {
			created, err := w.client.Features().Create(ctx, featureRequest(m))
			if err != nil {
				return fmt.Errorf("create feature %s: %w", m.LookupKey, err)
			}
			if created.FeatureResponse == nil {
				return fmt.Errorf("create feature %s: empty response", m.LookupKey)
			}
			feat = *created.FeatureResponse
		}
		fx.set(func(f *Fixtures) {
			if feat.ID != nil {
				f.featureIDs[m.Key] = *feat.ID
			}
			if feat.MeterID != nil {
				f.meterIDs[m.Key] = *feat.MeterID
			}
		})
	}
	return nil
}

func featureRequest(m e2eprobe.Meter) types.CreateFeatureRequest {
	agg := m.Aggregation
	meter := types.CreateMeterRequest{
		Name:        m.EventName,
		EventName:   m.EventName,
		ResetUsage:  types.ResetUsageBillingPeriod,
		Aggregation: types.MeterAggregation{Type: &agg},
	}
	if m.Field != "" {
		meter.Aggregation.Field = ptr(m.Field)
	}
	if m.MeterBucket != "" {
		bucket := m.MeterBucket
		meter.Aggregation.BucketSize = &bucket
	}
	if m.Multiplier != "" {
		meter.Aggregation.Multiplier = ptr(m.Multiplier)
	}
	if m.FilterKey != "" {
		meter.Filters = []types.MeterFilter{{Key: ptr(m.FilterKey), Values: m.FilterValues}}
	}
	return types.CreateFeatureRequest{
		Name:      m.DisplayName,
		Type:      types.FeatureTypeMetered,
		LookupKey: ptr(m.LookupKey),
		Meter:     &meter,
		Metadata:  map[string]string{"e2eprobe": "true", "e2eprobe_role": "fixture", "aggregation": m.Key},
	}
}

// ensureCoupon looks the shared coupon up by code: the search endpoint is
// case-sensitive while codes are stored lowercased.
func (w *World) ensureCoupon(ctx context.Context, fx *Fixtures) error {
	fx.set(func(f *Fixtures) { f.couponCode = e2eprobe.SharedCouponCode })
	existing, err := w.client.Coupons().GetByCode(ctx, e2eprobe.SharedCouponCode)
	if err == nil && existing != nil && existing.CouponResponse != nil && existing.CouponResponse.ID != nil {
		fx.set(func(f *Fixtures) { f.couponID = *existing.CouponResponse.ID })
		return nil
	}
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("lookup coupon %s: %w", e2eprobe.SharedCouponCode, err)
	}
	created, err := w.client.Coupons().Create(ctx, types.CreateCouponRequest{
		Name:          "E2EProbe 10% Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		CouponCode:    ptr(e2eprobe.SharedCouponCode),
		PercentageOff: ptr("10"),
		Metadata:      map[string]string{"e2eprobe": "true", "e2eprobe_role": "fixture"},
	})
	if err != nil {
		if IsAlreadyExists(err) {
			again, retryErr := w.client.Coupons().GetByCode(ctx, e2eprobe.SharedCouponCode)
			if retryErr == nil && again != nil && again.CouponResponse != nil && again.CouponResponse.ID != nil {
				fx.set(func(f *Fixtures) { f.couponID = *again.CouponResponse.ID })
				return nil
			}
		}
		return fmt.Errorf("create coupon %s: %w", e2eprobe.SharedCouponCode, err)
	}
	if created.CouponResponse != nil && created.CouponResponse.ID != nil {
		fx.set(func(f *Fixtures) { f.couponID = *created.CouponResponse.ID })
	}
	return nil
}

// ensureTaxRate adopts the shared rate by code and creates it when none is listed.
func (w *World) ensureTaxRate(ctx context.Context, fx *Fixtures) error {
	fx.set(func(f *Fixtures) { f.taxRateCode = e2eprobe.SharedTaxRateCode })
	listed, err := w.client.TaxRates().List(ctx, dtos.GetTaxRatesRequest{TaxrateCodes: []string{e2eprobe.SharedTaxRateCode}})
	if err != nil {
		return fmt.Errorf("list tax rates: %w", err)
	}
	if listed != nil && listed.ListTaxRatesResponse != nil {
		for _, tr := range listed.ListTaxRatesResponse.Items {
			if tr.ID != nil && tr.Code != nil && *tr.Code == e2eprobe.SharedTaxRateCode {
				fx.setTaxRateID(*tr.ID)
				return nil
			}
		}
	}
	scope := types.TaxRateScopeExternal
	rateType := types.TaxRateTypePercentage
	created, err := w.client.TaxRates().Create(ctx, types.CreateTaxRateRequest{
		Code:            e2eprobe.SharedTaxRateCode,
		Name:            "E2EProbe 10% Tax",
		PercentageValue: ptr("10"),
		Scope:           &scope,
		TaxRateType:     &rateType,
		Metadata:        map[string]string{"e2eprobe": "true", "e2eprobe_role": "fixture"},
	})
	if err != nil {
		return fmt.Errorf("create tax rate %s: %w", e2eprobe.SharedTaxRateCode, err)
	}
	if created.TaxRateResponse == nil || created.TaxRateResponse.ID == nil {
		return fmt.Errorf("create tax rate %s: empty response", e2eprobe.SharedTaxRateCode)
	}
	fx.setTaxRateID(*created.TaxRateResponse.ID)
	return nil
}

func (w *World) ensurePlan(ctx context.Context, fx *Fixtures) error {
	resp, err := w.client.Plans().Query(ctx, types.PlanFilter{LookupKey: ptr(e2eprobe.PlanLookupKey)})
	if err != nil {
		return fmt.Errorf("query plans: %w", err)
	}
	if resp.ListPlansResponse != nil && len(resp.ListPlansResponse.Items) > 0 && resp.ListPlansResponse.Items[0].ID != nil {
		id := *resp.ListPlansResponse.Items[0].ID
		fx.set(func(f *Fixtures) { f.planID = id })
		return nil
	}
	created, err := w.client.Plans().Create(ctx, types.CreatePlanRequest{
		Name:        "E2EProbe Plan",
		LookupKey:   ptr(e2eprobe.PlanLookupKey),
		Description: ptr("Plan used by the e2eprobe synthetic monitor"),
		Metadata:    map[string]string{"e2eprobe": "true", "e2eprobe_role": "fixture"},
	})
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if created.PlanResponse == nil || created.PlanResponse.ID == nil {
		return fmt.Errorf("create plan: empty response")
	}
	id := *created.PlanResponse.ID
	fx.set(func(f *Fixtures) { f.planID = id })
	return nil
}

// planPrices lists the published plan prices; entity_ids is the filter the
// server honours.
func planPriceFilter(planID string) types.PriceFilter {
	published := types.StatusPublished
	entityType := types.PriceEntityTypePlan
	return types.PriceFilter{EntityIds: []string{planID}, EntityType: &entityType, Status: &published, Limit: ptr(int64(1000))}
}

func (w *World) ensurePrices(ctx context.Context, fx *Fixtures) error {
	planID := fx.PlanID()
	resp, err := w.client.Prices().Query(ctx, planPriceFilter(planID))
	if err != nil {
		return fmt.Errorf("query prices: %w", err)
	}
	byLookup := map[string]bool{}
	byMeter := map[string]bool{}
	if resp.ListPricesResponse != nil {
		for _, p := range resp.ListPricesResponse.Items {
			if p.LookupKey != nil {
				byLookup[*p.LookupKey] = true
			}
			if p.MeterID != nil {
				byMeter[*p.MeterID] = true
			}
		}
	}
	if !byLookup[basePriceLookupKey] {
		if _, err := w.client.Prices().Create(ctx, priceRequest(planID, types.PriceTypeFixed, "19.99", nil, "E2EProbe Base Fee", basePriceLookupKey)); err != nil {
			return fmt.Errorf("create base price: %w", err)
		}
	}
	for _, m := range e2eprobe.Meters() {
		meterID := fx.MeterID(m.Key)
		lookup := usagePricePrefix + m.EventName
		if meterID == "" || byMeter[meterID] || byLookup[lookup] {
			continue
		}
		req := priceRequest(planID, types.PriceTypeUsage, "0.01", ptr(meterID), "E2EProbe "+m.DisplayName+" Usage", lookup)
		if _, err := w.client.Prices().CreateBucketed(ctx, req, string(m.PriceBucket)); err != nil {
			return fmt.Errorf("create usage price for %s: %w", m.Key, err)
		}
	}
	return w.indexPrices(ctx, fx)
}

func priceRequest(planID string, priceType types.PriceType, amount string, meterID *string, name, lookup string) types.CreatePriceRequest {
	return types.CreatePriceRequest{
		EntityID:           planID,
		EntityType:         types.PriceEntityTypePlan,
		Type:               priceType,
		BillingModel:       types.BillingModelFlatFee,
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: ptr(int64(1)),
		InvoiceCadence:     types.InvoiceCadenceArrear,
		PriceUnitType:      types.PriceUnitTypeFiat,
		Amount:             ptr(amount),
		Currency:           "USD",
		MeterID:            meterID,
		DisplayName:        ptr(name),
		LookupKey:          ptr(lookup),
	}
}

func (w *World) indexPrices(ctx context.Context, fx *Fixtures) error {
	resp, err := w.client.Prices().Query(ctx, planPriceFilter(fx.PlanID()))
	if err != nil {
		return fmt.Errorf("index prices: %w", err)
	}
	if resp.ListPricesResponse == nil {
		return nil
	}
	fx.set(func(f *Fixtures) {
		for _, p := range resp.ListPricesResponse.Items {
			if p.ID == nil {
				continue
			}
			if key, ok := priceKey(p, f.meterIDs); ok {
				f.priceIDs[key] = *p.ID
			}
		}
	})
	return nil
}

// priceKey names the catalog meter a plan price serves: by lookup key when the
// probe created it, else by meter id when the price was adopted.
func priceKey(p types.PriceResponse, meterIDs map[string]string) (string, bool) {
	if p.LookupKey != nil {
		if *p.LookupKey == basePriceLookupKey {
			return "base", true
		}
		if m, ok := e2eprobe.MeterByEventName(strings.TrimPrefix(*p.LookupKey, usagePricePrefix)); ok {
			return m.Key, true
		}
	}
	if p.MeterID != nil {
		for key, id := range meterIDs {
			if id == *p.MeterID {
				return key, true
			}
		}
	}
	return "", false
}

// softLimitEligible is every meter that takes the plan's 100-unit soft limit:
// bucketed meters reject entitlements, sum_commit opts out and the grant
// meter's (plan, feature) slot belongs to the additive grant.
func softLimitEligible(m e2eprobe.Meter) bool {
	return !m.Bucketed() && !m.NoEntitlement && m.Key != grantMeterKey
}

func (w *World) ensureSoftLimits(ctx context.Context, fx *Fixtures) error {
	planID := fx.PlanID()
	resp, err := w.client.Entitlements().Query(ctx, types.EntitlementFilter{PlanIds: []string{planID}})
	if err != nil {
		return fmt.Errorf("query plan entitlements: %w", err)
	}
	byFeature := map[string]string{}
	if resp.ListEntitlementsResponse != nil {
		for _, e := range resp.ListEntitlementsResponse.Items {
			if e.FeatureID != nil && e.ID != nil {
				byFeature[*e.FeatureID] = *e.ID
			}
		}
	}
	for _, m := range e2eprobe.Meters() {
		if !softLimitEligible(m) {
			continue
		}
		featID := fx.FeatureID(m.Key)
		if featID == "" {
			continue
		}
		id, ok := byFeature[featID]
		if !ok {
			featureType := types.FeatureTypeMetered
			entityType := types.EntitlementEntityTypePlan
			resetPeriod := types.EntitlementUsageResetPeriodMonthly
			created, err := w.client.Entitlements().Create(ctx, types.CreateEntitlementRequest{
				FeatureID:        ptr(featID),
				FeatureType:      &featureType,
				EntityID:         ptr(planID),
				EntityType:       &entityType,
				PlanID:           ptr(planID),
				UsageLimit:       ptr(softLimitUnits),
				UsageResetPeriod: &resetPeriod,
				IsSoftLimit:      ptr(true),
				IsEnabled:        ptr(true),
			})
			if err != nil {
				return fmt.Errorf("create soft limit for %s: %w", m.Key, err)
			}
			if created.EntitlementResponse == nil || created.EntitlementResponse.ID == nil {
				continue
			}
			id = *created.EntitlementResponse.ID
		}
		key := m.Key
		fx.set(func(f *Fixtures) { f.softLimitIDs[key] = id })
	}
	return nil
}

// ensureGrant keeps one additive grant on the reserved feature. A legacy
// soft-limit entitlement on that feature is removed first, since the server
// allows one non-parallel entitlement per (plan, feature).
func (w *World) ensureGrant(ctx context.Context, fx *Fixtures) error {
	planID, featID := fx.PlanID(), fx.FeatureID(grantMeterKey)
	if featID == "" {
		return fmt.Errorf("grant feature %s not provisioned", grantMeterKey)
	}
	resp, err := w.client.Entitlements().Query(ctx, types.EntitlementFilter{PlanIds: []string{planID}, FeatureIds: []string{featID}})
	if err != nil {
		return fmt.Errorf("query grant entitlements: %w", err)
	}
	var grantID, legacyID string
	if resp.ListEntitlementsResponse != nil {
		for _, e := range resp.ListEntitlementsResponse.Items {
			if e.ID == nil {
				continue
			}
			raw, err := w.client.Entitlements().GetRaw(ctx, *e.ID)
			if err != nil {
				// Never classify on a failed read: a transient error would delete a healthy grant.
				return fmt.Errorf("read entitlement %s: %w", *e.ID, err)
			}
			if raw.GrantMeasure != "" {
				grantID = *e.ID
				break
			}
			legacyID = *e.ID
		}
	}
	if grantID == "" {
		if legacyID != "" {
			if _, err := w.client.Entitlements().Delete(ctx, legacyID); err != nil && !IsNotFound(err) {
				return fmt.Errorf("delete legacy soft limit %s: %w", legacyID, err)
			}
			w.info(ctx, "world: deleted legacy soft-limit entitlement to make room for the grant", "entitlement_id", legacyID)
		}
		id, err := w.client.Entitlements().CreateWithGrant(ctx, e2eprobe.GrantEntitlementInput{
			FeatureID:          featID,
			FeatureType:        "metered",
			PlanID:             planID,
			EntityType:         string(types.EntitlementEntityTypePlan),
			EntityID:           planID,
			IsEnabled:          true,
			GrantMeasure:       grantMeasure,
			GrantQuota:         grantQuota,
			GrantDurationValue: grantDurationValue,
			GrantDurationUnit:  grantDurationUnit,
			AggregationMode:    grantAggregationMode,
		})
		if err != nil {
			return fmt.Errorf("create grant entitlement: %w", err)
		}
		grantID = id
	}
	if err := w.verifyGrantEcho(ctx, grantID); err != nil {
		return err
	}
	fx.set(func(f *Fixtures) { f.grantID = grantID })
	return nil
}

// verifyGrantEcho reads the grant back so a server that silently drops a field
// is caught at provisioning rather than as a mysterious usage mismatch.
func (w *World) verifyGrantEcho(ctx context.Context, id string) error {
	raw, err := w.client.Entitlements().GetRaw(ctx, id)
	if err != nil {
		return fmt.Errorf("read grant %s: %w", id, err)
	}
	quota, _ := decimal.NewFromString(raw.GrantQuota)
	switch {
	case raw.GrantMeasure != grantMeasure:
		return fmt.Errorf("grant %s: grant_measure %q, want %q", id, raw.GrantMeasure, grantMeasure)
	case !quota.Equal(decimal.RequireFromString(grantQuota)):
		return fmt.Errorf("grant %s: grant_quota %q, want %s", id, raw.GrantQuota, grantQuota)
	case raw.GrantDurationValue == nil || *raw.GrantDurationValue != grantDurationValue || raw.GrantDurationUnit != grantDurationUnit:
		return fmt.Errorf("grant %s: duration %v %s, want %d %s", id, raw.GrantDurationValue, raw.GrantDurationUnit, grantDurationValue, grantDurationUnit)
	case raw.AggregationMode != grantAggregationMode:
		return fmt.Errorf("grant %s: aggregation_mode %q, want %q", id, raw.AggregationMode, grantAggregationMode)
	case !raw.IsEnabled:
		return fmt.Errorf("grant %s is disabled", id)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
