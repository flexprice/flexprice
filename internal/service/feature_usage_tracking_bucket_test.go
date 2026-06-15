package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	eventsDomain "github.com/flexprice/flexprice/internal/domain/events"
	meterDomain "github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	subscriptionDomain "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatRatePriceService is a minimal PriceService stub for unit tests.
// CalculateCost returns price.Amount * quantity; all other methods panic.
type flatRatePriceService struct{}

func (f *flatRatePriceService) CalculateCost(_ context.Context, p *priceDomain.Price, qty decimal.Decimal) decimal.Decimal {
	return p.Amount.Mul(qty)
}
func (f *flatRatePriceService) CalculateBucketedCost(_ context.Context, p *priceDomain.Price, vals []decimal.Decimal) decimal.Decimal {
	sum := decimal.Zero
	for _, v := range vals {
		sum = sum.Add(p.Amount.Mul(v))
	}
	return sum
}
func (f *flatRatePriceService) CalculateCostFromUsageResults(_ context.Context, _ *priceDomain.Price, _ []eventsDomain.UsageResult) decimal.Decimal {
	return decimal.Zero
}
func (f *flatRatePriceService) CalculateCostWithBreakup(_ context.Context, p *priceDomain.Price, qty decimal.Decimal, _ bool) dto.CostBreakup {
	return dto.CostBreakup{FinalCost: p.Amount.Mul(qty)}
}
func (f *flatRatePriceService) CalculateCostSheetPrice(_ context.Context, p *priceDomain.Price, qty decimal.Decimal) decimal.Decimal {
	return p.Amount.Mul(qty)
}

