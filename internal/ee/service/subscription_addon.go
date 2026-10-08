package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// pendingAddAddonCheckoutSessions returns EVERY open addon checkout on the subscription.
// A scan for one association has to see all of them, not whichever row came back first.
func pendingAddAddonCheckoutSessions(
	ctx context.Context,
	sp ServiceParams,
	customerID string,
	subscriptionID string,
) ([]*domainCheckout.CheckoutSession, error) {
	return sp.CheckoutSessionRepo.List(ctx,
		pendingCheckoutSessionFilter(customerID, subscriptionID, types.CheckoutActionAddAddon))
}

// pendingCheckoutSessionForAssociation reports whether an outstanding checkout already gates this
// association's removal. In a mixed batch the association stays active while the session is open,
// so without this a concurrent detach would remove it and completion would remove it again.
func (s *subscriptionService) pendingCheckoutSessionForAssociation(
	ctx context.Context,
	sub *subscription.Subscription,
	associationID string,
) (bool, error) {
	sessions, err := pendingAddAddonCheckoutSessions(ctx, s.ServiceParams, sub.CustomerID, sub.ID)
	if err != nil {
		return false, err
	}

	for _, session := range sessions {
		cfg := session.Configuration.ToCheckoutConfiguration()
		if cfg.AddAddonParams == nil {
			continue
		}
		for _, ref := range cfg.AddAddonParams.Removes {
			if ref.AssociationID == associationID {
				return true, nil
			}
		}
	}

	return false, nil
}

// applyAddAddonCheckoutParams replays the change a completed checkout session gated: its
// removals are applied for the first time and its attaches are activated, as ONE change so a
// swap closes and reopens each shared feature's grant window once.
//
// The charge is NOT recomputed — it is already locked on the session's draft invoice, and
// settling is finalizeCheckoutInvoiceAndPayment's job. Credit-grant proration is not
// suppressed, so grants scale exactly as they would have pay-later.
func (s *subscriptionService) applyAddAddonCheckoutParams(ctx context.Context, params *types.AddAddonParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	return s.DB.WithTx(ctx, func(ctx context.Context) error {
		sub, err := s.loadSubscriptionForChange(ctx, params.SubscriptionID, true)
		if err != nil {
			return err
		}

		// The subscription can be cancelled while its checkout is outstanding; applying the
		// change then would attach addons to a dead subscription.
		if sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return ierr.NewError("subscription is no longer active").
				WithHint("The subscription was cancelled or paused while its checkout was outstanding").
				WithReportableDetails(map[string]any{
					"subscription_id":     sub.ID,
					"subscription_status": sub.SubscriptionStatus,
				}).
				Mark(ierr.ErrValidation)
		}

		req, err := s.replayAddonChangeRequest(ctx, sub, params)
		if err != nil {
			return err
		}
		if len(req.Adds) == 0 && len(req.Removes) == 0 {
			s.Logger.Info(ctx, "checkout replay already applied, nothing to do",
				"subscription_id", sub.ID,
			)
			return nil
		}

		changeSvc := NewAddonChangeService(s.ServiceParams)
		config, err := changeSvc.Resolve(ctx, req)
		if err != nil {
			return err
		}

		return changeSvc.Persist(ctx, config)
	})
}

