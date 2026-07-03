package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	creditgrantdomain "github.com/flexprice/flexprice/internal/domain/creditgrant"
	creditgrantapplication "github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionLifecycleSuite tests read/update/lifecycle operations in subscription.go:
// GetSubscriptionV2, UpdateSubscription, ListSubscriptions, activation, schedules,
// cascades, billing periods and upcoming credit grant applications.
type SubscriptionLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	svc          SubscriptionService
	planService  PlanService
	priceService PriceService
	testData     struct {
		customer *customer.Customer
		plan     *plan.Plan
		sub      *subscription.Subscription
		now      time.Time
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	suite.Run(t, new(SubscriptionLifecycleSuite))
}

func (s *SubscriptionLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	s.svc = NewSubscriptionService(params)
	s.planService = NewPlanService(params)
	s.priceService = NewPriceService(params)
	s.setupTestData()
}

func (s *SubscriptionLifecycleSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionLifecycleSuite) internalService() *subscriptionService {
	return s.svc.(*subscriptionService)
}

func (s *SubscriptionLifecycleSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_lifecycle",
		Name:       "Lifecycle Customer",
		Email:      "lifecycle@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	planResp, err := s.planService.CreatePlan(ctx, dto.CreatePlanRequest{
		Name:        "Lifecycle Plan",
		Description: "Lifecycle test plan",
	})
	s.Require().NoError(err)
	s.testData.plan = planResp.Plan

	amt := decimal.NewFromInt(15)
	_, err = s.priceService.CreatePrice(ctx, dto.CreatePriceRequest{
		Amount:             &amt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
	})
	s.Require().NoError(err)

	subResp, err := s.svc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)
	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, subResp.Subscription.ID)
	s.Require().NoError(err)
	s.testData.sub = sub
}

// createBareSubscription writes a subscription straight through the repo (no service flow).
func (s *SubscriptionLifecycleSuite) createBareSubscription(mutate func(*subscription.Subscription)) *subscription.Subscription {
	ctx := s.GetContext()
	now := s.testData.now
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
		BillingAnchor:      now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	if mutate != nil {
		mutate(sub)
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionLifecycleSuite) TestGetSubscriptionV2() {
	ctx := s.GetContext()
	subID := s.testData.sub.ID

	s.Run("invalid_expand_returns_error", func() {
		_, err := s.svc.GetSubscriptionV2(ctx, subID, types.NewExpand("bogus_field"))
		s.Error(err)
	})

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.svc.GetSubscriptionV2(ctx, "sub_missing", types.NewExpand(""))
		s.Error(err)
	})

	s.Run("base_response_without_expand", func() {
		resp, err := s.svc.GetSubscriptionV2(ctx, subID, types.NewExpand(""))
		s.Require().NoError(err)
		s.Equal(subID, resp.Subscription.ID)
		s.Nil(resp.Plan)
		s.Nil(resp.Customer)
		s.Empty(resp.LineItems)
	})

	s.Run("expands_plan_and_customer", func() {
		resp, err := s.svc.GetSubscriptionV2(ctx, subID, types.NewExpand("plan,customer"))
		s.Require().NoError(err)
		s.Require().NotNil(resp.Plan)
		s.Equal(s.testData.plan.ID, resp.Plan.Plan.ID)
		s.Require().NotNil(resp.Customer)
		s.Equal(s.testData.customer.ID, resp.Customer.Customer.ID)
	})

	s.Run("expands_line_items_with_prices", func() {
		resp, err := s.svc.GetSubscriptionV2(ctx, subID, types.NewExpand("subscription_line_items,prices"))
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.LineItems)
		for _, li := range resp.LineItems {
			s.Require().NotNil(li.Price, "line item price should be expanded")
			s.Equal(li.PriceID, li.Price.ID)
		}
	})

	s.Run("expands_line_items_without_prices", func() {
		resp, err := s.svc.GetSubscriptionV2(ctx, subID, types.NewExpand("subscription_line_items"))
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.LineItems)
		for _, li := range resp.LineItems {
			s.Nil(li.Price)
		}
	})
}

