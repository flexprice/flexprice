package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
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
		if archErr := s.archiveDraftCheckoutSubscription(ctx, sub.ID); archErr != nil {
			s.Logger.Error(ctx, "failed to archive draft checkout subscription", "error", archErr, "subscription_id", sub.ID)
		}
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
		if archErr := s.archiveDraftCheckoutSubscription(ctx, sub.ID); archErr != nil {
			s.Logger.Error(ctx, "failed to archive draft checkout subscription", "error", archErr, "subscription_id", sub.ID)
		}
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
		if err := s.SubRepo.Update(activity.WithAction(ctx, sub.ID, activity.SubscriptionActivated), sub); err != nil {
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
	NotifySubscriptionChanged(ctx, s.ServiceParams, sub.ID)
	return nil
}

func (s *subscriptionService) archiveDraftCheckoutSubscription(ctx context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return nil
	}

	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusDraft {
		return ierr.NewError("checkout subscription is no longer draft").
			WithHint("subscription was already activated").
			Mark(ierr.ErrAlreadyExists)
	}

	children, err := s.childSubscriptions(ctx, subscriptionID)
	if err != nil {
		return err
	}

	for _, child := range children {
		if err := s.archiveDraftSubscriptionDependencies(ctx, child.ID); err != nil {
			return err
		}
		if err := s.SubRepo.Delete(ctx, child.ID); err != nil {
			return err
		}
	}

	if err := s.archiveDraftSubscriptionDependencies(ctx, subscriptionID); err != nil {
		return err
	}
	return s.SubRepo.Delete(ctx, subscriptionID)
}

func (s *subscriptionService) archiveDraftSubscriptionDependencies(ctx context.Context, subscriptionID string) error {
	notifySubscriptionArchived(ctx, s.ServiceParams, subscriptionID)

	addonFilter := types.NewNoLimitAddonAssociationFilter()
	addonFilter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
	addonFilter.EntityIDs = []string{subscriptionID}
	associations, err := s.AddonAssociationRepo.List(ctx, addonFilter)
	if err != nil {
		return err
	}
	for _, association := range associations {
		if err := s.AddonAssociationRepo.Delete(ctx, association.ID); err != nil {
			return err
		}
	}

	couponFilter := &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
	}
	couponAssociations, err := s.CouponAssociationRepo.List(ctx, couponFilter)
	if err != nil {
		return err
	}
	for _, association := range couponAssociations {
		if err := s.CouponAssociationRepo.Delete(ctx, association.ID); err != nil {
			return err
		}
	}

	// Applications first: the credit-grant cron reads applications, so leaving them behind after
	// archiving their grant is the one ordering that could still credit a wallet.
	applicationFilter := types.NewNoLimitCreditGrantApplicationFilter()
	applicationFilter.SubscriptionIDs = []string{subscriptionID}
	applications, err := s.CreditGrantApplicationRepo.List(ctx, applicationFilter)
	if err != nil {
		return err
	}
	for _, application := range applications {
		if err := s.CreditGrantApplicationRepo.Delete(ctx, application); err != nil {
			return err
		}
	}

	grantFilter := types.NewNoLimitCreditGrantFilter()
	grantFilter.SubscriptionIDs = []string{subscriptionID}
	grants, err := s.CreditGrantRepo.List(ctx, grantFilter)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if err := s.CreditGrantRepo.Delete(ctx, grant.ID); err != nil {
			return err
		}
	}

	commitmentFilter := commitment.NewNoLimitFilter()
	commitmentFilter.SubscriptionIDs = []string{subscriptionID}
	commitments, err := s.CommitmentRepo.List(ctx, commitmentFilter)
	if err != nil {
		return err
	}
	for _, c := range commitments {
		if err := s.CommitmentRepo.Delete(ctx, c.ID); err != nil {
			return err
		}
	}

	taxFilter := &types.TaxAssociationFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    subscriptionID,
	}
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, taxFilter)
	if err != nil {
		return err
	}
	for _, association := range taxAssociations {
		if err := s.TaxAssociationRepo.Delete(ctx, association); err != nil {
			return err
		}
	}
	return nil
}
