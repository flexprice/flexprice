package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestCalculateCreditGrantPeriods exercises the pure period-math helper that
// decouples credit grant schedules from subscription cron processing.
func TestCalculateCreditGrantPeriods(t *testing.T) {
	newGrant := func(period types.CreditGrantPeriod, periodCount int, anchor time.Time) creditgrant.CreditGrant {
		return creditgrant.CreditGrant{
			ID:                "cg_period_math",
			Period:            lo.ToPtr(period),
			PeriodCount:       lo.ToPtr(periodCount),
			CreditGrantAnchor: lo.ToPtr(anchor),
		}
	}

	t.Run("monthly_grant_produces_contiguous_monthly_periods", func(t *testing.T) {
		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)

		periods, err := CalculateCreditGrantPeriods(newGrant(types.CREDIT_GRANT_PERIOD_MONTHLY, 1, start), start, &end, "UTC")
		require.NoError(t, err)
		require.Len(t, periods, 3)

		assert.True(t, periods[0].Start.Equal(start))
		assert.True(t, periods[0].End.Equal(time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)))
		assert.True(t, periods[1].Start.Equal(periods[0].End), "periods must be contiguous")
		assert.True(t, periods[1].End.Equal(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)))
		assert.True(t, periods[2].Start.Equal(periods[1].End))
		assert.True(t, periods[2].End.Equal(end))
	})

	t.Run("month_end_anchor_clamps_to_shorter_months", func(t *testing.T) {
		start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

		periods, err := CalculateCreditGrantPeriods(newGrant(types.CREDIT_GRANT_PERIOD_MONTHLY, 1, start), start, &end, "UTC")
		require.NoError(t, err)
		require.Len(t, periods, 2)

		assert.True(t, periods[0].End.Equal(time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)),
			"February period end must clamp to Feb 28, got %s", periods[0].End)
		assert.True(t, periods[1].End.Equal(end), "anchor day 31 must be restored in March, got %s", periods[1].End)
	})

	t.Run("weekly_grant_produces_weekly_periods", func(t *testing.T) {
		start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)

		periods, err := CalculateCreditGrantPeriods(newGrant(types.CREDIT_GRANT_PERIOD_WEEKLY, 1, start), start, &end, "UTC")
		require.NoError(t, err)
		require.Len(t, periods, 2)
		assert.True(t, periods[0].End.Equal(time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)))
		assert.True(t, periods[1].End.Equal(end))
	})

	t.Run("end_date_caps_the_final_period", func(t *testing.T) {
		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

		periods, err := CalculateCreditGrantPeriods(newGrant(types.CREDIT_GRANT_PERIOD_MONTHLY, 1, start), start, &end, "UTC")
		require.NoError(t, err)
		require.NotEmpty(t, periods)
		assert.True(t, periods[0].Start.Equal(start))
		assert.True(t, periods[len(periods)-1].End.Equal(end), "final period must be capped at the grant end date")
	})

	t.Run("invalid_credit_grant_period_returns_error", func(t *testing.T) {
		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)

		_, err := CalculateCreditGrantPeriods(newGrant(types.CreditGrantPeriod("BOGUS"), 1, start), start, &end, "UTC")
		assert.Error(t, err)
	})
}

// CreditGrantPeriodsSuite covers the state-machine branches of credit grant
// application processing: catch-up of backdated periods, skip (paused), defer
// (incomplete), cancel (cancelled), failure handling, and cron idempotency.
type CreditGrantPeriodsSuite struct {
	testutil.BaseServiceTestSuite
	service  CreditGrantService
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
		wallet   *wallet.Wallet
		now      time.Time
	}
}

func TestCreditGrantPeriodsSuite(t *testing.T) {
	suite.Run(t, new(CreditGrantPeriodsSuite))
}

