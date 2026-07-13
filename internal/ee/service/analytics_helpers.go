package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/shopspring/decimal"
)

// PriceMatch pairs a resolved price with its meter, used by usage-tracking
// paths that need to match an event to a price+meter tuple.
type PriceMatch struct {
	Price *price.Price
	Meter *meter.Meter
}

// buildBucketSummaries produces one BucketSummary per CommitmentTimeBucket on
// the line item. It sums usage per-bucket from the supplied per-point series
// and rolls up the pre-stamped commitment fields (utilized / overage / true-up).
func buildBucketSummaries(
	ctx context.Context,
	priceService PriceService,
	points []events.UsageAnalyticPoint,
	lineItem *subscription.SubscriptionLineItem,
	data *AnalyticsData,
) []dto.BucketSummary {
	buckets := lineItem.CommitmentTimeBuckets
	summaries := make([]dto.BucketSummary, 0, len(buckets))
	for _, b := range buckets {
		r := rollupBucketPoints(ctx, priceService, points, b.ID, data.Prices[b.PriceID])
		summaries = append(summaries, dto.BucketSummary{
			BucketID:               b.ID,
			Start:                  b.Start,
			End:                    b.End,
			SubscriptionLineItemID: lineItem.ID,
			PriceID:                b.PriceID,
			CommitmentType:         string(b.CommitmentType),
			CommitmentValue:        b.CommitmentValue,
			TotalUsage:             r.usage,
			BaseCharge:             r.base,
			ComputedUtilized:       r.utilized,
			ComputedOverage:        r.overage,
			ComputedTrueUp:         r.trueUp,
		})
	}
	return summaries
}

type bucketPointRollup struct {
	usage, base, utilized, overage, trueUp decimal.Decimal
}

func rollupBucketPoints(
	ctx context.Context,
	priceService PriceService,
	points []events.UsageAnalyticPoint,
	bucketID string,
	p *price.Price,
) bucketPointRollup {
	var r bucketPointRollup
	for _, pt := range points {
		if pt.BucketID != bucketID {
			continue
		}
		r.usage = r.usage.Add(pt.Usage)
		if p != nil {
			r.base = r.base.Add(priceService.CalculateCost(ctx, p, pt.Usage))
		}
		r.utilized = r.utilized.Add(pt.ComputedCommitmentUtilizedAmount)
		r.overage = r.overage.Add(pt.ComputedOverageAmount)
		r.trueUp = r.trueUp.Add(pt.ComputedTrueUpAmount)
	}
	return r
}
