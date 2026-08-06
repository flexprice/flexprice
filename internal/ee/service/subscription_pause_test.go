package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

// SubscriptionPauseTestSuite tests pause functionality
type SubscriptionPauseTestSuite struct {
	testutil.BaseServiceTestSuite
	service       SubscriptionService
	pauseTestData struct {
		customer             *customer.Customer
		plan                 *plan.Plan
		activeSubscription   *subscription.Subscription
		pausedSubscription   *subscription.Subscription
		subscriptionWithPlan *subscription.Subscription
		pauseRecord          *subscription.SubscriptionPause
	}
}

func TestSubscriptionPauseService(t *testing.T) {
	suite.Run(t, new(SubscriptionPauseTestSuite))
}

func (s *SubscriptionPauseTestSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *SubscriptionPauseTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	// Initialize the service with our own BaseServiceTestSuite
	s.service = NewSubscriptionService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})

	s.setupPauseTestData()
}

func (s *SubscriptionPauseTestSuite) TearDownTest() {
	// Call the parent TearDownTest to clean up
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionPauseTestSuite) setupPauseTestData() {
	// Ensure we have a valid context
	ctx := s.GetContext()
	now := time.Now().UTC()

	// Create test customer
	s.pauseTestData.customer = &customer.Customer{
		ID:         "cust_pause_123",
		ExternalID: "ext_cust_pause_123",
		Name:       "Test Pause Customer",
		Email:      "test_pause@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.pauseTestData.customer))

	// Create test plan
	s.pauseTestData.plan = &plan.Plan{
		ID:          "plan_pause_123",
		Name:        "Test Pause Plan",
		Description: "Test Pause Plan Description",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.pauseTestData.plan))

	// Create an active subscription for pause tests
	s.pauseTestData.activeSubscription = &subscription.Subscription{
		ID:                 "sub_active_for_pause",
		PlanID:             s.pauseTestData.plan.ID,
		CustomerID:         s.pauseTestData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-10 * 24 * time.Hour),
		CurrentPeriodEnd:   now.Add(20 * 24 * time.Hour),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		PauseStatus:        types.PauseStatusNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.pauseTestData.activeSubscription, s.pauseTestData.activeSubscription.LineItems))

	// Create a paused subscription for resume tests
	pauseID := "pause_123"
	s.pauseTestData.pausedSubscription = &subscription.Subscription{
		ID:                 "sub_paused",
		PlanID:             s.pauseTestData.plan.ID,
		CustomerID:         s.pauseTestData.customer.ID,
		StartDate:          now.Add(-60 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-15 * 24 * time.Hour),
		CurrentPeriodEnd:   now.Add(15 * 24 * time.Hour),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusPaused,
		PauseStatus:        types.PauseStatusActive,
		ActivePauseID:      lo.ToPtr(pauseID),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.pauseTestData.pausedSubscription, s.pauseTestData.pausedSubscription.LineItems))

	// Create a pause record for the paused subscription
	s.pauseTestData.pauseRecord = &subscription.SubscriptionPause{
		ID:                  pauseID,
		SubscriptionID:      s.pauseTestData.pausedSubscription.ID,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		ResumeMode:          types.ResumeModeAuto,
		PauseStart:          now.Add(-5 * 24 * time.Hour),
		OriginalPeriodStart: now.Add(-15 * 24 * time.Hour),
		OriginalPeriodEnd:   now.Add(15 * 24 * time.Hour),
		Reason:              "Test pause",
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, s.pauseTestData.pauseRecord))
}

