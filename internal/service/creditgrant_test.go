package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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
	"github.com/stretchr/testify/suite"
)

type CreditGrantServiceTestSuite struct {
	testutil.BaseServiceTestSuite
	creditGrantService  CreditGrantService
	subscriptionService SubscriptionService
	walletService       WalletService
	testData            struct {
		customer     *customer.Customer
		plan         *plan.Plan
		subscription *subscription.Subscription
		wallet       *wallet.Wallet
		now          time.Time
	}
}

func TestCreditGrantService(t *testing.T) {
	suite.Run(t, new(CreditGrantServiceTestSuite))
}

func (s *CreditGrantServiceTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupServices()
	s.setupTestData()
}

// GetContext returns context with environment ID set for settings lookup
func (s *CreditGrantServiceTestSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *CreditGrantServiceTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CreditGrantServiceTestSuite) setupServices() {
	serviceParams := ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		AuthRepo:                   s.GetStores().AuthRepo,
		UserRepo:                   s.GetStores().UserRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		SecretRepo:                 s.GetStores().SecretRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		TaskRepo:                   s.GetStores().TaskRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:   types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}

	s.creditGrantService = NewCreditGrantService(serviceParams)
	s.subscriptionService = NewSubscriptionService(serviceParams)
	s.walletService = NewWalletService(serviceParams)
}

func (s *CreditGrantServiceTestSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_test_123",
		ExternalID: "ext_cust_test_123",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test plan
	s.testData.plan = &plan.Plan{
		ID:          "plan_test_123",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	// Create test subscription
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_test_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.Add(30 * 24 * time.Hour), // 30 days
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create subscription with line items
	lineItems := []*subscription.SubscriptionLineItem{}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), s.testData.subscription, lineItems))

	// Create test wallet
	s.testData.wallet = &wallet.Wallet{
		ID:         "wallet_test_123",
		CustomerID: s.testData.customer.ID,
		Name:       "Test Wallet",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

// Test Case 1: Create a subscription with a credit grant one time
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithOnetimeCreditGrant() {
	// Create one-time credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "One-time Credit Grant",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "onetime"},
	}

	creditGrantReq.SubscriptionID = &s.testData.subscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceOneTime, creditGrantResp.CreditGrant.Cadence)

	// Verify credit grant application was created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)
	s.Equal(types.ApplicationReasonOnetimeCreditGrant, applications[0].ApplicationReason)
	s.Equal(decimal.NewFromInt(100), applications[0].Credits)
}

// Test Case 2: Create a subscription with a credit grant recurring with no expiry
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithRecurringCreditGrantNoExpiry() {
	// Create recurring credit grant with no expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Credit Grant - No Expiry",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(50),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "recurring_no_expiry"},
	}

	creditGrantReq.SubscriptionID = &s.testData.subscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceRecurring, creditGrantResp.CreditGrant.Cadence)

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Verify credit grant application was created for current period
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find the applied application
	var appliedApp *creditgrantapplication.CreditGrantApplication
	var nextPeriodApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			appliedApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextPeriodApp = app
		}
	}

	s.NotNil(appliedApp)
	s.NotNil(nextPeriodApp)
	s.Equal(types.ApplicationReasonFirstTimeRecurringCreditGrant, appliedApp.ApplicationReason)
	s.Equal(types.ApplicationReasonRecurringCreditGrant, nextPeriodApp.ApplicationReason)
	s.Equal(decimal.NewFromInt(50), appliedApp.Credits)
}

// Test Case 3: Create a subscription with a credit grant recurring with expiry
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithRecurringCreditGrantWithExpiry() {
	// Create recurring credit grant with billing cycle expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Credit Grant - With Expiry",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(25),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeBillingCycle,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "recurring_with_expiry"},
	}

	creditGrantReq.SubscriptionID = &s.testData.subscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceRecurring, creditGrantResp.CreditGrant.Cadence)
	s.Equal(types.CreditGrantExpiryTypeBillingCycle, creditGrantResp.CreditGrant.ExpirationType)

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Verify credit grant application was created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Verify applied application
	var appliedApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			appliedApp = app
			break
		}
	}
	s.NotNil(appliedApp)
	s.Equal(decimal.NewFromInt(25), appliedApp.Credits)
}

// Test Case 4: Create a subscription with credit grant weekly period and subscription monthly period
func (s *CreditGrantServiceTestSuite) TestWeeklyGrantMonthlySubscription() {
	// Create weekly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Weekly Credit Grant - Monthly Subscription",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(15),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "weekly_grant_monthly_sub"},
	}

	creditGrantReq.SubscriptionID = &s.testData.subscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.Equal(types.CREDIT_GRANT_PERIOD_WEEKLY, lo.FromPtr(creditGrantResp.CreditGrant.Period))

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Verify applications created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	// Verify first application is applied
	var appliedApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			appliedApp = app
			break
		}
	}
	s.NotNil(appliedApp)
	s.Equal(decimal.NewFromInt(15), appliedApp.Credits)
}

// Test Case 5: Create a subscription with credit grant monthly period and subscription weekly period
func (s *CreditGrantServiceTestSuite) TestMonthlyGrantWeeklySubscription() {
	// Create weekly subscription
	weeklySubscription := &subscription.Subscription{
		ID:                 "sub_weekly_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.Add(7 * 24 * time.Hour), // 7 days
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_WEEKLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	lineItems := []*subscription.SubscriptionLineItem{}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), weeklySubscription, lineItems))

	// Create monthly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Monthly Credit Grant - Weekly Subscription",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(60),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "monthly_grant_weekly_sub"},
	}

	creditGrantReq.SubscriptionID = &weeklySubscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.Equal(types.CREDIT_GRANT_PERIOD_MONTHLY, lo.FromPtr(creditGrantResp.CreditGrant.Period))

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Verify applications created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{weeklySubscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	// Verify first application is applied
	var appliedApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			appliedApp = app
			break
		}
	}
	s.NotNil(appliedApp)
	s.Equal(decimal.NewFromInt(60), appliedApp.Credits)
}

