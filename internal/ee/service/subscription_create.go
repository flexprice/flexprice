package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// startCreateSubscriptionCheckout opens the checkout session that gates activation. The amount is
// already locked on the draft invoice, so completion never re-prices.
func (s *subscriptionService) startCreateSubscriptionCheckout(
	ctx context.Context,
	response *dto.SubscriptionResponse,
	invResp *dto.InvoiceResponse,
	checkout *dto.CheckoutParams,
) error {
	sub := response.Subscription

	checkoutParams := &types.CreateSubscriptionParams{SubscriptionID: sub.ID}
	if err := checkoutParams.Validate(); err != nil {
		s.archiveDraftCheckoutSubscription(ctx, sub.ID)
		return err
	}

	checkoutSvc := NewCheckoutSessionService(s.ServiceParams)
	sessionResp, err := checkoutSvc.StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: sub.CustomerID,
		Action:     types.CheckoutActionCreateSubscription,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: checkoutParams,
		},
		DraftInvoice: &invResp.Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		s.archiveDraftCheckoutSubscription(ctx, sub.ID)
		return err
	}

	latestInvoice, invErr := NewInvoiceService(s.ServiceParams).GetInvoice(ctx, invResp.ID)
	if invErr != nil {
		latestInvoice = invResp
	}

	response.LatestInvoice = latestInvoice
	response.CheckoutSession = sessionResp
	return nil
}

func (s *subscriptionService) childSubscriptions(ctx context.Context, parentID string, statuses ...types.SubscriptionStatus) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.ParentSubscriptionIDs = []string{parentID}
	filter.SubscriptionTypes = []types.SubscriptionType{
		types.SubscriptionTypeInherited,
		types.SubscriptionTypeGroupedInvoicing,
	}
	filter.SubscriptionStatus = statuses

	return s.SubRepo.List(ctx, filter)
}

func (s *subscriptionService) activateDraftSubscription(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return ierr.NewError("subscription is required to activate a draft create").
			Mark(ierr.ErrValidation)
	}

	if sub.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.childSubscriptions(ctx, sub.ID, types.SubscriptionStatusDraft)
		if err != nil {
			return err
		}

		for _, child := range children {
			if err := s.activateDraftSubscription(ctx, child); err != nil {
				return err
			}
		}
	}

	if sub.SubscriptionStatus == types.SubscriptionStatusDraft {
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		if err := s.SubRepo.Update(ctx, sub); err != nil {
			return err
		}
	}

	if err := s.processPendingCreditGrantsForSubscription(ctx, sub); err != nil {
		s.Logger.Error(ctx, "failed to process pending credit grants for checkout-activated subscription",
			"error", err,
			"subscription_id", sub.ID,
		)
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionActivated, sub.ID)
	return nil
}

func (s *subscriptionService) archiveDraftCheckoutSubscription(ctx context.Context, subscriptionID string) {
	if subscriptionID == "" {
		return
	}

	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		s.Logger.Error(ctx, "failed to load subscription for checkout cleanup",
			"error", err,
			"subscription_id", subscriptionID,
		)
		return
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusDraft {
		s.Logger.Info(ctx, "checkout subscription is no longer draft, skipping archival",
			"subscription_id", subscriptionID,
			"subscription_status", sub.SubscriptionStatus,
		)
		return
	}

	// Every status: the parent is still draft, so nothing here was ever paid for, whatever
	// status its children resolved to.
	children, err := s.childSubscriptions(ctx, subscriptionID)
	if err != nil {
		s.Logger.Error(ctx, "failed to list children for checkout cleanup, leaving the group intact",
			"error", err,
			"subscription_id", subscriptionID,
		)
		return
	}

	for _, child := range children {
		s.archiveDraftSubscriptionDependencies(ctx, child.ID)
		if err := s.SubRepo.Delete(ctx, child.ID); err != nil {
			// Dependency archival was attempted for this child just above and
			// reports failures only through its own logs, so an unknown subset of
			// them is already gone. This child row survives without them, and the
			// parent and any remaining children are untouched.
			s.Logger.Error(ctx, "failed to archive child subscription, group cleanup attempted and may be partial",
				"error", err,
				"subscription_id", subscriptionID,
				"child_subscription_id", child.ID,
			)
			return
		}
	}

	s.archiveDraftSubscriptionDependencies(ctx, subscriptionID)

	if err := s.SubRepo.Delete(ctx, subscriptionID); err != nil {
		s.Logger.Error(ctx, "failed to archive draft checkout subscription",
			"error", err,
			"subscription_id", subscriptionID,
		)
	}
}