// replayAddonChangeRequest rebuilds the gated change from the session payload, dropping the
// entries a previous completion already applied so a double-delivery is a no-op.
func (s *subscriptionService) replayAddonChangeRequest(
	ctx context.Context,
	sub *subscription.Subscription,
	params *types.AddAddonParams,
) (AddonChangeRequest, error) {
	ids := make([]string, 0, len(params.Addons)+len(params.Removes))
	for _, ref := range params.Addons {
		ids = append(ids, ref.AssociationID)
	}
	for _, ref := range params.Removes {
		ids = append(ids, ref.AssociationID)
	}

	associations, err := s.AddonAssociationRepo.GetByIDs(ctx, ids)
	if err != nil {
		return AddonChangeRequest{}, err
	}
	byID := lo.KeyBy(associations, func(a *addonassociation.AddonAssociation) string { return a.ID })

	req := AddonChangeRequest{Subscription: sub}

	for _, ref := range params.Removes {
		association, ok := byID[ref.AssociationID]
		if !ok {
			return AddonChangeRequest{}, ierr.NewError("addon association named by the checkout no longer exists").
				WithReportableDetails(map[string]any{"association_id": ref.AssociationID}).
				Mark(ierr.ErrNotFound)
		}

		// Already ended by a previous completion of this same session.
		if association.EndDate != nil {
			s.Logger.Info(ctx, "addon association already removed, skipping checkout replay",
				"association_id", association.ID,
				"subscription_id", sub.ID,
			)
			continue
		}

		req.Removes = append(req.Removes, &dto.RemoveAddonRequest{
			AddonAssociationID: ref.AssociationID,
			Reason:             ref.Reason,
			ProrationBehavior:  ref.ProrationBehavior,
			EffectiveDate:      lo.ToPtr(ref.EffectiveDate),
			// This completion IS the pending session, so its own removals must not trip the
			// guard that blocks concurrent detaches.
			SkipPendingCheckoutGuard: true,
		})
	}

	for _, ref := range params.Addons {
		association, ok := byID[ref.AssociationID]
		if !ok {
			return AddonChangeRequest{}, ierr.NewError("addon association named by the checkout no longer exists").
				WithReportableDetails(map[string]any{"association_id": ref.AssociationID}).
				Mark(ierr.ErrNotFound)
		}

		switch association.AddonStatus {
		case types.AddonStatusActive:
			s.Logger.Info(ctx, "addon association already active, skipping checkout replay",
				"association_id", association.ID,
				"addon_id", ref.AddonID,
				"subscription_id", sub.ID,
			)
			continue
		case types.AddonStatusPending:
			// The state we expect; fall through and activate.
		default:
			return AddonChangeRequest{}, ierr.NewError("addon association is not pending activation").
				WithHint("The addon was cancelled or removed while its checkout was outstanding").
				WithReportableDetails(map[string]any{
					"association_id":  association.ID,
					"addon_id":        ref.AddonID,
					"subscription_id": sub.ID,
					"addon_status":    association.AddonStatus,
				}).
				Mark(ierr.ErrValidation)
		}

		req.Adds = append(req.Adds, AddonAdd{
			Request: &dto.AddAddonToSubscriptionRequest{
				AddonID:           ref.AddonID,
				Cadence:           ref.Cadence,
				StartDate:         lo.ToPtr(ref.StartDate),
				ProrationBehavior: ref.ProrationBehavior,
				Metadata:          association.Metadata,
			},
			Existing: association,
		})
	}

	return req, nil
}

// createAddonDetachParams resolves everything a removal needs — validations, the association,
// the line items still to close and the effective date — and writes NOTHING.
func (s *subscriptionService) createAddonDetachParams(
	ctx context.Context,
	sub *subscription.Subscription,
	req *dto.RemoveAddonRequest,
) (*addonDetachParams, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	association, err := s.AddonAssociationRepo.GetByID(ctx, req.AddonAssociationID)
	if err != nil {
		return nil, err
	}

	if association.EntityID != sub.ID {
		return nil, ierr.NewError("addon association does not belong to this subscription").
			WithHint("The addon association belongs to a different subscription").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"subscription_id":      sub.ID,
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

	if !req.SkipPendingCheckoutGuard {
		gated, err := s.pendingCheckoutSessionForAssociation(ctx, sub, association.ID)
		if err != nil {
			return nil, err
		}
		if gated {
			return nil, ierr.NewError("addon removal is pending payment").
				WithHint("Complete or cancel the pending checkout for this subscription first").
				WithReportableDetails(map[string]interface{}{
					"addon_association_id": association.ID,
					"subscription_id":      sub.ID,
				}).
				Mark(ierr.ErrValidation)
		}
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
