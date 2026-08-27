package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// AttachAddon attaches an addon and settles the proration it raises.
func (s *subscriptionService) AttachAddon(
	ctx context.Context,
	sub *subscription.Subscription,
	req *dto.AddAddonToSubscriptionRequest,
	checkout *dto.CheckoutParams,
) (*dto.AddonChangeResult, error) {
	if req.PreviewOnly {
		return s.previewAttachAddon(ctx, sub, req)
	}

	if checkout != nil {
		if err := checkout.Validate(); err != nil {
			return nil, err
		}

		if len(req.OverrideLineItems) > 0 || len(req.LineItemCommitments) > 0 {
			return nil, ierr.NewError("override_line_items and line_item_commitments are not supported with checkout").
				WithHint("Attach without checkout to use price overrides or line item commitments").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"addon_id":        req.AddonID,
				}).
				Mark(ierr.ErrValidation)
		}

		if sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return nil, ierr.NewError("subscription status does not allow a payment-gated addon attach").
				WithHint("Checkout is only supported for active subscriptions").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":     sub.ID,
					"subscription_status": sub.SubscriptionStatus,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	params, err := s.createAddonAttachParams(ctx, sub, req, nil)
	if err != nil {
		return nil, err
	}

	if checkout != nil {
		summary, err := s.calculateAddonProration(ctx, params)
		if err != nil {
			return nil, err
		}

		if summary.TotalChargeAmount.GreaterThan(decimal.Zero) {
			resp, err := s.settleAddAddonPayFirst(ctx, params, summary, checkout)
			if err != nil {
				return nil, err
			}

			return &dto.AddonChangeResult{
				Association:     resp.AddonAssociation,
				CheckoutSession: resp.CheckoutSession,
				Invoice:         resp.Invoice,
			}, nil
		}
		// Zero or negative net → nothing to collect, so fall through and attach immediately.
	}

	if err := s.persistAddonAttach(ctx, params); err != nil {
		return nil, err
	}

	return &dto.AddonChangeResult{
		Association:      params.getAssociation(),
		CreatedLineItems: params.getLineItems(),
		ChangedInvoices:  s.settleAddonAttach(ctx, params),
		EffectiveDate:    params.getEffectiveDate(),
	}, nil
}

// previewAttachAddon quotes an attach without writing anything.
func (s *subscriptionService) previewAttachAddon(
	ctx context.Context,
	sub *subscription.Subscription,
	req *dto.AddAddonToSubscriptionRequest,
) (*dto.AddonChangeResult, error) {
	params, err := s.createAddonAttachParams(ctx, sub, req, nil)
	if err != nil {
		return nil, err
	}

	summary, err := s.calculateAddonProration(ctx, params)
	if err != nil {
		return nil, err
	}

	changedInvoices, err := s.previewAddonSettlement(ctx, sub, summary, params.getEffectiveDate())
	if err != nil {
		return nil, err
	}

	return &dto.AddonChangeResult{
		Association:      params.getAssociation(),
		CreatedLineItems: params.getLineItems(),
		ChangedInvoices:  changedInvoices,
		EffectiveDate:    params.getEffectiveDate(),
	}, nil
}

// addonAttachProrationRequest builds the attach's proration request, or nil when there is
// nothing to prorate, so neither preview nor settlement has to repeat the condition.
func (s *subscriptionService) addonAttachProrationRequest(
	ctx context.Context,
	params *addonAttachParams,
) (*LineItemProrationRequest, error) {
	behavior := params.getRequest().ProrationBehavior
	if behavior != types.ProrationBehaviorCreateProrations {
		return nil, nil
	}

	entries, err := s.buildAddonProrationEntries(ctx, params.getLineItems(), types.ProrationActionAddItem)
	if err != nil {
		return nil, err
	}

	return &LineItemProrationRequest{
		Subscription:   params.getSubscription(),
		Entries:        entries,
		EffectiveDate:  params.getEffectiveDate(),
		Behavior:       behavior,
		IdempotencyKey: params.prorationIdempotencyKey(),
	}, nil
}