func (s *subscriptionService) archiveDraftSubscriptionDependencies(ctx context.Context, subscriptionID string) {
	addonFilter := types.NewNoLimitAddonAssociationFilter()
	addonFilter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
	addonFilter.EntityIDs = []string{subscriptionID}
	if associations, err := s.AddonAssociationRepo.List(ctx, addonFilter); err != nil {
		s.Logger.Error(ctx, "failed to list addon associations for draft subscription cleanup",
			"error", err, "subscription_id", subscriptionID)
	} else {
		for _, association := range associations {
			if err := s.AddonAssociationRepo.Delete(ctx, association.ID); err != nil {
				s.Logger.Error(ctx, "failed to archive addon association for draft subscription",
					"error", err, "subscription_id", subscriptionID, "association_id", association.ID)
			}
		}
	}

	couponFilter := &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
	}
	if associations, err := s.CouponAssociationRepo.List(ctx, couponFilter); err != nil {
		s.Logger.Error(ctx, "failed to list coupon associations for draft subscription cleanup",
			"error", err, "subscription_id", subscriptionID)
	} else {
		for _, association := range associations {
			if err := s.CouponAssociationRepo.Delete(ctx, association.ID); err != nil {
				s.Logger.Error(ctx, "failed to archive coupon association for draft subscription",
					"error", err, "subscription_id", subscriptionID, "association_id", association.ID)
			}
		}
	}

	// Applications first: the credit-grant cron reads applications, so leaving them behind after
	// archiving their grant is the one ordering that could still credit a wallet.
	applicationFilter := types.NewNoLimitCreditGrantApplicationFilter()
	applicationFilter.SubscriptionIDs = []string{subscriptionID}
	if applications, err := s.CreditGrantApplicationRepo.List(ctx, applicationFilter); err != nil {
		s.Logger.Error(ctx, "failed to list credit grant applications for draft subscription cleanup",
			"error", err, "subscription_id", subscriptionID)
	} else {
		for _, application := range applications {
			if err := s.CreditGrantApplicationRepo.Delete(ctx, application); err != nil {
				s.Logger.Error(ctx, "failed to archive credit grant application for draft subscription",
					"error", err, "subscription_id", subscriptionID, "application_id", application.ID)
			}
		}
	}

	grantFilter := types.NewNoLimitCreditGrantFilter()
	grantFilter.SubscriptionIDs = []string{subscriptionID}
	if grants, err := s.CreditGrantRepo.List(ctx, grantFilter); err != nil {
		s.Logger.Error(ctx, "failed to list credit grants for draft subscription cleanup",
			"error", err, "subscription_id", subscriptionID)
	} else {
		for _, grant := range grants {
			if err := s.CreditGrantRepo.Delete(ctx, grant.ID); err != nil {
				s.Logger.Error(ctx, "failed to archive credit grant for draft subscription",
					"error", err, "subscription_id", subscriptionID, "credit_grant_id", grant.ID)
			}
		}
	}

	taxFilter := &types.TaxAssociationFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    subscriptionID,
	}
	if associations, err := s.TaxAssociationRepo.List(ctx, taxFilter); err != nil {
		s.Logger.Error(ctx, "failed to list tax associations for draft subscription cleanup",
			"error", err, "subscription_id", subscriptionID)
	} else {
		for _, association := range associations {
			if err := s.TaxAssociationRepo.Delete(ctx, association); err != nil {
				s.Logger.Error(ctx, "failed to archive tax association for draft subscription",
					"error", err, "subscription_id", subscriptionID, "tax_association_id", association.ID)
			}
		}
	}
}
