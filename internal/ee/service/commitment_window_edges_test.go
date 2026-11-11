package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// hourlyGPUs stores a MAX meter of GPUs bucketed by the hour.
func (s *CommitmentE2ESuite) hourlyGPUs() *meter.Meter {
	ctx := s.GetContext()
	m := &meter.Meter{
		ID:          "meter_e2e_hourly_gpus",
		Name:        "Hourly GPUs",
		EventName:   "hourly_gpus",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeHour},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	return m
}

// subscribeIn subscribes the customer, living in timezone, to the plan from start on the given billing cycle.
func (s *CommitmentE2ESuite) subscribeIn(p *price.Price, start time.Time, cycle types.BillingCycle, timezone string) string {
	ctx := s.GetContext()
	s.customer.Timezone = timezone
	s.Require().NoError(s.GetStores().CustomerRepo.Update(ctx, s.customer))

	resp, err := s.subscriptions.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.customer.ID,
		PlanID:             p.EntityID,
		StartDate:          lo.ToPtr(start),
		Currency:           "usd",
		BillingPeriod:      p.BillingPeriod,
		BillingPeriodCount: 1,
		BillingCycle:       cycle,
	})
	s.Require().NoError(err)
	return resp.ID
}

// reserveGPUs commits the subscription to two GPUs in every bucket from start until end, or open-ended when end is
// zero, at $4 each and $5 above them.
func (s *CommitmentE2ESuite) reserveGPUs(subscriptionID string, p *price.Price, start, end time.Time) *dto.CommitmentResponse {
	req := floorRequest("Reserved GPUs", p.ID, "2", start, end)
	if end.IsZero() {
		req.EndDate = nil
	}
	req.Type, req.Measurement = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("4"))}
	req.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("5"))}
	return s.commit(subscriptionID, req)
}

// billedUsage returns the units the invoices bill as usage, within the commitment or not, and their amount due.
func billedUsage(invoices ...*dto.CreateInvoiceRequest) (units, due decimal.Decimal) {
	for _, inv := range invoices {
		due = due.Add(inv.AmountDue)
		for _, line := range inv.LineItems {
			if types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind]) != types.CommitmentLineTrueUp {
				units = units.Add(line.Quantity)
			}
		}
	}

	return units, due
}

// A billing period edge off the bucket grid cuts one bucket in two. Two periods around the edge bill that bucket's
// GPUs once, within its capacity, and the capacity of every hour (or day) of the two periods once.
func (s *CommitmentE2ESuite) TestWindowBucketCutByPeriodEdge_IsBilledOnce() {
	tests := []struct {
		name     string
		cycle    types.BillingCycle
		timezone string
		hourly   bool
		start    time.Time
		edge     time.Time // between the first and second period
		end      time.Time // of the second period
		usage    map[time.Time]string
		units    string // GPUs the cut bucket uses
		due      string // both periods' floors, as every GPU fits its bucket's capacity
	}{
		{
			// Months in IST start at 18:30 UTC, inside the 18:00 hour; 1,464 hours at two GPUs and $4.
			name:     "calendar months in Asia/Kolkata, hourly",
			cycle:    types.BillingCycleCalendar,
			timezone: "Asia/Kolkata",
			hourly:   true,
			start:    time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC),
			edge:     time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC),
			end:      time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC),
			usage: map[time.Time]string{
				time.Date(2026, 10, 31, 18, 10, 0, 0, time.UTC): "2",
				time.Date(2026, 10, 31, 18, 45, 0, 0, time.UTC): "2",
			},
			units: "2",
			due:   "11712",
		},
		{
			name:     "anniversary from 10:15, hourly",
			cycle:    types.BillingCycleAnniversary,
			timezone: "UTC",
			hourly:   true,
			start:    time.Date(2026, 5, 15, 10, 15, 0, 0, time.UTC),
			edge:     time.Date(2026, 6, 15, 10, 15, 0, 0, time.UTC),
			end:      time.Date(2026, 7, 15, 10, 15, 0, 0, time.UTC),
			usage:    map[time.Time]string{time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC): "2"},
			units:    "2",
			due:      "11712",
		},
		{
			// Usage is bucketed by UTC day, and 31 Oct UTC holds the IST month edge; 61 days at two GPUs and $4.
			name:     "calendar months in Asia/Kolkata, daily",
			cycle:    types.BillingCycleCalendar,
			timezone: "Asia/Kolkata",
			start:    time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC),
			edge:     time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC),
			end:      time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC),
			usage: map[time.Time]string{
				time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC): "2",
				time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC): "2",
			},
			units: "2",
			due:   "488",
		},
		{
			// Control: an edge on the hour cuts no bucket.
			name:     "anniversary from 10:00, hourly",
			cycle:    types.BillingCycleAnniversary,
			timezone: "UTC",
			hourly:   true,
			start:    time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			edge:     time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
			usage:    map[time.Time]string{time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC): "2"},
			units:    "2",
			due:      "11712",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			gpus := s.gpus
			if tt.hourly {
				gpus = s.hourlyGPUs()
			}

			p := s.planWith("cut_bucket", types.BILLING_PERIOD_MONTHLY, gpus, "5")
			sub := s.subscribeIn(p, tt.start, tt.cycle, tt.timezone)
			s.reserveGPUs(sub, p, tt.start, time.Time{})
			for at, qty := range tt.usage {
				s.record(gpus, at, qty)
			}

			first, second := s.invoiceFor(sub, tt.start, tt.edge), s.invoiceFor(sub, tt.edge, tt.end)
			units, due := billedUsage(first, second)
			s.True(units.Equal(dec(tt.units)), "GPUs billed: want %s, got %s", tt.units, units)
			s.True(due.Equal(dec(tt.due)), "amount due of both periods: want %s, got %s", tt.due, due)
			for _, inv := range []*dto.CreateInvoiceRequest{first, second} {
				for _, line := range inv.LineItems {
					kind := types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind])
					s.Contains([]types.CommitmentLineKind{types.CommitmentLineUtilized, types.CommitmentLineTrueUp}, kind,
						"%s line %q bills %s, though every GPU fits its bucket", inv.PeriodStart, lo.FromPtr(line.DisplayName), line.Amount)
				}
			}
		})
	}
}

