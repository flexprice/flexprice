package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// checkpointResolution is the smallest instant Postgres stores, so "strictly before
// aggregated_until" is "at or before aggregated_until minus one microsecond".
const checkpointResolution = time.Microsecond

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
	return s.checkpointedUsage(ctx, req, req.Until)
}

func (s *meterUsageService) SaveUsageCheckpoint(
	ctx context.Context,
	req CheckpointedUsageRequest,
	invoiceID *string,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	if s.MeterUsageCheckpointRepo == nil {
		return nil, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	// Build from the checkpoint before this one, so recomputing a period replaces its
	// checkpoint rather than reading it back.
	total, err := s.checkpointedUsage(ctx, req, req.Until.Add(-checkpointResolution))
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

// checkpointedUsage combines the latest checkpoint at or before checkpointAt with the
// usage in [checkpoint.aggregated_until, req.Until). With no checkpoint, the usage is
// aggregated from the start.
func (s *meterUsageService) checkpointedUsage(
	ctx context.Context,
	req CheckpointedUsageRequest,
	checkpointAt time.Time,
) (decimal.Decimal, error) {
	if !req.AggregationType.SupportsCheckpoint() {
		return decimal.Zero, ierr.NewError("aggregation type does not support checkpoints").
			WithHint("Only aggregations that combine from a partial result can be checkpointed.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         req.MeterID,
				"aggregation_type": req.AggregationType,
			}).
			Mark(ierr.ErrValidation)
	}
	if s.MeterUsageCheckpointRepo == nil || s.repo == nil {
		return decimal.Zero, ierr.NewError("meter usage checkpoints are not configured").Mark(ierr.ErrSystem)
	}

	checkpoint, err := s.MeterUsageCheckpointRepo.GetLatestAtOrBefore(ctx, meterusagecheckpoint.Key{
		CustomerID:      req.CustomerID,
		MeterID:         req.MeterID,
		AggregationType: req.AggregationType,
	}, checkpointAt)
	if err != nil {
		return decimal.Zero, err
	}

	var from time.Time
	if checkpoint != nil {
		from = checkpoint.AggregatedUntil
	}
	if !req.Until.After(from) || len(req.ExternalCustomerIDs) == 0 {
		return combineCheckpoint(req.AggregationType, checkpoint, nil), nil
	}

	window, err := s.repo.GetUsage(ctx, (&dto.UsageTotalRequest{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: req.ExternalCustomerIDs,
		MeterID:             req.MeterID,
		AggregationType:     req.AggregationType,
		StartTime:           from,
		EndTime:             req.Until,
	}).ToParams())
	if err != nil {
		return decimal.Zero, err
	}
	return combineCheckpoint(req.AggregationType, checkpoint, window), nil
}

// combineCheckpoint folds the usage after a checkpoint into it. Either side may be
// missing; an empty window leaves the checkpoint as it is.
func combineCheckpoint(
	aggType types.AggregationType,
	checkpoint *meterusagecheckpoint.MeterUsageCheckpoint,
	window *events.MeterUsageAggregationResult,
) decimal.Decimal {
	hasWindow := window != nil && window.EventCount > 0
	switch {
	case checkpoint == nil && !hasWindow:
		return decimal.Zero
	case checkpoint == nil:
		return window.TotalValue
	case !hasWindow:
		return checkpoint.Total
	}

	switch aggType {
	case types.AggregationMax:
		return decimal.Max(checkpoint.Total, window.TotalValue)
	case types.AggregationLatest:
		return window.TotalValue
	default:
		return checkpoint.Total.Add(window.TotalValue)
	}
}
