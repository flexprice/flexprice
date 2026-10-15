package service

import (
	"time"

	"github.com/shopspring/decimal"
)

func (s *MeterUsageServiceSuite) TestSignedSumTotal() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ext := []string{s.customer.ExternalID}

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-48*time.Hour), 40)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(24*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(10*24*time.Hour), -4)

	total, err := s.svc.SignedSumTotal(ctx, s.customer.ID, ext, s.meterAPI.ID, nov1)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "no checkpoint sums all history, got %s", total)

	_, err = s.svc.SaveSignedSumCheckpoint(ctx, s.customer.ID, ext, s.meterAPI.ID, oct1, nil)
	s.Require().NoError(err)

	total, err = s.svc.SignedSumTotal(ctx, s.customer.ID, ext, s.meterAPI.ID, nov1)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "checkpoint plus changes since, got %s", total)

	// An event timestamped before the checkpoint arrives late: it is not counted.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-time.Hour), 100)
	total, err = s.svc.SignedSumTotal(ctx, s.customer.ID, ext, s.meterAPI.ID, nov1)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(total), "late event behind the checkpoint is ignored, got %s", total)
}

func (s *MeterUsageServiceSuite) TestSignedSumTotal_GoesBelowZero() {
	ctx := s.GetContext()
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-48*time.Hour), 10)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, nov1.Add(-24*time.Hour), -15)

	total, err := s.svc.SignedSumTotal(ctx, s.customer.ID, []string{s.customer.ExternalID}, s.meterAPI.ID, nov1)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(-5).Equal(total), "stored totals stay exact, got %s", total)
}

func (s *MeterUsageServiceSuite) TestSaveSignedSumCheckpoint_RecomputeReplaces() {
	ctx := s.GetContext()
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ext := []string{s.customer.ExternalID}

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(-time.Hour), 40)
	_, err := s.svc.SaveSignedSumCheckpoint(ctx, s.customer.ID, ext, s.meterAPI.ID, oct1, nil)
	s.Require().NoError(err)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(5*24*time.Hour), 6)
	first, err := s.svc.SaveSignedSumCheckpoint(ctx, s.customer.ID, ext, s.meterAPI.ID, nov1, nil)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(46).Equal(first.Total), "got %s", first.Total)

	// A change inside the period lands before the invoice is recomputed.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, oct1.Add(20*24*time.Hour), -10)
	second, err := s.svc.SaveSignedSumCheckpoint(ctx, s.customer.ID, ext, s.meterAPI.ID, nov1, nil)
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "the same aggregated_until keeps one checkpoint")
	s.True(decimal.NewFromInt(36).Equal(second.Total), "recompute adds the period once, got %s", second.Total)
}