// The later period bills the cut bucket's peak even when it falls before the edge: three GPUs at 18:10 and one at 18:45
// bill two within the bucket's capacity and one above it, at $5.
func (s *CommitmentE2ESuite) TestWindowBucketCutByPeriodEdge_BillsItsPeakBeforeTheEdge() {
	gpus := s.hourlyGPUs()
	p := s.planWith("cut_bucket_peak", types.BILLING_PERIOD_MONTHLY, gpus, "5")
	start := time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC)
	edge, end := time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC), time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC)
	sub := s.subscribeIn(p, start, types.BillingCycleCalendar, "Asia/Kolkata")
	s.reserveGPUs(sub, p, start, time.Time{})
	s.record(gpus, edge.Add(-20*time.Minute), "3")
	s.record(gpus, edge.Add(15*time.Minute), "1")

	first, second := s.invoiceFor(sub, start, edge), s.invoiceFor(sub, edge, end)
	units, due := billedUsage(first, second)
	s.True(units.Equal(dec("3")), "GPUs billed: want 3, got %s", units)
	s.True(due.Equal(dec("11717")), "amount due of both periods: want 11717, got %s", due)
	firstUnits, _ := billedUsage(first)
	s.True(firstUnits.IsZero(), "the earlier period bills %s GPUs of the bucket its end cuts", firstUnits)
}

// The subscription's first period starts at 10:15. The 10:00 bucket has no earlier period, so this period bills its
// GPUs, once.
func (s *CommitmentE2ESuite) TestWindowBucketCutByFirstPeriodStart_IsBilledOnce() {
	gpus := s.hourlyGPUs()
	p := s.planWith("first_cut_bucket", types.BILLING_PERIOD_MONTHLY, gpus, "5")
	start, edge := time.Date(2026, 5, 15, 10, 15, 0, 0, time.UTC), time.Date(2026, 6, 15, 10, 15, 0, 0, time.UTC)
	sub := s.subscribeIn(p, start, types.BillingCycleAnniversary, "UTC")
	s.reserveGPUs(sub, p, start, time.Time{})
	s.record(gpus, time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC), "2")
	s.record(gpus, time.Date(2026, 5, 15, 11, 30, 0, 0, time.UTC), "2")

	units, _ := billedUsage(s.invoiceFor(sub, start, edge), s.invoiceFor(sub, edge, edge.AddDate(0, 1, 0)))
	s.True(units.Equal(dec("4")), "GPUs billed: want 4, got %s", units)
}