func (s *SubscriptionLifecycleSuite) TestUpdateSubscription() {
	ctx := s.GetContext()

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.svc.UpdateSubscription(ctx, "sub_missing", dto.UpdateSubscriptionRequest{})
		s.Error(err)
	})

	s.Run("sets_cancel_at_and_end_date", func() {
		sub := s.createBareSubscription(nil)
		cancelAt := s.testData.now.Add(10 * 24 * time.Hour)

		resp, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			CancelAt:          &cancelAt,
			CancelAtPeriodEnd: true,
		})
		s.Require().NoError(err)
		s.Equal(sub.ID, resp.Subscription.ID)

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Require().NotNil(stored.CancelAt)
		s.True(stored.CancelAt.Equal(cancelAt))
		s.Require().NotNil(stored.EndDate)
		s.True(stored.EndDate.Equal(cancelAt))
		s.True(stored.CancelAtPeriodEnd)
	})

	s.Run("updates_subscription_status", func() {
		sub := s.createBareSubscription(nil)

		_, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			Status: types.SubscriptionStatusPaused,
		})
		s.Require().NoError(err)

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusPaused, stored.SubscriptionStatus)
	})

	s.Run("rejects_parent_on_non_standalone_subscription", func() {
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionType = types.SubscriptionTypeParent
		})
		parent := s.createBareSubscription(nil)

		_, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			ParentSubscriptionID: &parent.ID,
		})
		s.Error(err)
	})

	s.Run("rejects_self_as_parent", func() {
		sub := s.createBareSubscription(nil)
		_, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			ParentSubscriptionID: &sub.ID,
		})
		s.Error(err)
	})

	s.Run("rejects_inactive_parent", func() {
		sub := s.createBareSubscription(nil)
		parent := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		})

		_, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			ParentSubscriptionID: &parent.ID,
		})
		s.Error(err)
	})

	s.Run("sets_and_clears_parent_subscription", func() {
		sub := s.createBareSubscription(nil)
		parent := s.createBareSubscription(nil)

		_, err := s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			ParentSubscriptionID: &parent.ID,
		})
		s.Require().NoError(err)

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Equal(parent.ID, lo.FromPtr(stored.ParentSubscriptionID))

		empty := ""
		_, err = s.svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{
			ParentSubscriptionID: &empty,
		})
		s.Require().NoError(err)

		stored, err = s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Nil(stored.ParentSubscriptionID)
	})
}

func (s *SubscriptionLifecycleSuite) TestListSubscriptions() {
	ctx := s.GetContext()

	s.Run("nil_filter_returns_default_page", func() {
		resp, err := s.svc.ListSubscriptions(ctx, nil)
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.Items)
		s.Equal(s.testData.sub.ID, resp.Items[0].Subscription.ID)
		s.Require().NotNil(resp.Items[0].Plan)
		s.Equal(s.testData.plan.ID, resp.Items[0].Plan.Plan.ID)
	})

	s.Run("resolves_external_customer_id", func() {
		filter := types.NewSubscriptionFilter()
		filter.ExternalCustomerID = s.testData.customer.ExternalID

		resp, err := s.svc.ListSubscriptions(ctx, filter)
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.Items)
		for _, item := range resp.Items {
			s.Equal(s.testData.customer.ID, item.Subscription.CustomerID)
		}
	})

	s.Run("unknown_external_customer_id_returns_not_found", func() {
		filter := types.NewSubscriptionFilter()
		filter.ExternalCustomerID = "ext_does_not_exist"

		_, err := s.svc.ListSubscriptions(ctx, filter)
		s.Error(err)
	})

	s.Run("invalid_expand_returns_error", func() {
		filter := types.NewSubscriptionFilter()
		filter.Expand = lo.ToPtr("bogus_field")

		_, err := s.svc.ListSubscriptions(ctx, filter)
		s.Error(err)
	})

	s.Run("expands_customers_in_bulk", func() {
		filter := types.NewSubscriptionFilter()
		filter.CustomerID = s.testData.customer.ID
		filter.Expand = lo.ToPtr("customer")

		resp, err := s.svc.ListSubscriptions(ctx, filter)
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.Items)
		for _, item := range resp.Items {
			s.Require().NotNil(item.Customer)
			s.Equal(s.testData.customer.ID, item.Customer.Customer.ID)
		}
	})
}