func (s *CreditGrantPeriodsSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewCreditGrantService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_cg_periods",
		ExternalID: "ext_cust_cg_periods",
		Name:       "CG Periods Customer",
		Email:      "cg_periods@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_cg_periods",
		Name:      "CG Periods Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	// Prepaid wallet that credit grants top up.
	s.testData.wallet = &wallet.Wallet{
		ID:                  "wallet_cg_periods",
		CustomerID:          s.testData.customer.ID,
		Name:                "CG Periods Wallet",
		Currency:            "usd",
		Balance:             decimal.Zero,
		CreditBalance:       decimal.Zero,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          types.WalletTypePrePaid,
		AlertState:          types.AlertStateOk,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

func (s *CreditGrantPeriodsSuite) createSubscription(id string, status types.SubscriptionStatus, startDate time.Time) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		Currency:           "usd",
		StartDate:          startDate,
		BillingAnchor:      startDate,
		CurrentPeriodStart: startDate,
		CurrentPeriodEnd:   s.testData.now.Add(15 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: status,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
	return sub
}

func (s *CreditGrantPeriodsSuite) createGrant(id string, subID string, cadence types.CreditGrantCadence, credits decimal.Decimal, startDate time.Time, mutate func(*creditgrant.CreditGrant)) *creditgrant.CreditGrant {
	cg := &creditgrant.CreditGrant{
		ID:                id,
		Name:              "Grant " + id,
		Scope:             types.CreditGrantScopeSubscription,
		SubscriptionID:    &subID,
		PlanID:            &s.testData.plan.ID,
		Credits:           credits,
		Cadence:           cadence,
		ExpirationType:    types.CreditGrantExpiryTypeNever,
		Priority:          lo.ToPtr(1),
		StartDate:         &startDate,
		CreditGrantAnchor: &startDate,
		EnvironmentID:     types.GetEnvironmentID(s.GetContext()),
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	if cadence == types.CreditGrantCadenceRecurring {
		cg.Period = lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY)
		cg.PeriodCount = lo.ToPtr(1)
	}
	if mutate != nil {
		mutate(cg)
	}
	created, err := s.GetStores().CreditGrantRepo.Create(s.GetContext(), cg)
	s.NoError(err)
	return created
}

func (s *CreditGrantPeriodsSuite) createCGA(id string, grant *creditgrant.CreditGrant, subID string, subStatus types.SubscriptionStatus, periodStart time.Time, periodEnd *time.Time, credits decimal.Decimal) *creditgrantapplication.CreditGrantApplication {
	cga := &creditgrantapplication.CreditGrantApplication{
		ID:                              id,
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subID,
		ScheduledFor:                    periodStart,
		PeriodStart:                     periodStart,
		PeriodEnd:                       periodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		Credits:                         credits,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: subStatus,
		IdempotencyKey:                  "idem_" + id,
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), cga))
	return cga
}

func (s *CreditGrantPeriodsSuite) listGrantApplications(grantID string) []*creditgrantapplication.CreditGrantApplication {
	apps, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), &types.CreditGrantApplicationFilter{
		QueryFilter:    types.NewNoLimitQueryFilter(),
		CreditGrantIDs: []string{grantID},
	})
	s.NoError(err)
	return apps
}

func (s *CreditGrantPeriodsSuite) walletBalance() decimal.Decimal {
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	return w.Balance
}