// Unimplemented stubs — these should not be called during bucket-summary tests.
func (f *flatRatePriceService) CreatePrice(_ context.Context, _ dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) CreateBulkPrice(_ context.Context, _ dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPrice(_ context.Context, _ string) (*dto.PriceResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPricesByPlanID(_ context.Context, _ dto.GetPricesByPlanRequest) (*dto.ListPricesResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPricesBySubscriptionID(_ context.Context, _ string) (*dto.ListPricesResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPricesByAddonID(_ context.Context, _ string) (*dto.ListPricesResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPricesByCostsheetID(_ context.Context, _ string) (*dto.ListPricesResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) GetPrices(_ context.Context, _ *types.PriceFilter) (*dto.ListPricesResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) UpdatePrice(_ context.Context, _ string, _ dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	panic("not implemented")
}
func (f *flatRatePriceService) DeletePrice(_ context.Context, _ string, _ dto.DeletePriceRequest) error {
	panic("not implemented")
}
func (f *flatRatePriceService) GetByLookupKey(_ context.Context, _ string) (*dto.PriceResponse, error) {
	panic("not implemented")
}

// buildHourlyPoints returns a slice of 24 dto.UsageAnalyticPoints at hourly boundaries
// starting at baseDate. Each point carries 10 units of usage and no bucket attribution.
func buildHourlyPoints(baseDate time.Time) []dto.UsageAnalyticPoint {
	pts := make([]dto.UsageAnalyticPoint, 24)
	for i := 0; i < 24; i++ {
		pts[i] = dto.UsageAnalyticPoint{
			Timestamp: baseDate.Add(time.Duration(i) * time.Hour),
			Usage:     decimal.NewFromInt(10),
		}
	}
	return pts
}

// TestAnalytics_PerPointBucketAttribution verifies that when breakdown_bucket=true:
//   - Points whose Timestamp falls within the [09:00, 17:00) bucket receive BucketID/PriceID.
//   - Points outside the bucket receive empty BucketID/PriceID.
func TestAnalytics_PerPointBucketAttribution(t *testing.T) {
	ctx := context.Background()

	bucketID := "bkt_0001"
	bucketPriceID := "price_bucket"

	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:      "li_001",
		PriceID: "price_default",
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:      bucketID,
				Start:   types.Bucket{Hour: 9, Minute: 0},
				End:     types.Bucket{Hour: 17, Minute: 0},
				PriceID: bucketPriceID,
			},
		},
	}

	data := &AnalyticsData{
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{
			"li_001": lineItem,
		},
		Prices: map[string]*priceDomain.Price{
			"price_default": {ID: "price_default", Amount: decimal.NewFromFloat(1.0)},
			"price_bucket":  {ID: "price_bucket", Amount: decimal.NewFromFloat(2.0)},
		},
	}

	// Build 24 hourly points starting at midnight UTC on an arbitrary day.
	baseDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	points := buildHourlyPoints(baseDate)

	// Stamp the points with bucket attribution.
	for i := range points {
		if idx, ok := lineItem.CommitmentTimeBuckets.BucketIndexAt([]time.Time{points[i].Timestamp}, 0); ok {
			points[i].BucketID = lineItem.CommitmentTimeBuckets[idx].ID
			points[i].PriceID = lineItem.CommitmentTimeBuckets[idx].PriceID
		}
	}

	// Assert: hours 0-8 and 17-23 are out-of-bucket (empty BucketID).
	for i := 0; i < 9; i++ {
		assert.Empty(t, points[i].BucketID, "hour %d should be out-of-bucket", i)
		assert.Empty(t, points[i].PriceID, "hour %d should have no bucket PriceID", i)
	}
	// Assert: hours 9-16 are in-bucket.
	for i := 9; i < 17; i++ {
		assert.Equal(t, bucketID, points[i].BucketID, "hour %d should be in-bucket", i)
		assert.Equal(t, bucketPriceID, points[i].PriceID, "hour %d should have bucket PriceID", i)
	}
	// Assert: hours 17-23 are out-of-bucket again.
	for i := 17; i < 24; i++ {
		assert.Empty(t, points[i].BucketID, "hour %d should be out-of-bucket", i)
	}

	// Build bucket summaries from the stamped points.
	summaries := buildBucketSummaries(ctx, &flatRatePriceService{}, points, lineItem, data)

	// One summary per configured bucket; out-of-bucket usage is not summarized
	// (the line item's own CommitmentInfo carries those totals).
	require.Len(t, summaries, 1, "expected one summary per configured bucket")

	bucketSummary := summaries[0]
	assert.Equal(t, bucketID, bucketSummary.BucketID)
	// 8 in-bucket hours * 10 usage/hour = 80
	assert.True(t, bucketSummary.TotalUsage.Equal(decimal.NewFromInt(80)),
		"bucket total usage: got %s, want 80", bucketSummary.TotalUsage)
	// BaseCharge: 80 * $2 = $160
	assert.True(t, bucketSummary.BaseCharge.Equal(decimal.NewFromInt(160)),
		"bucket base charge: got %s, want 160", bucketSummary.BaseCharge)
}

// TestAnalytics_BucketSummaries_WithAmountCommitment exercises the commitment math path.
// Bucket has CommitmentType=AMOUNT with CommitmentValue=$50, OverageFactor=1.
// Usage in bucket = 80 units * $2 = $160 base charge.
// Expected: utilized=$50, overage=$110 (overageCharge = ($160-$50)*1 = $110), trueUp=0.
func TestAnalytics_BucketSummaries_WithAmountCommitment(t *testing.T) {
	ctx := context.Background()

	overageFactor := decimal.NewFromInt(1)
	bucketID := "bkt_0002"
	bucketPriceID := "price_bucket2"

	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:      "li_002",
		PriceID: "price_default2",
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:              bucketID,
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 17, Minute: 0},
				PriceID:         bucketPriceID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5),
				OverageFactor:   &overageFactor,
				TrueUpEnabled:   false,
			},
		},
	}

	data := &AnalyticsData{
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{
			"li_002": lineItem,
		},
		Prices: map[string]*priceDomain.Price{
			"price_default2": {ID: "price_default2", Amount: decimal.NewFromFloat(1.0)},
			"price_bucket2":  {ID: "price_bucket2", Amount: decimal.NewFromFloat(2.0)},
		},
	}

	baseDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	points := buildHourlyPoints(baseDate)
	// Stamp bucket attribution AND the per-point commitment fields that
	// calculatePointCosts populates in production. Per-window, each in-bucket
	// window (10 units × $2 = $20 ≥ $5 commitment, overage 1x) yields
	// utilized=$5, overage=($15×1)=$15. buildBucketSummaries must SUM these.
	for i := range points {
		if idx, ok := lineItem.CommitmentTimeBuckets.BucketIndexAt([]time.Time{points[i].Timestamp}, 0); ok {
			points[i].BucketID = lineItem.CommitmentTimeBuckets[idx].ID
			points[i].PriceID = lineItem.CommitmentTimeBuckets[idx].PriceID
			points[i].ComputedCommitmentUtilizedAmount = decimal.NewFromInt(5)
			points[i].ComputedOverageAmount = decimal.NewFromInt(15)
		}
	}

	summaries := buildBucketSummaries(ctx, &flatRatePriceService{}, points, lineItem, data)
	require.Len(t, summaries, 1, "one summary per configured bucket; no out-of-bucket row")

	bucketSummary := summaries[0]
	assert.Equal(t, bucketID, bucketSummary.BucketID)
	assert.Equal(t, string(types.COMMITMENT_TYPE_AMOUNT), bucketSummary.CommitmentType)
	// 8 in-bucket windows: base 8×$20=$160; utilized 8×$5=$40; overage 8×$15=$120.
	assert.True(t, bucketSummary.BaseCharge.Equal(decimal.NewFromInt(160)),
		"base charge: got %s, want 160", bucketSummary.BaseCharge)
	assert.True(t, bucketSummary.ComputedUtilized.Equal(decimal.NewFromInt(40)),
		"utilized: got %s, want 40", bucketSummary.ComputedUtilized)
	assert.True(t, bucketSummary.ComputedOverage.Equal(decimal.NewFromInt(120)),
		"overage: got %s, want 120", bucketSummary.ComputedOverage)
	assert.True(t, bucketSummary.ComputedTrueUp.Equal(decimal.Zero),
		"true-up: got %s, want 0", bucketSummary.ComputedTrueUp)
}

