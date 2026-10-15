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

// checkpointResolution is the smallest instant Postgres stores, so "strictly before
// as_of" is "at or before as_of minus one microsecond".
const checkpointResolution = time.Microsecond

func (s *meterUsageService) SignedSumTotal(
	ctx context.Context,
	customerID string,
	externalCustomerIDs []string,
	meterID string,
	at time.Time,
) (decimal.Decimal, error) {
	return s.signedSumTotalFrom(ctx, customerID, externalCustomerIDs, meterID, at, at)
}

func (s *meterUsageService) SaveSignedSumCheckpoint(
	ctx context.Context,
	customerID string,
	externalCustomerIDs []string,
	meterID string,
	asOf time.Time,
	invoiceID *string,
) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	if s.MeterUsageCheckpointRepo == nil {
		return nil, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	// Build from the checkpoint before this one, so recomputing a period replaces
	// its checkpoint rather than reading it back.
	total, err := s.signedSumTotalFrom(ctx, customerID, externalCustomerIDs, meterID, asOf.Add(-checkpointResolution), asOf)
	if err != nil {
		return nil, err
	}

	return s.MeterUsageCheckpointRepo.Upsert(ctx, &meterusagecheckpoint.MeterUsageCheckpoint{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT),
		CustomerID:    customerID,
		MeterID:       meterID,
		Total:         total,
		AsOf:          asOf,
		InvoiceID:     invoiceID,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	})
}

// signedSumTotalFrom adds the changes in [checkpoint.as_of, at) to the latest checkpoint
// at or before checkpointAt. With no checkpoint, the changes are summed from the start.
func (s *meterUsageService) signedSumTotalFrom(
	ctx context.Context,
	customerID string,
	externalCustomerIDs []string,
	meterID string,
	checkpointAt, at time.Time,
) (decimal.Decimal, error) {
	if s.MeterUsageCheckpointRepo == nil {
		return decimal.Zero, ierr.NewError("meter usage checkpoint repository is not configured").Mark(ierr.ErrSystem)
	}

	checkpoint, err := s.MeterUsageCheckpointRepo.GetLatestAtOrBefore(ctx, customerID, meterID, checkpointAt)
	if err != nil {
		return decimal.Zero, err
	}

	total, from := decimal.Zero, time.Time{}
	if checkpoint != nil {
		total, from = checkpoint.Total, checkpoint.AsOf
	}
	if !at.After(from) || len(externalCustomerIDs) == 0 {
		return total, nil
	}

	changes, err := s.GetUsageTotal(ctx, &dto.UsageTotalRequest{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		MeterID:             meterID,
		AggregationType:     types.AggregationSignedSum,
		StartTime:           from,
		EndTime:             at,
	})
	if err != nil {
		return decimal.Zero, err
	}
	return total.Add(changes), nil
}
