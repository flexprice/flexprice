package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CheckpointedUsageRequest names one customer's running aggregate of a meter: every
// change before Until, aggregated by AggregationType.
type CheckpointedUsageRequest struct {
	CustomerID          string
	ExternalCustomerIDs []string
	MeterID             string
	AggregationType     types.AggregationType
	Until               time.Time
	// IgnoreCheckpoints sums from the first event instead of starting at a saved checkpoint.
	IgnoreCheckpoints bool
}

func (s *meterUsageService) GetCheckpointedUsage(ctx context.Context, req CheckpointedUsageRequest) (decimal.Decimal, error) {
	return s.checkpointedUsage(ctx, req)
}

func (s *meterUsageService) SaveUsageCheckpoint(
	ctx context.Context,
	req CheckpointedUsageRequest,
	invoiceID *string,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	total, err := s.checkpointedUsage(ctx, req)
	if err != nil {
		return nil, err
	}

	return s.MeterUsageCheckpointRepo.Upsert(ctx, &meterusagecheckpoint.MeterUsageCheckpoint{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT),
		CustomerID:      req.CustomerID,
		MeterID:         req.MeterID,
		AggregationType: req.AggregationType,
		Total:           total,
		AggregatedUntil: req.Until,
		InvoiceID:       invoiceID,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	})
}

// checkpointedUsage adds the usage in [checkpoint.aggregated_until, req.Until) to the
// latest checkpoint strictly before req.Until, so saving the same instant again rebuilds
// it from the one before. With no checkpoint, the usage is summed from the start.
func (s *meterUsageService) checkpointedUsage(ctx context.Context, req CheckpointedUsageRequest) (decimal.Decimal, error) {
	if req.CustomerID == "" || req.MeterID == "" || req.Until.IsZero() || len(req.ExternalCustomerIDs) == 0 {
		return decimal.Zero, ierr.NewError("customer, meter, external customer IDs and cutoff are required").Mark(ierr.ErrValidation)
	}
	if types.GetTenantID(ctx) == "" {
		return decimal.Zero, ierr.NewError("tenant is required for checkpointed usage").Mark(ierr.ErrValidation)
	}
	if req.AggregationType != types.AggregationSum {
		return decimal.Zero, ierr.NewError("aggregation type does not support checkpoints").
			WithHint("Only SUM meters can be checkpointed.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         req.MeterID,
				"aggregation_type": req.AggregationType,
			}).
			Mark(ierr.ErrValidation)
	}
	if s.MeterUsageCheckpointRepo == nil {
		return decimal.Zero, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	total, from := decimal.Zero, time.Time{}
	if !req.IgnoreCheckpoints {
		checkpoint, err := s.MeterUsageCheckpointRepo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
			CustomerID:      req.CustomerID,
			MeterID:         req.MeterID,
			AggregationType: req.AggregationType,
			Before:          req.Until,
		})
		if err != nil {
			return decimal.Zero, err
		}
		if checkpoint != nil {
			total, from = checkpoint.Total, checkpoint.AggregatedUntil
		}
	}

	changes, err := s.GetUsageTotal(ctx, &dto.UsageTotalRequest{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: req.ExternalCustomerIDs,
		MeterID:             req.MeterID,
		AggregationType:     req.AggregationType,
		StartTime:           from,
		EndTime:             req.Until,
	})
	if err != nil {
		return decimal.Zero, err
	}
	return total.Add(changes), nil
}

// keepsRunningAggregate reports whether a meter retains usage across billing periods.
func keepsRunningAggregate(m *meter.Meter) bool {
	return m != nil && m.Aggregation.Type == types.AggregationSum && m.ResetUsage == types.ResetUsageNever
}

// usesCheckpoints reports whether a subscription's running totals are checkpointed. A parent's
// total spans its current children, so one row keyed by the parent cannot hold it.
func usesCheckpoints(sub *subscription.Subscription) bool {
	return sub != nil && sub.SubscriptionType != types.SubscriptionTypeParent
}

func (s *meterUsageService) SaveUsageCheckpointsForSubscription(
	ctx context.Context,
	sub *subscription.Subscription,
	until time.Time,
	invoiceID *string,
) error {
	if !usesCheckpoints(sub) || s.MeterRepo == nil {
		return nil
	}

	meterIDs := make([]string, 0, len(sub.LineItems))
	for _, item := range sub.LineItems {
		if item != nil && item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" && item.StartDate.Before(until) {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	if len(meterIDs) == 0 {
		return nil
	}

	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(meterIDs))
	if err != nil {
		return err
	}
	meters = lo.Filter(meters, func(m *meter.Meter, _ int) bool {
		return keepsRunningAggregate(m)
	})
	if len(meters) == 0 {
		return nil
	}

	externalCustomerIDs, err := s.resolveExternalCustomerIDs(ctx, sub, true)
	if err != nil {
		return err
	}
	for _, m := range meters {
		req := CheckpointedUsageRequest{
			CustomerID:          sub.CustomerID,
			ExternalCustomerIDs: externalCustomerIDs,
			MeterID:             m.ID,
			AggregationType:     m.Aggregation.Type,
			Until:               until,
		}
		if _, err := s.SaveUsageCheckpoint(ctx, req, invoiceID); err != nil {
			return err
		}
	}
	return nil
}
