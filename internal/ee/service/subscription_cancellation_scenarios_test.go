package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *SubscriptionServiceSuite) TestCancelSubscription_SchedulesRequestedDate() {
	ctx := s.GetContext()
	today := s.GetNow().Truncate(time.Second)
	periodStart := today.AddDate(0, 0, -5)
	periodEnd := today.AddDate(0, 0, 25)
	past := today.AddDate(0, 0, -1)
	inThisPeriod := today.AddDate(0, 0, 15)
	inNextPeriod := today.AddDate(0, 0, 30)
	const reason = "customer requested cancellation"

	cases := []struct {
		name          string
		cancelAt      time.Time
		wantPeriodEnd time.Time
	}{
		{
			name:          "future date inside the period shortens it",
			cancelAt:      inThisPeriod,
			wantPeriodEnd: inThisPeriod,
		},
		{
			name:          "backdated date inside the period shortens it",
			cancelAt:      past,
			wantPeriodEnd: past,
		},
		{
			name:          "date in the next period keeps the current boundary",
			cancelAt:      inNextPeriod,
			wantPeriodEnd: periodEnd,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			sub := s.cancellationSubscription(periodStart, periodEnd)
			s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

			response, err := s.service.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
				CancellationType:               types.CancellationTypeScheduledDate,
				CancelAt:                       lo.ToPtr(tc.cancelAt),
				Reason:                         reason,
				CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
			})
			s.Require().NoError(err)
			s.Equal(tc.cancelAt, response.EffectiveDate)
			s.Equal(types.CancellationTypeScheduledDate, response.CancellationType)
			s.Equal(types.SubscriptionStatusActive, response.Status)
			s.Contains(response.Message, tc.cancelAt.Format(time.DateOnly))

			stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
			s.True(stored.CancelAtPeriodEnd)
			s.Equal(&tc.cancelAt, stored.CancelAt)
			s.Equal(&tc.cancelAt, stored.EndDate)
			s.Equal(tc.wantPeriodEnd, stored.CurrentPeriodEnd)
			s.NotNil(stored.CancelledAt)
			s.Equal("scheduled_date", stored.Metadata["cancellation_type"])
			s.Equal(reason, stored.Metadata["cancellation_reason"])
			s.Equal(tc.cancelAt.Format(time.RFC3339), stored.Metadata["effective_date"])

			schedules, err := s.GetStores().SubscriptionScheduleRepo.GetBySubscriptionID(ctx, sub.ID)
			s.Require().NoError(err)
			s.Require().Len(schedules, 1)
			s.Equal(types.SubscriptionScheduleChangeTypeCancellation, schedules[0].ScheduleType)
			s.Equal(types.ScheduleStatusPending, schedules[0].Status)
			s.Equal(tc.cancelAt, schedules[0].ScheduledAt)

			filter := types.NewInvoiceFilter()
			filter.SubscriptionID = sub.ID
			invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
			s.Require().NoError(err)
			s.Empty(invoices, "scheduling should produce no immediate invoice")
		})
	}
}

func (s *SubscriptionServiceSuite) TestCancelSubscription_RejectsInvalidSchedules() {
	ctx := s.GetContext()
	today := s.GetNow().Truncate(time.Second)
	periodStart := today.AddDate(0, 0, -5)
	periodEnd := today.AddDate(0, 0, 25)
	future := today.AddDate(0, 0, 15)
	existing := today.AddDate(0, 0, 5)
	scheduledDate := types.CancellationTypeScheduledDate
	periodEndMode := types.CancellationTypeEndOfPeriod
	active := types.SubscriptionStatusActive

	cases := []struct {
		name                  string
		status                types.SubscriptionStatus
		existingCancelAt      *time.Time
		existingPeriodEndFlag bool
		mode                  types.CancellationType
		requestedCancelAt     *time.Time
		wantError             string
	}{
		{
			name: "scheduled date is missing", status: active, mode: scheduledDate,
			wantError: "cancel_at is required",
		},
		{
			name: "date precedes the current period", status: active, mode: scheduledDate,
			requestedCancelAt: lo.ToPtr(periodStart.AddDate(0, 0, -1)),
			wantError:         "cancel_at must be after current period start",
		},
		{
			name: "date equals the current period start", status: active, mode: scheduledDate,
			requestedCancelAt: &periodStart,
			wantError:         "cancel_at must be after current period start",
		},
		{
			name: "custom date would overwrite a stored date", status: active, mode: scheduledDate,
			existingCancelAt: &existing, requestedCancelAt: &future,
			wantError: "already scheduled",
		},
		{
			name: "custom date would overwrite a flagged cancellation", status: active, mode: scheduledDate,
			existingCancelAt: &existing, existingPeriodEndFlag: true, requestedCancelAt: &future,
			wantError: "already scheduled",
		},
		{
			name: "period-end request would overwrite a stored date", status: active, mode: periodEndMode,
			existingCancelAt: &existing,
			wantError:        "already scheduled",
		},
		{
			name: "subscription is already cancelled", status: types.SubscriptionStatusCancelled, mode: scheduledDate,
			requestedCancelAt: &future,
			wantError:         "already cancelled",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			sub := s.cancellationSubscription(periodStart, periodEnd)
			sub.SubscriptionStatus = tc.status
			if tc.existingCancelAt != nil {
				sub.CancelAt = lo.ToPtr(*tc.existingCancelAt)
			}
			sub.CancelAtPeriodEnd = tc.existingPeriodEndFlag
			s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

			_, err := s.service.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
				CancellationType: tc.mode,
				CancelAt:         tc.requestedCancelAt,
			})
			s.Require().ErrorContains(err, tc.wantError)
			s.True(ierr.IsValidation(err), "expected a validation error")

			stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(tc.status, stored.SubscriptionStatus)
			s.Equal(tc.existingCancelAt, stored.CancelAt)
			s.Equal(tc.existingPeriodEndFlag, stored.CancelAtPeriodEnd)
			s.Equal(periodEnd, stored.CurrentPeriodEnd)
			s.Nil(stored.CancelledAt)
			s.Nil(stored.EndDate)

			schedules, err := s.GetStores().SubscriptionScheduleRepo.GetBySubscriptionID(ctx, sub.ID)
			s.Require().NoError(err)
			s.Empty(schedules, "rejected cancellation should create no schedule")
		})
	}
}

func (s *SubscriptionServiceSuite) cancellationSubscription(periodStart, periodEnd time.Time) *subscription.Subscription {
	return &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           types.DefaultTimezone,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
}
