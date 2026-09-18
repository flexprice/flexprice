package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Pay-first for the `addons` batch. The correctness point throughout: a gated batch writes its
// attaches as pending and leaves its removals completely alone, so a customer who never pays
// keeps exactly the subscription they had.
//
// The suite has no payment provider, so StartPayFirstCheckoutSession always fails here. Tests
// either drive the real path and assert the compensation, or seed the session the way the
// single-addon suite does and drive completion from there.

func (s *SubscriptionServiceSuite) addonsCheckoutRequest(params *dto.SubModifyBulkAddonParams) dto.ExecuteSubscriptionModifyRequest {
	return dto.ExecuteSubscriptionModifyRequest{
		Type:            dto.SubscriptionModifyTypeAddon,
		BulkAddonParams: params,
		Checkout:        s.razorpayCheckoutParams(),
	}
}

// seedPayFirstAddonBatchCheckout produces the state a gated batch commits before its provider
// call: pending attaches, untouched removals, and one draft for the net.
func (s *SubscriptionServiceSuite) seedPayFirstAddonBatchCheckout(
	addonID string,
	outgoingAssociationID string,
	at time.Time,
) (*domainCheckout.CheckoutSession, *addonChangeConfig, *dto.InvoiceResponse) {
	ctx := s.GetContext()
	params := s.service.(*subscriptionService).ServiceParams
	sub := s.testData.subscription

	changeSvc := NewAddonChangeService(params)
	req := AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{{Request: s.modifyAdd(addonID, at)}},
	}
	if outgoingAssociationID != "" {
		req.Removes = []*dto.RemoveAddonRequest{s.modifyRemove(outgoingAssociationID, at)}
	}

	config, err := changeSvc.Resolve(ctx, req)
	s.Require().NoError(err)
	s.Require().True(config.getQuote().NetAmount().GreaterThan(decimal.Zero),
		"the batch must owe money for there to be anything to gate on")

	s.Require().NoError(changeSvc.PersistPending(ctx, config))

	drafted, err := changeSvc.Settle(ctx, config, SettleModeDraft)
	s.Require().NoError(err)
	draft := drafted.Draft

	checkoutSvc := &checkoutSessionService{ServiceParams: params}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draft.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      sub.CustomerID,
		Action:          types.CheckoutActionAddAddon,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			AddAddonParams: addonChangeCheckoutParams(config),
		}),
		CheckoutInvoiceID: &draft.ID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	return session, config, draft
}

// The gated state itself: the add is only an intent, the removal has not happened, and the net
// sits on a draft nobody has paid.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_GatedBatch_PersistsPendingAddsAndNoRemovals() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pf_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pf_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pf_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	session, config, draft := s.seedPayFirstAddonBatchCheckout("addon_pf_in", outgoing, at)

	pendingID := config.getAttaches()[0].getAssociation().ID
	pending, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pendingID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusPending, pending.AddonStatus, "the attach is written as an intent only")
	s.Empty(s.addonLineItemsFor(sub.ID, "addon_pf_in"), "pay-first defers line items to completion")

	stillActive, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, stillActive.AddonStatus, "a gated removal must not cancel anything")
	s.Nil(stillActive.EndDate, "a gated removal must not schedule an end date")
	s.Len(s.addonLineItemsFor(sub.ID, "addon_pf_out"), 1, "the outgoing addon keeps billing until payment")

	s.Equal(types.InvoiceStatusDraft, draft.InvoiceStatus)
	s.True(draft.AmountDue.Equal(config.getQuote().NetAmount()), "the draft locks the net, not the gross charge")
	s.Empty(s.prorationCredits(), "a gated batch moves no money")

	// The session records both halves so completion can replay them.
	cfg := session.Configuration.ToCheckoutConfiguration()
	s.Require().NotNil(cfg.AddAddonParams)
	s.Len(cfg.AddAddonParams.Addons, 1)
	s.Require().Len(cfg.AddAddonParams.Removes, 1)
	s.Equal(outgoing, cfg.AddAddonParams.Removes[0].AssociationID)
	s.False(cfg.AddAddonParams.Removes[0].EffectiveDate.IsZero(),
		"the removal date is resolved at execute time so completion replays the same one")
	s.NoError(cfg.AddAddonParams.Validate())
}

// Payment lands: the removals apply for the first time, the attaches activate, and the draft
// the customer paid is the only charge.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_Completion_AppliesRemovesAndActivatesAdds() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfc_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfc_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfc_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	session, config, draft := s.seedPayFirstAddonBatchCheckout("addon_pfc_in", outgoing, at)
	pendingID := config.getAttaches()[0].getAssociation().ID

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_addons_batch_001",
	}))

	removed, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.NotNil(removed.EndDate, "completion applies the gated removal")

	activated, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pendingID)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, activated.AddonStatus, "payment activates the attach")
	s.Len(s.addonLineItemsFor(sub.ID, "addon_pfc_in"), 1, "completion materialises the attach")

	finalized, err := NewInvoiceService(subService.ServiceParams).GetInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)

	s.Len(s.oneOffInvoicesFor(sub.ID), 1,
		"completion finalizes the draft, it never raises a second charge")
	s.Empty(s.prorationCredits(), "the credit was already netted into the draft")
}

// Two webhooks for the same session must not remove twice or attach twice.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_Completion_ReplayIsIdempotent() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfr_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfr_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfr_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	session, _, _ := s.seedPayFirstAddonBatchCheckout("addon_pfr_in", outgoing, at)
	cfg := session.Configuration.ToCheckoutConfiguration()

	s.Require().NoError(subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams))
	s.Require().NoError(subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams),
		"a second delivery must be a no-op, not a second removal")

	s.Len(s.addonLineItemsFor(sub.ID, "addon_pfr_in"), 1, "a replay must not duplicate line items")
	s.Len(s.oneOffInvoicesFor(sub.ID), 1, "a replay must not raise another charge")
	s.Empty(s.prorationCredits(), "a replay must not credit the wallet")
}