func (s *CreditGrantPeriodsSuite) TestBackdatedGrantCatchesUpAllPastPeriods() {
	start := s.testData.now.AddDate(0, 0, -75)
	sub := s.createSubscription("subs_cg_catchup", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_catchup", sub.ID, types.CreditGrantCadenceRecurring, decimal.RequireFromString("10.5"), start, nil)

	_, firstPeriodEnd, err := CalculateNextCreditGrantPeriod(lo.FromPtr(grant), start, "UTC")
	s.NoError(err)
	cga := s.createCGA("cga_catchup_1", grant, sub.ID, types.SubscriptionStatusActive, start, &firstPeriodEnd, grant.Credits)

	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	// ~75 days at monthly cadence = 3 past-due periods applied, 1 future pending.
	s.True(s.walletBalance().Equal(decimal.RequireFromString("31.5")),
		"expected 3 catch-up applications of 10.5 credits each, wallet has %s", s.walletBalance())

	apps := s.listGrantApplications(grant.ID)
	s.Len(apps, 4)

	applied := 0
	pending := 0
	for _, app := range apps {
		switch app.ApplicationStatus {
		case types.ApplicationStatusApplied:
			applied++
			s.NotNil(app.AppliedAt)
		case types.ApplicationStatusPending:
			pending++
			s.True(app.ScheduledFor.After(s.testData.now), "the remaining pending CGA must be future-dated")
		}
	}
	s.Equal(3, applied)
	s.Equal(1, pending)
}

func (s *CreditGrantPeriodsSuite) TestAppliedApplicationIsNotReprocessedByCron() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_idem", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_idem", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(100), start, nil)
	cga := s.createCGA("cga_idem_1", grant, sub.ID, types.SubscriptionStatusActive, start, nil, grant.Credits)

	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))
	s.True(s.walletBalance().Equal(decimal.NewFromInt(100)))

	stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, stored.ApplicationStatus)
	s.NotNil(stored.AppliedAt)

	// Cron sweep after application: nothing is pending/failed, so nothing may be
	// re-applied and the wallet must not be credited twice.
	resp, err := s.service.ProcessScheduledCreditGrantApplications(s.GetContext())
	s.NoError(err)
	s.Equal(0, resp.TotalApplicationsCount)
	s.True(s.walletBalance().Equal(decimal.NewFromInt(100)), "cron must not double-apply an applied grant")
}

func (s *CreditGrantPeriodsSuite) TestPausedSubscriptionSkipsApplication() {
	s.Run("recurring_grant_skips_and_schedules_next_period", func() {
		start := s.testData.now.AddDate(0, 0, -15)
		sub := s.createSubscription("subs_cg_skip_rec", types.SubscriptionStatusPaused, start)
		grant := s.createGrant("cg_skip_rec", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(50), start, nil)

		_, periodEnd, err := CalculateNextCreditGrantPeriod(lo.FromPtr(grant), start, "UTC")
		s.NoError(err)
		cga := s.createCGA("cga_skip_rec_1", grant, sub.ID, types.SubscriptionStatusPaused, start, &periodEnd, grant.Credits)

		s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

		stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
		s.NoError(err)
		s.Equal(types.ApplicationStatusSkipped, stored.ApplicationStatus)
		s.Nil(stored.AppliedAt)
		s.True(s.walletBalance().IsZero(), "skipped application must not credit the wallet")

		apps := s.listGrantApplications(grant.ID)
		s.Len(apps, 2, "a next-period application must be scheduled for recurring grants")
		for _, app := range apps {
			if app.ID == cga.ID {
				continue
			}
			s.Equal(types.ApplicationStatusPending, app.ApplicationStatus)
			s.True(app.PeriodStart.Equal(periodEnd), "next period must start where the skipped period ended")
		}
	})

	s.Run("onetime_grant_skips_without_next_period", func() {
		start := s.testData.now.AddDate(0, 0, -15)
		sub := s.createSubscription("subs_cg_skip_once", types.SubscriptionStatusPaused, start)
		grant := s.createGrant("cg_skip_once", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(50), start, nil)
		cga := s.createCGA("cga_skip_once_1", grant, sub.ID, types.SubscriptionStatusPaused, start, nil, grant.Credits)

		s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

		stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
		s.NoError(err)
		s.Equal(types.ApplicationStatusSkipped, stored.ApplicationStatus)
		s.Len(s.listGrantApplications(grant.ID), 1, "one-time grants must not schedule a next period")
	})
}

