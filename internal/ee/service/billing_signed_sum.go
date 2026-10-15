package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// signedSumMetersByID returns the SIGNED_SUM meters among meterIDs, keyed by id.
func (s *billingService) signedSumMetersByID(ctx context.Context, meterIDs []string) (map[string]*meter.Meter, error) {
	meterIDs = lo.Uniq(lo.Filter(meterIDs, func(id string, _ int) bool { return id != "" }))
	if len(meterIDs) == 0 || s.MeterRepo == nil {
		return map[string]*meter.Meter{}, nil
	}
	meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*meter.Meter, len(meters))
	for _, m := range meters {
		if m != nil && m.Aggregation.Type == types.AggregationSignedSum {
			out[m.ID] = m
		}
	}
	return out, nil
}

// setSignedSumSummaryUsage sets the exact usage of each SIGNED_SUM feature served by sub: the
// running total now for meters that never reset, this period's net otherwise.
func (s *billingService) setSignedSumSummaryUsage(
	ctx context.Context,
	sub *subscription.Subscription,
	externalCustomerIDs []string,
	signedSumMeters map[string]*meter.Meter,
	featureMeterMap map[string]string,
	featureSubscriptionMap map[string]*subscription.Subscription,
	usageByFeature map[string]decimal.Decimal,
) error {
	if len(signedSumMeters) == 0 {
		return nil
	}

	meterUsageSvc := NewMeterUsageService(s.ServiceParams)
	now := time.Now().UTC()
	for featureID, meterID := range featureMeterMap {
		m := signedSumMeters[meterID]
		featureSub := featureSubscriptionMap[featureID]
		if m == nil || featureSub == nil || featureSub.ID != sub.ID {
			continue
		}

		var (
			usage decimal.Decimal
			err   error
		)
		if isRunningSignedSum(m) {
			usage, err = meterUsageSvc.SignedSumTotal(ctx, sub.CustomerID, externalCustomerIDs, meterID, now)
		} else {
			usage, err = meterUsageSvc.GetUsageTotal(ctx, &dto.UsageTotalRequest{
				TenantID:            types.GetTenantID(ctx),
				EnvironmentID:       types.GetEnvironmentID(ctx),
				ExternalCustomerIDs: externalCustomerIDs,
				MeterID:             meterID,
				AggregationType:     types.AggregationSignedSum,
				StartTime:           sub.CurrentPeriodStart,
				EndTime:             now,
			})
		}
		if err != nil {
			return err
		}
		usageByFeature[featureID] = usage
	}
	return nil
}