// Terminated at 14:20 inside a period, the commitment no longer holds the 14:00 bucket, which ends after it: the period
// bills the bucket's GPUs once, at the line's price, beside the capacity of the 124 hours before it.
func (s *CommitmentE2ESuite) TestWindowBucketCutByTermination_IsBilledOnce() {
	gpus := s.hourlyGPUs()
	p := s.planWith("terminated_cut_bucket", types.BILLING_PERIOD_MONTHLY, gpus, "5")
	start := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	sub := s.subscribeIn(p, start, types.BillingCycleAnniversary, "UTC")
	created := s.reserveGPUs(sub, p, start, time.Time{})
	_, err := s.commitments.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt: lo.ToPtr(time.Date(2026, 5, 20, 14, 20, 0, 0, time.UTC)),
	})
	s.Require().NoError(err)
	s.record(gpus, time.Date(2026, 5, 20, 14, 10, 0, 0, time.UTC), "2")
	s.record(gpus, time.Date(2026, 5, 20, 14, 40, 0, 0, time.UTC), "2")

	units, due := billedUsage(s.invoiceFor(sub, start, start.AddDate(0, 1, 0)))
	s.True(units.Equal(dec("2")), "GPUs billed: want 2, got %s", units)
	s.True(due.Equal(dec("1002")), "amount due: want 1002, got %s", due)
}

// A commitment that starts or ends at a period edge inside a bucket does not carry the bucket across the edge, so each
// period bills its own part of it, as pricing without a commitment does: no GPU before or after the edge goes unbilled.
func (s *CommitmentE2ESuite) TestWindowBucketCutWhereTheCommitmentStartsOrEnds_BillsBothParts() {
	start := time.Date(2026, 5, 15, 10, 15, 0, 0, time.UTC)
	edge, end := start.AddDate(0, 1, 0), start.AddDate(0, 2, 0)
	tests := []struct {
		name       string
		from, till time.Time // the commitment's term; a zero till leaves it open-ended
		due        string
	}{
		// May's 744 hours at two GPUs and $4, and $5 a GPU for the 10:00 bucket on each side of the edge.
		{name: "ends at the edge", from: start, till: edge, due: "5972"},
		// June's 720 hours, from the 10:00 bucket the edge cuts, and May's $5 a GPU before the edge.
		{name: "starts at the edge", from: edge, due: "5770"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			gpus := s.hourlyGPUs()
			p := s.planWith("commitment_edge_cut_bucket", types.BILLING_PERIOD_MONTHLY, gpus, "5")
			sub := s.subscribeIn(p, start, types.BillingCycleAnniversary, "UTC")
			s.reserveGPUs(sub, p, tt.from, tt.till)
			s.record(gpus, edge.Add(-10*time.Minute), "2")
			s.record(gpus, edge.Add(15*time.Minute), "2")

			units, due := billedUsage(s.invoiceFor(sub, start, edge), s.invoiceFor(sub, edge, end))
			s.True(units.Equal(dec("4")), "GPUs billed: want 4, got %s", units)
			s.True(due.Equal(dec(tt.due)), "amount due of both periods: want %s, got %s", tt.due, due)
		})
	}
}

// Usage analytics cut inside a billing period, off the bucket grid, adds up to the whole period and its invoice.
func (s *CommitmentE2ESuite) TestAnalytics_RangesCutInsideABucketAddUpToThePeriod() {
	gpus := s.hourlyGPUs()
	p := s.planWith("analytics_cut_bucket", types.BILLING_PERIOD_MONTHLY, gpus, "5")
	jun, jul := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	sub := s.subscribeIn(p, jun, types.BillingCycleAnniversary, "UTC")
	s.reserveGPUs(sub, p, jun, time.Time{})
	s.record(gpus, time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC), "2")

	cost := func(from, to time.Time) decimal.Decimal {
		resp, err := s.analytics.GetDetailedAnalytics(s.GetContext(), &events.MeterUsageDetailedAnalyticsParams{
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			ExternalCustomerID: s.customer.ExternalID,
			StartTime:          from,
			EndTime:            to,
		})
		s.Require().NoError(err)

		total := decimal.Zero
		for _, item := range resp.Items {
			total = total.Add(item.TotalCost)
		}

		return total
	}

	cut := time.Date(2026, 6, 15, 10, 15, 0, 0, time.UTC)
	whole, parts := cost(jun, jul), cost(jun, cut).Add(cost(cut, jul))
	s.True(whole.Equal(s.invoiceFor(sub, jun, jul).AmountDue), "whole period %s", whole)
	s.True(parts.Equal(whole), "ranges cut at %s add up to %s, the whole period is %s", cut, parts, whole)
}
