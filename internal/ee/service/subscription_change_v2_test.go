package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionChangeV2Suite struct {
	testutil.BaseServiceTestSuite
	svc interfaces.SubscriptionService
	td  changeV2TestData
}

type changeV2TestData struct {
	customer    *customer.Customer
	starter     *plan.Plan
	pro         *plan.Plan
	starterBase *price.Price
	proBase     *price.Price
	sub         *subscription.Subscription
	baseLine    *subscription.SubscriptionLineItem
	periodStart time.Time
	periodEnd   time.Time
}

func TestSubscriptionChangeV2(t *testing.T) {
	suite.Run(t, new(SubscriptionChangeV2Suite))
}

func (s *SubscriptionChangeV2Suite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(s.serviceParams())
	s.setupTestData()
}

func (s *SubscriptionChangeV2Suite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionChangeV2Suite) serviceParams() ServiceParams {
	st := s.GetStores()
	return ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		SubRepo:                    st.SubscriptionRepo,
		SubscriptionLineItemRepo:   st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      st.SubscriptionPhaseRepo,
		SubScheduleRepo:            st.SubscriptionScheduleRepo,
		PlanRepo:                   st.PlanRepo,
		PriceRepo:                  st.PriceRepo,
		PriceUnitRepo:              st.PriceUnitRepo,
		EventRepo:                  st.EventRepo,
		MeterRepo:                  st.MeterRepo,
		CustomerRepo:               st.CustomerRepo,
		InvoiceRepo:                st.InvoiceRepo,
		InvoiceLineItemRepo:        st.InvoiceLineItemRepo,
		EntitlementRepo:            st.EntitlementRepo,
		EnvironmentRepo:            st.EnvironmentRepo,
		FeatureRepo:                st.FeatureRepo,
		TenantRepo:                 st.TenantRepo,
		UserRepo:                   st.UserRepo,
		AuthRepo:                   st.AuthRepo,
		WalletRepo:                 st.WalletRepo,
		PaymentRepo:                st.PaymentRepo,
		CreditGrantRepo:            st.CreditGrantRepo,
		CreditGrantApplicationRepo: st.CreditGrantApplicationRepo,
		CouponRepo:                 st.CouponRepo,
		CouponAssociationRepo:      st.CouponAssociationRepo,
		CouponApplicationRepo:      st.CouponApplicationRepo,
		AddonRepo:                  st.AddonRepo,
		AddonAssociationRepo:       st.AddonAssociationRepo,
		ConnectionRepo:             st.ConnectionRepo,
		SettingsRepo:               st.SettingsRepo,
		TaxAssociationRepo:         st.TaxAssociationRepo,
		TaxRateRepo:                st.TaxRateRepo,
		AlertLogsRepo:              st.AlertLogsRepo,
		PlanPriceSyncRepo:          st.PlanPriceSyncRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
	}
}

func (s *SubscriptionChangeV2Suite) setupTestData() {
	ctx := s.GetContext()

	// Period must contain time.Now() (change effective date).
	s.td.periodStart = time.Now().UTC().Truncate(time.Hour).AddDate(0, 0, -10)
	s.td.periodEnd = s.td.periodStart.AddDate(0, 0, 30)

	s.td.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "cust_change_v2",
		Name:       "Change V2 Co",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.td.customer))

	s.td.starter = s.createPlan("Starter", "starter")
	s.td.pro = s.createPlan("Pro", "pro")

	s.td.starterBase = s.createFixedPrice(s.td.starter.ID, "base_fee", 20)
	s.td.proBase = s.createFixedPrice(s.td.pro.ID, "base_fee", 50)

	s.td.sub = s.createSubscription(s.td.starter.ID)
	s.td.baseLine = s.createLineItem(s.td.sub, s.td.starterBase, s.td.starter)
}

func (s *SubscriptionChangeV2Suite) createPlan(name, lookupKey string) *plan.Plan {
	ctx := s.GetContext()
	p := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      name,
		LookupKey: lookupKey,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, p))
	return p
}

func (s *SubscriptionChangeV2Suite) createFixedPrice(planID, lookupKey string, amount int64) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(amount),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		LookupKey:          lookupKey,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