func (s *CreditGrantPeriodsSuite) TestIncompleteSubscriptionDefersApplication() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_defer", types.SubscriptionStatusIncomplete, start)
	grant := s.createGrant("cg_defer", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(50), start, nil)
	cga := s.createCGA("cga_defer_1", grant, sub.ID, types.SubscriptionStatusIncomplete, start, nil, grant.Credits)

	before := time.Now().UTC()
	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusPending, stored.ApplicationStatus, "deferred application stays pending")
	s.Equal(1, stored.RetryCount)
	s.True(stored.ScheduledFor.After(before.Add(29*time.Minute)),
		"first deferral must reschedule ~30 minutes out, got %s", stored.ScheduledFor)
	s.True(stored.ScheduledFor.Before(before.Add(31 * time.Minute)))
	s.True(s.walletBalance().IsZero())
}

func (s *CreditGrantPeriodsSuite) TestCancelledSubscriptionCancelsApplications() {
	start := s.testData.now.AddDate(0, 0, -15)
	sub := s.createSubscription("subs_cg_cancel", types.SubscriptionStatusCancelled, start)
	grant := s.createGrant("cg_cancel", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(50), start, nil)

	_, periodEnd, err := CalculateNextCreditGrantPeriod(lo.FromPtr(grant), start, "UTC")
	s.NoError(err)
	target := s.createCGA("cga_cancel_1", grant, sub.ID, types.SubscriptionStatusCancelled, start, &periodEnd, grant.Credits)
	future := s.createCGA("cga_cancel_2", grant, sub.ID, types.SubscriptionStatusCancelled, periodEnd, nil, grant.Credits)

	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), target.ID))

	storedTarget, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), target.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusCancelled, storedTarget.ApplicationStatus)
	s.Nil(storedTarget.AppliedAt)

	storedFuture, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), future.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusCancelled, storedFuture.ApplicationStatus,
		"future applications of the grant must be cancelled too")
	s.True(s.walletBalance().IsZero())
}

func (s *CreditGrantPeriodsSuite) TestUnpublishedGrantIsIgnored() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_archived", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_unpublished", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(50), start, func(cg *creditgrant.CreditGrant) {
		cg.Status = types.StatusDeleted
	})
	cga := s.createCGA("cga_unpublished_1", grant, sub.ID, types.SubscriptionStatusActive, start, nil, grant.Credits)

	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusPending, stored.ApplicationStatus, "non-published grants must be left untouched")
	s.True(s.walletBalance().IsZero())
}

func (s *CreditGrantPeriodsSuite) TestProcessCreditGrantApplicationErrors() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_errors", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_errors", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(50), start, nil)

	s.Run("unknown_application_id_returns_error", func() {
		s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), "cga_does_not_exist"))
	})

	s.Run("unknown_subscription_returns_error", func() {
		cga := s.createCGA("cga_err_no_sub", grant, "subs_does_not_exist", types.SubscriptionStatusActive, start, nil, grant.Credits)
		s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))
	})

	s.Run("unknown_grant_returns_error", func() {
		cga := &creditgrantapplication.CreditGrantApplication{
			ID:                              "cga_err_no_grant",
			CreditGrantID:                   "cg_does_not_exist",
			SubscriptionID:                  sub.ID,
			ScheduledFor:                    start,
			PeriodStart:                     start,
			ApplicationStatus:               types.ApplicationStatusPending,
			Credits:                         decimal.NewFromInt(50),
			ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
			SubscriptionStatusAtApplication: types.SubscriptionStatusActive,
			IdempotencyKey:                  "idem_cga_err_no_grant",
			EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
			BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), cga))
		s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))
	})
}

func (s *CreditGrantPeriodsSuite) TestFailedApplicationIsMarkedFailedAndRetryCounted() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_fail", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_fail", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(50), start, nil)
	// Zero credits make the wallet top-up validation fail deterministically,
	// driving the handleCreditGrantFailure path.
	cga := s.createCGA("cga_fail_1", grant, sub.ID, types.SubscriptionStatusActive, start, nil, decimal.Zero)

	s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusFailed, stored.ApplicationStatus)
	s.Nil(stored.AppliedAt)
	s.Contains(lo.FromPtr(stored.FailureReason), "Transaction failed during credit grant application")
	s.Equal(0, stored.RetryCount, "first failure is not a retry")
	s.True(s.walletBalance().IsZero(), "failed application must not credit the wallet")

	// Retrying the failed application increments the retry count and fails again.
	s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	stored, err = s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusFailed, stored.ApplicationStatus)
	s.Equal(1, stored.RetryCount)
	s.True(s.walletBalance().IsZero())
}

