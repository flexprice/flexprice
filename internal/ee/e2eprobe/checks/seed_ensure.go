package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

const (
	PersistentCustomerCount = 10
	PreFundedWalletCount    = 3

	// AlertCanaryExternalCustomerID owns the dedicated $30 wallet used by
	// LowBalanceAlertProbe to exercise the low-balance webhook pipeline
	// end-to-end. Held outside PreFundedCustomerIDs so wallet_debit_verification
	// / wallet_balance_probe never touch it.
	AlertCanaryExternalCustomerID = "e2eprobe-cust-alert-canary"

	// AlertCanaryInitialBalance sits $5 above the info threshold (25).
	// Any drop of >$5 in current-period usage crosses info; >$20 crosses
	// warning; >$30 crosses critical.
	AlertCanaryInitialBalance = "30.00"
)

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }

const (
	SharedCouponCode = "E2EPROBE_COUPON_10PCT"
	SharedCouponName = "E2EProbe 10% Coupon"

	SharedTaxRateCode = "E2EPROBE_TAX_10PCT"
	SharedTaxRateName = "E2EProbe 10% Tax"
)

// Grant coverage constants (2026-08-13 spec). See
// docs/superpowers/specs/2026-08-13-e2eprobe-entitlement-grants-design.md
// for rationale. The DB constraint "one non-parallel entitlement per
// (entity, feature)" means the additive-grant feature MUST NOT also get
// the legacy soft-limit entitlement — ensurePlanEntitlements skips it via
// the grantOnlyFeatures set defined below.
const (
	AdditiveGrantFeatureLookupKey = "e2eprobe_sum_multiplier_feature"
	AdditiveGrantQuota            = "1000"
	AdditiveGrantDurationValue    = 1
	AdditiveGrantDurationUnit     = "hour"
)

// lowBalanceAlertSettings returns the alert thresholds seed wallets are
// created with: info at 25, warning at 10, critical at 0 (all "below").
// Fires wallet.credit_balance.dropped webhooks; consumed by the
// low-wallet-alert-listener check. Thresholds are decimal strings —
// go-sdk v2.0.24 corrected AlertThreshold.Threshold to *string.
func lowBalanceAlertSettings() *types.AlertSettings {
	below := types.AlertConditionBelow
	return &types.AlertSettings{
		AlertEnabled: boolPtr(true),
		Info:         &types.AlertThreshold{Threshold: strPtr("25"), Condition: &below},
		Warning:      &types.AlertThreshold{Threshold: strPtr("10"), Condition: &below},
		Critical:     &types.AlertThreshold{Threshold: strPtr("0"), Condition: &below},
	}
}

func persistentExternalCustomerID(i int) string {
	return fmt.Sprintf("e2eprobe-cust-persistent-%d", i)
}

type SeedEnsure struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	logger *logger.Logger
}