// The subscription died while the checkout was outstanding: the change must not apply to it.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_Completion_SubscriptionNoLongerActive() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfd_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfd_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfd_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	session, _, _ := s.seedPayFirstAddonBatchCheckout("addon_pfd_in", outgoing, at)

	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	cfg := session.Configuration.ToCheckoutConfiguration()
	err := subService.applyAddAddonCheckoutParams(ctx, cfg.AddAddonParams)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)

	kept, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.Nil(kept.EndDate, "a refused completion must not half-apply the removal")
}

// The batch is abandoned: the pending attaches are archived and the addons the customer wanted
// removed are still there, still billing.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_Cleanup_ArchivesAddsAndKeepsRemovalsBillable() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfx_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfx_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfx_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	session, config, _ := s.seedPayFirstAddonBatchCheckout("addon_pfx_in", outgoing, at)
	pendingID := config.getAttaches()[0].getAssociation().ID

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, nil))

	archived, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, pendingID)
	if err == nil {
		s.Equal(types.StatusArchived, archived.Status, "an unpaid attach is archived, not left pending")
	}

	kept, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, kept.AddonStatus)
	s.Nil(kept.EndDate, "an abandoned checkout must not remove the addon the customer still pays for")
	s.Len(s.addonLineItemsFor(sub.ID, "addon_pfx_out"), 1)
}

// While a batch's removal is gated its association stays active, so nothing but this guard
// stops a pay-later detach from removing it and completion removing it again.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_ConcurrentDetachOfGatedRemovalRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfg_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfg_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfg_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	s.seedPayFirstAddonBatchCheckout("addon_pfg_in", outgoing, at)

	_, err := s.service.(*subscriptionService).detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: outgoing,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
	}, sub.ID)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)

	stillActive, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.Nil(stillActive.EndDate)
}

// An addon NOT named by the pending session is still detachable — the guard must be specific.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_DetachOfUngatedAddonStillAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfu_gated", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfu_free", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfu_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	gated := s.attachForRemoval("addon_pfu_gated", 30)
	free := s.attachForRemoval("addon_pfu_free", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	s.seedPayFirstAddonBatchCheckout("addon_pfu_in", gated, at)

	_, err := s.service.(*subscriptionService).detachAddon(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: free,
		ProrationBehavior:  types.ProrationBehaviorNone,
	}, sub.ID)
	s.Require().NoError(err, "only the gated association is protected")
}

// The provider call is the one step outside the transaction. When it fails, the inert state it
// left behind must go — and the removals must still be untouched.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_ProviderFailure_ArchivesPendingAndLeavesRemovalsAlone() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pff_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pff_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pff_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	// No payment provider is configured in this suite, so the session creation fails.
	_, err := s.modificationService().Execute(ctx, sub.ID, s.addonsCheckoutRequest(&dto.SubModifyBulkAddonParams{
		Adds:    []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_pff_in", at)},
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().Error(err)

	// The in-memory association store only applies the published/archived filter inside its
	// time-window branch, so the archived row still lists; assert on its status.
	assocFilter := types.NewNoLimitAddonAssociationFilter()
	assocFilter.EntityIDs = []string{sub.ID}
	assocFilter.AddonIDs = []string{"addon_pff_in"}
	live, listErr := s.GetStores().AddonAssociationRepo.List(ctx, assocFilter)
	s.Require().NoError(listErr)
	s.Require().Len(live, 1)
	s.Equal(types.StatusArchived, live[0].Status,
		"the pending attach must not outlive the failed provider call")

	kept, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.Equal(types.AddonStatusActive, kept.AddonStatus)
	s.Nil(kept.EndDate, "a failed gated batch must not have removed anything")

	s.Empty(s.addonLineItemsFor(sub.ID, "addon_pff_in"))
	s.Empty(s.prorationCredits())
}

// A batch that owes the customer money has nothing to collect, so checkout is ignored and the
// change applies immediately.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_NetNotPositive_AppliesImmediately() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfz_out", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfz_in", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_pfz_out", 90)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	resp, err := s.modificationService().Execute(ctx, sub.ID, s.addonsCheckoutRequest(&dto.SubModifyBulkAddonParams{
		Adds:    []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_pfz_in", at)},
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().NoError(err)

	s.Nil(resp.CheckoutSession, "there is nothing to collect, so no session is opened")
	s.Len(s.addonLineItemsFor(sub.ID, "addon_pfz_in"), 1, "the change applied immediately")

	removed, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, outgoing)
	s.Require().NoError(err)
	s.NotNil(removed.EndDate)
}

// A second payment-gated change cannot start while one is outstanding.
func (s *SubscriptionServiceSuite) TestAddonsCheckout_ConcurrentGuard() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_pfq_in", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_pfq_second", decimal.NewFromInt(90), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	s.seedPayFirstAddonBatchCheckout("addon_pfq_in", "", at)

	_, err := s.modificationService().Execute(ctx, sub.ID, s.addonsCheckoutRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_pfq_second", at)},
	}))
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "got %v", err)

	s.Empty(s.addonLineItemsFor(sub.ID, "addon_pfq_second"))
	assocFilter := types.NewNoLimitAddonAssociationFilter()
	assocFilter.EntityIDs = []string{sub.ID}
	assocFilter.AddonIDs = []string{"addon_pfq_second"}
	blocked, err := s.GetStores().AddonAssociationRepo.List(ctx, assocFilter)
	s.Require().NoError(err)
	s.Empty(blocked, "a batch rejected under the lock rolls back before writing anything")
}