func (s *SubscriptionChangeV2Suite) createSubscription(planID string) *subscription.Subscription {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         s.td.customer.ID,
		PlanID:             planID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		Currency:           "usd",
		BillingAnchor:      s.td.periodStart,
		StartDate:          s.td.periodStart,
		CurrentPeriodStart: s.td.periodStart,
		CurrentPeriodEnd:   s.td.periodEnd,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingCycle:       types.BillingCycleAnniversary,
		Timezone:           "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionChangeV2Suite) createLineItem(
	sub *subscription.Subscription,
	p *price.Price,
	pl *plan.Plan,
) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	item := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            p.ID,
		PriceType:          p.Type,
		Quantity:           decimal.NewFromInt(1),
		Currency:           sub.Currency,
		BillingPeriod:      p.BillingPeriod,
		BillingPeriodCount: p.BillingPeriodCount,
		InvoiceCadence:     p.InvoiceCadence,
		StartDate:          sub.CurrentPeriodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, item))
	return item
}

func (s *SubscriptionChangeV2Suite) recordBilled(lineItemID string, amount decimal.Decimal) {
	ctx := s.GetContext()
	periodStart := s.td.periodStart
	periodEnd := s.td.periodEnd

	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             s.td.customer.ID,
		SubscriptionID:         &s.td.sub.ID,
		SubscriptionLineItemID: &lineItemID,
		Amount:                 amount,
		Quantity:               decimal.NewFromInt(1),
		Currency:               "usd",
		PeriodStart:            &periodStart,
		PeriodEnd:              &periodEnd,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}))
}

func (s *SubscriptionChangeV2Suite) changeRequest(targetPlanID string, behavior types.ProrationBehavior) dto.SubscriptionChangeV2Request {
	return dto.SubscriptionChangeV2Request{
		TargetPlanID:      targetPlanID,
		ProrationBehavior: behavior,
	}
}

func (s *SubscriptionChangeV2Suite) liveLineItems() []*subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	s.Require().NoError(err)

	live := make([]*subscription.SubscriptionLineItem, 0, len(items))
	for _, item := range items {
		if item.EndDate.IsZero() {
			live = append(live, item)
		}
	}
	return live
}

func (s *SubscriptionChangeV2Suite) TestExecute_UpgradeSwapsInPlace() {
	ctx := s.GetContext()
	effectiveFrom := time.Now().UTC()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	s.Equal(types.SubscriptionChangeTypeUpgrade, resp.ChangeType, "$20 → $50 is an upgrade")
	s.Equal(s.td.starter.ID, resp.FromPlan.ID)
	s.Equal(s.td.pro.ID, resp.ToPlan.ID)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.sub.ID, reloaded.ID, "the subscription row survives a plan change")
	s.Equal(s.td.pro.ID, reloaded.PlanID)
	s.True(reloaded.BillingAnchor.Equal(s.td.sub.BillingAnchor), "the anchor is the metronome; it must not move")
	s.True(reloaded.CurrentPeriodStart.Equal(s.td.sub.CurrentPeriodStart))
	s.True(reloaded.CurrentPeriodEnd.Equal(s.td.sub.CurrentPeriodEnd))

	live := s.liveLineItems()
	s.Require().Len(live, 1, "exactly one live base-fee line after the swap")
	s.Equal(s.td.proBase.ID, live[0].PriceID)

	closed, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.td.baseLine.ID)
	s.Require().NoError(err)
	s.False(closed.EndDate.IsZero(), "the old line is closed, not deleted")
	s.True(!closed.EndDate.Before(effectiveFrom), "closed at the effective date")
	s.True(live[0].StartDate.Equal(closed.EndDate), "line items must tile with no gap or overlap")
}