// TestAnalytics_BreakdownBucketFlag_NoLineItem verifies that when breakdown_bucket=true
// but the analytic item has no SubLineItemID or no CommitmentTimeBuckets, points are
// emitted without BucketID and no BucketSummaries are appended.
func TestAnalytics_BreakdownBucketFlag_NoLineItem(t *testing.T) {
	ctx := context.Background()

	// Line item WITHOUT CommitmentTimeBuckets.
	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:      "li_003",
		PriceID: "price_x",
	}

	data := &AnalyticsData{
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{
			"li_003": lineItem,
		},
		Prices: map[string]*priceDomain.Price{
			"price_x": {ID: "price_x", Amount: decimal.NewFromFloat(1.0)},
		},
	}

	// Verify HasCommitmentTimeBuckets returns false.
	assert.False(t, lineItem.HasCommitmentTimeBuckets())

	baseDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	points := buildHourlyPoints(baseDate)

	// No attribution should occur.
	for i := range points {
		_, ok := lineItem.CommitmentTimeBuckets.BucketIndexAt([]time.Time{points[i].Timestamp}, 0)
		assert.False(t, ok, "no bucket should match when CommitmentTimeBuckets is empty")
	}

	// When lineItemForBucket is nil (no buckets), buildBucketSummaries is not called.
	// Verify it would not panic if called with an empty bucket slice by passing a
	// line item with empty CommitmentTimeBuckets explicitly.
	summaries := buildBucketSummaries(ctx, &flatRatePriceService{}, points, lineItem, data)
	// No defined buckets => no summaries (out-of-bucket usage is not summarized).
	require.Empty(t, summaries, "expect no summaries when no buckets defined")
}