func (s *subscriptionService) calculateAddonProration(
	ctx context.Context,
	params *addonAttachParams,
) (*LineItemProrationSummary, error) {
	req, err := s.addonAttachProrationRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return emptyProrationSummary(params.getSubscription()), nil
	}

	return NewLineItemProrationService(s.ServiceParams).Compute(ctx, *req)
}

func (s *subscriptionService) settleAddonAttach(
	ctx context.Context,
	params *addonAttachParams,
) []dto.ChangedInvoice {
	logFailure := func(cause error) {
		s.Logger.Error(ctx, "failed to create proration invoice for addon add; addon was persisted and is UNBILLED for this period",
			"error", cause,
			"association_id", params.getAssociation().ID,
			"addon_id", params.getRequest().AddonID,
			"subscription_id", params.getSubscription().ID,
			"effective_date", params.getEffectiveDate(),
			"idempotency_key", params.prorationIdempotencyKey(),
		)
	}

	prorationReq, err := s.addonAttachProrationRequest(ctx, params)
	if err != nil {
		logFailure(err)
		return nil
	}
	if prorationReq == nil {
		return nil
	}

	settled, err := NewLineItemProrationService(s.ServiceParams).Apply(ctx, *prorationReq)
	if err != nil {
		logFailure(err)
		return nil
	}

	return settled
}