func (s *CreditGrantPeriodsSuite) TestCreateCreditGrantScopeValidation() {
	planGrantReq := func(mutate func(*dto.CreateCreditGrantRequest)) dto.CreateCreditGrantRequest {
		req := dto.CreateCreditGrantRequest{
			Name:           "Plan Scoped Grant",
			Scope:          types.CreditGrantScopePlan,
			PlanID:         &s.testData.plan.ID,
			Credits:        decimal.NewFromInt(10),
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
			Priority:       lo.ToPtr(1),
		}
		if mutate != nil {
			mutate(&req)
		}
		return req
	}

	s.Run("plan_not_found_returns_error", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), planGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.PlanID = lo.ToPtr("plan_does_not_exist")
		}))
		s.Error(err)
	})

	s.Run("first_plan_grant_is_created_with_conversion_rate", func() {
		resp, err := s.service.CreateCreditGrant(s.GetContext(), planGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.ConversionRate = lo.ToPtr(decimal.NewFromInt(2))
			r.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(2))
		}))
		s.NoError(err)
		s.NotNil(resp)

		stored, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), resp.CreditGrant.ID)
		s.NoError(err)
		s.Equal(types.CreditGrantScopePlan, stored.Scope)
		s.True(stored.ConversionRate.Equal(decimal.NewFromInt(2)))
	})

	s.Run("conflicting_conversion_rate_on_same_plan_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), planGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.ConversionRate = lo.ToPtr(decimal.NewFromInt(5))
			r.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(2))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("conflicting_topup_conversion_rate_on_same_plan_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), planGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.ConversionRate = lo.ToPtr(decimal.NewFromInt(2))
			r.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(9))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("matching_conversion_rates_on_same_plan_are_accepted", func() {
		resp, err := s.service.CreateCreditGrant(s.GetContext(), planGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.Name = "Second Plan Grant"
			r.ConversionRate = lo.ToPtr(decimal.NewFromInt(2))
			r.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(2))
		}))
		s.NoError(err)
		s.NotNil(resp)
	})

	subStart := s.testData.now.Add(-48 * time.Hour)
	subEnd := s.testData.now.Add(30 * 24 * time.Hour)
	sub := s.createSubscription("subs_cg_create_val", types.SubscriptionStatusActive, subStart)
	sub.EndDate = &subEnd
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	cancelledSub := s.createSubscription("subs_cg_create_cancelled", types.SubscriptionStatusCancelled, subStart)

	subGrantReq := func(mutate func(*dto.CreateCreditGrantRequest)) dto.CreateCreditGrantRequest {
		req := dto.CreateCreditGrantRequest{
			Name:           "Subscription Scoped Grant",
			Scope:          types.CreditGrantScopeSubscription,
			SubscriptionID: &sub.ID,
			PlanID:         &s.testData.plan.ID,
			Credits:        decimal.NewFromInt(10),
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
			Priority:       lo.ToPtr(1),
			StartDate:      lo.ToPtr(s.testData.now.Add(-1 * time.Hour)),
		}
		if mutate != nil {
			mutate(&req)
		}
		return req
	}

	s.Run("cancelled_subscription_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.SubscriptionID = &cancelledSub.ID
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("start_date_before_subscription_start_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.StartDate = lo.ToPtr(subStart.Add(-24 * time.Hour))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("end_date_after_subscription_end_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.EndDate = lo.ToPtr(subEnd.Add(24 * time.Hour))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("anchor_before_subscription_start_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.CreditGrantAnchor = lo.ToPtr(subStart.Add(-24 * time.Hour))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("anchor_after_subscription_end_is_rejected", func() {
		_, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(func(r *dto.CreateCreditGrantRequest) {
			r.CreditGrantAnchor = lo.ToPtr(subEnd.Add(24 * time.Hour))
		}))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("valid_subscription_grant_creates_pending_application_and_applies_eagerly", func() {
		resp, err := s.service.CreateCreditGrant(s.GetContext(), subGrantReq(nil))
		s.NoError(err)
		s.NotNil(resp)

		apps := s.listGrantApplications(resp.CreditGrant.ID)
		s.Len(apps, 1, "workflow initialization must create the first application")
		s.Equal(types.ApplicationStatusApplied, apps[0].ApplicationStatus,
			"past-anchored grant must be applied eagerly")
		s.True(s.walletBalance().Equal(decimal.NewFromInt(10)))
	})
}

func (s *CreditGrantPeriodsSuite) TestUpdateCreditGrant() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_update", types.SubscriptionStatusActive, start)

	s.Run("unknown_grant_returns_error", func() {
		_, err := s.service.UpdateCreditGrant(s.GetContext(), "cg_missing", dto.UpdateCreditGrantRequest{
			Name: lo.ToPtr("New Name"),
		})
		s.Error(err)
	})

	s.Run("updates_name_and_metadata", func() {
		grant := s.createGrant("cg_update_basic", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), start, nil)

		resp, err := s.service.UpdateCreditGrant(s.GetContext(), grant.ID, dto.UpdateCreditGrantRequest{
			Name:     lo.ToPtr("Renamed Grant"),
			Metadata: lo.ToPtr(types.Metadata{"team": "billing"}),
		})
		s.NoError(err)
		s.Equal("Renamed Grant", resp.CreditGrant.Name)

		stored, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), grant.ID)
		s.NoError(err)
		s.Equal("Renamed Grant", stored.Name)
		s.Equal("billing", stored.Metadata["team"])
	})

	s.Run("end_date_sets_grant_end_and_cancels_future_applications", func() {
		grant := s.createGrant("cg_update_end", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(10), start, nil)
		futureStart := s.testData.now.Add(15 * 24 * time.Hour)
		cga := s.createCGA("cga_update_end_1", grant, sub.ID, types.SubscriptionStatusActive, futureStart, nil, grant.Credits)

		endDate := s.testData.now.Add(1 * time.Hour).Truncate(time.Second)
		_, err := s.service.UpdateCreditGrant(s.GetContext(), grant.ID, dto.UpdateCreditGrantRequest{
			EndDate: &endDate,
		})
		s.NoError(err)

		stored, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), grant.ID)
		s.NoError(err)
		s.NotNil(stored.EndDate)
		s.True(endDate.Equal(lo.FromPtr(stored.EndDate)))

		storedCGA, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
		s.NoError(err)
		s.Equal(types.ApplicationStatusCancelled, storedCGA.ApplicationStatus,
			"pending future applications must be cancelled when the grant is ended")
	})
}