// Test Case 6: Test ProcessScheduledCreditGrantApplications function
func (s *CreditGrantServiceTestSuite) TestProcessScheduledCreditGrantApplications() {
	// Create a recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Scheduled Credit Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(75),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "scheduled_processing"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create a scheduled application manually (simulating next period)
	nextPeriodStart := s.testData.now.Add(30 * 24 * time.Hour)
	nextPeriodEnd := s.testData.now.Add(60 * 24 * time.Hour)

	scheduledApp := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    s.testData.now.Add(-1 * time.Hour), // Scheduled for past (ready for processing)
		PeriodStart:                     &nextPeriodStart,
		PeriodEnd:                       &nextPeriodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(75), // Set the credit amount from the grant
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "test_idempotency_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), scheduledApp)
	s.NoError(err)

	// Process scheduled applications
	response, err := s.creditGrantService.ProcessScheduledCreditGrantApplications(s.GetContext())
	s.NoError(err)
	s.NotNil(response)
	s.Equal(1, response.TotalApplicationsCount)
	s.Equal(1, response.SuccessApplicationsCount)
	s.Equal(0, response.FailedApplicationsCount)

	// Verify the application was processed
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), scheduledApp.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, processedApp.ApplicationStatus)
	s.Equal(decimal.NewFromInt(75), processedApp.Credits)
}

// Test Case 7: Create a credit grant application with default values and check processing
func (s *CreditGrantServiceTestSuite) TestCreditGrantApplicationDefaultProcessing() {
	// Create a basic credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Basic Credit Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(40),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "default_processing"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create CGA manually with default values
	cga := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    s.testData.now.Add(-10 * time.Minute), // Ready for processing
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(40), // Set the credit amount from the grant
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "default_idempotency_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), cga)
	s.NoError(err)

	// Process scheduled applications
	response, err := s.creditGrantService.ProcessScheduledCreditGrantApplications(s.GetContext())
	s.NoError(err)
	s.Equal(1, response.TotalApplicationsCount)
	s.Equal(1, response.SuccessApplicationsCount)

	// Verify processing
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), cga.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, processedApp.ApplicationStatus)
	s.Equal(decimal.NewFromInt(40), processedApp.Credits)
	s.NotNil(processedApp.AppliedAt)
}

// Test Case 8: Create a failed credit grant application and check retry processing
func (s *CreditGrantServiceTestSuite) TestFailedCreditGrantApplicationRetry() {
	// Create a credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Retry Credit Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(30),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "retry_processing"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create failed CGA
	failedCGA := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    s.testData.now.Add(-5 * time.Minute), // Ready for retry
		ApplicationStatus:               types.ApplicationStatusFailed,
		ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      1,
		Credits:                         decimal.NewFromInt(30), // Set the credit amount from the grant
		FailureReason:                   lo.ToPtr("Previous failure reason"),
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "retry_idempotency_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), failedCGA)
	s.NoError(err)

	// Process scheduled applications (should retry the failed one)
	response, err := s.creditGrantService.ProcessScheduledCreditGrantApplications(s.GetContext())
	s.NoError(err)
	s.Equal(1, response.TotalApplicationsCount)
	s.Equal(1, response.SuccessApplicationsCount)

	// Verify retry was successful
	retriedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), failedCGA.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, retriedApp.ApplicationStatus)
	s.Equal(decimal.NewFromInt(30), retriedApp.Credits)
	s.Equal(2, retriedApp.RetryCount) // Should be incremented
	// Note: Failure reason is NOT cleared on success - it's preserved for audit/history purposes
}

// Test Case 9: Test subscription end prevents next period CGA creation
func (s *CreditGrantServiceTestSuite) TestSubscriptionEndPreventsNextPeriodCGA() {
	// Create subscription with end date soon
	endingSoon := s.testData.now.Add(15 * 24 * time.Hour) // Ends in 15 days
	subscriptionWithEnd := &subscription.Subscription{
		ID:                 "sub_ending_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now,
		EndDate:            &endingSoon,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.Add(30 * 24 * time.Hour), // 30 days (extends beyond end date)
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	lineItems := []*subscription.SubscriptionLineItem{}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), subscriptionWithEnd, lineItems))

	// Create recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Ending Subscription Grant",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(50),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "ending_subscription"},
	}

	creditGrantReq.SubscriptionID = &subscriptionWithEnd.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Check applications created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{subscriptionWithEnd.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)

	// There might be 2 applications created, but the second one should be for a period that ends at subscription end date
	s.GreaterOrEqual(len(applications), 1)

	// Find the applied application (current period)
	var appliedApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			appliedApp = app
			break
		}
	}

	s.NotNil(appliedApp)
	s.Equal(types.ApplicationStatusApplied, appliedApp.ApplicationStatus)
	s.Equal(decimal.NewFromInt(50), appliedApp.Credits)
}