func (s *SubscriptionPauseTestSuite) TestPauseSubscription() {
	// Ensure we have a valid context
	ctx := s.GetContext()
	now := time.Now().UTC()

	testCases := []struct {
		name           string
		subscriptionID string
		pauseMode      types.PauseMode
		pauseEnd       *time.Time
		pauseDays      *int
		reason         string
		metadata       types.Metadata
		wantErr        bool
		errorContains  string
	}{
		{
			name:           "successful_immediate_pause",
			subscriptionID: "sub_active_for_pause", // Use string directly to avoid reusing the same subscription
			pauseMode:      types.PauseModeImmediate,
			pauseDays:      lo.ToPtr(30),
			reason:         "Customer requested",
			metadata:       types.Metadata{"requested_by": "customer"},
			wantErr:        false,
		},
		{
			name:           "invalid_subscription_id",
			subscriptionID: "non_existent_sub",
			pauseMode:      types.PauseModeImmediate,
			pauseDays:      lo.ToPtr(30),
			wantErr:        true,
			errorContains:  "item not found",
		},
		{
			name:           "already_paused_subscription",
			subscriptionID: "sub_paused",
			pauseMode:      types.PauseModeImmediate,
			pauseDays:      lo.ToPtr(30),
			wantErr:        true,
			errorContains:  "invalid subscription status",
		},
		{
			name:           "both_pause_end_and_days_specified",
			subscriptionID: "sub_active_for_pause_2", // Create a new subscription for this test
			pauseMode:      types.PauseModeImmediate,
			pauseEnd:       lo.ToPtr(now.Add(60 * 24 * time.Hour)),
			pauseDays:      lo.ToPtr(30),
			wantErr:        true,
			errorContains:  "invalid pause parameters",
		},
	}

	// Create a second active subscription for the last test case
	secondActiveSub := &subscription.Subscription{
		ID:                 "sub_active_for_pause_2",
		PlanID:             s.pauseTestData.plan.ID,
		CustomerID:         s.pauseTestData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-10 * 24 * time.Hour),
		CurrentPeriodEnd:   now.Add(20 * 24 * time.Hour),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		PauseStatus:        types.PauseStatusNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, secondActiveSub, secondActiveSub.LineItems))

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.PauseSubscription(
				ctx,
				tc.subscriptionID,
				&dto.PauseSubscriptionRequest{
					PauseMode: tc.pauseMode,
					PauseEnd:  tc.pauseEnd,
					PauseDays: tc.pauseDays,
					Reason:    tc.reason,
					Metadata:  tc.metadata,
				},
			)

			if tc.wantErr {
				s.Error(err)
				if tc.errorContains != "" {
					s.Contains(err.Error(), tc.errorContains)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp.Subscription)
			s.NotNil(resp.Pause)

			// Verify subscription state
			s.Equal(types.SubscriptionStatusPaused, resp.Subscription.SubscriptionStatus)
			s.Equal(types.PauseStatusActive, resp.Subscription.PauseStatus)
			s.NotNil(resp.Subscription.ActivePauseID)

			// Verify pause record
			s.Equal(resp.Subscription.ID, resp.Pause.SubscriptionID)
			s.Equal(types.PauseStatusActive, resp.Pause.PauseStatus)
			s.Equal(tc.pauseMode, resp.Pause.PauseMode)
			s.Equal(tc.reason, resp.Pause.Reason)

			// Verify pause end date
			if tc.pauseEnd != nil {
				s.NotNil(resp.Pause.PauseEnd)
				s.Equal(tc.pauseEnd.Unix(), resp.Pause.PauseEnd.Unix())
			}

			// Verify metadata
			if tc.metadata != nil {
				for k, v := range tc.metadata {
					s.Equal(v, resp.Pause.Metadata[k])
				}
			}
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestResumeSubscription() {
	// Ensure we have a valid context
	ctx := s.GetContext()

	testCases := []struct {
		name           string
		subscriptionID string
		resumeMode     types.ResumeMode
		metadata       types.Metadata
		wantErr        bool
		errorContains  string
	}{
		{
			name:           "successful_immediate_resume",
			subscriptionID: s.pauseTestData.pausedSubscription.ID,
			resumeMode:     types.ResumeModeImmediate,
			metadata:       types.Metadata{"requested_by": "customer"},
			wantErr:        false,
		},
		{
			name:           "invalid_subscription_id",
			subscriptionID: "non_existent_sub",
			resumeMode:     types.ResumeModeImmediate,
			wantErr:        true,
			errorContains:  "item not found",
		},
		{
			name:           "not_paused_subscription",
			subscriptionID: s.pauseTestData.activeSubscription.ID,
			resumeMode:     types.ResumeModeImmediate,
			wantErr:        true,
			errorContains:  "invalid subscription status",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.ResumeSubscription(
				ctx,
				tc.subscriptionID,
				&dto.ResumeSubscriptionRequest{
					ResumeMode: tc.resumeMode,
					Metadata:   tc.metadata,
				},
			)

			if tc.wantErr {
				s.Error(err)
				if tc.errorContains != "" {
					s.Contains(err.Error(), tc.errorContains)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp.Subscription)
			s.NotNil(resp.Pause)

			// Verify subscription state
			s.Equal(types.SubscriptionStatusActive, resp.Subscription.SubscriptionStatus)
			s.Equal(types.PauseStatusNone, resp.Subscription.PauseStatus)
			s.Nil(resp.Subscription.ActivePauseID)

			// Verify pause record
			s.Equal(resp.Subscription.ID, resp.Pause.SubscriptionID)
			s.Equal(types.PauseStatusCompleted, resp.Pause.PauseStatus)
			s.NotNil(resp.Pause.ResumedAt)

			// Verify metadata
			if tc.metadata != nil {
				for k, v := range tc.metadata {
					s.Equal(v, resp.Pause.Metadata[k])
				}
			}
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestGetPause() {
	// Ensure we have a valid context
	ctx := s.GetContext()

	testCases := []struct {
		name          string
		pauseID       string
		wantErr       bool
		errorContains string
	}{
		{
			name:    "existing_pause",
			pauseID: s.pauseTestData.pauseRecord.ID,
			wantErr: false,
		},
		{
			name:          "non_existent_pause",
			pauseID:       "non_existent_pause",
			wantErr:       true,
			errorContains: "not found",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			pause, err := s.service.GetPause(ctx, tc.pauseID)

			if tc.wantErr {
				s.Error(err)
				if tc.errorContains != "" {
					s.Contains(err.Error(), tc.errorContains)
				}
				return
			}

			s.NoError(err)
			s.NotNil(pause)
			s.Equal(tc.pauseID, pause.ID)
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestListPauses() {
	// Ensure we have a valid context
	ctx := s.GetContext()

	testCases := []struct {
		name           string
		subscriptionID string
		expectedCount  int
		wantErr        bool
	}{
		{
			name:           "subscription_with_pauses",
			subscriptionID: s.pauseTestData.pausedSubscription.ID,
			expectedCount:  1,
			wantErr:        false,
		},
		{
			name:           "subscription_without_pauses",
			subscriptionID: s.pauseTestData.activeSubscription.ID,
			expectedCount:  0,
			wantErr:        false,
		},
		{
			name:           "non_existent_subscription",
			subscriptionID: "non_existent_sub",
			expectedCount:  0,
			wantErr:        false, // ListPauses should return empty list, not error
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.ListPauses(ctx, tc.subscriptionID)

			if tc.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.Len(resp.Items, tc.expectedCount)

			if tc.expectedCount > 0 {
				s.Equal(tc.subscriptionID, resp.Items[0].SubscriptionPause.SubscriptionID)
			}
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestCalculatePauseImpact() {
	// Ensure we have a valid context
	ctx := s.GetContext()
	now := time.Now().UTC()

	// Create a fresh active subscription for this test
	activeSub := &subscription.Subscription{
		ID:                 "sub_active_for_impact",
		PlanID:             s.pauseTestData.plan.ID,
		CustomerID:         s.pauseTestData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-10 * 24 * time.Hour),
		CurrentPeriodEnd:   now.Add(20 * 24 * time.Hour),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		PauseStatus:        types.PauseStatusNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, activeSub, activeSub.LineItems))

	testCases := []struct {
		name           string
		subscriptionID string
		pauseMode      types.PauseMode
		pauseEnd       *time.Time
		pauseDays      *int
		wantErr        bool
		errorContains  string
	}{
		{
			name:           "calculate_impact_with_days",
			subscriptionID: "sub_active_for_impact",
			pauseMode:      types.PauseModeImmediate,
			pauseDays:      lo.ToPtr(30),
			wantErr:        false,
		},
		{
			name:           "calculate_impact_with_end_date",
			subscriptionID: "sub_active_for_impact",
			pauseMode:      types.PauseModeImmediate,
			pauseEnd:       lo.ToPtr(now.Add(60 * 24 * time.Hour)),
			wantErr:        false,
		},
		{
			name:           "invalid_subscription_id",
			subscriptionID: "non_existent_sub",
			pauseMode:      types.PauseModeImmediate,
			pauseDays:      lo.ToPtr(30),
			wantErr:        true,
			errorContains:  "item not found",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			impact, err := s.service.CalculatePauseImpact(
				ctx,
				tc.subscriptionID,
				&dto.PauseSubscriptionRequest{
					PauseMode: tc.pauseMode,
					PauseEnd:  tc.pauseEnd,
					PauseDays: tc.pauseDays,
				},
			)

			if tc.wantErr {
				s.Error(err)
				if tc.errorContains != "" {
					s.Contains(err.Error(), tc.errorContains)
				}
				return
			}

			s.NoError(err)
			s.NotNil(impact)

			// Verify impact details
			s.NotNil(impact.OriginalPeriodStart)
			s.NotNil(impact.OriginalPeriodEnd)
			s.NotNil(impact.AdjustedPeriodStart)
			s.NotNil(impact.AdjustedPeriodEnd)

			// If pauseDays is specified, verify PauseDurationDays
			if tc.pauseDays != nil {
				s.Equal(*tc.pauseDays, impact.PauseDurationDays)
			}

			// If pauseEnd is specified, verify NextBillingDate
			if tc.pauseEnd != nil {
				s.NotNil(impact.NextBillingDate)
				s.Equal(tc.pauseEnd.Unix(), impact.NextBillingDate.Unix())
			}
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestCalculateResumeImpact() {
	// Ensure we have a valid context
	ctx := s.GetContext()

	testCases := []struct {
		name           string
		subscriptionID string
		resumeMode     types.ResumeMode
		wantErr        bool
		errorContains  string
	}{
		{
			name:           "calculate_resume_impact",
			subscriptionID: s.pauseTestData.pausedSubscription.ID,
			resumeMode:     types.ResumeModeImmediate,
			wantErr:        false,
		},
		{
			name:           "invalid_subscription_id",
			subscriptionID: "non_existent_sub",
			resumeMode:     types.ResumeModeImmediate,
			wantErr:        true,
			errorContains:  "item not found",
		},
		{
			name:           "not_paused_subscription",
			subscriptionID: s.pauseTestData.activeSubscription.ID,
			resumeMode:     types.ResumeModeImmediate,
			wantErr:        true,
			errorContains:  "invalid subscription status",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			impact, err := s.service.CalculateResumeImpact(
				ctx,
				tc.subscriptionID,
				&dto.ResumeSubscriptionRequest{
					ResumeMode: tc.resumeMode,
				},
			)

			if tc.wantErr {
				s.Error(err)
				if tc.errorContains != "" {
					s.Contains(err.Error(), tc.errorContains)
				}
				return
			}

			s.NoError(err)
			s.NotNil(impact)

			// Verify impact details
			s.NotNil(impact.OriginalPeriodStart)
			s.NotNil(impact.OriginalPeriodEnd)
			s.NotNil(impact.AdjustedPeriodStart)
			s.NotNil(impact.AdjustedPeriodEnd)
			s.NotNil(impact.NextBillingDate)
			s.Greater(impact.PauseDurationDays, 0)
		})
	}
}

func (s *SubscriptionPauseTestSuite) TestUpdateBillingPeriodsWithPausedSubscriptions() {
	// Ensure we have a valid context
	ctx := s.GetContext()
	now := time.Now().UTC()

	// Create a paused subscription that should be skipped during billing period updates
	pausedSub := &subscription.Subscription{
		ID:                 "sub_paused_for_billing_test",
		PlanID:             s.pauseTestData.plan.ID,
		CustomerID:         s.pauseTestData.customer.ID,
		StartDate:          now.Add(-90 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-60 * 24 * time.Hour),
		CurrentPeriodEnd:   now.Add(-30 * 24 * time.Hour), // Period ended in the past
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusPaused,
		PauseStatus:        types.PauseStatusActive,
		ActivePauseID:      lo.ToPtr("pause_billing_test"),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, pausedSub, pausedSub.LineItems))

	// Create a pause record for this subscription
	pauseRecord := &subscription.SubscriptionPause{
		ID:                  "pause_billing_test",
		SubscriptionID:      pausedSub.ID,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		ResumeMode:          types.ResumeModeAuto,
		PauseStart:          now.Add(-35 * 24 * time.Hour),
		OriginalPeriodStart: now.Add(-60 * 24 * time.Hour),
		OriginalPeriodEnd:   now.Add(-30 * 24 * time.Hour),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, pauseRecord))

	// Run the billing period update
	response, err := s.service.UpdateBillingPeriods(ctx)
	s.NoError(err)
	s.NotNil(response)

	// Verify the paused subscription was not updated
	updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, pausedSub.ID)
	s.NoError(err)
	s.Equal(pausedSub.CurrentPeriodStart, updatedSub.CurrentPeriodStart)
	s.Equal(pausedSub.CurrentPeriodEnd, updatedSub.CurrentPeriodEnd)
}