func (s *CreditGrantPeriodsSuite) TestDeleteCreditGrant() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_delete", types.SubscriptionStatusActive, start)

	s.Run("unknown_grant_returns_error", func() {
		s.Error(s.service.DeleteCreditGrant(s.GetContext(), dto.DeleteCreditGrantRequest{CreditGrantID: "cg_missing"}))
	})

	s.Run("non_published_grant_is_rejected", func() {
		grant := s.createGrant("cg_delete_unpub", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), start, func(cg *creditgrant.CreditGrant) {
			cg.Status = types.StatusDeleted
		})

		err := s.service.DeleteCreditGrant(s.GetContext(), dto.DeleteCreditGrantRequest{CreditGrantID: grant.ID})
		s.Error(err)
	})

	s.Run("plan_scoped_grant_is_archived", func() {
		grant := s.createGrant("cg_delete_plan", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), start, func(cg *creditgrant.CreditGrant) {
			cg.Scope = types.CreditGrantScopePlan
			cg.SubscriptionID = nil
			cg.StartDate = nil
			cg.CreditGrantAnchor = nil
		})

		s.NoError(s.service.DeleteCreditGrant(s.GetContext(), dto.DeleteCreditGrantRequest{CreditGrantID: grant.ID}))

		_, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), grant.ID)
		s.Error(err, "archived plan-scoped grants must no longer be retrievable")
	})

	s.Run("subscription_scoped_grant_gets_end_date_and_future_applications_cancelled", func() {
		grant := s.createGrant("cg_delete_sub", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(10), start, nil)
		futureStart := s.testData.now.Add(15 * 24 * time.Hour)
		cga := s.createCGA("cga_delete_sub_1", grant, sub.ID, types.SubscriptionStatusActive, futureStart, nil, grant.Credits)

		effective := s.testData.now.Truncate(time.Second)
		s.NoError(s.service.DeleteCreditGrant(s.GetContext(), dto.DeleteCreditGrantRequest{
			CreditGrantID: grant.ID,
			EffectiveDate: &effective,
		}))

		stored, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), grant.ID)
		s.NoError(err)
		s.Equal(types.StatusPublished, stored.Status, "subscription-scoped grants are ended, not archived")
		s.True(effective.Equal(lo.FromPtr(stored.EndDate)))

		storedCGA, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
		s.NoError(err)
		s.Equal(types.ApplicationStatusCancelled, storedCGA.ApplicationStatus)
	})
}