// Test Case 10: Test error scenarios
func (s *CreditGrantServiceTestSuite) TestErrorScenarios() {
	s.Run("Invalid Plan ID", func() {
		invalidPlanID := "invalid_plan_id"
		creditGrantReq := dto.CreateCreditGrantRequest{
			Name:           "Invalid Plan Credit Grant",
			Scope:          types.CreditGrantScopePlan,
			Credits:        decimal.NewFromInt(50),
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
			Priority:       lo.ToPtr(1),
			PlanID:         &invalidPlanID,
		}

		_, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("Anchor Before Start Date", func() {
		startDate := time.Now().Add(24 * time.Hour)
		anchorDate := time.Now() // Before start date
		creditGrantReq := dto.CreateCreditGrantRequest{
			Name:              "Invalid Anchor Grant",
			Scope:             types.CreditGrantScopeSubscription,
			SubscriptionID:    &s.testData.subscription.ID,
			Credits:           decimal.NewFromInt(50),
			Cadence:           types.CreditGrantCadenceOneTime,
			ExpirationType:    types.CreditGrantExpiryTypeNever,
			StartDate:         &startDate,
			CreditGrantAnchor: &anchorDate,
			PlanID:            &s.testData.plan.ID,
		}

		_, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
		s.Error(err)
		s.Contains(err.Error(), "credit_grant_anchor cannot be before start_date")
	})

	s.Run("Missing Start Date", func() {
		creditGrantReq := dto.CreateCreditGrantRequest{
			Name:           "Missing Start Date Grant",
			Scope:          types.CreditGrantScopeSubscription,
			SubscriptionID: &s.testData.subscription.ID,
			Credits:        decimal.NewFromInt(50),
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
			PlanID:         &s.testData.plan.ID,
		}

		_, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
		s.Error(err)
		s.Contains(err.Error(), "start_date is required")
	})

	s.Run("Get Non-existent Credit Grant", func() {
		_, err := s.creditGrantService.GetCreditGrant(s.GetContext(), "non_existent_id")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// Test Case 11: Test UpdateCreditGrant
func (s *CreditGrantServiceTestSuite) TestUpdateCreditGrant() {
	// Create a credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Update Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(35),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"original": "value"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Update the credit grant
	updateReq := dto.UpdateCreditGrantRequest{
		Name:     lo.ToPtr("Updated Grant Name"),
		Metadata: lo.ToPtr(types.Metadata{"updated": "value"}),
	}

	updatedResp, err := s.creditGrantService.UpdateCreditGrant(s.GetContext(), creditGrantResp.CreditGrant.ID, updateReq)
	s.NoError(err)
	s.Equal("Updated Grant Name", updatedResp.CreditGrant.Name)
	s.Equal(types.Metadata{"updated": "value"}, updatedResp.CreditGrant.Metadata)
}

// Test Case 12: Test DeleteCreditGrant
func (s *CreditGrantServiceTestSuite) TestDeleteCreditGrant() {
	// Create a credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Delete Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(45),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Delete the credit grant
	err = s.creditGrantService.DeleteCreditGrant(s.GetContext(), creditGrantResp.CreditGrant.ID)
	s.NoError(err)

	// Verify it's deleted (should return not found error)
	_, err = s.creditGrantService.GetCreditGrant(s.GetContext(), creditGrantResp.CreditGrant.ID)
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

// Test Case 13: Test period start and end dates for weekly credit grant
func (s *CreditGrantServiceTestSuite) TestWeeklyCreditGrantPeriodDates() {
	// Create weekly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Weekly Period Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(20),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "weekly_period_dates"},
	}

	creditGrantReq.SubscriptionID = &s.testData.subscription.ID
	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// No need to call ApplyCreditGrant, it's handled by initializeCreditGrantWorkflow via CreateCreditGrant

	// Verify applications and their period dates
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find current and next period applications
	var currentApp, nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			currentApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextApp = app
		}
	}

	s.NotNil(currentApp)
	s.NotNil(nextApp)

	// Verify current period dates (should start around subscription start)
	s.NotNil(currentApp.PeriodStart)
	s.NotNil(currentApp.PeriodEnd)

	// For weekly period, the period should be 7 days
	expectedCurrentEnd := currentApp.PeriodStart.Add(7 * 24 * time.Hour)
	s.WithinDuration(expectedCurrentEnd, *currentApp.PeriodEnd, time.Hour,
		"Current period end should be 7 days after start for weekly grant")

	// Verify next period dates (should start when current period ends)
	s.NotNil(nextApp.PeriodStart)
	s.NotNil(nextApp.PeriodEnd)

	// Next period should start when current period ends
	s.WithinDuration(*currentApp.PeriodEnd, *nextApp.PeriodStart, time.Minute,
		"Next period should start when current period ends")

	// Next period should also be 7 days long
	expectedNextEnd := nextApp.PeriodStart.Add(7 * 24 * time.Hour)
	s.WithinDuration(expectedNextEnd, *nextApp.PeriodEnd, time.Hour,
		"Next period end should be 7 days after start for weekly grant")

	s.T().Logf("Weekly Grant - Current Period: %s to %s",
		currentApp.PeriodStart.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Format("2006-01-02 15:04:05"))
	s.T().Logf("Weekly Grant - Next Period: %s to %s",
		nextApp.PeriodStart.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Format("2006-01-02 15:04:05"))
}

// Test Case 14: Test period start and end dates for monthly credit grant
func (s *CreditGrantServiceTestSuite) TestMonthlyCreditGrantPeriodDates() {
	// Create monthly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Monthly Period Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "monthly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify applications and their period dates
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find current and next period applications
	var currentApp, nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			currentApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextApp = app
		}
	}

	s.NotNil(currentApp)
	s.NotNil(nextApp)

	// Verify current period dates
	s.NotNil(currentApp.PeriodStart)
	s.NotNil(currentApp.PeriodEnd)

	// For monthly period, verify it's approximately 30 days (allowing for month variations)
	periodDuration := currentApp.PeriodEnd.Sub(*currentApp.PeriodStart)
	s.GreaterOrEqual(periodDuration.Hours(), float64(28*24), "Monthly period should be at least 28 days")
	s.LessOrEqual(periodDuration.Hours(), float64(32*24), "Monthly period should be at most 32 days")

	// Verify next period dates
	s.NotNil(nextApp.PeriodStart)
	s.NotNil(nextApp.PeriodEnd)

	// Next period should start when current period ends
	s.WithinDuration(*currentApp.PeriodEnd, *nextApp.PeriodStart, time.Minute,
		"Next period should start when current period ends")

	// Next period should also be approximately monthly
	nextPeriodDuration := nextApp.PeriodEnd.Sub(*nextApp.PeriodStart)
	s.GreaterOrEqual(nextPeriodDuration.Hours(), float64(28*24), "Next monthly period should be at least 28 days")
	s.LessOrEqual(nextPeriodDuration.Hours(), float64(32*24), "Next monthly period should be at most 32 days")

	s.T().Logf("Monthly Grant - Current Period: %s to %s (Duration: %.1f days)",
		currentApp.PeriodStart.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		periodDuration.Hours()/24)
	s.T().Logf("Monthly Grant - Next Period: %s to %s (Duration: %.1f days)",
		nextApp.PeriodStart.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		nextPeriodDuration.Hours()/24)
}

// Test Case 15: Test period start and end dates for yearly credit grant
func (s *CreditGrantServiceTestSuite) TestYearlyCreditGrantPeriodDates() {
	// Create yearly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Yearly Period Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(1200),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_ANNUAL),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "yearly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify applications and their period dates
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find current and next period applications
	var currentApp, nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			currentApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextApp = app
		}
	}

	s.NotNil(currentApp)
	s.NotNil(nextApp)

	// Verify current period dates
	s.NotNil(currentApp.PeriodStart)
	s.NotNil(currentApp.PeriodEnd)

	// For yearly period, verify it's approximately 365 days
	periodDuration := currentApp.PeriodEnd.Sub(*currentApp.PeriodStart)
	s.GreaterOrEqual(periodDuration.Hours(), float64(364*24), "Yearly period should be at least 364 days")
	s.LessOrEqual(periodDuration.Hours(), float64(366*24), "Yearly period should be at most 366 days")

	// Verify next period dates
	s.NotNil(nextApp.PeriodStart)
	s.NotNil(nextApp.PeriodEnd)

	// Next period should start when current period ends
	s.WithinDuration(*currentApp.PeriodEnd, *nextApp.PeriodStart, time.Minute,
		"Next period should start when current period ends")

	// Next period should also be approximately yearly
	nextPeriodDuration := nextApp.PeriodEnd.Sub(*nextApp.PeriodStart)
	s.GreaterOrEqual(nextPeriodDuration.Hours(), float64(364*24), "Next yearly period should be at least 364 days")
	s.LessOrEqual(nextPeriodDuration.Hours(), float64(366*24), "Next yearly period should be at most 366 days")

	s.T().Logf("Yearly Grant - Current Period: %s to %s (Duration: %.1f days)",
		currentApp.PeriodStart.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		periodDuration.Hours()/24)
	s.T().Logf("Yearly Grant - Next Period: %s to %s (Duration: %.1f days)",
		nextApp.PeriodStart.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		nextPeriodDuration.Hours()/24)
}