func NewSeedEnsure(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *SeedEnsure {
	return &SeedEnsure{client: c, reg: r, runID: runID, logger: lg}
}

func (s *SeedEnsure) Name() string        { return "seed-ensure" }
func (s *SeedEnsure) Kind() e2eprobe.Kind { return e2eprobe.KindBootstrap }
func (s *SeedEnsure) Run(ctx context.Context) error {
	seeds := e2eprobe.Seeds{
		MeterIDs: map[string]string{},
	}
	// Order matters: features first (provides MeterIDs), then customers, plan, prices,
	// subscriptions (needs plan + customers), wallets (needs customers).
	if err := s.ensureFeatures(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureCoupons(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureTaxRates(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureCustomers(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensurePlan(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensurePrices(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensurePlanEntitlements(ctx, &seeds); err != nil {
		return err
	}
	// ensureEntitlementGrants is intentionally NON-FATAL. It provisions
	// additive-grant coverage on ONE reserved feature (2026-08-13 plan).
	// If the server rejects the raw-HTTP grant config, an SDK/server
	// version drift misses a field, or config-echo drifts, we log and
	// continue — every downstream step (subscriptions, wallets, tax
	// association) and every existing probe (new-customer-lifecycle,
	// commitment, tax, coupon, etc.) must keep working. Otherwise a
	// grant-side failure poisons LoadSeeds → all ephemeral-creating
	// probes soft-skip on empty PlanIDs → no customers appear in the
	// tenant. See the "no ephemeral customers on staging" report.
	if err := s.ensureEntitlementGrants(ctx, &seeds); err != nil {
		if s.logger != nil {
			s.logger.Info(ctx, "seed-ensure: grant provisioning failed; continuing without grant coverage",
				"error", err.Error(),
			)
		}
		// Intentional: clear GrantEntitlementIDs so the grant probe
		// soft-skips instead of hitting the half-populated map.
		seeds.GrantEntitlementIDs = nil
	}
	if err := s.ensureSubscriptions(ctx, &seeds); err != nil {
		return err
	}
	// Non-fatal: repairs line-item drift on pre-existing subs. A failure here
	// degrades bucketed-meter coverage, it doesn't invalidate the seed set.
	if err := s.ensureSubscriptionPriceSync(ctx, &seeds); err != nil {
		if s.logger != nil {
			s.logger.Info(ctx, "seed-ensure: subscription price sync failed; continuing",
				"error", err.Error(),
			)
		}
	}
	if err := s.ensurePersistentTaxAssociation(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureWallets(ctx, &seeds); err != nil {
		return err
	}
	s.reg.LoadSeeds(seeds)
	return nil
}

func seedMetadata(agg string) map[string]string {
	return map[string]string{"e2eprobe": "true", "e2eprobe_role": "seed", "aggregation": agg}
}

type featureSpec struct {
	lookupKey   string
	eventName   string
	displayName string
	aggType     types.AggregationType
	field       *string
	// bucketSize puts the bucket on the METER — the deprecated, grandfathered
	// path. Mutually exclusive with priceBucketSize: the server rejects a
	// price-level bucket when the meter already defines one.
	bucketSize *types.WindowSize
	// priceBucketSize puts the bucket on the PRICE — the supported path. The
	// meter is created unbucketed and windowing is resolved from the price.
	priceBucketSize *types.WindowSize
	multiplier      *string
	expression      *string
	filters         []types.MeterFilter
	aggLabel        string
	// noEntitlement keeps ensurePlanEntitlements from provisioning the
	// standard 100-unit monthly entitlement on this feature. Probes that
	// assert exact billed amounts need a meter whose usage is never
	// partially absorbed by an entitlement allowance.
	noEntitlement bool
}

// isBucketed reports whether the spec is windowed at all, from either source.
// Everything gated on bucketing — entitlement skips, BucketedFeatureIDs — cares
// about the resolved answer, not where it came from.
func (f featureSpec) isBucketed() bool {
	return f.bucketSize != nil || f.priceBucketSize != nil
}

var seedFeatureSpecs = func() []featureSpec {
	hourBucket := types.WindowSizeHour
	return []featureSpec{
		{
			lookupKey: "e2eprobe_count_feature", eventName: "e2eprobe_count",
			displayName: "E2EProbe Count", aggType: types.AggregationTypeCount,
			aggLabel: "count",
		},
		{
			lookupKey: "e2eprobe_sum_feature", eventName: "e2eprobe_sum",
			displayName: "E2EProbe Sum", aggType: types.AggregationTypeSum,
			field: strPtr("amount"), aggLabel: "sum",
		},
		{
			lookupKey: "e2eprobe_avg_feature", eventName: "e2eprobe_avg",
			displayName: "E2EProbe Avg", aggType: types.AggregationTypeAvg,
			field: strPtr("amount"), aggLabel: "avg",
		},
		{
			lookupKey: "e2eprobe_count_unique_feature", eventName: "e2eprobe_count_unique",
			displayName: "E2EProbe CountUnique", aggType: types.AggregationTypeCountUnique,
			field: strPtr("user_id"), aggLabel: "count_unique",
		},
		{
			lookupKey: "e2eprobe_latest_feature", eventName: "e2eprobe_latest",
			displayName: "E2EProbe Latest", aggType: types.AggregationTypeLatest,
			field: strPtr("amount"), aggLabel: "latest",
		},
		{
			lookupKey: "e2eprobe_max_feature", eventName: "e2eprobe_max",
			displayName: "E2EProbe Max", aggType: types.AggregationTypeMax,
			field: strPtr("amount"), bucketSize: &hourBucket, aggLabel: "max",
		},
		{
			lookupKey: "e2eprobe_sum_multiplier_feature", eventName: "e2eprobe_sum_multiplier",
			displayName: "E2EProbe SumMul", aggType: types.AggregationTypeSumWithMultiplier,
			field: strPtr("amount"), multiplier: strPtr("1000"), aggLabel: "sum_with_multiplier",
		},
		{
			lookupKey: "e2eprobe_sum_filtered_feature", eventName: "e2eprobe_sum_filtered",
			displayName: "E2EProbe Sum (api-only)", aggType: types.AggregationTypeSum,
			field: strPtr("amount"),
			filters: []types.MeterFilter{
				{Key: strPtr("source"), Values: []string{"api"}},
			},
			aggLabel: "sum_filtered",
		},
		{
			lookupKey: "e2eprobe_max_15min_feature", eventName: "e2eprobe_max_15min",
			displayName: "E2EProbe Max 15min", aggType: types.AggregationTypeMax,
			field: strPtr("amount"), bucketSize: bucketSizePtr(types.WindowSizeFifteenMin), aggLabel: "max_15min",
		},
		{
			lookupKey: "e2eprobe_sum_hour_feature", eventName: "e2eprobe_sum_hour",
			displayName: "E2EProbe Sum Hour", aggType: types.AggregationTypeSum,
			field: strPtr("amount"), bucketSize: bucketSizePtr(types.WindowSizeHour), aggLabel: "sum_hour",
		},
		{
			lookupKey: "e2eprobe_max_day_feature", eventName: "e2eprobe_max_day",
			displayName: "E2EProbe Max Day", aggType: types.AggregationTypeMax,
			field: strPtr("amount"), bucketSize: bucketSizePtr(types.WindowSizeDay), aggLabel: "max_day",
		},
		{
			// Bucket lives on the PRICE, meter is unbucketed. Pairs with
			// e2eprobe_max_feature (same MAX aggregation, bucket on the meter) so
			// both branches of price.ResolveBucketSize are exercised against
			// identical event shapes.
			lookupKey: "e2eprobe_max_price_hour_feature", eventName: "e2eprobe_max_price_hour",
			displayName: "E2EProbe Max (price bucket)", aggType: types.AggregationTypeMax,
			field: strPtr("amount"), priceBucketSize: bucketSizePtr(types.WindowSizeHour),
			aggLabel: "max_price_hour",
		},
		{
			// SUM counterpart. Bucketing does not change a SUM total, so this is
			// the spec that proves price-level bucketing leaves SUM quantities
			// untouched while still driving per-window pricing.
			lookupKey: "e2eprobe_sum_price_hour_feature", eventName: "e2eprobe_sum_price_hour",
			displayName: "E2EProbe Sum (price bucket)", aggType: types.AggregationTypeSum,
			field: strPtr("amount"), priceBucketSize: bucketSizePtr(types.WindowSizeHour),
			aggLabel: "sum_price_hour",
		},
		{
			// Reserved for CommitmentTrueUpProbe: entitlement-free so the
			// billed amount equals units x price with nothing absorbed by an
			// allowance. See CommitmentEventName.
			lookupKey: "e2eprobe_sum_commit_feature", eventName: CommitmentEventName,
			displayName: "E2EProbe Sum (commitment)", aggType: types.AggregationTypeSum,
			field: strPtr("amount"), aggLabel: "sum_commit", noEntitlement: true,
		},
	}
}()

// ensureFeatures creates 11 features with embedded meters idempotently.
// MeterIDs and FeatureIDs are populated into out.
func (s *SeedEnsure) ensureFeatures(ctx context.Context, out *e2eprobe.Seeds) error {
	// Build lookup-key index of existing features.
	lookupKeys := make([]string, 0, len(seedFeatureSpecs))
	for _, spec := range seedFeatureSpecs {
		lookupKeys = append(lookupKeys, spec.lookupKey)
	}
	existResp, err := s.client.Features().Query(ctx, types.FeatureFilter{
		LookupKeys: lookupKeys,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_features"}, "query features: %w", err)
	}
	byLookup := map[string]types.FeatureResponse{}
	if existResp.ListFeaturesResponse != nil {
		for _, f := range existResp.ListFeaturesResponse.Items {
			if f.LookupKey != nil {
				byLookup[*f.LookupKey] = f
			}
		}
	}

	for _, spec := range seedFeatureSpecs {
		if existing, ok := byLookup[spec.lookupKey]; ok {
			// Already exists — record IDs.
			if existing.ID != nil {
				out.FeatureIDs = append(out.FeatureIDs, *existing.ID)
				if spec.isBucketed() {
					if out.BucketedFeatureIDs == nil {
						out.BucketedFeatureIDs = map[string]string{}
					}
					out.BucketedFeatureIDs[spec.lookupKey] = *existing.ID
				}
			}
			if existing.MeterID != nil {
				out.MeterIDs[spec.eventName] = *existing.MeterID
			}
			continue
		}

		aggType := spec.aggType
		meterReq := types.CreateMeterRequest{
			Name:       spec.eventName,
			EventName:  spec.eventName,
			ResetUsage: types.ResetUsageBillingPeriod,
			Aggregation: types.MeterAggregation{
				Type: &aggType,
			},
		}
		if spec.field != nil {
			meterReq.Aggregation.Field = spec.field
		}
		// Deprecated but still honoured: meters that set bucket_size keep bucketing
		// exactly as before. Kept in the seed on purpose — it is the grandfathered
		// path price.ResolveBucketSize falls back to, so it needs coverage.
		if spec.bucketSize != nil {
			meterReq.Aggregation.BucketSize = spec.bucketSize
		}
		if spec.multiplier != nil {
			meterReq.Aggregation.Multiplier = spec.multiplier
		}
		if spec.expression != nil {
			meterReq.Aggregation.Expression = spec.expression
		}
		if len(spec.filters) > 0 {
			meterReq.Filters = spec.filters
		}

		req := types.CreateFeatureRequest{
			Name:      spec.displayName,
			Type:      types.FeatureTypeMetered,
			LookupKey: strPtr(spec.lookupKey),
			Meter:     &meterReq,
			Metadata:  seedMetadata(spec.aggLabel),
		}
		resp, err := s.client.Features().Create(ctx, req)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"feature_lookup_key": spec.lookupKey}, "create feature %s: %w", spec.lookupKey, err)
		}
		if resp.FeatureResponse == nil {
			return e2eprobe.Errorf(map[string]string{"feature_lookup_key": spec.lookupKey}, "create feature %s: empty response", spec.lookupKey)
		}
		feat := resp.FeatureResponse
		if feat.ID != nil {
			out.FeatureIDs = append(out.FeatureIDs, *feat.ID)
			if spec.isBucketed() {
				if out.BucketedFeatureIDs == nil {
					out.BucketedFeatureIDs = map[string]string{}
				}
				out.BucketedFeatureIDs[spec.lookupKey] = *feat.ID
			}
		}
		if feat.MeterID != nil {
			out.MeterIDs[spec.eventName] = *feat.MeterID
		}
	}
	return nil
}

// ensureCoupons idempotently provisions the shared E2EPROBE_COUPON_10PCT
// coupon reused by coupon-application-probe and by seed's attachment on
// persistent cust #1.
//
// Idempotency uses GetCouponByCode (GET /coupons/code/{code}) rather than
// the search endpoint. The repo lowercases coupon_code on INSERT and the
// GetByCode path normalizes the query and filters to StatusPublished
// (see internal/repository/ent/coupon.go:159) — the search endpoint's
// CouponCodes filter is case-sensitive and was silently missing the
// stored lowercase row on staging, causing the create below to 409 on
// every run.
func (s *SeedEnsure) ensureCoupons(ctx context.Context, out *e2eprobe.Seeds) error {
	existResp, err := s.client.Coupons().GetByCode(ctx, SharedCouponCode)
	if err == nil && existResp != nil && existResp.CouponResponse != nil && existResp.CouponResponse.ID != nil {
		out.SharedCouponID = *existResp.CouponResponse.ID
		out.SharedCouponCode = SharedCouponCode
		return nil
	}
	if err != nil && !isNotFound(err) {
		return e2eprobe.Errorf(map[string]string{"step": "get_coupon_by_code", "coupon_code": SharedCouponCode}, "lookup coupon by code: %w", err)
	}

	code := SharedCouponCode
	percentage := "10"
	createResp, err := s.client.Coupons().Create(ctx, types.CreateCouponRequest{
		Name:          SharedCouponName,
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		CouponCode:    &code,
		PercentageOff: &percentage,
		Metadata: map[string]string{
			"e2eprobe":      "true",
			"e2eprobe_role": "seed",
		},
	})
	if err != nil {
		// A concurrent seed run (or a manual create) can win the race between
		// the lookup above and this create; re-fetch by code so the caller
		// still has SharedCouponID populated.
		if isAlreadyExists(err) {
			retryResp, retryErr := s.client.Coupons().GetByCode(ctx, SharedCouponCode)
			if retryErr == nil && retryResp != nil && retryResp.CouponResponse != nil && retryResp.CouponResponse.ID != nil {
				out.SharedCouponID = *retryResp.CouponResponse.ID
				out.SharedCouponCode = SharedCouponCode
				return nil
			}
		}
		return e2eprobe.Errorf(map[string]string{"coupon_code": SharedCouponCode}, "create coupon: %w", err)
	}
	if createResp.CouponResponse != nil && createResp.CouponResponse.ID != nil {
		out.SharedCouponID = *createResp.CouponResponse.ID
	}
	out.SharedCouponCode = SharedCouponCode
	return nil
}

// ensureTaxRates idempotently provisions the shared E2EPROBE_TAX_10PCT tax
// rate (10% percentage, EXTERNAL scope) reused by tax-application-probe and
// by the persistent tax association on customer #0.
//
// SDK v2.0.24's TaxRates.List (GET /taxes/rates) is broken: the server
// returns {items, pagination} but the SDK expects a bare array (schema-
// annotation mismatch in the server's Swagger doc). Rather than call the
// broken List method, this step attempts Create and treats an already-
// exists response (HTTP 409 or Code=="already_exists") as success. On a
// duplicate we cannot recover the existing ID (there is no exposed
// GET-by-code endpoint), so out.SharedTaxRateID stays empty on that path
// and downstream callers must tolerate that.
func (s *SeedEnsure) ensureTaxRates(ctx context.Context, out *e2eprobe.Seeds) error {
	// Populate the code first so downstream soft-skips (that only need code)
	// still get it even if Create returns an unexpected error below.
	out.SharedTaxRateCode = SharedTaxRateCode

	percentage := "10"
	scope := types.TaxRateScopeExternal
	trType := types.TaxRateTypePercentage
	createResp, err := s.client.TaxRates().Create(ctx, types.CreateTaxRateRequest{
		Code:            SharedTaxRateCode,
		Name:            SharedTaxRateName,
		PercentageValue: &percentage,
		Scope:           &scope,
		TaxRateType:     &trType,
		Metadata: map[string]string{
			"e2eprobe":      "true",
			"e2eprobe_role": "seed",
		},
	})
	if err == nil {
		if createResp.TaxRateResponse != nil && createResp.TaxRateResponse.ID != nil {
			out.SharedTaxRateID = *createResp.TaxRateResponse.ID
		}
		return nil
	}
	if isAlreadyExists(err) {
		if s.logger != nil {
			s.logger.Info(ctx, "ensureTaxRates: tax rate already exists (no ID lookup available in SDK v2.0.24, downstream will fall back to code match)",
				"tax_rate_code", SharedTaxRateCode,
			)
		}
		return nil
	}
	return e2eprobe.Errorf(map[string]string{"tax_rate_code": SharedTaxRateCode}, "create tax rate: %w", err)
}

// isAlreadyExists returns true when the given SDK error represents a
// "resource already exists" (HTTP 409, or an ErrorResponse with Code
// "already_exists"). Used by seed steps for create-then-swallow-duplicate
// idempotency where a list-then-create round-trip is unavailable.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.HTTPStatusCode != nil && *errResp.HTTPStatusCode == http.StatusConflict {
			return true
		}
		if errResp.Code != nil && *errResp.Code == types.ErrorCodeAlreadyExists {
			return true
		}
	}
	var apiErr *sdkerrors.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusConflict {
			return true
		}
		// The generic 4xx SDK path returns *APIError with the raw JSON body;
		// fall back to substring match on the error code within the body.
		if strings.Contains(apiErr.Body, `"code":"already_exists"`) {
			return true
		}
	}
	return false
}

// bucketedFeatureLookupKeys is the set of lookup keys whose features are
// bucketed meters — plan-level entitlements are NOT provisioned for these
// because the server rejects entitlements on bucketed max meters
// ("Bucketed max meters process each bucket independently and cannot
// have entitlements"). Derived from seedFeatureSpecs (bucketSize != nil)
// so a new bucketed spec is auto-skipped without touching this file.
var bucketedFeatureLookupKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, spec := range seedFeatureSpecs {
		if spec.isBucketed() {
			m[spec.lookupKey] = true
		}
	}
	return m
}()

// entitlementExemptLookupKeys is every feature ensurePlanEntitlements must
// leave alone: bucketed meters (server rejects entitlements on them) plus
// specs that opted out via noEntitlement.
var entitlementExemptLookupKeys = func() map[string]bool {
	m := map[string]bool{}
	for k := range bucketedFeatureLookupKeys {
		m[k] = true
	}
	for _, spec := range seedFeatureSpecs {
		if spec.noEntitlement {
			m[spec.lookupKey] = true
		}
	}
	return m
}()

// grantOnlyFeatures is the set of feature lookup keys reserved for
// grant entitlements. ensurePlanEntitlements skips these; the grant
// entitlement is created by ensureEntitlementGrants instead. The DB
// enforces at most one non-parallel entitlement per (entity, feature),
// so trying to add both a soft-limit AND an additive grant here would
// conflict at INSERT time.
var grantOnlyFeatures = map[string]bool{
	AdditiveGrantFeatureLookupKey: true,
}

// ensurePlanEntitlements idempotently provisions one plan-level soft-limit
// entitlement per non-bucketed metered feature (limit=100, reset MONTHLY).
// Soft-limit is deliberate: hard-limit would reject the ingest driver's
// traffic and pollute every other probe.
func (s *SeedEnsure) ensurePlanEntitlements(ctx context.Context, out *e2eprobe.Seeds) error {
	if len(out.PlanIDs) == 0 {
		return nil
	}
	planID := out.PlanIDs[0]

	existResp, err := s.client.Entitlements().Query(ctx, types.EntitlementFilter{
		PlanIds: []string{planID},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"plan_id": planID}, "query plan entitlements: %w", err)
	}
	existByFeature := map[string]string{}
	if existResp.ListEntitlementsResponse != nil {
		for _, e := range existResp.ListEntitlementsResponse.Items {
			if e.FeatureID != nil && e.ID != nil {
				existByFeature[*e.FeatureID] = *e.ID
			}
		}
	}

	// Resolve non-bucketed feature IDs by lookup key. Also skip the
	// grant-only feature — ensureEntitlementGrants owns its (plan, feature)
	// slot, and adding a soft-limit here would DB-conflict.
	lookupKeys := make([]string, 0, len(seedFeatureSpecs))
	for _, spec := range seedFeatureSpecs {
		if entitlementExemptLookupKeys[spec.lookupKey] {
			continue
		}
		if grantOnlyFeatures[spec.lookupKey] {
			continue
		}
		lookupKeys = append(lookupKeys, spec.lookupKey)
	}
	featResp, err := s.client.Features().Query(ctx, types.FeatureFilter{LookupKeys: lookupKeys})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"plan_id": planID}, "query features for entitlements: %w", err)
	}
	if featResp.ListFeaturesResponse == nil {
		return nil
	}

	limit := int64(100)
	resetPeriod := types.EntitlementUsageResetPeriodMonthly
	softLimit := true
	enabled := true
	planIDCopy := planID
	entityType := types.EntitlementEntityTypePlan

	for _, feat := range featResp.ListFeaturesResponse.Items {
		if feat.ID == nil {
			continue
		}
		featID := *feat.ID
		if existID, ok := existByFeature[featID]; ok {
			out.PlanEntitlementIDs = append(out.PlanEntitlementIDs, existID)
			continue
		}
		createResp, err := s.client.Entitlements().Create(ctx, types.CreateEntitlementRequest{
			FeatureID:        featID,
			FeatureType:      types.FeatureTypeMetered,
			EntityID:         &planIDCopy,
			EntityType:       &entityType,
			PlanID:           &planIDCopy,
			UsageLimit:       &limit,
			UsageResetPeriod: &resetPeriod,
			IsSoftLimit:      &softLimit,
			IsEnabled:        &enabled,
		})
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"plan_id": planID, "feature_id": featID}, "create entitlement for feature %s: %w", featID, err)
		}
		if createResp.EntitlementResponse != nil && createResp.EntitlementResponse.ID != nil {
			out.PlanEntitlementIDs = append(out.PlanEntitlementIDs, *createResp.EntitlementResponse.ID)
		}
	}
	return nil
}

