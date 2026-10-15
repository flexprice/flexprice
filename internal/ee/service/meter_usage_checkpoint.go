package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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
	if !req.AggregationType.SupportsCheckpoint() {
		return decimal.Zero, ierr.NewError("aggregation type does not support checkpoints").
			WithHint("Only SIGNED_SUM meters can be checkpointed.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         req.MeterID,
				"aggregation_type": req.AggregationType,
			}).
			Mark(ierr.ErrValidation)
	}
	if s.MeterUsageCheckpointRepo == nil {
		return decimal.Zero, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	checkpoint, err := s.MeterUsageCheckpointRepo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
		CustomerID:      req.CustomerID,
		MeterID:         req.MeterID,
		AggregationType: req.AggregationType,
		Before:          req.Until,
	})
	if err != nil {
		return decimal.Zero, err
	}

	total, from := decimal.Zero, time.Time{}
	if checkpoint != nil {
		total, from = checkpoint.Total, checkpoint.AggregatedUntil
	}
	if len(req.ExternalCustomerIDs) == 0 {
		return total, nil
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