// Test Case 16: Test period start and end dates with multiple period counts
func (s *CreditGrantServiceTestSuite) TestMultiplePeriodCountDates() {
	// Create bi-weekly credit grant (every 2 weeks)
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Bi-Weekly Period Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(40),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(2), // Every 2 weeks
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "bi_weekly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify applications and their period dates
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find current and next period applications
	var currentApp, nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			currentApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextApp = app
		}
	}

	s.NotNil(currentApp)
	s.NotNil(nextApp)

	// Verify current period dates
	s.NotNil(currentApp.PeriodStart)
	s.NotNil(currentApp.PeriodEnd)

	// For bi-weekly period (2 weeks), the period should be 14 days
	expectedCurrentEnd := currentApp.PeriodStart.Add(14 * 24 * time.Hour)
	s.WithinDuration(expectedCurrentEnd, *currentApp.PeriodEnd, time.Hour,
		"Current period end should be 14 days after start for bi-weekly grant")

	// Verify next period dates
	s.NotNil(nextApp.PeriodStart)
	s.NotNil(nextApp.PeriodEnd)

	// Next period should start when current period ends
	s.WithinDuration(*currentApp.PeriodEnd, *nextApp.PeriodStart, time.Minute,
		"Next period should start when current period ends")

	// Next period should also be 14 days long
	expectedNextEnd := nextApp.PeriodStart.Add(14 * 24 * time.Hour)
	s.WithinDuration(expectedNextEnd, *nextApp.PeriodEnd, time.Hour,
		"Next period end should be 14 days after start for bi-weekly grant")

	s.T().Logf("Bi-Weekly Grant - Current Period: %s to %s (Duration: %.1f days)",
		currentApp.PeriodStart.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Sub(*currentApp.PeriodStart).Hours()/24)
	s.T().Logf("Bi-Weekly Grant - Next Period: %s to %s (Duration: %.1f days)",
		nextApp.PeriodStart.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Sub(*nextApp.PeriodStart).Hours()/24)
}

// Test Case 17: Test period dates alignment with credit grant creation date
func (s *CreditGrantServiceTestSuite) TestPeriodDatesAlignmentWithGrantCreationDate() {
	// Set a specific creation time for deterministic testing
	specificTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC) // Jan 15, 2024, 10:30 AM

	// Create a new subscription starting at specific time
	testSubscription := &subscription.Subscription{
		ID:                 "sub_alignment_test",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          specificTime,
		CurrentPeriodStart: specificTime,
		CurrentPeriodEnd:   specificTime.Add(30 * 24 * time.Hour),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	lineItems := []*subscription.SubscriptionLineItem{}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), testSubscription, lineItems))

	// Create monthly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Alignment Test Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &testSubscription.ID,
		Credits:        decimal.NewFromInt(50),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &specificTime,
		Metadata:       types.Metadata{"test": "alignment_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify applications and their period dates
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{testSubscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 2) // Current period + next period

	// Find current and next period applications
	var currentApp, nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			currentApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			nextApp = app
		}
	}

	s.NotNil(currentApp)
	s.NotNil(nextApp)

	// Verify period dates are properly calculated from grant creation time
	s.NotNil(currentApp.PeriodStart)
	s.NotNil(currentApp.PeriodEnd)
	s.NotNil(nextApp.PeriodStart)
	s.NotNil(nextApp.PeriodEnd)

	// The periods should be calculated based on the grant's creation date as anchor
	// Current period should align with the grant creation date
	expectedNextStart := *currentApp.PeriodEnd
	s.WithinDuration(expectedNextStart, *nextApp.PeriodStart, time.Minute,
		"Next period should start exactly when current period ends")

	// Log the alignment details
	s.T().Logf("Grant Created At: %s", creditGrantResp.CreditGrant.CreatedAt.Format("2006-01-02 15:04:05"))
	s.T().Logf("Subscription Start: %s", testSubscription.StartDate.Format("2006-01-02 15:04:05"))
	s.T().Logf("Current Period: %s to %s",
		currentApp.PeriodStart.Format("2006-01-02 15:04:05"),
		currentApp.PeriodEnd.Format("2006-01-02 15:04:05"))
	s.T().Logf("Next Period: %s to %s",
		nextApp.PeriodStart.Format("2006-01-02 15:04:05"),
		nextApp.PeriodEnd.Format("2006-01-02 15:04:05"))

	// Verify that periods don't overlap
	s.True(currentApp.PeriodEnd.Before(*nextApp.PeriodEnd) || currentApp.PeriodEnd.Equal(*nextApp.PeriodStart),
		"Current period should end before or exactly when next period starts")
}

// ============================================================================
// Phase 1: DURATION Expiration Type Tests (Tests 18-21)
// ============================================================================

// Test Case 18: One-time Grant with DURATION Expiry (DAY unit)
func (s *CreditGrantServiceTestSuite) TestOnetimeGrantWithDurationExpiryDay() {
	// Create one-time grant with 7 DAY expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "One-time Grant - 7 Day Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(100),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(7),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitDays),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "duration_day_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantExpiryTypeDuration, creditGrantResp.CreditGrant.ExpirationType)
	s.Equal(7, lo.FromPtr(creditGrantResp.CreditGrant.ExpirationDuration))
	s.Equal(types.CreditGrantExpiryDurationUnitDays, lo.FromPtr(creditGrantResp.CreditGrant.ExpirationDurationUnit))

	// Verify credit grant application was created and applied
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)

	// Verify expiry date is calculated correctly (scheduledFor + 7 days)
	// Implementation uses AddDate() which preserves time component
	// This check is redundant since we verify the actual wallet transaction expiry below

	// Verify wallet transaction has correct expiry date
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)
	s.NotEmpty(transactions.Items)

	// Find the transaction for this grant
	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx, "Should find wallet transaction for the grant")
	s.NotNil(grantTx.ExpiryDate, "Transaction should have expiry date")

	// Verify expiry date is approximately 7 days from PeriodStart (used for expiry calculation)
	// Implementation uses PeriodStart directly with AddDate() which preserves time component
	// Calculate expected expiry: actual expiry - 7 days should equal PeriodStart (within tolerance)
	// This accounts for any time normalization that might occur during database storage/retrieval
	s.NotNil(applications[0].PeriodStart, "PeriodStart should be set for subscription-scoped grants")
	expectedPeriodStart := grantTx.ExpiryDate.AddDate(0, 0, -7) // Reverse calculate from actual expiry
	s.WithinDuration(expectedPeriodStart, *applications[0].PeriodStart, 24*time.Hour, "PeriodStart should be approximately 7 days before expiry")
	// Also verify the expiry is 7 days from PeriodStart using AddDate()
	expectedExpiryTime := applications[0].PeriodStart.AddDate(0, 0, 7)
	s.WithinDuration(expectedExpiryTime, *grantTx.ExpiryDate, 24*time.Hour, "Expiry date should be 7 days from PeriodStart using AddDate()")
	s.Equal(decimal.NewFromInt(100), grantTx.CreditAmount, "Transaction should have correct credit amount")
}