func (s *CreditGrantPeriodsSuite) TestCancelFutureSubscriptionGrants() {
	s.Run("unknown_subscription_is_a_noop", func() {
		s.NoError(s.service.CancelFutureSubscriptionGrants(s.GetContext(), dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: "subs_without_grants",
		}))
	})

	s.Run("cancels_all_published_grants_for_the_subscription", func() {
		start := s.testData.now.Add(-1 * time.Hour)
		sub := s.createSubscription("subs_cg_cancel_all", types.SubscriptionStatusActive, start)
		grantA := s.createGrant("cg_cancel_all_a", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(10), start, nil)
		grantB := s.createGrant("cg_cancel_all_b", sub.ID, types.CreditGrantCadenceRecurring, decimal.NewFromInt(20), start, nil)
		cgaA := s.createCGA("cga_cancel_all_a1", grantA, sub.ID, types.SubscriptionStatusActive, s.testData.now.Add(15*24*time.Hour), nil, grantA.Credits)
		cgaB := s.createCGA("cga_cancel_all_b1", grantB, sub.ID, types.SubscriptionStatusActive, s.testData.now.Add(15*24*time.Hour), nil, grantB.Credits)

		effective := s.testData.now.Truncate(time.Second)
		s.NoError(s.service.CancelFutureSubscriptionGrants(s.GetContext(), dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: sub.ID,
			EffectiveDate:  &effective,
		}))

		for _, grantID := range []string{grantA.ID, grantB.ID} {
			stored, err := s.GetStores().CreditGrantRepo.Get(s.GetContext(), grantID)
			s.NoError(err)
			s.True(effective.Equal(lo.FromPtr(stored.EndDate)), "grant %s must be ended at the effective date", grantID)
		}
		for _, cgaID := range []string{cgaA.ID, cgaB.ID} {
			stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cgaID)
			s.NoError(err)
			s.Equal(types.ApplicationStatusCancelled, stored.ApplicationStatus)
		}
	})
}

func (s *CreditGrantPeriodsSuite) TestListCreditGrantsAndApplicationsDefaults() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_list", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_list", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), start, nil)
	s.createCGA("cga_list_1", grant, sub.ID, types.SubscriptionStatusActive, start, nil, grant.Credits)

	s.Run("nil_grant_filter_uses_defaults", func() {
		resp, err := s.service.ListCreditGrants(s.GetContext(), nil)
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal(grant.ID, resp.Items[0].CreditGrant.ID)
		s.Equal(1, resp.Pagination.Total)
	})

	s.Run("nil_application_filter_uses_defaults", func() {
		resp, err := s.service.ListCreditGrantApplications(s.GetContext(), nil)
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal("cga_list_1", resp.Items[0].CreditGrantApplication.ID)
		s.Equal(1, resp.Pagination.Total)
	})

	s.Run("grants_by_subscription_returns_subscription_scoped_grants", func() {
		resp, err := s.service.GetCreditGrantsBySubscription(s.GetContext(), sub.ID)
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal(grant.ID, resp.Items[0].CreditGrant.ID)
	})
}

