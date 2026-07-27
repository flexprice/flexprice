package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type SubscriptionGroupedInvoicingTestSuite struct {
	testutil.BaseServiceTestSuite
	subscriptionService *subscriptionService
}

func TestSubscriptionGroupedInvoicingTestSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionGroupedInvoicingTestSuite))
}

func (s *SubscriptionGroupedInvoicingTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupServices()
}

func (s *SubscriptionGroupedInvoicingTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionGroupedInvoicingTestSuite) setupServices() {
	serviceParams := ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		UserRepo:                     s.GetStores().UserRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		TaskRepo:                     s.GetStores().TaskRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
	s.subscriptionService = NewSubscriptionService(serviceParams).(*subscriptionService)
}

// createTestCustomer creates and persists a test customer.
func (s *SubscriptionGroupedInvoicingTestSuite) createTestCustomer() *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:        s.GetUUID(),
		Name:      "Test Customer",
		Email:     "test@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

// baseAnchor is a fixed anchor used across tests for predictable comparisons.
var baseAnchor = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// makeParentSub creates and persists a minimal parent subscription.
func (s *SubscriptionGroupedInvoicingTestSuite) makeParentSub(customerID string, anchor time.Time, start time.Time) *subscription.Subscription {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                 s.GetUUID(),
		CustomerID:         customerID,
		PlanID:             s.GetUUID(),
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeParent,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   start.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

// makeChildSub creates and persists a minimal standalone subscription suitable for grouping.
func (s *SubscriptionGroupedInvoicingTestSuite) makeChildSub(customerID string, anchor time.Time, start time.Time, overrides ...func(*subscription.Subscription)) *subscription.Subscription {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                 s.GetUUID(),
		CustomerID:         customerID,
		PlanID:             s.GetUUID(),
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   start.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	for _, fn := range overrides {
		fn(sub)
	}
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

// -----------------------------------------------------------------------
// addToGroupedInvoicing
// -----------------------------------------------------------------------

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_Success() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor)

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.NoError(s.T(), err)

	// Verify persisted state
	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionTypeGroupedInvoicing, updated.SubscriptionType)
	require.NotNil(s.T(), updated.ParentSubscriptionID)
	require.Equal(s.T(), parent.ID, *updated.ParentSubscriptionID)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsNonStandaloneChild() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	})

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsBillingPeriodMismatch() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.BillingPeriod = types.BILLING_PERIOD_ANNUAL
	})

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsBillingPeriodCountMismatch() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.BillingPeriodCount = 3
	})

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsChildStartBeforeParent() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parentStart := baseAnchor.AddDate(0, 1, 0) // parent starts one month later
	parent := s.makeParentSub(cust.ID, baseAnchor, parentStart)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor) // child starts earlier

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsAlreadyParentedChild() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	otherParentID := s.GetUUID()
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.ParentSubscriptionID = &otherParentID
	})

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsBillingAnchorMismatch() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	differentAnchor := baseAnchor.Add(24 * time.Hour)
	child := s.makeChildSub(cust.ID, differentAnchor, baseAnchor)

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsInactiveChild() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	})

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsInactiveParent() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	parent.SubscriptionStatus = types.SubscriptionStatusCancelled
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, parent))

	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor)

	err := s.subscriptionService.addToGroupedInvoicing(ctx, parent, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

// -----------------------------------------------------------------------
// removeFromGroupedInvoicing
// -----------------------------------------------------------------------

func (s *SubscriptionGroupedInvoicingTestSuite) TestRemoveFromGroupedInvoicing_Success() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
	})

	err := s.subscriptionService.removeFromGroupedInvoicing(ctx, child.ID)
	require.NoError(s.T(), err)

	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionTypeStandalone, updated.SubscriptionType)
	require.Nil(s.T(), updated.ParentSubscriptionID)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestRemoveFromGroupedInvoicing_RejectsNonGroupedType() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor) // standalone

	err := s.subscriptionService.removeFromGroupedInvoicing(ctx, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "expected validation error, got: %v", err)
}

// -----------------------------------------------------------------------
// getGroupedInvoicingSubscriptions
// -----------------------------------------------------------------------

func (s *SubscriptionGroupedInvoicingTestSuite) TestGetGroupedInvoicingSubscriptions_ReturnsCorrectChildren() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)

	// Two grouped_invoicing children
	child1 := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
	})
	child2 := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
	})

	// One unrelated standalone sub — should NOT appear
	_ = s.makeChildSub(cust.ID, baseAnchor, baseAnchor)

	// One cancelled grouped_invoicing sub — should NOT appear
	_ = s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
		sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	})

	subs, err := s.subscriptionService.getGroupedInvoicingSubscriptions(ctx, parent.ID)
	require.NoError(s.T(), err)
	require.Len(s.T(), subs, 2)

	ids := make(map[string]bool)
	for _, sub := range subs {
		ids[sub.ID] = true
	}
	require.True(s.T(), ids[child1.ID])
	require.True(s.T(), ids[child2.ID])
}

// -----------------------------------------------------------------------
// validateAddToGroupedInvoicingDryRun / validateRemoveFromGroupedInvoicingDryRun
// -----------------------------------------------------------------------

func (s *SubscriptionGroupedInvoicingTestSuite) TestValidateAddToGroupedInvoicingDryRun_Success() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor)

	err := s.subscriptionService.validateAddToGroupedInvoicingDryRun(ctx, parent, child.ID)
	require.NoError(s.T(), err)

	// Ensure no mutation happened
	unchanged, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionTypeStandalone, unchanged.SubscriptionType)
	require.Nil(s.T(), unchanged.ParentSubscriptionID)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestValidateRemoveFromGroupedInvoicingDryRun_Success() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
	})

	err := s.subscriptionService.validateRemoveFromGroupedInvoicingDryRun(ctx, child.ID)
	require.NoError(s.T(), err)

	// No mutation
	unchanged, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionTypeGroupedInvoicing, unchanged.SubscriptionType)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestValidateRemoveFromGroupedInvoicingDryRun_RejectsNonGrouped() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor) // standalone

	err := s.subscriptionService.validateRemoveFromGroupedInvoicingDryRun(ctx, child.ID)
	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err))
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestExecuteGroupedInvoicingMembership_PublishesSubscriptionUpdated() {
	ctx := s.GetContext()
	cust := s.createTestCustomer()

	parent := s.makeParentSub(cust.ID, baseAnchor, baseAnchor)
	child := s.makeChildSub(cust.ID, baseAnchor, baseAnchor)

	modSvc := NewSubscriptionModificationService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		WebhookPublisher:         s.GetWebhookPublisher(),
	})

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	_, err := modSvc.Execute(ctx, parent.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeGroupedInvoicing,
		GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
			Action:               dto.GroupedInvoicingActionAdd,
			ParentSubscriptionID: parent.ID,
			ChildSubscriptionIDs: []string{child.ID},
		},
	})
	require.NoError(s.T(), err)

	updatedCount := 0
	for _, e := range s.GetPublishedWebhooks() {
		if e.EventName == types.WebhookEventSubscriptionUpdated {
			updatedCount++
		}
	}
	// child updated; parent may also be promoted from standalone → parent
	require.GreaterOrEqual(s.T(), updatedCount, 1)
}
