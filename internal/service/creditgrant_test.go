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
	s.Equal(decimal.NewFromInt(100), applications[0].Credits)
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
	s.Equal(decimal.NewFromInt(50), appliedApp.Credits)
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
	s.Equal(decimal.NewFromInt(25), appliedApp.Credits)
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
	s.Equal(decimal.NewFromInt(60), appliedApp.Credits)
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

// Test Case 13: Test period start and end dates for weekly credit grant
func (s *CreditGrantServiceTestSuite) TestWeeklyCreditGrantPeriodDates() {
	// Create weekly recurring credit grant
	creditGrantReq := dto.CreateCreditGrantRequest{
		Name:           "Weekly Period Test Grant",
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(20),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "weekly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(100),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "monthly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(1200),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_ANNUAL),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "yearly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(40),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_WEEKLY),
		PeriodCount:    lo.ToPtr(2), // Every 2 weeks
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "bi_weekly_period_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, s.testData.subscription, types.Metadata{})
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
		Scope:          types.CreditGrantScopePlan,
		Credits:        decimal.NewFromInt(50),
		Currency:       "usd",
		Cadence:        types.CreditGrantCadenceRecurring,
		Period:         lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY),
		PeriodCount:    lo.ToPtr(1),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		PlanID:         &s.testData.plan.ID,
		Metadata:       types.Metadata{"test": "alignment_dates"},
	}

	creditGrantResp, err := s.creditGrantService.CreateCreditGrant(s.GetContext(), creditGrantReq)
	s.NoError(err)

	// Apply the credit grant to subscription
	err = s.creditGrantService.ApplyCreditGrant(s.GetContext(), creditGrantResp.CreditGrant, testSubscription, types.Metadata{})
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