func (s *subscriptionService) settleAddAddonPayFirst(
	ctx context.Context,
	params *addonAttachParams,
	summary *LineItemProrationSummary,
	checkout *dto.CheckoutParams,
) (*dto.AddAddonToSubscriptionResponse, error) {
	if params == nil || summary == nil || checkout == nil {
		return nil, ierr.NewError("pay-first addon attach requires a plan, proration and checkout").
			Mark(ierr.ErrValidation)
	}

	sub := params.getSubscription()
	req := params.getRequest()

	checkoutParams := &types.AddAddonParams{
		SubscriptionID: sub.ID,
		Addons: []types.AddAddonRef{{
			AssociationID:     params.getAssociation().ID,
			AddonID:           req.AddonID,
			Cadence:           req.Cadence,
			ProrationBehavior: req.ProrationBehavior,
			StartDate:         params.getRequestedStart(),
		}},
	}
	if err := checkoutParams.Validate(); err != nil {
		return nil, err
	}

	existing, err := s.getAnyPendingAddonCheckoutSession(ctx, sub.CustomerID, sub.ID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, ierr.NewError("a pending checkout session already exists for this subscription").
			WithHint("Complete or cancel the existing checkout before starting another payment-gated change").
			WithReportableDetails(map[string]any{
				"subscription_id":     sub.ID,
				"checkout_session_id": existing[0].ID,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	pendingAssociation := params.getAssociation()
	pendingAssociation.AddonStatus = types.AddonStatusPending
	if err := s.AddonAssociationRepo.Create(ctx, pendingAssociation); err != nil {
		return nil, err
	}

	draftInvoice, err := s.createAddonProrationDraftInvoice(ctx, params, summary)
	if err != nil {
		s.archivePendingAddonAssociation(ctx, pendingAssociation, err)
		return nil, err
	}

	checkoutSvc := NewCheckoutSessionService(s.ServiceParams)
	sessionResp, err := checkoutSvc.StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: sub.CustomerID,
		Action:     types.CheckoutActionAddAddon,
		Configuration: types.CheckoutConfiguration{
			AddAddonParams: checkoutParams,
		},
		DraftInvoice: &draftInvoice.Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		s.archivePendingAddonAssociation(ctx, pendingAssociation, err)
		return nil, err
	}

	latestInvoice, invErr := NewInvoiceService(s.ServiceParams).GetInvoice(ctx, draftInvoice.ID)
	if invErr != nil {
		latestInvoice = draftInvoice
	}

	return &dto.AddAddonToSubscriptionResponse{
		AddonAssociation: pendingAssociation,
		CheckoutSession:  sessionResp,
		Invoice:          latestInvoice,
	}, nil
}

// createAddonProrationDraftInvoice locks the charge on a DRAFT ONE_OFF using the same request
// builder the pay-later charge uses, so the amount the customer is asked to pay is exactly the
// amount pay-later would have billed.
func (s *subscriptionService) createAddonProrationDraftInvoice(
	ctx context.Context,
	params *addonAttachParams,
	summary *LineItemProrationSummary,
) (*dto.InvoiceResponse, error) {
	if !summary.TotalChargeAmount.GreaterThan(decimal.Zero) || len(summary.ChargeLineItems) == 0 {
		return nil, ierr.NewError("no proration charge to collect via checkout").
			WithHint("Expected a positive proration charge").
			Mark(ierr.ErrValidation)
	}

	req := buildLineItemProrationChargeInvoiceRequest(
		params.getSubscription(),
		summary,
		params.getEffectiveDate(),
		params.prorationIdempotencyKey(),
	)

	inv, skipped, err := NewInvoiceService(s.ServiceParams).CreateComputedDraftInvoice(ctx, req)
	if err != nil {
		s.Logger.Error(ctx, "failed to create draft proration invoice for payment-gated addon attach",
			"error", err,
			"subscription_id", params.getSubscription().ID,
			"association_id", params.getAssociation().ID,
		)
		return nil, err
	}
	if skipped {
		return nil, ierr.NewError("draft invoice was skipped").
			WithHint("Expected a non-zero invoice amount").
			WithReportableDetails(map[string]any{"invoice_id": inv.GetId()}).
			Mark(ierr.ErrValidation)
	}

	return inv, nil
}

func (s *subscriptionService) getAnyPendingAddonCheckoutSession(
	ctx context.Context,
	customerID string,
	subscriptionID string,
) ([]*domainCheckout.CheckoutSession, error) {
	filter := &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
		CustomerIDs: []string{customerID},
		Actions: []types.CheckoutAction{
			types.CheckoutActionModifySubscription,
			types.CheckoutActionAddAddon,
		},
		CheckoutStatuses: []types.CheckoutStatus{
			types.CheckoutStatusInitiated,
			types.CheckoutStatusPending,
		},
		Configuration: &types.CheckoutConfigurationFilter{SubscriptionID: subscriptionID},
	}
	filter.Limit = lo.ToPtr(1)

	return s.CheckoutSessionRepo.List(ctx, filter)
}

func (s *subscriptionService) archivePendingAddonAssociation(
	ctx context.Context,
	association *addonassociation.AddonAssociation,
	cause error,
) {
	if err := s.AddonAssociationRepo.Delete(ctx, association.ID); err != nil {
		s.Logger.Error(ctx, "failed to archive pending addon association after pay-first failure",
			"error", err,
			"association_id", association.ID,
			"original_error", cause,
		)
	}
}

// applyAddAddonParams activates the pending associations a completed checkout session gated.
//
// The charge is NOT recomputed — it is already locked on the session's draft invoice, and
// settling is finalizeCheckoutInvoiceAndPayment's job. Credit-grant proration is not
// suppressed, so grants scale exactly as they would have pay-later.
func (s *subscriptionService) applyAddAddonCheckoutParams(ctx context.Context, params *types.AddAddonParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, params.SubscriptionID)
	if err != nil {
		return err
	}
	sub.LineItems = lineItems

	for _, ref := range params.Addons {
		if err := s.applyAddAddonRef(ctx, sub, ref); err != nil {
			return err
		}
	}

	return nil
}