func (s *SeedEnsure) ensureCustomers(ctx context.Context, out *e2eprobe.Seeds) error {
	for i := 0; i < PersistentCustomerCount; i++ {
		ext := persistentExternalCustomerID(i)
		out.PersistentCustomerIDs = append(out.PersistentCustomerIDs, ext)
		_, err := s.client.Customers().GetByExternalID(ctx, ext)
		if err == nil {
			continue // already exists
		}
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode != http.StatusNotFound {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": ext}, "lookup customer %s: %w", ext, err)
		}
		req := types.CreateCustomerRequest{
			ExternalID: ext,
			Name:       fmt.Sprintf("E2EProbe Persistent %d", i),
			Email:      strPtr(fmt.Sprintf("%s@e2eprobe.flexprice.invalid", ext)),
			Metadata: map[string]string{
				"e2eprobe":        "true",
				"e2eprobe_cohort": "persistent",
				"e2eprobe_role":   "seed",
				"e2eprobe_run_id": s.runID,
				"created_unix_ns": fmt.Sprintf("%d", time.Now().UnixNano()),
			},
		}
		if _, err := s.client.Customers().Create(ctx, req); err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": ext}, "create customer %s: %w", ext, err)
		}
	}
	for i := 0; i < PreFundedWalletCount && i < PersistentCustomerCount; i++ {
		out.PreFundedCustomerIDs = append(out.PreFundedCustomerIDs, persistentExternalCustomerID(i))
	}

	// Snapshot IngestCustomerIDs BEFORE the canary is appended. Ingest driver
	// and read-side aggregation checks target this list so the canary never
	// receives random events — otherwise its real_time_balance drifts and
	// LowBalanceAlertProbe can't drive a known drop across critical.
	out.IngestCustomerIDs = append(out.IngestCustomerIDs, out.PersistentCustomerIDs...)

	// Alert-canary customer: separate persistent customer used only for
	// low-balance webhook pipeline verification. Added to PersistentCustomerIDs
	// so ensureSubscriptions gives it a plan sub (required for ongoing-balance
	// projection), but deliberately kept out of PreFundedCustomerIDs and
	// IngestCustomerIDs.
	if err := s.ensureAlertCanaryCustomer(ctx); err != nil {
		return err
	}
	out.PersistentCustomerIDs = append(out.PersistentCustomerIDs, AlertCanaryExternalCustomerID)
	out.AlertCanaryExternalCustomerID = AlertCanaryExternalCustomerID
	return nil
}