func TestCalculateNextCreditGrantPeriodInvalidPeriod(t *testing.T) {
	grant := creditgrant.CreditGrant{
		ID:                "cg_bad_period",
		Period:            lo.ToPtr(types.CreditGrantPeriod("BOGUS")),
		PeriodCount:       lo.ToPtr(1),
		CreditGrantAnchor: lo.ToPtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
	_, _, err := CalculateNextCreditGrantPeriod(grant, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "UTC")
	assert.Error(t, err)
}

func (s *CreditGrantPeriodsSuite) TestCreateCreditGrantRejectsUnpublishedPlan() {
	archivedPlan := &plan.Plan{
		ID:        "plan_cg_archived",
		Name:      "Archived Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	archivedPlan.Status = types.StatusArchived
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), archivedPlan))

	_, err := s.service.CreateCreditGrant(s.GetContext(), dto.CreateCreditGrantRequest{
		Name:           "Grant On Archived Plan",
		Scope:          types.CreditGrantScopePlan,
		PlanID:         &archivedPlan.ID,
		Credits:        decimal.NewFromInt(10),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *CreditGrantPeriodsSuite) TestInvalidExpirationDurationUnitFailsApplication() {
	start := s.testData.now.Add(-1 * time.Hour)
	sub := s.createSubscription("subs_cg_bad_unit", types.SubscriptionStatusActive, start)
	grant := s.createGrant("cg_bad_unit", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), start, func(cg *creditgrant.CreditGrant) {
		cg.ExpirationType = types.CreditGrantExpiryTypeDuration
		cg.ExpirationDuration = lo.ToPtr(30)
		cg.ExpirationDurationUnit = lo.ToPtr(types.CreditGrantExpiryDurationUnit("FORTNIGHTS"))
	})
	cga := s.createCGA("cga_bad_unit_1", grant, sub.ID, types.SubscriptionStatusActive, start, nil, grant.Credits)

	s.Error(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))
	s.True(s.walletBalance().IsZero(), "invalid expiry configuration must not credit the wallet")
}

func (s *CreditGrantPeriodsSuite) TestBillingCycleExpiryOutsideCurrentPeriodSkipsExpiredApplication() {
	// Subscription started 75 days ago; its current period is [now-15d, now+15d].
	// The CGA belongs to the first (long-past) billing period, so its
	// billing-cycle expiry is resolved via historical period calculation and has
	// already elapsed — the application must be skipped, not applied.
	subStart := s.testData.now.AddDate(0, 0, -75)
	sub := s.createSubscription("subs_cg_cycle_exp", types.SubscriptionStatusActive, subStart)
	sub.CurrentPeriodStart = s.testData.now.AddDate(0, 0, -15)
	sub.CurrentPeriodEnd = s.testData.now.AddDate(0, 0, 15)
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	grant := s.createGrant("cg_cycle_exp", sub.ID, types.CreditGrantCadenceOneTime, decimal.NewFromInt(10), subStart, func(cg *creditgrant.CreditGrant) {
		cg.ExpirationType = types.CreditGrantExpiryTypeBillingCycle
	})
	cga := s.createCGA("cga_cycle_exp_1", grant, sub.ID, types.SubscriptionStatusActive, subStart, nil, grant.Credits)

	s.NoError(s.service.ProcessCreditGrantApplication(s.GetContext(), cga.ID))

	stored, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusSkipped, stored.ApplicationStatus)
	s.Nil(stored.AppliedAt)
	s.Contains(lo.FromPtr(stored.FailureReason), "expired")
	s.True(s.walletBalance().IsZero(), "expired billing-cycle grant must not credit the wallet")
}