func (s *SubscriptionLifecycleSuite) TestActivateIncompleteSubscription() {
	ctx := s.GetContext()

	s.Run("subscription_not_found_returns_error", func() {
		s.Error(s.svc.ActivateIncompleteSubscription(ctx, "sub_missing"))
	})

	s.Run("non_incomplete_subscription_is_noop", func() {
		sub := s.createBareSubscription(nil) // active
		s.NoError(s.svc.ActivateIncompleteSubscription(ctx, sub.ID))

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})

	s.Run("activates_incomplete_subscription", func() {
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		})

		s.NoError(s.svc.ActivateIncompleteSubscription(ctx, sub.ID))

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})
}

// createCreditGrantWithApplication persists a credit grant and a pending application for a subscription.
func (s *SubscriptionLifecycleSuite) createCreditGrantWithApplication(subID string, status types.ApplicationStatus) (*creditgrantdomain.CreditGrant, *creditgrantapplication.CreditGrantApplication) {
	ctx := s.GetContext()
	grant := &creditgrantdomain.CreditGrant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:           "Lifecycle Grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: &subID,
		Credits:        decimal.NewFromInt(25),
		Cadence:        types.CreditGrantCadenceOneTime,
		StartDate:      lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
		ExpirationType: types.CreditGrantExpiryTypeNever,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().CreditGrantRepo.Create(ctx, grant)
	s.Require().NoError(err)

	app := &creditgrantapplication.CreditGrantApplication{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:     grant.ID,
		SubscriptionID:    subID,
		ScheduledFor:      s.testData.now.Add(-1 * time.Hour),
		PeriodStart:       s.testData.now.Add(-1 * time.Hour),
		ApplicationStatus: status,
		Credits:           grant.Credits,
		ApplicationReason: types.ApplicationReasonOnetimeCreditGrant,
		IdempotencyKey:    types.GenerateUUIDWithPrefix("idem"),
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CreditGrantApplicationRepo.Create(ctx, app))
	return grant, app
}

func (s *SubscriptionLifecycleSuite) TestProcessPendingCreditGrantsForSubscription() {
	ctx := s.GetContext()

	s.Run("no_pending_applications_is_noop", func() {
		sub := s.createBareSubscription(nil)
		s.NoError(s.internalService().processPendingCreditGrantsForSubscription(ctx, sub))
	})

	s.Run("applies_pending_credit_grant_to_wallet", func() {
		sub := s.createBareSubscription(nil)
		_, app := s.createCreditGrantWithApplication(sub.ID, types.ApplicationStatusPending)

		s.NoError(s.internalService().processPendingCreditGrantsForSubscription(ctx, sub))

		storedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(ctx, app.ID)
		s.Require().NoError(err)
		s.Equal(types.ApplicationStatusApplied, storedApp.ApplicationStatus)

		// The credits were topped up into a wallet for the customer.
		wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, sub.CustomerID)
		s.Require().NoError(err)
		s.Require().NotEmpty(wallets)
		s.True(wallets[0].Balance.Equal(decimal.NewFromInt(25)), "expected balance 25, got %s", wallets[0].Balance)
	})

	s.Run("missing_credit_grant_returns_error", func() {
		sub := s.createBareSubscription(nil)
		app := &creditgrantapplication.CreditGrantApplication{
			ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
			CreditGrantID:     "cg_missing",
			SubscriptionID:    sub.ID,
			ScheduledFor:      s.testData.now.Add(-1 * time.Hour),
			PeriodStart:       s.testData.now.Add(-1 * time.Hour),
			ApplicationStatus: types.ApplicationStatusPending,
			Credits:           decimal.NewFromInt(5),
			ApplicationReason: types.ApplicationReasonOnetimeCreditGrant,
			IdempotencyKey:    types.GenerateUUIDWithPrefix("idem"),
			EnvironmentID:     types.GetEnvironmentID(ctx),
			BaseModel:         types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().CreditGrantApplicationRepo.Create(ctx, app))

		err := s.internalService().processPendingCreditGrantsForSubscription(ctx, sub)
		s.Error(err)
	})

	s.Run("non_active_subscription_skips_application", func() {
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		})
		_, app := s.createCreditGrantWithApplication(sub.ID, types.ApplicationStatusPending)

		s.NoError(s.internalService().processPendingCreditGrantsForSubscription(ctx, sub))

		storedApp, err := s.GetStores().CreditGrantApplicationRepo.Get(ctx, app.ID)
		s.Require().NoError(err)
		s.Equal(types.ApplicationStatusPending, storedApp.ApplicationStatus, "deferred application must remain pending")
	})
}

