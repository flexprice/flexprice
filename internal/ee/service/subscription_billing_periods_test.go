package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

// SubscriptionBillingPeriodsSuite covers period generation for subscriptions that carry an
// end date. An end date landing inside the open period used to produce a period running
// backwards, which every downstream invoice validation rejects — wedging the billing cron
// before it reached the step that marks the subscription cancelled.
type SubscriptionBillingPeriodsSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionService
}

func TestSubscriptionBillingPeriods(t *testing.T) {
	suite.Run(t, new(SubscriptionBillingPeriodsSuite))
}

func (s *SubscriptionBillingPeriodsSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.service = NewSubscriptionService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		IntegrationFactory:       s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionBillingPeriodsSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

// monthStart returns the first of the month, monthsAgo months before now, in UTC.
func monthStart(monthsAgo int) time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -monthsAgo, 0)
}

func (s *SubscriptionBillingPeriodsSuite) newSubscription(
	id string,
	periodStart, periodEnd time.Time,
	endDate *time.Time,
) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             "plan_periods",
		CustomerID:         "cust_periods",
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		EndDate:            endDate,
		Currency:           "usd",
		BillingCycle:       types.BillingCycleCalendar,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive),
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// TestEndDateMidPeriod_NoInvertedPeriod reproduces the production wedge: the end date lands
// inside the still-open period, NextBillingDate clamps the next period end back to the end
// date, and the generated period runs backwards.
func (s *SubscriptionBillingPeriodsSuite) TestEndDateMidPeriod_NoInvertedPeriod() {
	periodStart := monthStart(2)
	periodEnd := periodStart.AddDate(0, 1, 0)
	endDate := periodStart.AddDate(0, 0, 9)

	sub := s.newSubscription("sub_mid_period_end", periodStart, periodEnd, &endDate)

	periods, err := s.service.CalculateBillingPeriods(s.GetContext(), sub.ID)
	s.NoError(err)

	for i, p := range periods {
		s.Falsef(p.End.Before(p.Start),
			"period %d runs backwards: start=%s end=%s", i, p.Start, p.End)
	}

	// The billing workflow only proceeds past CalculatePeriods — and therefore only reaches
	// the cancellation check — when more than one period is produced.
	s.Greater(len(periods), 1, "must produce a terminal period so the cancellation check runs")

	last := periods[len(periods)-1]
	s.True(last.End.Equal(endDate), "final period must land on the end date, got %s", last.End)
}

// TestEndDateOnPeriodEnd_StillTerminates guards the behaviour CheckCancellationActivity
// depends on: a zero-length terminal period whose end equals the end date is what triggers
// cancellation.
func (s *SubscriptionBillingPeriodsSuite) TestEndDateOnPeriodEnd_StillTerminates() {
	periodStart := monthStart(2)
	periodEnd := periodStart.AddDate(0, 1, 0)
	endDate := periodEnd

	sub := s.newSubscription("sub_end_on_boundary", periodStart, periodEnd, &endDate)

	periods, err := s.service.CalculateBillingPeriods(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Len(periods, 2)
	s.True(periods[0].Start.Equal(periodStart))
	s.True(periods[0].End.Equal(periodEnd))
	s.True(periods[1].Start.Equal(endDate))
	s.True(periods[1].End.Equal(endDate))
}

// TestNoEndDate_Unchanged guards the ordinary rollover path.
func (s *SubscriptionBillingPeriodsSuite) TestNoEndDate_Unchanged() {
	periodStart := monthStart(2)
	periodEnd := periodStart.AddDate(0, 1, 0)

	sub := s.newSubscription("sub_no_end_date", periodStart, periodEnd, nil)

	periods, err := s.service.CalculateBillingPeriods(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Greater(len(periods), 1)
	for i, p := range periods {
		s.Falsef(p.End.Before(p.Start), "period %d runs backwards", i)
	}
	s.True(periods[len(periods)-1].End.After(time.Now().UTC()),
		"last period should extend past now")
}
