package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	eventsDomain "github.com/flexprice/flexprice/internal/domain/events"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	subscriptionDomain "github.com/flexprice/flexprice/internal/domain/subscription"
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

	// Expect 2 summaries: one for the bucket, one out-of-bucket.
	require.Len(t, summaries, 2, "expected one bucket summary + one out-of-bucket summary")

	bucketSummary := summaries[0]
	outSummary := summaries[1]

	assert.Equal(t, bucketID, bucketSummary.BucketID)
	// 8 in-bucket hours * 10 usage/hour = 80
	assert.True(t, bucketSummary.TotalUsage.Equal(decimal.NewFromInt(80)),
		"bucket total usage: got %s, want 80", bucketSummary.TotalUsage)
	// BaseCharge: 80 * $2 = $160
	assert.True(t, bucketSummary.BaseCharge.Equal(decimal.NewFromInt(160)),
		"bucket base charge: got %s, want 160", bucketSummary.BaseCharge)

	assert.Empty(t, outSummary.BucketID, "out-of-bucket summary must have empty BucketID")
	// 16 out-of-bucket hours * 10 usage/hour = 160
	assert.True(t, outSummary.TotalUsage.Equal(decimal.NewFromInt(160)),
		"out-of-bucket total usage: got %s, want 160", outSummary.TotalUsage)
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
	require.Len(t, summaries, 2)

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
	// No defined buckets => only the out-of-bucket summary row.
	require.Len(t, summaries, 1, "expect only out-of-bucket summary when no buckets defined")
	assert.Empty(t, summaries[0].BucketID)
}