func (s *SubscriptionLifecycleSuite) TestHandleSubscriptionActivatingInvoicePaid() {
	ctx := s.GetContext()

	s.Run("nil_invoice_is_noop", func() {
		s.NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, nil))
	})

	s.Run("invoice_without_subscription_is_noop", func() {
		s.NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, &invoice.Invoice{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			BillingReason: string(types.InvoiceBillingReasonSubscriptionCreate),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}))
	})

	s.Run("non_activating_billing_reason_is_noop", func() {
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		})
		s.NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, &invoice.Invoice{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			SubscriptionID: &sub.ID,
			BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}))

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusIncomplete, stored.SubscriptionStatus, "cycle invoices must not activate")
	})

	s.Run("subscription_create_invoice_activates_incomplete_subscription", func() {
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		})
		s.NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, &invoice.Invoice{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			SubscriptionID: &sub.ID,
			BillingReason:  string(types.InvoiceBillingReasonSubscriptionCreate),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}))

		stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})
}

func (s *SubscriptionLifecycleSuite) TestGetSubscriptionsForBillingPeriodUpdate() {
	ctx := s.GetContext()

	s.Run("nil_filter_lists_all_subscriptions", func() {
		resp, err := s.svc.GetSubscriptionsForBillingPeriodUpdate(ctx, nil)
		s.Require().NoError(err)
		s.Require().NotEmpty(resp.Items)
		ids := lo.Map(resp.Items, func(item *dto.SubscriptionResponse, _ int) string { return item.Subscription.ID })
		s.Contains(ids, s.testData.sub.ID)
	})

	s.Run("filter_by_status_excludes_non_matching", func() {
		filter := types.NewNoLimitSubscriptionFilter()
		filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusPaused}

		resp, err := s.svc.GetSubscriptionsForBillingPeriodUpdate(ctx, filter)
		s.Require().NoError(err)
		s.Empty(resp.Items)
	})
}

func (s *SubscriptionLifecycleSuite) TestGetActiveAddonAssociations() {
	ctx := s.GetContext()

	resp, err := s.svc.GetActiveAddonAssociations(ctx, s.testData.sub.ID)
	s.Require().NoError(err)
	s.Empty(resp.Items)
}

func (s *SubscriptionLifecycleSuite) TestCalculateBillingPeriods() {
	ctx := s.GetContext()

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.svc.CalculateBillingPeriods(ctx, "sub_missing")
		s.Error(err)
	})

	s.Run("current_period_in_future_returns_single_period", func() {
		sub := s.createBareSubscription(nil) // period ends 6 days from now
		periods, err := s.svc.CalculateBillingPeriods(ctx, sub.ID)
		s.Require().NoError(err)
		s.Require().Len(periods, 1)
		s.True(periods[0].Start.Equal(sub.CurrentPeriodStart))
		s.True(periods[0].End.Equal(sub.CurrentPeriodEnd))
	})

	s.Run("past_periods_are_rolled_forward_to_now", func() {
		start := s.testData.now.AddDate(0, -3, 0)
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.StartDate = start
			sub.BillingAnchor = start
			sub.CurrentPeriodStart = start
			sub.CurrentPeriodEnd = start.AddDate(0, 1, 0)
		})

		periods, err := s.svc.CalculateBillingPeriods(ctx, sub.ID)
		s.Require().NoError(err)
		s.GreaterOrEqual(len(periods), 3)
		// Periods must be contiguous.
		for i := 1; i < len(periods); i++ {
			s.True(periods[i].Start.Equal(periods[i-1].End), "period %d must start where the previous ended", i)
		}
		// The last period must cover now.
		last := periods[len(periods)-1]
		s.False(last.End.Before(s.testData.now))
	})

	s.Run("stops_at_scheduled_cancellation", func() {
		// NextBillingDate returns second-precision timestamps; keep fixtures aligned.
		start := s.testData.now.AddDate(0, -3, 0).Truncate(time.Second)
		cancelAt := start.AddDate(0, 2, 0)
		sub := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.StartDate = start
			sub.BillingAnchor = start
			sub.CurrentPeriodStart = start
			sub.CurrentPeriodEnd = start.AddDate(0, 1, 0)
			sub.CancelAtPeriodEnd = true
			sub.CancelAt = &cancelAt
		})

		periods, err := s.svc.CalculateBillingPeriods(ctx, sub.ID)
		s.Require().NoError(err)
		s.Require().Len(periods, 2)
		s.True(periods[1].End.Equal(cancelAt))
	})
}