func (s *SeedEnsure) ensureAlertCanaryCustomer(ctx context.Context) error {
	ext := AlertCanaryExternalCustomerID
	_, err := s.client.Customers().GetByExternalID(ctx, ext)
	if err == nil {
		return nil
	}
	var apiErr *sdkerrors.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode != http.StatusNotFound {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": ext}, "lookup alert canary: %w", err)
	}
	req := types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       "E2EProbe Alert Canary",
		Email:      strPtr(fmt.Sprintf("%s@e2eprobe.flexprice.invalid", ext)),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "persistent",
			"e2eprobe_role":   "alert-canary",
			"e2eprobe_run_id": s.runID,
			"created_unix_ns": fmt.Sprintf("%d", time.Now().UnixNano()),
		},
	}
	if _, err := s.client.Customers().Create(ctx, req); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": ext}, "create alert canary: %w", err)
	}
	return nil
}

const e2eprobePlanLookupKey = "e2eprobe_plan"

// ensurePlan creates a single e2eprobe plan if it doesn't exist.
func (s *SeedEnsure) ensurePlan(ctx context.Context, out *e2eprobe.Seeds) error {
	resp, err := s.client.Plans().Query(ctx, types.PlanFilter{
		LookupKey: strPtr(e2eprobePlanLookupKey),
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_plans"}, "query plans: %w", err)
	}
	if resp.ListPlansResponse != nil && len(resp.ListPlansResponse.Items) > 0 {
		plan := resp.ListPlansResponse.Items[0]
		if plan.ID != nil {
			out.PlanIDs = []string{*plan.ID}
		}
		return nil
	}

	req := types.CreatePlanRequest{
		Name:        "E2EProbe Plan",
		LookupKey:   strPtr(e2eprobePlanLookupKey),
		Description: strPtr("Plan used by the e2eprobe synthetic monitoring harness"),
		Metadata: map[string]string{
			"e2eprobe":      "true",
			"e2eprobe_role": "seed",
		},
	}
	createResp, err := s.client.Plans().Create(ctx, req)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"plan_lookup_key": e2eprobePlanLookupKey}, "create plan: %w", err)
	}
	if createResp.PlanResponse == nil || createResp.PlanResponse.ID == nil {
		return e2eprobe.Errorf(map[string]string{"plan_lookup_key": e2eprobePlanLookupKey}, "create plan: empty response")
	}
	out.PlanIDs = []string{*createResp.PlanResponse.ID}
	return nil
}

// ensurePrices attaches prices to the plan: 1 base fixed price + 1 usage price per feature.
// Prices are not stored in Seeds — they're internal to the plan.
func (s *SeedEnsure) ensurePrices(ctx context.Context, seeds *e2eprobe.Seeds) error {
	if len(seeds.PlanIDs) == 0 {
		return nil // no plan, skip
	}
	planID := seeds.PlanIDs[0]
	planEntityType := types.PriceEntityTypePlan

	// Query existing prices for this plan.
	existResp, err := s.client.Prices().Query(ctx, types.PriceFilter{
		PlanIds:    []string{planID},
		EntityType: &planEntityType,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"plan_id": planID}, "query prices for plan %s: %w", planID, err)
	}

	existByLookup := map[string]bool{}
	existByMeter := map[string]bool{}
	if existResp.ListPricesResponse != nil {
		for _, p := range existResp.ListPricesResponse.Items {
			if p.LookupKey != nil {
				existByLookup[*p.LookupKey] = true
			}
			if p.MeterID != nil {
				existByMeter[*p.MeterID] = true
			}
		}
	}

	// Base recurring fixed price.
	if !existByLookup["e2eprobe_base_price"] {
		baseReq := types.CreatePriceRequest{
			EntityID:           planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeFixed,
			BillingModel:       types.BillingModelFlatFee,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("19.99"),
			Currency:           "USD",
			DisplayName:        strPtr("E2EProbe Base Fee"),
			LookupKey:          strPtr("e2eprobe_base_price"),
		}
		if _, err := s.client.Prices().Create(ctx, baseReq); err != nil {
			return e2eprobe.Errorf(map[string]string{"plan_id": planID, "price_lookup_key": "e2eprobe_base_price"}, "create base price: %w", err)
		}
	}

	// One usage price per feature/meter.
	for _, spec := range seedFeatureSpecs {
		meterID, ok := seeds.MeterIDs[spec.eventName]
		if !ok {
			continue // meter not provisioned, skip
		}
		if existByMeter[meterID] {
			continue // already has a price for this meter
		}
		usageKey := "e2eprobe_usage_" + spec.eventName
		if existByLookup[usageKey] {
			continue
		}
		usageReq := types.CreatePriceRequest{
			EntityID:           planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeUsage,
			BillingModel:       types.BillingModelFlatFee,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("0.01"),
			Currency:           "USD",
			MeterID:            strPtr(meterID),
			DisplayName:        strPtr("E2EProbe " + spec.displayName + " Usage"),
			LookupKey:          strPtr(usageKey),
		}
		// CreateBucketed falls through to the typed Create when the spec has no
		// price-level bucket, so unbucketed specs are unaffected.
		var priceBucket string
		if spec.priceBucketSize != nil {
			priceBucket = string(*spec.priceBucketSize)
		}
		if _, err := s.client.Prices().CreateBucketed(ctx, usageReq, priceBucket); err != nil {
			return e2eprobe.Errorf(map[string]string{"plan_id": planID, "event_name": spec.eventName, "price_bucket_size": priceBucket}, "create usage price for %s: %w", spec.eventName, err)
		}
	}
	return nil
}

