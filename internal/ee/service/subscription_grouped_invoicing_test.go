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
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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
		CommitmentRepo:               s.GetStores().CommitmentRepo,
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
// groupedInvoicingChildren
// -----------------------------------------------------------------------

func (s *SubscriptionGroupedInvoicingTestSuite) TestGroupedInvoicingChildren_ReturnsCorrectChildren() {
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

	subs, err := getGroupedInvoicingChildren(ctx, s.subscriptionService.ServiceParams, parent, false)
	require.NoError(s.T(), err)
	require.Len(s.T(), subs, 2)

	ids := make(map[string]bool)
	for _, sub := range subs {
		ids[sub.ID] = true
	}
	require.True(s.T(), ids[child1.ID])
	require.True(s.T(), ids[child2.ID])

	// An active parent must not pull in draft children — that widening applies only to a draft
	// (checkout) parent, and period processing only ever sees active parents.
	_ = s.makeChildSub(cust.ID, baseAnchor, baseAnchor, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
		sub.SubscriptionStatus = types.SubscriptionStatusDraft
	})
	subs, err = getGroupedInvoicingChildren(ctx, s.subscriptionService.ServiceParams, parent, false)
	require.NoError(s.T(), err)
	require.Len(s.T(), subs, 2, "draft child must be excluded under an active parent")
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

// -----------------------------------------------------------------------
// cancelling the parent
// -----------------------------------------------------------------------

// makeGroupedChild creates an active grouped_invoicing child of parent with one open line item.
func (s *SubscriptionGroupedInvoicingTestSuite) makeGroupedChild(parent *subscription.Subscription) (*subscription.Subscription, *subscription.SubscriptionLineItem) {
	ctx := s.GetContext()
	child := s.makeChildSub(s.createTestCustomer().ID, parent.BillingAnchor, parent.StartDate, func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = &parent.ID
	})
	lineItem := &subscription.SubscriptionLineItem{
		ID:             s.GetUUID(),
		SubscriptionID: child.ID,
		CustomerID:     child.CustomerID,
		PriceID:        s.GetUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(1),
		Currency:       child.Currency,
		BillingPeriod:  child.BillingPeriod,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      child.StartDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().SubscriptionLineItemRepo.Create(ctx, lineItem))
	return child, lineItem
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestCancelParentImmediately_CancelsGroupedChildren() {
	ctx := s.GetContext()
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -10)
	parent := s.makeParentSub(s.createTestCustomer().ID, start, start)
	child, lineItem := s.makeGroupedChild(parent)
	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()

	_, err := s.subscriptionService.CancelSubscription(ctx, parent.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		Reason:            types.CancellationReasonPaymentOverdue,
		ProrationBehavior: types.ProrationBehaviorNone,
	})
	require.NoError(s.T(), err)

	cancelledParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parent.ID)
	require.NoError(s.T(), err)
	cancelledChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionStatusCancelled, cancelledChild.SubscriptionStatus)
	require.Equal(s.T(), types.CancellationReasonPaymentOverdue, cancelledChild.Metadata["cancellation_reason"])
	require.NotNil(s.T(), cancelledChild.EndDate)
	require.True(s.T(), cancelledChild.EndDate.Equal(*cancelledParent.EndDate))

	endedLine, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, lineItem.ID)
	require.NoError(s.T(), err)
	require.True(s.T(), endedLine.EndDate.Equal(*cancelledParent.EndDate), "child line item ends with the parent")

	childCancelledWebhooks := lo.CountBy(s.GetPublishedWebhooks(), func(e *types.WebhookEvent) bool {
		return e.EventName == types.WebhookEventSubscriptionCancelled && e.EntityID == child.ID
	})
	require.Equal(s.T(), 1, childCancelledWebhooks)
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestCancelParentAtPeriodEnd_CancelsGroupedChildrenWhenItFires() {
	ctx := s.GetContext()
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -10)
	parent := s.makeParentSub(s.createTestCustomer().ID, start, start)
	child, _ := s.makeGroupedChild(parent)

	_, err := s.subscriptionService.CancelSubscription(ctx, parent.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeEndOfPeriod,
		ProrationBehavior: types.ProrationBehaviorNone,
	})
	require.NoError(s.T(), err)

	scheduledChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionStatusActive, scheduledChild.SubscriptionStatus, "child stays active until the parent's cancellation fires")

	// The parent's period-end run marks it cancelled at cancel_at, without setting an end date.
	scheduledParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parent.ID)
	require.NoError(s.T(), err)
	scheduledParent.SubscriptionStatus = types.SubscriptionStatusCancelled
	cancelled, err := s.subscriptionService.CascadeCancelToGroupedInvoicingSubscriptions(ctx, scheduledParent)
	require.NoError(s.T(), err)
	require.Len(s.T(), cancelled, 1)

	cancelledChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), types.SubscriptionStatusCancelled, cancelledChild.SubscriptionStatus)
	require.True(s.T(), cancelledChild.EndDate.Equal(*scheduledParent.CancelAt))

	// The parent's final invoice is computed after the cancellation and must still carry the child.
	billed, err := getGroupedInvoicingChildren(ctx, s.subscriptionService.ServiceParams, scheduledParent, false)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{child.ID}, lo.Map(billed, func(sub *subscription.Subscription, _ int) string { return sub.ID }))
}

func (s *SubscriptionGroupedInvoicingTestSuite) TestCancelledParentInvoice_LeavesOutChildrenCancelledEarlier() {
	ctx := s.GetContext()
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -10)
	parent := s.makeParentSub(s.createTestCustomer().ID, start, start)
	earlier, _ := s.makeGroupedChild(parent)
	earlierEnd := start.AddDate(0, 0, 2)
	earlier.SubscriptionStatus = types.SubscriptionStatusCancelled
	earlier.EndDate = &earlierEnd
	require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, earlier))

	parentEnd := start.AddDate(0, 0, 5)
	parent.SubscriptionStatus = types.SubscriptionStatusCancelled
	parent.EndDate = &parentEnd

	billed, err := getGroupedInvoicingChildren(ctx, s.subscriptionService.ServiceParams, parent, false)
	require.NoError(s.T(), err)
	require.Empty(s.T(), billed)
}