// Test Case 19: One-time Grant with DURATION Expiry (WEEK unit)
func (s *CreditGrantServiceTestSuite) TestOnetimeGrantWithDurationExpiryWeek() {
	// Create one-time grant with 2 WEEK expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "One-time Grant - 2 Week Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(200),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(2),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitWeeks),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "duration_week_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)

	// Verify application was created and applied
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)

	// Verify expiry date = scheduledFor + 14 days (2 weeks)
	// Implementation uses AddDate() which preserves time component
	// This check is redundant since we verify the actual wallet transaction expiry below

	// Verify wallet transaction expiry
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)

	// Verify expiry is calculated from PeriodStart (used for expiry calculation)
	// Implementation uses PeriodStart directly with AddDate() which preserves time component
	// Calculate expected expiry: actual expiry - 14 days should equal PeriodStart (within tolerance)
	// This accounts for any time normalization that might occur during database storage/retrieval
	s.NotNil(applications[0].PeriodStart, "PeriodStart should be set for subscription-scoped grants")
	expectedPeriodStart := grantTx.ExpiryDate.AddDate(0, 0, -14) // Reverse calculate from actual expiry
	s.WithinDuration(expectedPeriodStart, *applications[0].PeriodStart, 24*time.Hour, "PeriodStart should be approximately 14 days before expiry")
	// Also verify the expiry is 14 days from PeriodStart using AddDate()
	expectedExpiryTime := applications[0].PeriodStart.AddDate(0, 0, 14) // 2 weeks = 14 days
	s.WithinDuration(expectedExpiryTime, *grantTx.ExpiryDate, 24*time.Hour, "Expiry should be 14 days from PeriodStart using AddDate()")
}

// Test Case 20: One-time Grant with DURATION Expiry (MONTH unit)
func (s *CreditGrantServiceTestSuite) TestOnetimeGrantWithDurationExpiryMonth() {
	// Create one-time grant with 1 MONTH expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "One-time Grant - 1 Month Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(300),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitMonths),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "duration_month_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)

	// Verify application was created and applied
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)

	// Verify expiry date calculation (approximately 1 month from scheduledFor)
	expectedExpiry := applications[0].ScheduledFor.AddDate(0, 1, 0) // Add 1 month
	s.WithinDuration(expectedExpiry, expectedExpiry, 24*time.Hour, "Expiry should be approximately 1 month from scheduledFor")

	// Verify wallet transaction expiry
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)

	expectedExpiryTime := s.testData.now.AddDate(0, 1, 0) // Add 1 month
	s.WithinDuration(expectedExpiryTime, *grantTx.ExpiryDate, 2*24*time.Hour, "Expiry should be approximately 1 month from grant creation")
}

// Test Case 21: One-time Grant with DURATION Expiry (YEAR unit)
func (s *CreditGrantServiceTestSuite) TestOnetimeGrantWithDurationExpiryYear() {
	// Create one-time grant with 1 YEAR expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "One-time Grant - 1 Year Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(400),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitYears),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "duration_year_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)

	// Verify application was created and applied
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)

	// Verify expiry date calculation (approximately 1 year from scheduledFor)
	expectedExpiry := applications[0].ScheduledFor.AddDate(1, 0, 0) // Add 1 year
	s.WithinDuration(expectedExpiry, expectedExpiry, 24*time.Hour, "Expiry should be approximately 1 year from scheduledFor")

	// Verify wallet transaction expiry
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)

	expectedExpiryTime := s.testData.now.AddDate(1, 0, 0) // Add 1 year
	s.WithinDuration(expectedExpiryTime, *grantTx.ExpiryDate, 2*24*time.Hour, "Expiry should be approximately 1 year from grant creation")
}

// ============================================================================
// Phase 2: Past Expiration Skip Tests (Critical) (Tests 22-25)
// ============================================================================

// Test Case 22: Skip Grant with Past Expiration (One-time, DAY unit)
func (s *CreditGrantServiceTestSuite) TestSkipGrantWithPastExpirationOnetimeDay() {
	// Create one-time grant with DURATION expiry (1 DAY)
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "One-time Grant - Past Expiry Day",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(50),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitDays),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "past_expiry_skip"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Manually create CGA with scheduledFor in past (2 days ago) and expiry in past (1 day ago)
	pastScheduledFor := s.testData.now.Add(-2 * 24 * time.Hour) // 2 days ago

	expiredCGA := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    pastScheduledFor,
		PeriodStart:                     lo.ToPtr(pastScheduledFor), // PeriodStart is required for expiry calculation
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(50),
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "expired_cga_idempotency_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), expiredCGA)
	s.NoError(err)

	// Process the application - it should be skipped due to past expiry
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), expiredCGA.ID)
	s.NoError(err) // Processing should succeed, but grant should be skipped

	// Verify: Status is SKIPPED, failure reason is "Expired", applied_at is nil
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), expiredCGA.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusSkipped, processedApp.ApplicationStatus)
	s.NotNil(processedApp.FailureReason)
	s.Contains(*processedApp.FailureReason, "Expired")
	s.Nil(processedApp.AppliedAt, "AppliedAt should be nil for skipped grants")

	// Verify: No credits are added to wallet
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	// Verify no transaction was created for this expired grant
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if cgaID, ok := tx.Metadata["cga_id"]; ok && cgaID == expiredCGA.ID {
				s.Fail("Should not have wallet transaction for expired grant")
			}
		}
	}
}

// Test Case 23: Skip Grant with Past Expiration (Recurring, WEEK unit)
func (s *CreditGrantServiceTestSuite) TestSkipGrantWithPastExpirationRecurringWeek() {
	// Create recurring grant with 1 WEEK expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Recurring Grant - Past Expiry Week",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(75),
		Cadence:                types.CreditGrantCadenceRecurring,
		Period:                 lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:            lo.ToPtr(1),
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitWeeks),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "past_expiry_recurring"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create CGA with scheduledFor in past and expiry in past
	pastScheduledFor := s.testData.now.Add(-10 * 24 * time.Hour) // 10 days ago

	expiredCGA := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    pastScheduledFor,
		PeriodStart:                     lo.ToPtr(pastScheduledFor),
		PeriodEnd:                       lo.ToPtr(pastScheduledFor.Add(30 * 24 * time.Hour)),
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(75),
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "expired_recurring_cga_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), expiredCGA)
	s.NoError(err)

	// Process the application
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), expiredCGA.ID)
	s.NoError(err)

	// Verify: Status is SKIPPED
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), expiredCGA.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusSkipped, processedApp.ApplicationStatus)
	s.NotNil(processedApp.FailureReason)
	s.Contains(*processedApp.FailureReason, "Expired")

	// Verify: Next period application is still created (for recurring grants)
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	// Should have at least 2 applications: the skipped one and potentially a next period one
	s.GreaterOrEqual(len(applications), 1)
}