// ensureEntitlementGrants idempotently provisions one plan-level additive
// grant entitlement on AdditiveGrantFeatureLookupKey. Reconciliation cases:
//   - grant entitlement already present on (plan, feature): skip; capture id.
//   - legacy soft-limit entitlement present on (plan, feature): delete it
//     (frees the (entity, feature) slot the additive grant needs), then
//     create the grant.
//   - nothing present: create the grant.
//
// After Create, an immediate GetRaw round-trip verifies the server stored
// the grant config as sent — catches silent field drops or mistranslation.
func (s *SeedEnsure) ensureEntitlementGrants(ctx context.Context, out *e2eprobe.Seeds) error {
	if len(out.PlanIDs) == 0 {
		return nil
	}
	planID := out.PlanIDs[0]

	// Resolve the target feature.
	featResp, err := s.client.Features().Query(ctx, types.FeatureFilter{
		LookupKeys: []string{AdditiveGrantFeatureLookupKey},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_feature", "feature_lookup_key": AdditiveGrantFeatureLookupKey}, "query grant feature: %w", err)
	}
	if featResp.ListFeaturesResponse == nil || len(featResp.ListFeaturesResponse.Items) == 0 {
		return nil // grant feature not seeded yet — soft skip
	}
	if featResp.ListFeaturesResponse.Items[0].ID == nil {
		return nil
	}
	featID := *featResp.ListFeaturesResponse.Items[0].ID

	// Query existing entitlements on this (plan, feature).
	existResp, err := s.client.Entitlements().Query(ctx, types.EntitlementFilter{
		PlanIds:    []string{planID},
		FeatureIds: []string{featID},
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "query_existing_entitlements", "plan_id": planID, "feature_id": featID}, "query existing entitlements: %w", err)
	}

	var existingGrantID string
	var legacySoftLimitID string
	if existResp.ListEntitlementsResponse != nil {
		for _, e := range existResp.ListEntitlementsResponse.Items {
			if e.ID == nil {
				continue
			}
			// The SDK's EntitlementResponse doesn't expose grant fields, so
			// we use GetRaw to inspect grant_measure — its presence signals a
			// grant entitlement, its absence a legacy soft-limit one.
			raw, rawErr := s.client.Entitlements().GetRaw(ctx, *e.ID)
			if rawErr != nil {
				// Never classify on a failed read: a transient 5xx / timeout
				// would otherwise mark a healthy grant entitlement as legacy
				// and delete it below. Bubble the error up — the caller
				// (ensureEntitlementGrants) is non-fatal, so on-call gets one
				// log line and the next tick retries without destroying state.
				return e2eprobe.Errorf(map[string]string{
					"step":           "classify_existing_entitlement",
					"plan_id":        planID,
					"feature_id":     featID,
					"entitlement_id": *e.ID,
				}, "read existing entitlement for classification: %w", rawErr)
			}
			if raw.GrantMeasure != "" {
				existingGrantID = *e.ID
				break
			}
			legacySoftLimitID = *e.ID
		}
	}

	if existingGrantID != "" {
		// Idempotent-run case.
		if out.GrantEntitlementIDs == nil {
			out.GrantEntitlementIDs = map[string]string{}
		}
		out.GrantEntitlementIDs[AdditiveGrantFeatureLookupKey] = existingGrantID
		return s.assertGrantConfigEcho(ctx, existingGrantID, featID, planID)
	}

	if legacySoftLimitID != "" {
		// Legacy soft-limit entitlement from a pre-migration seed run.
		// Delete it to free the (plan, feature) slot for the grant.
		if _, err := s.client.Entitlements().Delete(ctx, legacySoftLimitID); err != nil && !isNotFound(err) {
			return e2eprobe.Errorf(map[string]string{"step": "delete_legacy_entitlement", "plan_id": planID, "feature_id": featID, "entitlement_id": legacySoftLimitID}, "delete legacy soft-limit entitlement: %w", err)
		}
		if s.logger != nil {
			s.logger.Info(ctx, "ensureEntitlementGrants: deleted legacy soft-limit entitlement to make room for grant",
				"plan_id", planID,
				"feature_id", featID,
				"entitlement_id", legacySoftLimitID,
			)
		}
	}

	// Create the additive grant entitlement.
	// Server enum is uppercase ("PLAN"/"SUBSCRIPTION"/"ADDON"); passing
	// lowercase fails validation with "Only PLAN, ADDON, and SUBSCRIPTION
	// entity types are supported" (see internal/types/entitlement.go:51 and
	// internal/ee/service/entitlement.go:107). Use the SDK constant to avoid drift.
	createReq := e2eprobe.GrantEntitlementInput{
		FeatureID:          featID,
		FeatureType:        "metered",
		PlanID:             planID,
		EntityType:         string(types.EntitlementEntityTypePlan),
		EntityID:           planID,
		IsEnabled:          true,
		GrantMeasure:       "quantity",
		GrantQuota:         AdditiveGrantQuota,
		GrantDurationValue: AdditiveGrantDurationValue,
		GrantDurationUnit:  AdditiveGrantDurationUnit,
		AggregationMode:    "additive",
	}
	id, err := s.client.Entitlements().CreateWithGrant(ctx, createReq)
	if err != nil {
		if isAlreadyExists(err) {
			if s.logger != nil {
				s.logger.Info(ctx, "ensureEntitlementGrants: grant entitlement already exists (concurrent create)",
					"plan_id", planID,
					"feature_id", featID,
				)
			}
			return nil
		}
		return e2eprobe.Errorf(map[string]string{"step": "create_grant_entitlement", "plan_id": planID, "feature_id": featID}, "create grant entitlement: %w", err)
	}

	if out.GrantEntitlementIDs == nil {
		out.GrantEntitlementIDs = map[string]string{}
	}
	out.GrantEntitlementIDs[AdditiveGrantFeatureLookupKey] = id

	return s.assertGrantConfigEcho(ctx, id, featID, planID)
}