// TestFeatureUsage_ZeroUsage_BucketTrueUp reproduces the production report where
// an addon line item with a per-bucket true-up commitment but NO top-level
// commitment returned total_cost 0 / true_up 0 through the FEATURE-USAGE path.
//
// The gate that engages the zero-usage window fill used the top-level
// CommitmentTrueUpEnabled flag only, so a bucket-level-only true-up never fired —
// the calculation fell through to applyLineItemCommitment(nil,nil), yielding the
// empty is_windowed commitment info the customer saw.
//
// Setup mirrors the report: MINUTE bucketed SUM meter, line item scoped to a
// single day, one bucket [11:00,11:30) with $3 amount commitment + true-up ON, no
// usage. 30 empty minute windows × $3 true-up = $90.
func TestFeatureUsage_ZeroUsage_BucketTrueUp(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.NewNoopLogger()
	priceStore := testutil.NewInMemoryPriceStore()
	params := ServiceParams{
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
		PriceRepo:     priceStore,
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
	}
	svc := &featureUsageTrackingService{ServiceParams: params}
	priceSvc := NewPriceService(params)

	bucketPrice := &priceDomain.Price{
		ID: "price_fu_bkt", Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPrice))

	linePrice := &priceDomain.Price{
		ID: "price_fu_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, linePrice))

	dayStart := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	overage := decimal.NewFromInt(2)
	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:                 "li_fu_zero_tu",
		SubscriptionID:     "sub_fu",
		PriceID:            linePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            "meter_fu",
		StartDate:          dayStart,
		EndDate:            dayEnd,
		CommitmentWindowed: true, // buckets only; no top-level commitment / true-up
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "bkt_fu", Start: types.Bucket{Hour: 11, Minute: 0}, End: types.Bucket{Hour: 11, Minute: 30},
			PriceID: bucketPrice.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(3), OverageFactor: &overage, TrueUpEnabled: true,
		}},
	}

	bucketedMeter := &meterDomain.Meter{
		ID: "meter_fu",
		Aggregation: meterDomain.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeMinute,
		},
	}

	item := &eventsDomain.DetailedUsageAnalytic{
		MeterID:         "meter_fu",
		PriceID:         linePrice.ID,
		SubLineItemID:   "li_fu_zero_tu",
		SubscriptionID:  "sub_fu",
		AggregationType: types.AggregationSum,
		// zero usage, no points
	}

	data := &AnalyticsData{
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{"li_fu_zero_tu": lineItem},
		SubscriptionsMap: map[string]*subscriptionDomain.Subscription{
			"sub_fu": {ID: "sub_fu", BillingAnchor: dayStart},
		},
		Prices: map[string]*priceDomain.Price{linePrice.ID: linePrice},
		Params: &eventsDomain.UsageAnalyticsParams{StartTime: dayStart, EndTime: dayEnd},
	}

	svc.calculateBucketedCost(ctx, priceSvc, item, linePrice, bucketedMeter, data, false)

	require.NotNil(t, item.CommitmentInfo, "windowed commitment must record commitment info")
	assert.True(t, item.CommitmentInfo.ComputedTrueUpAmount.Equal(decimal.NewFromInt(90)),
		"expected true-up $90 (30 empty minute windows × $3); got %s", item.CommitmentInfo.ComputedTrueUpAmount)
	assert.True(t, item.TotalCost.Equal(decimal.NewFromInt(90)),
		"expected total cost $90 from bucket true-up; got %s", item.TotalCost)
}