func (s *SubscriptionChangeV2Suite) TestExecute_LateralIdenticalPrice_CarriesLineToTargetPlan() {
	ctx := s.GetContext()

	lateral := s.createPlan("Lateral", "lateral")
	lateralBase := s.createFixedPrice(lateral.ID, "base_fee", 20)

	invoicesBefore := s.countInvoices()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(lateral.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)
	s.Equal(types.SubscriptionChangeTypeLateral, resp.ChangeType)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(lateral.ID, reloaded.PlanID, "the plan still swaps")

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.baseLine.ID, live[0].ID, "the line item keeps its id — the service was never interrupted")
	s.Equal(s.td.baseLine.StartDate, live[0].StartDate, "and its usage window")
	// Billing does not change, but ownership does: a line still pointing at the old
	// plan's price would bill off a plan the subscription has left, and plan-price
	// sync — re-anchored to the target here — would never see it.
	s.Equal(lateralBase.ID, live[0].PriceID, "the carried line bills off the target plan's price")
	s.Equal(lateral.ID, live[0].EntityID, "and is owned by the target plan")

	s.Require().Len(resp.ChangedResources.LineItems, 1, "repointing a carried line is a change the caller must see")
	s.Equal(dto.ChangedLineItemActionUpdated, resp.ChangedResources.LineItems[0].ChangeAction)
	s.Equal(lateralBase.ID, resp.ChangedResources.LineItems[0].PriceID)

	s.Equal(invoicesBefore, s.countInvoices(), "net zero on an unchanged service must raise no invoice")
}

func (s *SubscriptionChangeV2Suite) TestExecute_ProrationNone_SwapsWithoutMoney() {
	ctx := s.GetContext()
	invoicesBefore := s.countInvoices()

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone))
	s.Require().NoError(err)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.pro.ID, reloaded.PlanID, "the service still swaps")

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.proBase.ID, live[0].PriceID, "billed at the new price from here on")

	s.Equal(invoicesBefore, s.countInvoices(), "proration_behavior=none must not settle anything")
}

func (s *SubscriptionChangeV2Suite) TestExecute_DowngradeCreditsWallet() {
	ctx := s.GetContext()

	s.NoError(s.GetStores().SubscriptionLineItemRepo.Delete(ctx, s.td.baseLine.ID))
	s.Require().NoError(s.GetStores().SubscriptionRepo.UpdatePlan(ctx, s.td.sub.ID, s.td.pro.ID))
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.td.baseLine = s.createLineItem(sub, s.td.proBase, s.td.pro)

	// Need a billed charge so the removal credit has a basis.
	s.recordBilled(s.td.baseLine.ID, s.td.proBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.starter.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)
	s.Equal(types.SubscriptionChangeTypeDowngrade, resp.ChangeType)

	wallets, err := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Require().NoError(err)
	s.Require().NotEmpty(wallets, "the residual credit must reach the customer, not be discarded")
	s.True(wallets[0].Balance.GreaterThan(decimal.Zero))
}

func (s *SubscriptionChangeV2Suite) TestPreviewWritesNothing() {
	ctx := s.GetContext()
	invoicesBefore := s.countInvoices()

	resp, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.starter.ID, reloaded.PlanID, "preview must not swap the plan")

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.baseLine.ID, live[0].ID, "preview must not touch line items")
	s.Equal(invoicesBefore, s.countInvoices(), "preview must not raise an invoice")
}

func (s *SubscriptionChangeV2Suite) TestPreviewMatchesExecute() {
	ctx := s.GetContext()
	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)
	s.Require().Len(preview.ChangedResources.Invoices, 1)
	quoted := preview.ChangedResources.Invoices[0].Invoice.AmountDue

	executed, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)
	s.Require().Len(executed.ChangedResources.Invoices, 1)
	charged := executed.ChangedResources.Invoices[0].Invoice.AmountDue

	s.True(quoted.Equal(charged), "quoted %s but charged %s", quoted, charged)
	s.Equal(preview.ChangeType, executed.ChangeType)
	s.Len(preview.ChangedResources.LineItems, len(executed.ChangedResources.LineItems))
}

