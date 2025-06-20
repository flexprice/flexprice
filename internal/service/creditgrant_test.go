package service

import (
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
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
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
		Currency:           "usd",
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
		Currency:   "usd",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

// Test Case 1: Create a subscription with a credit grant one time
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithOnetimeCreditGrant() {
	// Create one-time credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "One-time Credit Grant",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(100),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "onetime"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceOneTime, creditGrantResp.CreditGrant.Cadence)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
	s.NoError(err)

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
	s.Equal(decimal.NewFromInt(100), applications[0].CreditsApplied)
}

// Test Case 2: Create a subscription with a credit grant recurring with no expiry
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithRecurringCreditGrantNoExpiry() {
	// Create recurring credit grant with no expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Credit Grant - No Expiry",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(50),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "recurring_no_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceRecurring, creditGrantResp.CreditGrant.Cadence)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
	s.NoError(err)

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
	s.Equal(decimal.NewFromInt(50), appliedApp.CreditsApplied)
}

// Test Case 3: Create a subscription with a credit grant recurring with expiry
func (s *CreditGrantServiceTestSuite) TestCreateSubscriptionWithRecurringCreditGrantWithExpiry() {
	// Create recurring credit grant with billing cycle expiry
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Recurring Credit Grant - With Expiry",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(25),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeBillingCycle,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "recurring_with_expiry"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.NotNil(creditGrantResp)
	s.Equal(types.CreditGrantCadenceRecurring, creditGrantResp.CreditGrant.Cadence)
	s.Equal(types.CreditGrantExpiryTypeBillingCycle, creditGrantResp.CreditGrant.ExpirationType)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
	s.NoError(err)

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
	s.Equal(decimal.NewFromInt(25), appliedApp.CreditsApplied)
}

// Test Case 4: Create a subscription with credit grant weekly period and subscription monthly period
func (s *CreditGrantServiceTestSuite) TestWeeklyGrantMonthlySubscription() {
	// Create weekly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Weekly Credit Grant - Monthly Subscription",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(15),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "weekly_grant_monthly_sub"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.Equal(types.CREDIT_GRANT_PERIOD_WEEKLY, lo.FromPtr(creditGrantResp.CreditGrant.Period))

	// Apply the credit grant to subscription (subscription is monthly)
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
	s.NoError(err)

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
	s.Equal(decimal.NewFromInt(15), appliedApp.CreditsApplied)
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(60),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "monthly_grant_weekly_sub"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)
	s.Equal(types.CREDIT_GRANT_PERIOD_MONTHLY, lo.FromPtr(creditGrantResp.CreditGrant.Period))

	// Apply the credit grant to weekly subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, weeklySubscription, types.Metadata{})
	s.NoError(err)

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
	s.Equal(decimal.NewFromInt(60), appliedApp.CreditsApplied)
}

// Test Case 6: Test ProcessScheduledCreditGrantApplications function
func (s *CreditGrantServiceTestSuite) TestProcessScheduledCreditGrantApplications() {
	// Create a recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Scheduled Credit Grant",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(75),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
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
		CreditsApplied:                  decimal.Zero,
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
	s.Equal(decimal.NewFromInt(75), processedApp.CreditsApplied)
}

// Test Case 7: Create a credit grant application with default values and check processing
func (s *CreditGrantServiceTestSuite) TestCreditGrantApplicationDefaultProcessing() {
	// Create a basic credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Basic Credit Grant",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(40),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
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
		CreditsApplied:                  decimal.Zero,
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
	s.Equal(decimal.NewFromInt(40), processedApp.CreditsApplied)
	s.NotNil(processedApp.AppliedAt)
}

// Test Case 8: Create a failed credit grant application and check retry processing
func (s *CreditGrantServiceTestSuite) TestFailedCreditGrantApplicationRetry() {
	// Create a credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Retry Credit Grant",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(30),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
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
		CreditsApplied:                  decimal.Zero,
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
	s.Equal(decimal.NewFromInt(30), retriedApp.CreditsApplied)
	s.Equal(2, retriedApp.RetryCount) // Should be incremented
	s.Nil(retriedApp.FailureReason)   // Should be cleared on success
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(50),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "ending_subscription"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply credit grant
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, subscriptionWithEnd, types.Metadata{})
	s.NoError(err)

	// Check applications created
	filter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{creditGrantResp.CreditGrant.ID},
		SubscriptionIDs: []string{subscriptionWithEnd.ID},
		QueryFilter:     types.NewDefaultQueryFilter(),
	}

	applications, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), filter)
	s.NoError(err)

	// Should only have 1 application (current period) since next period would be after subscription end
	s.Len(applications, 1)
	s.Equal(types.ApplicationStatusApplied, applications[0].ApplicationStatus)
	s.Equal(decimal.NewFromInt(50), applications[0].CreditsApplied)
}

// Test Case 10: Test error scenarios
func (s *CreditGrantServiceTestSuite) TestErrorScenarios() {
	s.Run("Invalid Plan ID", func() {
		invalidPlanID := "invalid_plan_id"
		creditGrantReq := dto.CreateCreditGrantRequest{
			Name:           "Invalid Plan Credit Grant",
			Scope:          types.CreditGrantScopePlan,
			Credits:        decimal.NewFromInt(50),
			Currency:       "usd",
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
			Priority:       lo.ToPtr(1),
			PlanID:         &invalidPlanID,
		}

		_, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(35),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(45),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
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