func (s *SubscriptionLifecycleSuite) TestCascadeCancelToInheritedSubscriptions() {
	ctx := s.GetContext()

	s.Run("non_parent_subscription_is_noop", func() {
		sub := s.createBareSubscription(nil)
		s.NoError(s.svc.CascadeCancelToInheritedSubscriptions(ctx, sub))
	})

	s.Run("copies_cancellation_fields_to_children", func() {
		parent := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionType = types.SubscriptionTypeParent
		})
		child := s.createBareSubscription(func(sub *subscription.Subscription) {
			sub.SubscriptionType = types.SubscriptionTypeInherited
			sub.ParentSubscriptionID = &parent.ID
		})

		cancelledAt := s.testData.now
		parent.SubscriptionStatus = types.SubscriptionStatusCancelled
		parent.CancelledAt = &cancelledAt
		parent.CancelAt = &cancelledAt
		parent.CancelAtPeriodEnd = true
		parent.EndDate = &cancelledAt

		s.NoError(s.svc.CascadeCancelToInheritedSubscriptions(ctx, parent))

		storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusCancelled, storedChild.SubscriptionStatus)
		s.Require().NotNil(storedChild.CancelledAt)
		s.True(storedChild.CancelledAt.Equal(cancelledAt))
		s.True(storedChild.CancelAtPeriodEnd)
		s.Require().NotNil(storedChild.EndDate)
		s.True(storedChild.EndDate.Equal(cancelledAt))
	})
}

func (s *SubscriptionLifecycleSuite) TestCascadePauseAndResumeToInherited() {
	ctx := s.GetContext()

	parent := s.createBareSubscription(func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeParent
	})
	child := s.createBareSubscription(func(sub *subscription.Subscription) {
		sub.SubscriptionType = types.SubscriptionTypeInherited
		sub.ParentSubscriptionID = &parent.ID
	})

	s.Run("pause_mirrors_status_on_children", func() {
		parent.SubscriptionStatus = types.SubscriptionStatusPaused
		parent.PauseStatus = types.PauseStatusActive

		s.NoError(s.internalService().cascadePauseToInherited(ctx, parent))

		storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusPaused, storedChild.SubscriptionStatus)
		s.Equal(types.PauseStatusActive, storedChild.PauseStatus)
		s.Nil(storedChild.ActivePauseID)
	})

	s.Run("resume_mirrors_status_and_periods_on_children", func() {
		// The paused child is not returned by getInheritedSubscriptions (active/trialing/draft
		// only), so reset it to active first to simulate resume-in-progress state.
		storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		storedChild.SubscriptionStatus = types.SubscriptionStatusActive
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, storedChild))

		parent.SubscriptionStatus = types.SubscriptionStatusActive
		parent.PauseStatus = types.PauseStatusNone
		parent.CurrentPeriodStart = s.testData.now
		parent.CurrentPeriodEnd = s.testData.now.AddDate(0, 1, 0)

		s.NoError(s.internalService().cascadeResumeToInherited(ctx, parent))

		storedChild, err = s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusActive, storedChild.SubscriptionStatus)
		s.Equal(types.PauseStatusNone, storedChild.PauseStatus)
		s.True(storedChild.CurrentPeriodStart.Equal(parent.CurrentPeriodStart))
		s.True(storedChild.CurrentPeriodEnd.Equal(parent.CurrentPeriodEnd))
	})
}