// A net credit is paid to the wallet, never invoiced, so the quote has to say so:
// an invoice in the preview that execute does not raise is a lie to the caller,
// and its totals would have to be negative to boot.
func (s *SubscriptionChangeV2Suite) TestPreviewMatchesExecute_OnADowngradeCredit() {
	ctx := s.GetContext()

	s.NoError(s.GetStores().SubscriptionLineItemRepo.Delete(ctx, s.td.baseLine.ID))
	s.Require().NoError(s.GetStores().SubscriptionRepo.UpdatePlan(ctx, s.td.sub.ID, s.td.pro.ID))
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.td.baseLine = s.createLineItem(sub, s.td.proBase, s.td.pro)
	s.recordBilled(s.td.baseLine.ID, s.td.proBase.Amount)

	req := s.changeRequest(s.td.starter.ID, types.ProrationBehaviorCreateProrations)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)
	s.Require().Len(preview.ChangedResources.Invoices, 1)
	quoted := preview.ChangedResources.Invoices[0]
	s.Equal(dto.ChangedInvoiceActionWalletCredit, quoted.Action, "a downgrade credit is not an invoice")
	s.Equal(dto.ChangedInvoiceStatusPreview, quoted.Status)
	s.Nil(quoted.Invoice, "quoting an invoice execute never creates misleads the caller")
	s.Require().NotNil(quoted.WalletTransaction)
	s.True(quoted.WalletTransaction.Amount.IsPositive(), "the credit is quoted as an amount owed to the customer")

	invoicesBefore := s.countInvoices()
	executed, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)
	s.Require().Len(executed.ChangedResources.Invoices, 1)
	settled := executed.ChangedResources.Invoices[0]

	s.Equal(quoted.Action, settled.Action)
	s.Equal(dto.ChangedInvoiceStatusWalletIssued, settled.Status)
	s.Require().NotNil(settled.WalletTransaction)
	s.True(quoted.WalletTransaction.Amount.Equal(settled.WalletTransaction.Amount),
		"quoted %s but credited %s", quoted.WalletTransaction.Amount, settled.WalletTransaction.Amount)
	s.Equal(invoicesBefore, s.countInvoices(), "a net credit raises no invoice")
}

// The plan lives on the subscription row, which is read through a cache, so any
// caller can be holding a copy from before a change. Only UpdatePlan may move it.
func (s *SubscriptionChangeV2Suite) TestExecute_PlanSurvivesAStaleConcurrentUpdate() {
	ctx := s.GetContext()

	stale, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	staleCopy := *stale
	s.Require().Equal(s.td.starter.ID, staleCopy.PlanID)

	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone))
	s.Require().NoError(err)

	// Whatever that caller was actually updating — status, period, metadata — it
	// must not carry the old plan back with it.
	staleCopy.SubscriptionStatus = types.SubscriptionStatusActive
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, &staleCopy))

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.pro.ID, reloaded.PlanID, "a stale writer must not undo a completed plan change")
}

func (s *SubscriptionChangeV2Suite) TestPreconditionsRejectBeforeAnyWrite() {
	ctx := s.GetContext()

	tests := []struct {
		name    string
		mutate  func(*subscription.Subscription)
		target  string
		because string
	}{
		{
			name:    "already on the target plan",
			target:  s.td.starter.ID,
			because: "a no-op change is a caller mistake, not a silent success",
		},
		{
			name:    "cancelled subscription",
			mutate:  func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusCancelled },
			because: "only active or trialing subscriptions can change plan",
		},
		{
			name:    "paused subscription",
			mutate:  func(sub *subscription.Subscription) { sub.PauseStatus = types.PauseStatusActive },
			because: "a paused subscription has no live billing to prorate",
		},
		{
			name:    "hierarchy subscription",
			mutate:  func(sub *subscription.Subscription) { sub.SubscriptionType = types.SubscriptionTypeParent },
			because: "child subscriptions would need to move too",
		},
		{
			name:    "pending cancellation",
			mutate:  func(sub *subscription.Subscription) { sub.CancelAtPeriodEnd = true },
			because: "the change would race the cancellation",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
			s.Require().NoError(err)
			if tt.mutate != nil {
				tt.mutate(sub)
				s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
			}

			target := tt.target
			if target == "" {
				target = s.td.pro.ID
			}

			_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(target, types.ProrationBehaviorCreateProrations))
			s.Error(err, tt.because)

			after, getErr := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
			s.Require().NoError(getErr)
			s.Equal(s.td.starter.ID, after.PlanID, "a rejected change must mutate nothing")

			// Restore for the next case.
			after.SubscriptionStatus = types.SubscriptionStatusActive
			after.PauseStatus = types.PauseStatusNone
			after.SubscriptionType = types.SubscriptionTypeStandalone
			after.CancelAtPeriodEnd = false
			s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, after))
		})
	}
}

