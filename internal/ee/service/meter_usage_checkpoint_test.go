package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func (s *MeterUsageServiceSuite) checkpointRequest(aggType types.AggregationType, until time.Time) CheckpointedUsageRequest {
	return CheckpointedUsageRequest{
		CustomerID:          s.customer.ID,
		ExternalCustomerIDs: []string{s.customer.ExternalID},
		MeterID:             s.meterAPI.ID,
		AggregationType:     aggType,
		Until:               until,
	}
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_RejectsUnsupportedType() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSum, nov1))
	s.Error(err)
	_, err = s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationLatest, nov1), nil)
	s.Error(err)
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_SignedSum() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-48*time.Hour), 40)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(24*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(10*24*time.Hour), -4)

	total, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "no checkpoint aggregates all history, got %s", total)

	_, err = s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSignedSum, oct1), nil)
	s.Require().NoError(err)

	total, err = s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "checkpoint plus changes since, got %s", total)

	// An event timestamped before the checkpoint arrives late: it is not counted.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-time.Hour), 100)
	total, err = s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "late event behind the checkpoint is ignored, got %s", total)
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_SignedSumGoesBelowZero() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-48*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-24*time.Hour), -15)

	total, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(-5).Equal(total), "stored totals stay exact, got %s", total)
}

func (s *MeterUsageServiceSuite) TestSaveUsageCheckpoint_RecomputeReplaces() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-time.Hour), 40)
	_, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSignedSum, oct1), nil)
	s.Require().NoError(err)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(5*24*time.Hour), 6)
	first, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1), nil)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(first.Total), "got %s", first.Total)

	// A change inside the period lands before the invoice is recomputed.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(20*24*time.Hour), -10)
	second, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationSignedSum, nov1), nil)
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "the same instant keeps one checkpoint")
	s.True(decimal.NewFromInt(36).Equal(second.Total), "recompute adds the period once, got %s", second.Total)
}