// newScheduleForSub persists a schedule of the given type/status for a subscription.
func (s *SubscriptionLifecycleSuite) newScheduleForSub(subID string, scheduleType types.SubscriptionScheduleChangeType, status types.ScheduleStatus, scheduledAt time.Time, targetPlanID string) *subscription.SubscriptionSchedule {
	ctx := s.GetContext()
	schedule := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: subID,
		ScheduleType:   scheduleType,
		ScheduledAt:    scheduledAt,
		Status:         status,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		CreatedBy:      types.GetUserID(ctx),
		UpdatedBy:      types.GetUserID(ctx),
		StatusColumn:   types.StatusPublished,
	}
	if scheduleType == types.SubscriptionScheduleChangeTypePlanChange {
		s.Require().NoError(schedule.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
			TargetPlanID:       targetPlanID,
			ProrationBehavior:  types.ProrationBehaviorNone,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		}))
	} else {
		s.Require().NoError(schedule.SetCancellationConfig(&subscription.CancellationConfiguration{
			CancellationType:  types.CancellationTypeEndOfPeriod,
			ProrationBehavior: types.ProrationBehaviorNone,
		}))
	}
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, schedule))
	return schedule
}

func (s *SubscriptionLifecycleSuite) TestCancelAllPendingSchedules() {
	ctx := s.GetContext()
	sub := s.createBareSubscription(nil)

	pendingPlanChange := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusPending, s.testData.now.Add(24*time.Hour), s.testData.plan.ID)
	pendingCancellation := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypeCancellation, types.ScheduleStatusPending, s.testData.now.Add(48*time.Hour), "")
	executed := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusExecuted, s.testData.now.Add(-24*time.Hour), s.testData.plan.ID)

	s.NoError(s.internalService().cancelAllPendingSchedules(ctx, sub.ID))

	for _, id := range []string{pendingPlanChange.ID, pendingCancellation.ID} {
		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.ScheduleStatusCancelled, stored.Status)
		s.NotNil(stored.CancelledAt)
	}

	storedExecuted, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, executed.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusExecuted, storedExecuted.Status)
}

func (s *SubscriptionLifecycleSuite) TestMarkCancellationScheduleAsExecuted() {
	ctx := s.GetContext()
	sub := s.createBareSubscription(nil)
	schedule := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypeCancellation, types.ScheduleStatusPending, s.testData.now, "")

	s.NoError(s.svc.MarkCancellationScheduleAsExecuted(ctx, sub.ID))

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusExecuted, stored.Status)
	s.NotNil(stored.ExecutedAt)
}

func (s *SubscriptionLifecycleSuite) TestProcessPendingPlanChanges() {
	ctx := s.GetContext()

	s.Run("future_schedule_is_skipped", func() {
		sub := s.createBareSubscription(nil)
		schedule := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusPending, s.testData.now.Add(24*time.Hour), s.testData.plan.ID)

		s.NoError(s.internalService().processPendingPlanChanges(ctx, sub))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.Require().NoError(err)
		s.Equal(types.ScheduleStatusPending, stored.Status, "not-yet-due schedule must stay pending")
	})

	s.Run("due_schedule_executes_plan_change", func() {
		// Target plan with a price so the change service can build the new subscription.
		targetPlanResp, err := s.planService.CreatePlan(ctx, dto.CreatePlanRequest{
			Name: "Lifecycle Target Plan",
		})
		s.Require().NoError(err)
		amt := decimal.NewFromInt(30)
		_, err = s.priceService.CreatePrice(ctx, dto.CreatePriceRequest{
			Amount:             &amt,
			Currency:           "usd",
			Type:               types.PRICE_TYPE_FIXED,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           targetPlanResp.Plan.ID,
		})
		s.Require().NoError(err)

		subResp, err := s.svc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			Currency:           "usd",
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		})
		s.Require().NoError(err)
		sub, gerr := s.GetStores().SubscriptionRepo.Get(ctx, subResp.Subscription.ID)
		s.Require().NoError(gerr)

		schedule := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusPending, s.testData.now.Add(-1*time.Minute), targetPlanResp.Plan.ID)

		s.NoError(s.internalService().processPendingPlanChanges(ctx, sub))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.Require().NoError(err)
		s.Equal(types.ScheduleStatusExecuted, stored.Status)
		s.NotNil(stored.ExecutedAt)

		result, err := stored.GetPlanChangeResult()
		s.Require().NoError(err)
		s.Require().NotNil(result)
		s.Equal(sub.ID, result.OldSubscriptionID)

		newSub, err := s.GetStores().SubscriptionRepo.Get(ctx, result.NewSubscriptionID)
		s.Require().NoError(err)
		s.Equal(targetPlanResp.Plan.ID, newSub.PlanID)
		s.Equal(types.SubscriptionStatusActive, newSub.SubscriptionStatus)
	})

	s.Run("due_schedule_with_missing_target_plan_marks_failed", func() {
		subResp, err := s.svc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			Currency:           "usd",
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		})
		s.Require().NoError(err)
		sub, gerr := s.GetStores().SubscriptionRepo.Get(ctx, subResp.Subscription.ID)
		s.Require().NoError(gerr)

		schedule := s.newScheduleForSub(sub.ID, types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusPending, s.testData.now.Add(-1*time.Minute), "plan_does_not_exist")

		err = s.internalService().processPendingPlanChanges(ctx, sub)
		s.Error(err)

		stored, gerr := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.Require().NoError(gerr)
		s.Equal(types.ScheduleStatusFailed, stored.Status)
		s.NotNil(stored.ErrorMessage)
	})
}