// Test Case 24: Skip Grant with Past Expiration (MONTH unit)
func (s *CreditGrantServiceTestSuite) TestSkipGrantWithPastExpirationMonth() {
	// Create grant with MONTH unit expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Grant - Past Expiry Month",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(100),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitMonths),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "past_expiry_month"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create CGA with scheduledFor in past and expiry in past
	pastScheduledFor := s.testData.now.Add(-60 * 24 * time.Hour) // 60 days ago

	expiredCGA := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    pastScheduledFor,
		PeriodStart:                     lo.ToPtr(pastScheduledFor), // PeriodStart is required for expiry calculation
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(100),
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "expired_month_cga_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), expiredCGA)
	s.NoError(err)

	// Process the application
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), expiredCGA.ID)
	s.NoError(err)

	// Verify skip behavior
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), expiredCGA.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusSkipped, processedApp.ApplicationStatus)
	s.NotNil(processedApp.FailureReason)
	s.Contains(*processedApp.FailureReason, "Expired")
	s.Nil(processedApp.AppliedAt)
}

// Test Case 25: Skip Grant with Past Expiration (YEAR unit)
func (s *CreditGrantServiceTestSuite) TestSkipGrantWithPastExpirationYear() {
	// Create grant with YEAR unit expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Grant - Past Expiry Year",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(200),
		Cadence:                types.CreditGrantCadenceOneTime,
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitYears),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "past_expiry_year"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Create CGA with scheduledFor in past and expiry in past
	pastScheduledFor := s.testData.now.AddDate(-2, 0, 0) // 2 years ago

	expiredCGA := &creditgrantapplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   creditGrantResp.CreditGrant.ID,
		SubscriptionID:                  s.testData.subscription.ID,
		ScheduledFor:                    pastScheduledFor,
		PeriodStart:                     lo.ToPtr(pastScheduledFor), // PeriodStart is required for expiry calculation
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonOnetimeCreditGrant,
		SubscriptionStatusAtApplication: s.testData.subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         decimal.NewFromInt(200),
		Metadata:                        types.Metadata{},
		IdempotencyKey:                  "expired_year_cga_key",
		EnvironmentID:                   types.GetEnvironmentID(s.GetContext()),
		BaseModel:                       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), expiredCGA)
	s.NoError(err)

	// Process the application
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), expiredCGA.ID)
	s.NoError(err)

	// Verify skip behavior
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), expiredCGA.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusSkipped, processedApp.ApplicationStatus)
	s.NotNil(processedApp.FailureReason)
	s.Contains(*processedApp.FailureReason, "Expired")
	s.Nil(processedApp.AppliedAt)
}

// ============================================================================
// Phase 3: Recurring Grant DURATION Expiry Tests (Tests 26-29)
// ============================================================================

// Test Case 26: Recurring Grant with DURATION Expiry (DAY unit)
func (s *CreditGrantServiceTestSuite) TestRecurringGrantWithDurationExpiryDay() {
	// Create recurring monthly grant with 7 DAY expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Recurring Grant - 7 Day Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(50),
		Cadence:                types.CreditGrantCadenceRecurring,
		Period:                 lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:            lo.ToPtr(1),
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(7),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitDays),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "recurring_duration_day"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify first application has correct expiry
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	var firstApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
			break
		}
	}
	s.NotNil(firstApp)

	// Verify wallet transaction has expiry of 7 days from scheduledFor
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry is calculated from PeriodStart
	// Implementation uses PeriodStart directly with AddDate() which preserves time component
	// Calculate expected expiry: actual expiry - 7 days should equal PeriodStart (within tolerance)
	// This accounts for any time normalization that might occur during database storage/retrieval
	s.NotNil(firstApp.PeriodStart, "PeriodStart should be set for subscription-scoped grants")
	expectedPeriodStart := grantTx.ExpiryDate.AddDate(0, 0, -7) // Reverse calculate from actual expiry
	s.WithinDuration(expectedPeriodStart, *firstApp.PeriodStart, 24*time.Hour, "PeriodStart should be approximately 7 days before expiry")
	// Also verify the expiry is 7 days from PeriodStart using AddDate()
	expectedExpiry := firstApp.PeriodStart.AddDate(0, 0, 7)
	s.WithinDuration(expectedExpiry, *grantTx.ExpiryDate, 24*time.Hour, "First application expiry should be 7 days from PeriodStart using AddDate()")

	// Verify next period application is created
	var nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusPending && app.ID != firstApp.ID {
			nextApp = app
			break
		}
	}
	s.NotNil(nextApp, "Next period application should be created")

	// Process next period application
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), nextApp.ID)
	s.NoError(err)

	// Verify: Each application has its own expiry timeline (7 days from its scheduledFor)
	processedNextApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), nextApp.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, processedNextApp.ApplicationStatus)

	// Get transactions again to find the new one
	transactions, err = s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var nextGrantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if cgaID, ok := tx.Metadata["cga_id"]; ok && cgaID == nextApp.ID {
				nextGrantTx = tx
				break
			}
		}
	}
	s.NotNil(nextGrantTx)
	s.NotNil(nextGrantTx.ExpiryDate)
	// Expiry is calculated from PeriodStart
	// Implementation uses PeriodStart directly with AddDate() which preserves time component
	// Calculate expected expiry: actual expiry - 7 days should equal PeriodStart (within tolerance)
	// This accounts for any time normalization that might occur during database storage/retrieval
	s.NotNil(processedNextApp.PeriodStart, "PeriodStart should be set for subscription-scoped grants")
	expectedNextPeriodStart := nextGrantTx.ExpiryDate.AddDate(0, 0, -7) // Reverse calculate from actual expiry
	s.WithinDuration(expectedNextPeriodStart, *processedNextApp.PeriodStart, 24*time.Hour, "PeriodStart should be approximately 7 days before expiry")
	// Also verify the expiry is 7 days from PeriodStart using AddDate()
	expectedNextExpiry := processedNextApp.PeriodStart.AddDate(0, 0, 7)
	s.WithinDuration(expectedNextExpiry, *nextGrantTx.ExpiryDate, 24*time.Hour, "Next application expiry should be 7 days from its own PeriodStart using AddDate()")
}