func (s *SubscriptionChangeV2Suite) TestExecute_ReanchorsPriceSyncSequence() {
	ctx := s.GetContext()

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)

	expected, err := s.GetStores().PlanPriceSyncRepo.CurrentPlanSequence(ctx, s.td.pro.ID)
	s.Require().NoError(err)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(expected, reloaded.SyncedPriceSequence,
		"the watermark must be re-anchored to the new plan, or sync will never select this sub again")
}

func (s *SubscriptionChangeV2Suite) countInvoices() int {
	ctx := s.GetContext()
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(err)
	return len(invoices)
}

func (s *SubscriptionChangeV2Suite) TestExecute_NettedInvoiceCarriesCreditLine() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)
	s.Require().Len(resp.ChangedResources.Invoices, 1)

	inv := resp.ChangedResources.Invoices[0].Invoice
	s.Require().NotNil(inv)

	var sawCredit bool
	for _, li := range inv.LineItems {
		if li.Amount.IsNegative() {
			sawCredit = true
		}
	}
	s.True(sawCredit, "the unused-time credit must appear as a negative line on the netted invoice")
}

func (s *SubscriptionChangeV2Suite) attachAddon(name string, amount int64) (*addon.Addon, *addonassociation.AddonAssociation, *subscription.SubscriptionLineItem) {
	ctx := s.GetContext()

	a := &addon.Addon{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ADDON),
		Name:      name,
		LookupKey: name,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().AddonRepo.Create(ctx, a))

	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(amount),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           a.ID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	assoc := &addonassociation.AddonAssociation{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ADDON_ASSOCIATION),
		EntityID:    s.td.sub.ID,
		EntityType:  types.AddonAssociationEntityTypeSubscription,
		AddonID:     a.ID,
		AddonStatus: types.AddonStatusActive,
		StartDate:   &s.td.periodStart,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().AddonAssociationRepo.Create(ctx, assoc))

	item := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     s.td.sub.ID,
		CustomerID:         s.td.sub.CustomerID,
		EntityID:           a.ID,
		EntityType:         types.SubscriptionLineItemEntityTypeAddon,
		PriceID:            p.ID,
		PriceType:          p.Type,
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      p.BillingPeriod,
		BillingPeriodCount: 1,
		InvoiceCadence:     p.InvoiceCadence,
		StartDate:          s.td.periodStart,
		AddonAssociationID: &assoc.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, item))

	return a, assoc, item
}

func (s *SubscriptionChangeV2Suite) dropAddonRequest(targetPlanID, associationID string) dto.SubscriptionChangeV2Request {
	req := s.changeRequest(targetPlanID, types.ProrationBehaviorCreateProrations)
	req.EntityPolicies = &dto.SubscriptionChangeEntityPolicies{
		Addons: &dto.EntityChangePolicy{
			Overrides: map[string]types.EntityChangeBehaviour{
				associationID: types.EntityChangeBehaviourDrop,
			},
		},
	}
	return req
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonCarriesByDefault() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)

	reloadedAssoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, reloadedAssoc.AddonStatus, "an unmentioned addon must be left alone")
	s.Nil(reloadedAssoc.EndDate)

	reloadedLine, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, addonLine.ID)
	s.Require().NoError(err)
	s.True(reloadedLine.EndDate.IsZero(), "the addon's line item must not be sliced by a plan change")

	s.Require().Len(resp.EntityChanges, 1)
	s.Equal(types.EntityChangeBehaviourCarry, resp.EntityChanges[0].Behaviour)
	s.Equal(assoc.ID, resp.EntityChanges[0].ReferenceID)
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonDropClosesAttachment() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.dropAddonRequest(s.td.pro.ID, assoc.ID))
	s.Require().NoError(err)

	reloadedAssoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusCancelled, reloadedAssoc.AddonStatus)
	s.Require().NotNil(reloadedAssoc.EndDate)

	reloadedLine, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, addonLine.ID)
	s.Require().NoError(err)
	s.False(reloadedLine.EndDate.IsZero(), "the dropped addon's line item closes with it")
	s.True(reloadedLine.EndDate.Equal(*reloadedAssoc.EndDate), "line item and association close together")

	s.Require().Len(resp.EntityChanges, 1)
	s.Equal(types.EntityChangeBehaviourDrop, resp.EntityChanges[0].Behaviour)
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonDropSettlesOnTheChangeInvoice() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)
	s.recordBilled(addonLine.ID, decimal.NewFromInt(10))

	invoicesBefore := s.countInvoices()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.dropAddonRequest(s.td.pro.ID, assoc.ID))
	s.Require().NoError(err)

	s.Equal(invoicesBefore+1, s.countInvoices(), "one invoice for the whole change, not one per moving part")
	s.Require().Len(resp.ChangedResources.Invoices, 1)

	var sawAddonCredit bool
	for _, li := range resp.ChangedResources.Invoices[0].Invoice.LineItems {
		if li.Amount.IsNegative() && li.SubscriptionLineItemID != nil && *li.SubscriptionLineItemID == addonLine.ID {
			sawAddonCredit = true
		}
	}
	s.True(sawAddonCredit, "the dropped addon's credit must be a line on the change invoice")
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonDropWithProrationNone() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)
	s.recordBilled(addonLine.ID, decimal.NewFromInt(10))

	invoicesBefore := s.countInvoices()

	req := s.dropAddonRequest(s.td.pro.ID, assoc.ID)
	req.ProrationBehavior = types.ProrationBehaviorNone

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)

	reloadedAssoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusCancelled, reloadedAssoc.AddonStatus, "the addon still goes away")
	s.Equal(invoicesBefore, s.countInvoices(), "but no money moves")
}