func (s *SubscriptionLifecycleSuite) TestGetUpcomingCreditGrantApplications() {
	ctx := s.GetContext()

	s.Run("empty_subscription_ids_returns_validation_error", func() {
		_, err := s.svc.GetUpcomingCreditGrantApplications(ctx, &dto.GetUpcomingCreditGrantApplicationsRequest{})
		s.Error(err)
	})

	s.Run("unknown_subscription_returns_not_found", func() {
		_, err := s.svc.GetUpcomingCreditGrantApplications(ctx, &dto.GetUpcomingCreditGrantApplicationsRequest{
			SubscriptionIDs: []string{"sub_missing"},
		})
		s.Error(err)
	})

	s.Run("returns_only_upcoming_applications_sorted", func() {
		sub := s.createBareSubscription(nil)
		grant, _ := s.createCreditGrantWithApplication(sub.ID, types.ApplicationStatusApplied)

		mkApp := func(scheduledFor time.Time, status types.ApplicationStatus) *creditgrantapplication.CreditGrantApplication {
			app := &creditgrantapplication.CreditGrantApplication{
				ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
				CreditGrantID:     grant.ID,
				SubscriptionID:    sub.ID,
				ScheduledFor:      scheduledFor,
				PeriodStart:       scheduledFor,
				ApplicationStatus: status,
				Credits:           decimal.NewFromInt(5),
				ApplicationReason: types.ApplicationReasonRecurringCreditGrant,
				IdempotencyKey:    types.GenerateUUIDWithPrefix("idem"),
				EnvironmentID:     types.GetEnvironmentID(ctx),
				BaseModel:         types.GetDefaultBaseModel(ctx),
			}
			s.Require().NoError(s.GetStores().CreditGrantApplicationRepo.Create(ctx, app))
			return app
		}

		pastPending := mkApp(s.testData.now.Add(-48*time.Hour), types.ApplicationStatusPending)
		futureLater := mkApp(s.testData.now.Add(72*time.Hour), types.ApplicationStatusPending)
		futureSooner := mkApp(s.testData.now.Add(24*time.Hour), types.ApplicationStatusFailed)

		resp, err := s.svc.GetUpcomingCreditGrantApplications(ctx, &dto.GetUpcomingCreditGrantApplicationsRequest{
			SubscriptionIDs: []string{sub.ID},
		})
		s.Require().NoError(err)
		s.Require().Len(resp.Items, 2)
		s.Equal(futureSooner.ID, resp.Items[0].CreditGrantApplication.ID, "items must be sorted by scheduled_for ascending")
		s.Equal(futureLater.ID, resp.Items[1].CreditGrantApplication.ID)
		s.Equal(2, resp.Pagination.Total)

		ids := lo.Map(resp.Items, func(item *dto.CreditGrantApplicationResponse, _ int) string {
			return item.CreditGrantApplication.ID
		})
		s.NotContains(ids, pastPending.ID, "past-due applications are not upcoming")
	})
}