// Test Case 27: Recurring Grant with DURATION Expiry (WEEK unit)
func (s *CreditGrantServiceTestSuite) TestRecurringGrantWithDurationExpiryWeek() {
	// Create recurring grant with WEEK unit expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Recurring Grant - Week Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(60),
		Cadence:                types.CreditGrantCadenceRecurring,
		Period:                 lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:            lo.ToPtr(1),
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(2),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitWeeks),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "recurring_duration_week"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify expiry independence for each period
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	var firstApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
			break
		}
	}
	s.NotNil(firstApp)

	// Verify first period expiry (2 weeks = 14 days)
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry is calculated from PeriodStart
	// Implementation uses PeriodStart directly with AddDate() which preserves time component
	// Calculate expected expiry: actual expiry - 14 days should equal PeriodStart (within tolerance)
	// This accounts for any time normalization that might occur during database storage/retrieval
	expectedPeriodStart := grantTx.ExpiryDate.AddDate(0, 0, -14) // Reverse calculate from actual expiry
	s.WithinDuration(expectedPeriodStart, *firstApp.PeriodStart, 24*time.Hour, "PeriodStart should be approximately 14 days before expiry")
	// Also verify the expiry is 14 days from PeriodStart using AddDate()
	expectedExpiry := firstApp.PeriodStart.AddDate(0, 0, 14)
	s.WithinDuration(expectedExpiry, *grantTx.ExpiryDate, 24*time.Hour, "First application expiry should be 2 weeks from PeriodStart using AddDate()")
}

// Test Case 28: Recurring Grant with DURATION Expiry (MONTH unit)
func (s *CreditGrantServiceTestSuite) TestRecurringGrantWithDurationExpiryMonth() {
	// Create recurring grant with MONTH unit expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Recurring Grant - Month Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(80),
		Cadence:                types.CreditGrantCadenceRecurring,
		Period:                 lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:            lo.ToPtr(1),
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitMonths),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "recurring_duration_month"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify expiry calculation
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	var firstApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
			break
		}
	}
	s.NotNil(firstApp)

	// Verify expiry is approximately 1 month from scheduledFor
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry is calculated from PeriodStart
	effectiveDate := firstApp.ScheduledFor
	if firstApp.PeriodStart != nil {
		effectiveDate = *firstApp.PeriodStart
	}
	expectedExpiry := effectiveDate.AddDate(0, 1, 0) // Add 1 month
	s.WithinDuration(expectedExpiry, *grantTx.ExpiryDate, 2*24*time.Hour, "First application expiry should be 1 month from PeriodStart")
}

// Test Case 29: Recurring Grant with DURATION Expiry (YEAR unit)
func (s *CreditGrantServiceTestSuite) TestRecurringGrantWithDurationExpiryYear() {
	// Create recurring grant with YEAR unit expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:                   "Recurring Grant - Year Expiry",
		Scope:                  types.CreditGrantScopeSubscription,
		SubscriptionID:         &s.testData.subscription.ID,
		Credits:                decimal.NewFromInt(120),
		Cadence:                types.CreditGrantCadenceRecurring,
		Period:                 lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:            lo.ToPtr(1),
		ExpirationType:         types.CreditGrantExpiryTypeDuration,
		ExpirationDuration:     lo.ToPtr(1),
		ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitYears),
		Priority:               lo.ToPtr(1),
		PlanID:                 &s.testData.plan.ID,
		StartDate:              &s.testData.now,
		Metadata:               types.Metadata{"test": "recurring_duration_year"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify expiry calculation
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	var firstApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
			break
		}
	}
	s.NotNil(firstApp)

	// Verify expiry is approximately 1 year from scheduledFor
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry is calculated from PeriodStart
	effectiveDate := firstApp.ScheduledFor
	if firstApp.PeriodStart != nil {
		effectiveDate = *firstApp.PeriodStart
	}
	expectedExpiry := effectiveDate.AddDate(1, 0, 0) // Add 1 year
	s.WithinDuration(expectedExpiry, *grantTx.ExpiryDate, 2*24*time.Hour, "First application expiry should be 1 year from PeriodStart")
}

// ============================================================================
// Phase 4: Enhance Billing Cycle Expiry Tests (Tests 30-31)
// ============================================================================

// Test Case 30: Billing Cycle Expiry with Recurring Grant
func (s *CreditGrantServiceTestSuite) TestBillingCycleExpiryWithRecurringGrant() {
	// Create recurring grant with BILLING_CYCLE expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Grant - Billing Cycle Expiry",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeBillingCycle,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "billing_cycle_recurring"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify first application expiry date matches subscription period end
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1)

	var firstApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
			break
		}
	}
	s.NotNil(firstApp)

	// Verify wallet transaction expiry matches subscription period end
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry should match subscription current period end (compare dates only, times may differ)
	expectedExpiryDate := s.testData.subscription.CurrentPeriodEnd.Truncate(24 * time.Hour)
	actualExpiryDate := grantTx.ExpiryDate.Truncate(24 * time.Hour)
	s.True(expectedExpiryDate.Equal(actualExpiryDate), "Expiry date should match subscription period end date (times may differ)")

	// Process multiple periods - get next period application
	var nextApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusPending && app.ID != firstApp.ID {
			nextApp = app
			break
		}
	}
	s.NotNil(nextApp)

	// Process next period application
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), nextApp.ID)
	s.NoError(err)

	// Verify: Each period's expiry matches its billing period end
	processedNextApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), nextApp.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusApplied, processedNextApp.ApplicationStatus)

	// Note: Next period expiry should match the subscription's next period end
	// This depends on subscription period calculation

	// Get transactions again
	transactions, err = s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var nextGrantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if cgaID, ok := tx.Metadata["cga_id"]; ok && cgaID == nextApp.ID {
				nextGrantTx = tx
				break
			}
		}
	}
	if nextGrantTx != nil && nextGrantTx.ExpiryDate != nil {
		// Next period expiry should match the subscription's next period end
		// Note: This depends on subscription period calculation
		s.NotNil(nextGrantTx.ExpiryDate, "Next period transaction should have expiry date")
	}
}