func (s *SubscriptionChangeV2Suite) TestExecute_UnknownAddonOverrideWarnsRatherThanFails() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.dropAddonRequest(s.td.pro.ID, "addon_assoc_does_not_exist"))
	s.Require().NoError(err, "a stale override key must not fail the change")
	s.Require().Len(resp.Warnings, 1)
	s.Contains(resp.Warnings[0], "addon_assoc_does_not_exist")

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.pro.ID, reloaded.PlanID, "and the change still happens")
}

func (s *SubscriptionChangeV2Suite) TestPreview_AddonDropWritesNothing() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)

	resp, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.dropAddonRequest(s.td.pro.ID, assoc.ID))
	s.Require().NoError(err)
	s.Require().Len(resp.EntityChanges, 1)
	s.Equal(types.EntityChangeBehaviourDrop, resp.EntityChanges[0].Behaviour)

	reloadedAssoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, reloadedAssoc.AddonStatus, "preview must not close the attachment")

	reloadedLine, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, addonLine.ID)
	s.Require().NoError(err)
	s.True(reloadedLine.EndDate.IsZero())
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonDropDoesNotSkewChangeType() {
	ctx := s.GetContext()
	_, assoc, _ := s.attachAddon("priority_support", 10)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.dropAddonRequest(s.td.pro.ID, assoc.ID))
	s.Require().NoError(err)

	s.Equal(types.SubscriptionChangeTypeUpgrade, resp.ChangeType,
		"the addon's value must not count towards the plan's change type")
}

func (s *SubscriptionChangeV2Suite) TestExecute_AddonDropIsReportedAsAChangedLineItem() {
	ctx := s.GetContext()
	_, assoc, addonLine := s.attachAddon("priority_support", 10)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.dropAddonRequest(s.td.pro.ID, assoc.ID))
	s.Require().NoError(err)

	var sawAddonLine bool
	for _, item := range resp.ChangedResources.LineItems {
		if item.ID == addonLine.ID && item.ChangeAction == dto.ChangedLineItemActionEnded {
			sawAddonLine = true
		}
	}
	s.True(sawAddonLine, "a dropped addon's line item moved, so the response must say so")
}

func (s *SubscriptionChangeV2Suite) TestExecute_CarriedLineFollowsTheSubscriptionOntoTheNewPlan() {
	ctx := s.GetContext()

	lateral := s.createPlan("Lateral", "lateral")
	s.createFixedPrice(lateral.ID, "base_fee", 20)

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.changeRequest(lateral.ID, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(err)

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.baseLine.ID, live[0].ID, "the line is carried, not replaced")
	s.Equal(lateral.ID, live[0].EntityID,
		"a carried line still belongs to the subscription, and the subscription moved")
	s.Equal(lateral.Name, live[0].PlanDisplayName,
		"plan_display_name is copied onto every future invoice line, so it must not name the old plan")
}
