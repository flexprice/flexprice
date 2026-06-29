package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Backward-compatibility / contract guarantees for subscription creation:
//   - the subscription always inherits the customer's timezone
//   - a timezone supplied in the subscription request is IGNORED (the customer record wins)
//   - a UTC customer's billing boundaries are the legacy UTC midnights (no behaviour change)

func (s *SubscriptionTimezoneTestSuite) TestSubscriptionIgnoresCallerSuppliedTimezone() {
	ctx := s.GetContext()

	// Customer is in New York; the subscription request will (wrongly) claim Kolkata.
	custResp, err := s.customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
		ExternalID: "ext-tz-ignore-caller",
		Name:       "NY Customer",
		Email:      "tz-ignore@example.com",
		Timezone:   "America/New_York",
	})
	s.Require().NoError(err)

	resp, err := s.subscriptionSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         custResp.Customer.ID,
		PlanID:             s.planID,
		StartDate:          lo.ToPtr(time.Date(2024, 3, 1, 5, 0, 0, 0, time.UTC)), // NY local midnight
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		Timezone:           "Asia/Kolkata", // must be ignored
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	s.Equal("America/New_York", resp.Timezone,
		"subscription must inherit the customer timezone, ignoring the request-supplied value")
}

func (s *SubscriptionTimezoneTestSuite) TestUTCCustomerSubscriptionUsesLegacyBoundaries() {
	ctx := s.GetContext()

	custResp, err := s.customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
		ExternalID: "ext-tz-utc-legacy",
		Name:       "UTC Customer",
		Email:      "tz-utc@example.com",
		Timezone:   "UTC",
	})
	s.Require().NoError(err)

	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	resp, err := s.subscriptionSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         custResp.Customer.ID,
		PlanID:             s.planID,
		StartDate:          lo.ToPtr(start),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)

	// Legacy UTC behaviour: monthly anniversary from Jan 15 → Feb 15, both at UTC midnight.
	s.True(resp.CurrentPeriodStart.UTC().Equal(start),
		"UTC customer period start must equal the start date unchanged")
	s.True(resp.CurrentPeriodEnd.UTC().Equal(time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)),
		"UTC customer period end must be the legacy UTC boundary")
}
