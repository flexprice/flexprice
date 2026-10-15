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

// For every type that supports checkpoints, a checkpoint plus the usage after it must
// equal the aggregate over the whole history.
func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_MatchesFullHistory() {
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		aggType      types.AggregationType
		windowEvents bool
		want         decimal.Decimal
	}{
		{types.AggregationSum, true, decimal.NewFromInt(85)},
		{types.AggregationSignedSum, true, decimal.NewFromInt(85)},
		{types.AggregationCount, true, decimal.NewFromInt(4)},
		{types.AggregationMax, true, decimal.NewFromInt(40)},
		{types.AggregationLatest, true, decimal.NewFromInt(30)},
		{types.AggregationLatest, false, decimal.NewFromInt(10)},
		{types.AggregationMax, false, decimal.NewFromInt(40)},
	}

	for _, tt := range tests {
		name := string(tt.aggType)
		if !tt.windowEvents {
			name += "_no_usage_after_checkpoint"
		}
		s.Run(name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-48*time.Hour), 40)
			s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-24*time.Hour), 10)
			if tt.windowEvents {
				s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(24*time.Hour), 5)
				s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(48*time.Hour), 30)
			}

			_, err := s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(tt.aggType, oct1), nil)
			s.Require().NoError(err)

			got, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(tt.aggType, nov1))
			s.Require().NoError(err)
			s.True(tt.want.Equal(got), "want %s, got %s", tt.want, got)
		})
	}
}

func (s *MeterUsageServiceSuite) TestGetCheckpointedUsage_RejectsUnsupportedType() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationCountUnique, nov1))
	s.Error(err)
	_, err = s.svc.SaveUsageCheckpoint(ctx, s.checkpointRequest(types.AggregationAvg, nov1), nil)
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

	// A checkpoint of another type is not read back: COUNT starts from the whole history.
	count, err := s.svc.GetCheckpointedUsage(ctx, s.checkpointRequest(types.AggregationCount, nov1))
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(4).Equal(count), "got %s", count)
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