// assertGrantConfigEcho fetches the entitlement via raw HTTP and asserts
// the grant fields we sent came back unchanged. Catches silent server-side
// field drops that would otherwise leave the probe running against a
// ghost entitlement.
func (s *SeedEnsure) assertGrantConfigEcho(ctx context.Context, entitlementID, featID, planID string) error {
	raw, err := s.client.Entitlements().GetRaw(ctx, entitlementID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "get_grant_entitlement", "entitlement_id": entitlementID, "plan_id": planID, "feature_id": featID}, "get grant entitlement for echo verification: %w", err)
	}
	if raw.GrantMeasure != "quantity" {
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "grant_measure", "want": "quantity", "got": raw.GrantMeasure}, "grant_measure did not round-trip: want %q, got %q", "quantity", raw.GrantMeasure)
	}
	if !decimalEquals(raw.GrantQuota, AdditiveGrantQuota) {
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "grant_quota", "want": AdditiveGrantQuota, "got": raw.GrantQuota}, "grant_quota did not round-trip: want %q, got %q", AdditiveGrantQuota, raw.GrantQuota)
	}
	if raw.GrantDurationValue == nil || *raw.GrantDurationValue != AdditiveGrantDurationValue {
		gotStr := "<nil>"
		if raw.GrantDurationValue != nil {
			gotStr = fmt.Sprintf("%d", *raw.GrantDurationValue)
		}
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "grant_duration_value", "want": fmt.Sprintf("%d", AdditiveGrantDurationValue), "got": gotStr}, "grant_duration_value did not round-trip")
	}
	if raw.GrantDurationUnit != AdditiveGrantDurationUnit {
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "grant_duration_unit", "want": AdditiveGrantDurationUnit, "got": raw.GrantDurationUnit}, "grant_duration_unit did not round-trip: want %q, got %q", AdditiveGrantDurationUnit, raw.GrantDurationUnit)
	}
	if raw.AggregationMode != "additive" {
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "aggregation_mode", "want": "additive", "got": raw.AggregationMode}, "aggregation_mode did not round-trip: want %q, got %q", "additive", raw.AggregationMode)
	}
	if !raw.IsEnabled {
		return e2eprobe.Errorf(map[string]string{"step": "assert_grant_echo", "entitlement_id": entitlementID, "field": "is_enabled", "want": "true", "got": "false"}, "is_enabled did not round-trip: server returned disabled")
	}
	return nil
}

// decimalEquals compares two decimal-string values with tolerance for
// server-side normalisation of trailing zeros (e.g. "1000" vs "1000.0"
// vs "1000.00"). Both empty strings compare unequal; empty vs non-empty
// compares unequal.
func decimalEquals(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	da, errA := decimal.NewFromString(a)
	db, errB := decimal.NewFromString(b)
	if errA != nil || errB != nil {
		return false
	}
	return da.Equal(db)
}

// ensureSubscriptions creates subscriptions for all persistent customers on the e2eprobe plan.
func (s *SeedEnsure) ensureSubscriptions(ctx context.Context, seeds *e2eprobe.Seeds) error {
	if len(seeds.PlanIDs) == 0 || len(seeds.PersistentCustomerIDs) == 0 {
		return nil // prerequisites missing, skip
	}
	planID := seeds.PlanIDs[0]

	for _, extCustID := range seeds.PersistentCustomerIDs {
		extID := extCustID // capture
		// Check for existing subscription for this customer on this plan.
		existResp, err := s.client.Subscriptions().Query(ctx, types.SubscriptionFilter{
			ExternalCustomerID: &extID,
			PlanID:             &planID,
		})
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extID, "plan_id": planID}, "query subs for customer %s: %w", extID, err)
		}
		if existResp.ListSubscriptionsResponse != nil && len(existResp.ListSubscriptionsResponse.Items) > 0 {
			existing := existResp.ListSubscriptionsResponse.Items[0]
			if existing.ID != nil {
				seeds.PersistentSubIDs = append(seeds.PersistentSubIDs, *existing.ID)
			}
			continue
		}

		billingCycle := types.BillingCycleAnniversary
		now := time.Now().UTC()
		commitAmount := "5.00"
		commitDuration := types.BillingPeriodMonthly
		overageFactor := "1.5"
		req := types.CreateSubscriptionRequest{
			ExternalCustomerID: &extID,
			PlanID:             planID,
			Currency:           "usd",
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			BillingCycle:       &billingCycle,
			StartDate:          &now,
			// Commitment applies to every newly-created persistent sub.
			// Existing subs are not migrated (would break cycle-invoice-probe baseline).
			CommitmentAmount:   &commitAmount,
			CommitmentDuration: &commitDuration,
			OverageFactor:      &overageFactor,
			Metadata: map[string]string{
				"e2eprobe":        "true",
				"e2eprobe_role":   "seed",
				"e2eprobe_cohort": "persistent",
			},
		}
		// Attach shared coupon to persistent cust #1 at sub-create time (the
		// only hook the SDK exposes for coupon attachment). Same "new subs
		// only" caveat as commitment.
		if extID == persistentExternalCustomerID(1) && seeds.SharedCouponCode != "" {
			req.SubscriptionCoupons = []types.SubscriptionCouponInput{
				{CouponCode: seeds.SharedCouponCode},
			}
		}
		createResp, err := s.client.Subscriptions().Create(ctx, req)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extID, "plan_id": planID}, "create sub for customer %s: %w", extID, err)
		}
		if createResp.SubscriptionResponse == nil || createResp.SubscriptionResponse.ID == nil {
			continue // defensive: empty response, skip
		}
		subID := *createResp.SubscriptionResponse.ID
		seeds.PersistentSubIDs = append(seeds.PersistentSubIDs, subID)

		// Activate if in draft status.
		subStatus := createResp.SubscriptionResponse.SubscriptionStatus
		if subStatus != nil && *subStatus == types.SubscriptionStatusDraft {
			_, activateErr := s.client.Subscriptions().ActivateSubscription(ctx, subID,
				types.ActivateDraftSubscriptionRequest{
					StartDate: now,
				},
			)
			if activateErr != nil && s.logger != nil {
				// Log warning but don't fail — sub will still work for most checks.
				// Recovered path (sub still works); Info per LL003.
				s.logger.Info(ctx, "subscription activation failed; sub will still work for most checks",
					"subscription_id", subID,
					"external_customer_id", extID,
					"error", activateErr.Error(),
				)
			}
		}
	}
	// Multi-cadence coverage: one persistent quarterly sub for cust #0 so the
	// fan-out (monthly prices on quarterly sub → N monthly line items) is
	// exercised on every invoice cycle. Idempotent: query by metadata cohort
	// and reuse if present.
	if err := s.ensureMultiCadenceSubscription(ctx, seeds, planID); err != nil {
		return err
	}
	return nil
}

// multiCadenceCohort tags the quarterly-with-monthly-charges seed sub so
// ensureMultiCadenceSubscription can find it without name collision against
// the standard PersistentSub cohort.
const multiCadenceCohort = "multi_cadence_quarterly"