// TestFeatureUsage_CoarseRequestWindow_BucketSummariesNonZero verifies the ported
// coarse-window summary fix on the feature-usage path: with a MINUTE-bucketed
// meter and a HOUR request window (coarser than the buckets), per-bucket summaries
// must still reflect the bucket's usage/commitment instead of rolling up to zero.
func TestFeatureUsage_CoarseRequestWindow_BucketSummariesNonZero(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.NewNoopLogger()
	priceStore := testutil.NewInMemoryPriceStore()
	params := ServiceParams{
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
		PriceRepo:     priceStore,
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
	}
	svc := &featureUsageTrackingService{ServiceParams: params}
	priceSvc := NewPriceService(params)

	bucketPrice := &priceDomain.Price{
		ID: "price_fu_cw_bkt", Amount: decimal.NewFromInt(2), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPrice))
	linePrice := &priceDomain.Price{
		ID: "price_fu_cw_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, linePrice))

	dayStart := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	overage := decimal.NewFromInt(2)
	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:                 "li_fu_cw",
		SubscriptionID:     "sub_fu_cw",
		PriceID:            linePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            "meter_fu_cw",
		StartDate:          dayStart,
		EndDate:            dayEnd,
		CommitmentWindowed: true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "bkt_fu_cw", Start: types.Bucket{Hour: 11, Minute: 0}, End: types.Bucket{Hour: 11, Minute: 30},
			PriceID: bucketPrice.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage, TrueUpEnabled: false,
		}},
	}
	bucketedMeter := &meterDomain.Meter{
		ID:          "meter_fu_cw",
		Aggregation: meterDomain.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeMinute},
	}

	// One in-bucket minute point at 11:15 with 10 units (bucket grain).
	item := &eventsDomain.DetailedUsageAnalytic{
		MeterID:         "meter_fu_cw",
		PriceID:         linePrice.ID,
		SubLineItemID:   "li_fu_cw",
		SubscriptionID:  "sub_fu_cw",
		AggregationType: types.AggregationSum,
		Points: []eventsDomain.UsageAnalyticPoint{{
			Timestamp:   time.Date(2026, 1, 5, 11, 15, 0, 0, time.UTC),
			WindowStart: time.Date(2026, 1, 5, 11, 15, 0, 0, time.UTC),
			Usage:       decimal.NewFromInt(10),
		}},
	}

	data := &AnalyticsData{
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{"li_fu_cw": lineItem},
		SubscriptionsMap: map[string]*subscriptionDomain.Subscription{
			"sub_fu_cw": {ID: "sub_fu_cw", BillingAnchor: dayStart},
		},
		Prices: map[string]*priceDomain.Price{linePrice.ID: linePrice, bucketPrice.ID: bucketPrice},
		// HOUR request window — coarser than the MINUTE bucket.
		Params: &eventsDomain.UsageAnalyticsParams{StartTime: dayStart, EndTime: dayEnd, WindowSize: types.WindowSizeHour},
	}

	svc.calculateBucketedCost(ctx, priceSvc, item, linePrice, bucketedMeter, data, false)

	// The fix: bucket-grain points are captured (with BucketID) before the HOUR
	// roll-up, and summaries are built from them.
	require.NotEmpty(t, item.BucketPoints, "bucket-grain points must be captured for summary building")
	require.Equal(t, "bkt_fu_cw", item.BucketPoints[0].BucketID, "bucket-grain point must carry its BucketID")

	summaries := buildBucketSummaries(ctx, priceSvc, bucketGrainDTOPoints(item.BucketPoints), lineItem, data)
	require.Len(t, summaries, 1, "expected one summary per configured bucket")
	bs := summaries[0]
	assert.Equal(t, "bkt_fu_cw", bs.BucketID)
	assert.True(t, bs.TotalUsage.Equal(decimal.NewFromInt(10)),
		"bucket usage should be 10 even with HOUR request window, got %s", bs.TotalUsage)
	assert.True(t, bs.ComputedUtilized.Equal(decimal.NewFromInt(5)),
		"bucket utilized should be $5, got %s", bs.ComputedUtilized)
	assert.True(t, bs.ComputedOverage.Equal(decimal.NewFromInt(30)),
		"bucket overage should be $30 (($20-$5)×2), got %s", bs.ComputedOverage)

	// Contrast: building summaries from the rolled-up HOUR points (the old path)
	// loses attribution and reports zero — exactly the bug this fix avoids.
	oldSummaries := buildBucketSummaries(ctx, priceSvc, hourPointsWithLegacyAttribution(item.Points, lineItem), lineItem, data)
	require.Len(t, oldSummaries, 1)
	assert.True(t, oldSummaries[0].TotalUsage.IsZero(),
		"rolled-up HOUR points cannot attribute to a sub-hour bucket; got %s", oldSummaries[0].TotalUsage)
}

// hourPointsWithLegacyAttribution rebuilds the pre-fix DTO points: rolled-up
// points whose BucketID is derived via bucketIDForPointWindow (full-containment).
func hourPointsWithLegacyAttribution(points []eventsDomain.UsageAnalyticPoint, lineItem *subscriptionDomain.SubscriptionLineItem) []dto.UsageAnalyticPoint {
	out := make([]dto.UsageAnalyticPoint, len(points))
	for i, p := range points {
		out[i] = dto.UsageAnalyticPoint{
			Timestamp:                        p.Timestamp,
			Usage:                            p.Usage,
			Cost:                             p.Cost,
			ComputedCommitmentUtilizedAmount: p.ComputedCommitmentUtilizedAmount,
			ComputedOverageAmount:            p.ComputedOverageAmount,
			ComputedTrueUpAmount:             p.ComputedTrueUpAmount,
		}
		if id, priceID, ok := bucketIDForPointWindow(lineItem.CommitmentTimeBuckets, p.Timestamp, types.WindowSizeHour); ok {
			out[i].BucketID = id
			out[i].PriceID = priceID
		}
	}
	return out
}