func (s *subscriptionService) applyAddAddonRef(
	ctx context.Context,
	sub *subscription.Subscription,
	ref types.AddAddonRef,
) error {
	association, err := s.AddonAssociationRepo.GetByID(ctx, ref.AssociationID)
	if err != nil {
		return err
	}

	switch association.AddonStatus {
	case types.AddonStatusActive:
		s.Logger.Info(ctx, "addon association already active, skipping checkout replay",
			"association_id", association.ID,
			"addon_id", ref.AddonID,
			"subscription_id", sub.ID,
		)
		return nil
	case types.AddonStatusPending:
		// The state we expect; fall through and activate.
	default:
		return ierr.NewError("addon association is not pending activation").
			WithHint("The addon was cancelled or removed while its checkout was outstanding").
			WithReportableDetails(map[string]any{
				"association_id":  association.ID,
				"addon_id":        ref.AddonID,
				"subscription_id": sub.ID,
				"addon_status":    association.AddonStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	req := &dto.AddAddonToSubscriptionRequest{
		AddonID:              ref.AddonID,
		Cadence:              ref.Cadence,
		StartDate:            lo.ToPtr(ref.StartDate),
		ProrationBehavior:    ref.ProrationBehavior,
		Metadata:             association.Metadata,
		SkipEntityValidation: true,
	}

	params, err := s.createAddonAttachParams(ctx, sub, req, association)
	if err != nil {
		return err
	}

	return s.persistAddonAttach(ctx, params)
}

// DetachAddon removes an addon and credits back the unused prepaid time it paid for.
func (s *subscriptionService) DetachAddon(
	ctx context.Context,
	req *dto.RemoveAddonRequest,
	subscriptionId string,
) (*dto.AddonChangeResult, error) {
	params, err := s.createAddonDetachParams(ctx, req, subscriptionId)
	if err != nil {
		return nil, err
	}

	if req.PreviewOnly {
		summary, err := s.calculateAddonDetachProration(ctx, params)
		if err != nil {
			return nil, err
		}

		changedInvoices, err := s.previewAddonSettlement(
			ctx, params.getSubscription(), summary, params.getEffectiveDate(),
		)
		if err != nil {
			return nil, err
		}

		// The cancelled association persistAddonDetach would write, built but not saved.
		cancelled := addonassociation.NewAddonAssociationBuilder(params.getAssociation()).
			WithCancellation(params.getEffectiveDate(), params.getReason()).
			Build()

		return &dto.AddonChangeResult{
			Association:     cancelled,
			EndedLineItems:  params.getLineItems(),
			ChangedInvoices: changedInvoices,
			EffectiveDate:   params.getEffectiveDate(),
		}, nil
	}

	if err := s.persistAddonDetach(ctx, params); err != nil {
		return nil, err
	}

	return &dto.AddonChangeResult{
		Association:     params.getAssociation(),
		EndedLineItems:  params.getLineItems(),
		ChangedInvoices: s.settleAddonDetach(ctx, params),
		EffectiveDate:   params.getEffectiveDate(),
	}, nil
}

// previewAddonSettlement quotes what Apply would raise, through the same invoice request builder
// and the same two independent branches, so preview and execute cannot drift.
func (s *subscriptionService) previewAddonSettlement(
	ctx context.Context,
	sub *subscription.Subscription,
	summary *LineItemProrationSummary,
	effectiveDate time.Time,
) ([]dto.ChangedInvoice, error) {
	quoted := make([]dto.ChangedInvoice, 0, 2)

	if summary.TotalChargeAmount.GreaterThan(decimal.Zero) && len(summary.ChargeLineItems) > 0 {
		inv, err := NewInvoiceService(s.ServiceParams).CreatePreviewInvoice(
			ctx, buildLineItemProrationChargeInvoiceRequest(sub, summary, effectiveDate, ""),
		)
		if err != nil {
			return nil, err
		}

		quoted = append(quoted, dto.ChangedInvoice{
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusPreview,
			Invoice: inv,
		})
	}

	if summary.TotalCreditAmount.GreaterThan(decimal.Zero) {
		quoted = append(quoted, walletCreditChangedInvoice(&dto.WalletTransactionResponse{
			Transaction: &wallet.Transaction{
				CustomerID:        sub.GetInvoicingCustomerID(),
				Amount:            summary.TotalCreditAmount,
				Currency:          sub.Currency,
				TransactionReason: types.TransactionReasonSubscriptionCredit,
			},
		}, dto.ChangedInvoiceStatusPreview))
	}

	return quoted, nil
}

// createAddonDetachParams resolves everything a removal needs — validations, the association,
// the line items still to close and the effective date — and writes NOTHING.
func (s *subscriptionService) createAddonDetachParams(
	ctx context.Context,
	req *dto.RemoveAddonRequest,
	subscriptionId string,
) (*addonDetachParams, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	association, err := s.AddonAssociationRepo.GetByID(ctx, req.AddonAssociationID)
	if err != nil {
		return nil, err
	}

	if subscriptionId != "" && association.EntityID != subscriptionId {
		return nil, ierr.NewError("addon association does not belong to this subscription").
			WithHint("The addon association belongs to a different subscription").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"subscription_id":      subscriptionId,
			}).
			Mark(ierr.ErrValidation)
	}

	if association.AddonStatus == types.AddonStatusPending {
		return nil, ierr.NewError("addon attach is pending payment").
			WithHint("Complete or cancel the pending checkout for this addon first").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"addon_id":             association.AddonID,
			}).
			Mark(ierr.ErrValidation)
	}

	if association.EndDate != nil {
		return nil, ierr.NewError("addon is already scheduled to be removed").
			WithHint("This addon is already marked for removal").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"end_date":             association.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	lineItemFilter := types.NewSubscriptionLineItemFilter()
	lineItemFilter.SubscriptionIDs = []string{association.EntityID}
	lineItemFilter.EntityIDs = []string{association.AddonID}
	lineItemFilter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)
	lineItemFilter.AddonAssociationIDs = []string{association.ID}

	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		return nil, err
	}

	// Onetime addons have EndDate set on ALL their line items — they are already scheduled to end.
	// We check ALL items: if any item has no EndDate (recurring), the addon is cancellable.
	// This handles the case where a previous association was cancelled at period-end (EndDate set)
	// while a new recurring association was added on top (EndDate zero).
	var onetimeEndDate time.Time
	allOnetime := len(lineItems) > 0
	for _, li := range lineItems {
		if li.EndDate.IsZero() {
			allOnetime = false
			break
		}
		onetimeEndDate = li.EndDate
	}
	if allOnetime {
		return nil, ierr.NewError("addon is already scheduled to end").
			WithHintf("This addon is already scheduled to end at %s", onetimeEndDate.Format("2 Jan 2006")).
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"expires_at":           onetimeEndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Keep only line items that are NOT already scheduled to end.
	// Line items from a previous association cancelled at period-end have EndDate set
	// and must be excluded — they are already handled and must not be re-processed.
	var activeLineItems []*subscription.SubscriptionLineItem
	for _, li := range lineItems {
		if li.EndDate.IsZero() {
			activeLineItems = append(activeLineItems, li)
		}
	}

	sub, err := s.SubRepo.Get(ctx, association.EntityID)
	if err != nil {
		return nil, err
	}

	effectiveEndDate := sub.CurrentPeriodEnd
	if req.EffectiveDate != nil {
		effectiveEndDate = *req.EffectiveDate
		if effectiveEndDate.Before(sub.CurrentPeriodStart) || effectiveEndDate.After(sub.CurrentPeriodEnd) {
			return nil, ierr.NewError("effective_date is outside the current billing period").
				WithHint("effective_date must be between the subscription's current period start and end").
				WithReportableDetails(map[string]any{
					"effective_date":       effectiveEndDate,
					"current_period_start": sub.CurrentPeriodStart,
					"current_period_end":   sub.CurrentPeriodEnd,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return &addonDetachParams{
		subscription:  sub,
		association:   association,
		lineItems:     activeLineItems,
		effectiveDate: effectiveEndDate,
		behavior:      req.ProrationBehavior,
		reason:        req.Reason,
	}, nil
}

func (s *subscriptionService) addonDetachProrationRequest(
	ctx context.Context,
	params *addonDetachParams,
) (*LineItemProrationRequest, error) {
	if params.getBehavior() != types.ProrationBehaviorCreateProrations {
		return nil, nil
	}

	entries, err := s.buildAddonProrationEntries(ctx, params.getLineItems(), types.ProrationActionRemoveItem)
	if err != nil {
		return nil, err
	}

	return &LineItemProrationRequest{
		Subscription:   params.getSubscription(),
		Entries:        entries,
		EffectiveDate:  params.getEffectiveDate(),
		Behavior:       params.getBehavior(),
		Reason:         params.getReason(),
		IdempotencyKey: params.prorationIdempotencyKey(),
	}, nil
}

func (s *subscriptionService) calculateAddonDetachProration(
	ctx context.Context,
	params *addonDetachParams,
) (*LineItemProrationSummary, error) {
	req, err := s.addonDetachProrationRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return emptyProrationSummary(params.getSubscription()), nil
	}

	return NewLineItemProrationService(s.ServiceParams).Compute(ctx, *req)
}

// persistAddonDetach cancels the association, ends its line items and stops future credit grants
// in one transaction. It raises no credit — that is settleAddonDetach's job.
func (s *subscriptionService) persistAddonDetach(ctx context.Context, params *addonDetachParams) error {
	association := addonassociation.NewAddonAssociationBuilder(params.getAssociation()).
		WithCancellation(params.getEffectiveDate(), params.getReason()).
		Build()

	if err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.AddonAssociationRepo.Update(ctx, association); err != nil {
			return err
		}

		deleteReq := dto.DeleteSubscriptionLineItemRequest{EffectiveFrom: lo.ToPtr(params.getEffectiveDate())}
		for _, lineItem := range params.getLineItems() {
			if _, err := s.deleteSubscriptionLineItem(ctx, lineItem.ID, deleteReq); err != nil {
				return err
			}
		}

		// End the entitlement grant windows this addon owns on its own slots. Pooled
		// (additive) windows survive the detach — see closeGrantsForRemovedECs.
		addonEnts, err := NewEntitlementService(s.ServiceParams).GetAddonEntitlements(ctx, association.AddonID)
		if err != nil {
			return err
		}
		addonECs := dto.ToEntitlements(addonEnts)
		if err := s.handleGrantsForRemovedECs(ctx, params.getSubscription(), addonECs, params.getEffectiveDate(), grantProrationSourceAddonDetach); err != nil {
			return err
		}

		// Cancel future applications of credit grants materialized from THIS addon only
		// (scoped by addon_id provenance). Already-granted credits are not clawed back;
		// plan-sourced and other-addon grants are left untouched.
		creditGrantService := NewCreditGrantService(s.ServiceParams)
		return creditGrantService.CancelFutureSubscriptionGrants(ctx, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: association.EntityID,
			AddonID:        lo.ToPtr(association.AddonID),
			EffectiveDate:  lo.ToPtr(params.getEffectiveDate()),
		})
	}); err != nil {
		return err
	}

	params.association = association
	return nil
}

func (s *subscriptionService) settleAddonDetach(
	ctx context.Context,
	params *addonDetachParams,
) []dto.ChangedInvoice {
	logFailure := func(cause error) {
		association := params.getAssociation()
		s.Logger.Error(ctx, "failed to issue proration credit for addon remove; removal was persisted and the credit is UNISSUED",
			"error", cause,
			"association_id", association.ID,
			"addon_id", association.AddonID,
			"subscription_id", association.EntityID,
		)
	}

	prorationReq, err := s.addonDetachProrationRequest(ctx, params)
	if err != nil {
		logFailure(err)
		return nil
	}
	if prorationReq == nil {
		return nil
	}

	settled, err := NewLineItemProrationService(s.ServiceParams).Apply(ctx, *prorationReq)
	if err != nil {
		logFailure(err)
		return nil
	}

	return settled
}