func (s *SeedEnsure) ensureMultiCadenceSubscription(
	ctx context.Context, seeds *e2eprobe.Seeds, planID string,
) error {
	if len(seeds.PersistentCustomerIDs) == 0 {
		return nil
	}
	// Use a dedicated customer to keep this sub isolated from other probes'
	// assumptions (cycle-invoice-probe, entitlement-and-usage, etc. iterate
	// PersistentSubIDs which we deliberately do NOT append this sub to).
	extID := persistentExternalCustomerID(0)

	existResp, err := s.client.Subscriptions().Query(ctx, types.SubscriptionFilter{
		ExternalCustomerID: &extID,
		PlanID:             &planID,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "multi_cadence_query", "external_customer_id": extID, "plan_id": planID}, "query subs: %w", err)
	}
	if existResp.ListSubscriptionsResponse != nil {
		for _, sub := range existResp.ListSubscriptionsResponse.Items {
			if sub.ID == nil || sub.Metadata == nil {
				continue
			}
			cohort, ok := sub.Metadata["e2eprobe_cohort"]
			if !ok || cohort != multiCadenceCohort {
				continue
			}
			// Found the seed sub. If it's still Draft (previous tick's activation
			// failed or was skipped), retry activation now — otherwise the probe
			// silently waits forever with no observable invoices. Only mark the
			// seed complete once activation succeeds so a failure re-runs next tick.
			if sub.SubscriptionStatus != nil && *sub.SubscriptionStatus == types.SubscriptionStatusDraft {
				if err := s.activateMultiCadenceSub(ctx, *sub.ID, extID, time.Now().UTC()); err != nil {
					return err
				}
			}
			seeds.MultiCadenceSubID = *sub.ID
			return nil
		}
	}

	// Fetch every published plan price so we can pass their IDs via
	// include_price_ids. Without the opt-in, the plan-attach filter now
	// defaults to strict-equal cadence (post-PR #2713) and a QUARTERLY sub
	// against a MONTHLY-only plan returns "no prices found for entity".
	published := types.StatusPublished
	pricesResp, err := s.client.Prices().Query(ctx, types.PriceFilter{
		PlanIds: []string{planID},
		Status:  &published,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "multi_cadence_prices_query", "plan_id": planID}, "list plan prices: %w", err)
	}
	var includePriceIDs []string
	if pricesResp != nil && pricesResp.ListPricesResponse != nil {
		for _, p := range pricesResp.ListPricesResponse.Items {
			if p.ID != nil && *p.ID != "" {
				includePriceIDs = append(includePriceIDs, *p.ID)
			}
		}
	}
	if len(includePriceIDs) == 0 {
		// No plan prices found → an empty include_price_ids would create a sub
		// with zero attached prices, which fails downstream. Skip and let the
		// next tick retry after the plan's price seeding catches up.
		if s.logger != nil {
			s.logger.Info(ctx, "multi-cadence seed sub: skipping create — plan has no published prices yet",
				"plan_id", planID,
			)
		}
		return nil
	}

	billingCycle := types.BillingCycleAnniversary
	now := time.Now().UTC()
	req := types.CreateSubscriptionRequest{
		ExternalCustomerID: &extID,
		PlanID:             planID,
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodQuarterly,
		BillingPeriodCount: int64Ptr(1),
		BillingCycle:       &billingCycle,
		StartDate:          &now,
		// Opt into multi-cadence: attach every monthly plan price to the
		// quarterly sub. Invoice generation fans each out per sub-window.
		IncludePriceIds: includePriceIDs,
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_role":   "seed",
			"e2eprobe_cohort": multiCadenceCohort,
		},
	}
	createResp, err := s.client.Subscriptions().Create(ctx, req)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"step": "multi_cadence_create", "external_customer_id": extID, "plan_id": planID}, "create quarterly sub: %w", err)
	}
	if createResp.SubscriptionResponse == nil || createResp.SubscriptionResponse.ID == nil {
		return nil
	}
	subID := *createResp.SubscriptionResponse.ID

	if createResp.SubscriptionResponse.SubscriptionStatus != nil &&
		*createResp.SubscriptionResponse.SubscriptionStatus == types.SubscriptionStatusDraft {
		if err := s.activateMultiCadenceSub(ctx, subID, extID, now); err != nil {
			// Do NOT record MultiCadenceSubID — leaves the sub for retry on next
			// tick (query-by-cohort will find it and activate again).
			return err
		}
	}
	seeds.MultiCadenceSubID = subID
	return nil
}

// activateMultiCadenceSub attempts to activate a draft multi-cadence seed sub.
// Returns a wrapped error if activation fails so the caller can propagate it
// and the next tick can retry.
func (s *SeedEnsure) activateMultiCadenceSub(ctx context.Context, subID, extID string, startDate time.Time) error {
	if _, err := s.client.Subscriptions().ActivateSubscription(ctx, subID,
		types.ActivateDraftSubscriptionRequest{StartDate: startDate}); err != nil {
		if s.logger != nil {
			s.logger.Info(ctx, "multi-cadence sub activation failed; will retry next tick",
				"subscription_id", subID,
				"external_customer_id", extID,
				"error", err.Error(),
			)
		}
		return e2eprobe.Errorf(map[string]string{
			"step":                 "multi_cadence_activate",
			"subscription_id":      subID,
			"external_customer_id": extID,
		}, "activate multi-cadence sub %s: %w", subID, err)
	}
	return nil
}

// ensurePersistentTaxAssociation attaches the shared E2EPROBE_TAX_10PCT
// tax rate to persistent cust #0's subscription. Idempotency lists all
// tax associations for the subscription (filtered by entity_type + entity_id
// only — NOT by tax_rate_id, since SharedTaxRateID may be empty when the
// tax rate pre-existed) and treats "any association present" as done. Safe
// because the seed only ever attaches one tax rate to this sub.
//
// Unlike coupons (attached at sub-create via SubscriptionCoupons), tax
// associations are a separate API call, so this covers both freshly-
// created and pre-existing seed subs.

// ensureSubscriptionPriceSync repairs persistent subscriptions whose line
// items predate a plan price.
//
// Subscription line items snapshot the plan at create time. Every usage read
// path filters by the meters on a customer's ACTIVE subscription line items
// (meterUsageService.activeSubscriptionMeterIDs), so a meter seeded after the
// persistent subs were created returns zero usage forever — even though its
// events ingest and aggregate fine. That is what silently broke
// meter-aggregation-probe and bucketed-meter-probe's analytics assertion for
// the three bucketed meters added on 2026-08-12.
//
// The repair is idempotent and only fires on drift: compare the meters the
// plan prices against the meters each persistent sub carries, and trigger the
// plan price sync when any are missing. The sync runs as a background
// workflow server-side; line items appear shortly after.
func (s *SeedEnsure) ensureSubscriptionPriceSync(ctx context.Context, seeds *e2eprobe.Seeds) error {
	if len(seeds.PlanIDs) == 0 || len(seeds.PersistentSubIDs) == 0 {
		return nil
	}
	planID := seeds.PlanIDs[0]

	planEntityType := types.PriceEntityTypePlan
	priceResp, err := s.client.Prices().Query(ctx, types.PriceFilter{
		PlanIds:    []string{planID},
		EntityType: &planEntityType,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"plan_id": planID}, "query prices for plan %s: %w", planID, err)
	}
	pricedMeters := map[string]bool{}
	if priceResp.ListPricesResponse != nil {
		for _, pr := range priceResp.ListPricesResponse.Items {
			if pr.MeterID != nil && *pr.MeterID != "" {
				pricedMeters[*pr.MeterID] = true
			}
		}
	}
	if len(pricedMeters) == 0 {
		return nil
	}

	missing := map[string]bool{}
	for _, subID := range seeds.PersistentSubIDs {
		subResp, err := s.client.Subscriptions().Get(ctx, subID)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"plan_id": planID, "subscription_id": subID}, "get sub %s: %w", subID, err)
		}
		if subResp.SubscriptionResponse == nil {
			continue
		}
		have := map[string]bool{}
		for _, li := range subResp.SubscriptionResponse.LineItems {
			if li.MeterID != nil && *li.MeterID != "" {
				have[*li.MeterID] = true
			}
		}
		for meterID := range pricedMeters {
			if !have[meterID] {
				missing[meterID] = true
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}

	if _, err := s.client.Plans().SyncPrices(ctx, planID); err != nil {
		// A sync already running for this plan answers 409 already_exists.
		// That's the outcome we want, not a failure — the in-flight run
		// covers the same drift.
		if isSyncInProgress(err) {
			if s.logger != nil {
				s.logger.Info(ctx, "seed-ensure: plan price sync already in progress; skipping",
					"plan_id", planID,
					"missing_meter_ids", strings.Join(sortedKeys(missing), ","),
				)
			}
			return nil
		}
		return e2eprobe.Errorf(map[string]string{"plan_id": planID, "missing_meter_ids": strings.Join(sortedKeys(missing), ",")}, "sync plan prices onto subscriptions: %w", err)
	}
	if s.logger != nil {
		s.logger.Info(ctx, "seed-ensure: synced plan prices onto persistent subscriptions",
			"plan_id", planID,
			"missing_meter_ids", strings.Join(sortedKeys(missing), ","),
			"subscription_count", len(seeds.PersistentSubIDs),
		)
	}
	return nil
}

// isSyncInProgress reports whether the error is the plan price sync's
// "a sync is already running for this plan" conflict.
//
// A bare 409 is deliberately not enough: an unrelated conflict would then be
// swallowed as "someone else is syncing" and never reach the caller's error
// path. Match the sync's own already_exists code instead.
func isSyncInProgress(err error) bool {
	if err == nil {
		return false
	}
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) && errResp.Code != nil && *errResp.Code == types.ErrorCodeAlreadyExists {
		return true
	}
	return strings.Contains(err.Error(), string(types.ErrorCodeAlreadyExists))
}