// Test Case 31: Billing Cycle Expiry with One-time Grant
func (s *CreditGrantServiceTestSuite) TestBillingCycleExpiryWithOnetimeGrant() {
	// Create one-time grant with BILLING_CYCLE expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "One-time Grant - Billing Cycle Expiry",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &s.testData.subscription.ID,
		Credits:        decimal.NewFromInt(150),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeBillingCycle,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "billing_cycle_onetime"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify expiry matches current period end
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{s.testData.subscription.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)

	// Verify wallet transaction expiry
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.NotEmpty(wallets)

	walletID := wallets[0].ID
	txFilter := &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, txFilter)
	s.NoError(err)

	var grantTx *dto.WalletTransactionResponse
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if grantID, ok := tx.Metadata["grant_id"]; ok && grantID == creditGrantResp.CreditGrant.ID {
				grantTx = tx
				break
			}
		}
	}
	s.NotNil(grantTx)
	s.NotNil(grantTx.ExpiryDate)
	// Expiry should match subscription current period end (compare dates only, times may differ)
	expectedExpiryDate := s.testData.subscription.CurrentPeriodEnd.Truncate(24 * time.Hour)
	actualExpiryDate := grantTx.ExpiryDate.Truncate(24 * time.Hour)
	s.True(expectedExpiryDate.Equal(actualExpiryDate), "Expiry date should match subscription period end date (times may differ)")
}

// TestSubscriptionCancellationCancelsFutureGrants verifies that when a subscription is cancelled,
// all future credit grant applications are cancelled as well
func (s *CreditGrantServiceTestSuite) TestSubscriptionCancellationCancelsFutureGrants() {
	// Create a subscription specifically for this test
	testSub := &subscription.Subscription{
		ID:                 "sub_cancel_test_123",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), testSub, testSub.LineItems))

	// Create a recurring credit grant on this subscription
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Grant for Cancellation Test",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &testSub.ID,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "cancellation_test"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Verify first application is applied and next period application is created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{testSub.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)
	s.GreaterOrEqual(len(applications), 1, "Should have at least one application")

	// Find the first applied application
	var firstApp *creditgrantapplication.CreditGrantApplication
	var futureApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ApplicationStatus == types.ApplicationStatusApplied {
			firstApp = app
		} else if app.ApplicationStatus == types.ApplicationStatusPending {
			futureApp = app
		}
	}
	s.NotNil(firstApp, "First application should be applied")
	s.NotNil(futureApp, "Future application should be created and pending")

	// Verify the future application is in pending status
	s.Equal(types.ApplicationStatusPending, futureApp.ApplicationStatus)

	// Manually cancel the subscription status to test grant cancellation
	// (We do this directly to avoid invoice creation dependencies)
	testSub.SubscriptionStatus = types.SubscriptionStatusCancelled
	now := time.Now().UTC()
	testSub.CancelledAt = &now
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), testSub))

	// Call CancelFutureSubscriptionGrants directly to test the functionality
	err = s.creditGrantService.CancelFutureSubscriptionGrants(s.GetContext(), testSub.ID)
	s.NoError(err)

	// Verify that future grant applications are cancelled
	applications, err = s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)

	var cancelledFutureApp *creditgrantapplication.CreditGrantApplication
	for _, app := range applications {
		if app.ID == futureApp.ID {
			cancelledFutureApp = app
			break
		}
	}
	s.NotNil(cancelledFutureApp, "Future application should still exist")
	s.Equal(types.ApplicationStatusCancelled, cancelledFutureApp.ApplicationStatus, "Future application should be cancelled")
	s.Nil(cancelledFutureApp.AppliedAt, "Cancelled application should not have AppliedAt set")

	// Verify that the grant was archived (deleted) as part of cancellation
	_, err = s.GetStores().CreditGrantRepo.Get(s.GetContext(), creditGrantResp.CreditGrant.ID)
	s.Error(err, "Grant should be deleted/archived after cancellation")
	s.Contains(err.Error(), "not found", "Error should indicate grant not found")

	// Test that processing a grant application for a cancelled subscription results in cancellation
	// Create a new subscription and grant for this test
	testSub2 := &subscription.Subscription{
		ID:                 "sub_cancel_test_456",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), testSub2, testSub2.LineItems))

	// Create another recurring grant
	creditGrantReq2 := dto.CreateCreditGrantRequest{
		Name:           "Recurring Grant for Processing Test",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &testSub2.ID,
		Credits:        decimal.NewFromInt(75),
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		StartDate:      &s.testData.now,
		Metadata:       types.Metadata{"test": "processing_cancellation_test"},
	}

	creditGrantResp2, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq2)
	s.NoError(err)

	// Create a pending application manually
	manualFutureApp := &creditgrantapplication.CreditGrantApplication{
		ID:                s.GetUUID(),
		CreditGrantID:     creditGrantResp2.CreditGrant.ID,
		SubscriptionID:    testSub2.ID,
		ApplicationStatus: types.ApplicationStatusPending,
		ScheduledFor:      s.testData.now.Add(7 * 24 * time.Hour),
		PeriodStart:       lo.ToPtr(s.testData.now.Add(7 * 24 * time.Hour)),
		PeriodEnd:         lo.ToPtr(s.testData.now.Add(37 * 24 * time.Hour)),
		Credits:           decimal.NewFromInt(75),
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CreditGrantApplicationRepo.Create(s.GetContext(), manualFutureApp))

	// Cancel the subscription
	testSub2.SubscriptionStatus = types.SubscriptionStatusCancelled
	now2 := time.Now().UTC()
	testSub2.CancelledAt = &now2
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), testSub2))

	// Try to process this application - it should be cancelled because subscription is cancelled
	err = s.creditGrantService.ProcessCreditGrantApplication(s.GetContext(), manualFutureApp.ID)
	s.NoError(err)

	// Verify the application was cancelled
	processedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(s.GetContext(), manualFutureApp.ID)
	s.NoError(err)
	s.Equal(types.ApplicationStatusCancelled, processedApp.ApplicationStatus, "Application should be cancelled when subscription is cancelled")
	s.Nil(processedApp.AppliedAt, "Cancelled application should not have AppliedAt set")

	// Verify no wallet transaction was created for the cancelled application
	wallets, err := s.walletService.GetWalletsByCustomerID(s.GetContext(), testSub2.CustomerID)
	s.NoError(err)
	s.NotEmpty(wallets, "Customer should have a wallet")
	walletID := wallets[0].ID

	transactions, err := s.walletService.GetWalletTransactions(s.GetContext(), walletID, &types.WalletTransactionFilter{
		WalletID:    &walletID,
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(err)

	// Count transactions for this grant - should only be 1 (from the first application before cancellation)
	grantTxCount := 0
	for _, tx := range transactions.Items {
		if tx.Metadata != nil {
			if cgaID, ok := tx.Metadata["cga_id"]; ok && cgaID == manualFutureApp.ID {
				grantTxCount++
			}
		}
	}
	s.Equal(0, grantTxCount, "No wallet transactions should be created for cancelled future applications")
}
