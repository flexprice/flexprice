package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

const (
	PersistentCustomerCount = 10
	PreFundedWalletCount    = 3
)

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }

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

func (s *SeedEnsure) Name() string         { return "seed-ensure" }
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
	if err := s.ensureCustomers(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensurePlan(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensurePrices(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureSubscriptions(ctx, &seeds); err != nil {
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
	bucketSize  *types.WindowSize
	multiplier  *string
	expression  *string
	filters     []types.MeterFilter
	aggLabel    string
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
			lookupKey: "e2eprobe_weighted_sum_feature", eventName: "e2eprobe_weighted_sum",
			displayName: "E2EProbe WeightedSum", aggType: types.AggregationTypeWeightedSum,
			field: strPtr("amount"), expression: strPtr("double(amount) * double(duration_ms)"), aggLabel: "weighted_sum",
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
	}
}()

// ensureFeatures creates 9 features with embedded meters idempotently.
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
	byLookup := map[string]types.DtoFeatureResponse{}
	if existResp.DtoListFeaturesResponse != nil {
		for _, f := range existResp.DtoListFeaturesResponse.Items {
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
			}
			if existing.MeterID != nil {
				out.MeterIDs[spec.eventName] = *existing.MeterID
			}
			continue
		}

		aggType := spec.aggType
		meterReq := types.DtoCreateMeterRequest{
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

		req := types.DtoCreateFeatureRequest{
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
		if resp.DtoFeatureResponse == nil {
			return e2eprobe.Errorf(map[string]string{"feature_lookup_key": spec.lookupKey}, "create feature %s: empty response", spec.lookupKey)
		}
		feat := resp.DtoFeatureResponse
		if feat.ID != nil {
			out.FeatureIDs = append(out.FeatureIDs, *feat.ID)
		}
		if feat.MeterID != nil {
			out.MeterIDs[spec.eventName] = *feat.MeterID
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
		req := types.DtoCreateCustomerRequest{
			ExternalID: ext,
			Name:       strPtr(fmt.Sprintf("E2EProbe Persistent %d", i)),
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
	if resp.DtoListPlansResponse != nil && len(resp.DtoListPlansResponse.Items) > 0 {
		plan := resp.DtoListPlansResponse.Items[0]
		if plan.ID != nil {
			out.PlanIDs = []string{*plan.ID}
		}
		return nil
	}

	req := types.DtoCreatePlanRequest{
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
	if createResp.DtoPlanResponse == nil || createResp.DtoPlanResponse.ID == nil {
		return e2eprobe.Errorf(map[string]string{"plan_lookup_key": e2eprobePlanLookupKey}, "create plan: empty response")
	}
	out.PlanIDs = []string{*createResp.DtoPlanResponse.ID}
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
	if existResp.DtoListPricesResponse != nil {
		for _, p := range existResp.DtoListPricesResponse.Items {
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
		baseReq := types.DtoCreatePriceRequest{
			EntityID:           planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeFixed,
			BillingCadence:     types.BillingCadenceRecurring,
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
		usageReq := types.DtoCreatePriceRequest{
			EntityID:           planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeUsage,
			BillingCadence:     types.BillingCadenceRecurring,
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
		if _, err := s.client.Prices().Create(ctx, usageReq); err != nil {
			return e2eprobe.Errorf(map[string]string{"plan_id": planID, "event_name": spec.eventName}, "create usage price for %s: %w", spec.eventName, err)
		}
	}
	return nil
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
		if existResp.DtoListSubscriptionsResponse != nil && len(existResp.DtoListSubscriptionsResponse.Items) > 0 {
			existing := existResp.DtoListSubscriptionsResponse.Items[0]
			if existing.ID != nil {
				seeds.PersistentSubIDs = append(seeds.PersistentSubIDs, *existing.ID)
			}
			continue
		}

		billingCycle := types.BillingCycleAnniversary
		now := time.Now().UTC()
		req := types.DtoCreateSubscriptionRequest{
			ExternalCustomerID: &extID,
			PlanID:             planID,
			Currency:           "usd",
			BillingCadence:     types.BillingCadenceRecurring,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			BillingCycle:       &billingCycle,
			StartDate:          strPtr(now.Format(time.RFC3339)),
			Metadata: map[string]string{
				"e2eprobe":        "true",
				"e2eprobe_role":   "seed",
				"e2eprobe_cohort": "persistent",
			},
		}
		createResp, err := s.client.Subscriptions().Create(ctx, req)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extID, "plan_id": planID}, "create sub for customer %s: %w", extID, err)
		}
		if createResp.DtoSubscriptionResponse == nil || createResp.DtoSubscriptionResponse.ID == nil {
			continue // defensive: empty response, skip
		}
		subID := *createResp.DtoSubscriptionResponse.ID
		seeds.PersistentSubIDs = append(seeds.PersistentSubIDs, subID)

		// Activate if in draft status.
		subStatus := createResp.DtoSubscriptionResponse.SubscriptionStatus
		if subStatus != nil && *subStatus == types.SubscriptionStatusDraft {
			_, activateErr := s.client.Subscriptions().ActivateSubscription(ctx, subID,
				types.DtoActivateDraftSubscriptionRequest{
					StartDate: now.Format(time.RFC3339),
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
	return nil
}

// ensureWallets creates and tops up a wallet for the first 3 persistent customers.
func (s *SeedEnsure) ensureWallets(ctx context.Context, seeds *e2eprobe.Seeds) error {
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}

	for _, extCustID := range seeds.PreFundedCustomerIDs {
		// Look up internal customer ID.
		custResp, err := s.client.Customers().GetByExternalID(ctx, extCustID)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID}, "get customer %s for wallet: %w", extCustID, err)
		}
		if custResp.DtoCustomerResponse == nil || custResp.DtoCustomerResponse.ID == nil {
			continue // can't look up wallets without internal ID
		}
		internalCustID := *custResp.DtoCustomerResponse.ID

		// Query wallets for this customer by internal ID.
		walletsResp, err := s.client.Wallets().GetWalletsByCustomerID(ctx, internalCustID)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "internal_customer_id": internalCustID}, "get wallets for customer %s: %w", extCustID, err)
		}
		if walletsResp != nil && len(walletsResp.DtoWalletResponses) > 0 {
			continue // wallet already exists
		}

		// Create wallet.
		createReq := types.DtoCreateWalletRequest{
			ExternalCustomerID: &extCustID,
			Currency:           "USD",
			Metadata: map[string]string{
				"e2eprobe":      "true",
				"e2eprobe_role": "seed",
			},
		}
		walletResp, err := s.client.Wallets().Create(ctx, createReq)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"external_customer_id": extCustID, "internal_customer_id": internalCustID}, "create wallet for customer %s: %w", extCustID, err)
		}
		if walletResp.DtoWalletResponse == nil || walletResp.DtoWalletResponse.ID == nil {
			continue // defensive
		}
		walletID := *walletResp.DtoWalletResponse.ID

		// Top up to starting balance of 100 USD.
		topUpReq := types.DtoTopUpWalletRequest{
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