// sortedKeys returns the map keys in stable order so log lines and error
// attributes don't churn between runs.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *SeedEnsure) ensurePersistentTaxAssociation(ctx context.Context, out *e2eprobe.Seeds) error {
	if out.SharedTaxRateCode == "" {
		return nil // tax rate seed didn't run — soft skip
	}
	if len(out.PersistentCustomerIDs) == 0 || len(out.PersistentSubIDs) == 0 {
		return nil
	}
	// PersistentSubIDs[0] corresponds to PersistentCustomerIDs[0] because
	// ensureSubscriptions iterates PersistentCustomerIDs in order. Defensive
	// alignment check keeps this safe if that invariant ever changes.
	if out.PersistentCustomerIDs[0] != persistentExternalCustomerID(0) {
		return nil
	}
	subID := out.PersistentSubIDs[0]

	// Server enum is lowercase ("subscription"); passing uppercase fails
	// validation with "Invalid tax rate entity type" (see
	// internal/types/taxrate.go:72). Use the SDK constant to avoid drift.
	entityType := string(types.TaxRateEntityTypeSubscription)
	listResp, err := s.client.TaxAssociations().List(ctx, &entityType, &subID, nil, nil)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID, "tax_rate_code": out.SharedTaxRateCode}, "list tax associations: %w", err)
	}
	// The seed only ever attaches ONE tax rate to persistent cust #0's sub,
	// so any existing association is by definition OUR association. On the
	// happy path we match by SharedTaxRateID; when the ID is unknown (SDK
	// v2.0.24 TaxRates.List is broken and CreateTaxRate returned
	// "already exists"), we opportunistically backfill SharedTaxRateID from
	// the existing association's TaxRateID so downstream janitor / probe
	// assertions can use the strong ID match.
	if listResp.ListTaxAssociationsResponse != nil {
		for _, ta := range listResp.ListTaxAssociationsResponse.Items {
			if out.SharedTaxRateID != "" && ta.TaxRateID != nil && *ta.TaxRateID == out.SharedTaxRateID {
				return nil
			}
			if ta.TaxRate != nil && ta.TaxRate.Code != nil && *ta.TaxRate.Code == out.SharedTaxRateCode {
				if out.SharedTaxRateID == "" && ta.TaxRateID != nil {
					out.SharedTaxRateID = *ta.TaxRateID
				}
				return nil
			}
			// Unknown-code, unknown-ID fallback: seed invariant guarantees only
			// our shared rate is ever attached to cust #0's sub, so any
			// association wins. Backfill the ID from it.
			if out.SharedTaxRateID == "" && ta.TaxRateID != nil {
				out.SharedTaxRateID = *ta.TaxRateID
				return nil
			}
		}
	}

	autoApply := true
	tType := types.TaxRateEntityTypeSubscription
	createResp, err := s.client.TaxAssociations().Create(ctx, types.CreateTaxAssociationRequest{
		TaxRateCode: out.SharedTaxRateCode,
		EntityID:    &subID,
		EntityType:  &tType,
		AutoApply:   &autoApply,
		Metadata: map[string]string{
			"e2eprobe":      "true",
			"e2eprobe_role": "seed",
		},
	})
	if err != nil {
		if isAlreadyExists(err) {
			return nil // benign — a prior seed already attached it
		}
		return e2eprobe.Errorf(map[string]string{"subscription_id": subID, "tax_rate_code": out.SharedTaxRateCode}, "create tax association: %w", err)
	}
	// Opportunistically backfill SharedTaxRateID from the association response
	// so downstream janitor / probe assertions can use the stronger ID match.
	if out.SharedTaxRateID == "" && createResp.TaxAssociationResponse != nil && createResp.TaxAssociationResponse.TaxRateID != nil {
		out.SharedTaxRateID = *createResp.TaxAssociationResponse.TaxRateID
	}
	return nil
}

// ensureWallets creates and tops up a wallet for the first 3 persistent customers
// plus the dedicated alert-canary customer (lower balance, alert-driven).
func (s *SeedEnsure) ensureWallets(ctx context.Context, seeds *e2eprobe.Seeds) error {
	if seeds.AlertCanaryExternalCustomerID != "" {
		if err := s.ensureAlertCanaryWallet(ctx, seeds.AlertCanaryExternalCustomerID); err != nil {
			return err
		}
	}
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}

	for _, extCustID := range seeds.PreFundedCustomerIDs {
		// Look up internal customer ID.
		custResp, err := s.client.Customers().GetByExternalID(ctx, extCustID)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID}, "get customer %s for wallet: %w", extCustID, err)
		}
		if custResp.CustomerResponse == nil || custResp.CustomerResponse.ID == nil {
			continue // can't look up wallets without internal ID
		}
		internalCustID := *custResp.CustomerResponse.ID

		// Query wallets for this customer by internal ID.
		walletsResp, err := s.client.Wallets().GetWalletsByCustomerID(ctx, internalCustID)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "internal_customer_id": internalCustID}, "get wallets for customer %s: %w", extCustID, err)
		}
		if walletsResp != nil && len(walletsResp.WalletResponses) > 0 {
			continue // wallet already exists
		}

		// Create wallet with low-balance alert thresholds (info=25, warning=10,
		// critical=0). Enables the low-wallet-alert-listener check end-to-end:
		// Flexprice fires wallet.credit_balance.dropped webhooks as the balance
		// crosses each threshold, and the listener validates + tracks receipts.
		createReq := types.CreateWalletRequest{
			ExternalCustomerID: &extCustID,
			Currency:           "USD",
			Metadata: map[string]string{
				"e2eprobe":      "true",
				"e2eprobe_role": "seed",
			},
			AlertSettings: lowBalanceAlertSettings(),
		}
		walletResp, err := s.client.Wallets().Create(ctx, createReq)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "internal_customer_id": internalCustID}, "create wallet for customer %s: %w", extCustID, err)
		}
		if walletResp.WalletResponse == nil || walletResp.WalletResponse.ID == nil {
			continue // defensive
		}
		walletID := *walletResp.WalletResponse.ID

		// Top up to starting balance of 100 USD.
		topUpReq := types.TopUpWalletRequest{
			Amount:            strPtr("100.00"),
			Description:       strPtr("e2eprobe initial seed top-up"),
			TransactionReason: types.TransactionReasonPurchasedCreditDirect,
		}
		if _, err := s.client.Wallets().TopUp(ctx, walletID, topUpReq); err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "wallet_id": walletID}, "top up wallet %s for customer %s: %w", walletID, extCustID, err)
		}
	}
	return nil
}

// ensureAlertCanaryWallet provisions the canary customer's single low-balance
// wallet ($30 initial) with the {info=25, warning=10, critical=0} thresholds
// enabled. Idempotent — skips when a wallet already exists.
func (s *SeedEnsure) ensureAlertCanaryWallet(ctx context.Context, extCustID string) error {
	custResp, err := s.client.Customers().GetByExternalID(ctx, extCustID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID}, "get alert canary %s: %w", extCustID, err)
	}
	if custResp.CustomerResponse == nil || custResp.CustomerResponse.ID == nil {
		return nil
	}
	internalCustID := *custResp.CustomerResponse.ID

	walletsResp, err := s.client.Wallets().GetWalletsByCustomerID(ctx, internalCustID)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "internal_customer_id": internalCustID}, "get alert canary wallets: %w", err)
	}
	if walletsResp != nil && len(walletsResp.WalletResponses) > 0 {
		return nil
	}

	createReq := types.CreateWalletRequest{
		ExternalCustomerID: strPtr(extCustID),
		Currency:           "USD",
		Metadata: map[string]string{
			"e2eprobe":      "true",
			"e2eprobe_role": "alert-canary",
		},
		AlertSettings: lowBalanceAlertSettings(),
	}
	walletResp, err := s.client.Wallets().Create(ctx, createReq)
	if err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID}, "create alert canary wallet: %w", err)
	}
	if walletResp.WalletResponse == nil || walletResp.WalletResponse.ID == nil {
		return nil
	}
	walletID := *walletResp.WalletResponse.ID

	topUpReq := types.TopUpWalletRequest{
		Amount:            strPtr(AlertCanaryInitialBalance),
		Description:       strPtr("e2eprobe alert canary initial top-up"),
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
	}
	if _, err := s.client.Wallets().TopUp(ctx, walletID, topUpReq); err != nil {
		return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "wallet_id": walletID}, "top up alert canary wallet: %w", err)
	}
	return nil
}

func bucketSizePtr(w types.WindowSize) *types.WindowSize { return &w }
