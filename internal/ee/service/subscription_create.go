package service

import (
	"context"
	"slices"
	"strings"
	"time"

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
		return ierr.Validation("subscription is required to activate a draft create")
	}
	if err := s.validatePaymentLifecycle(ctx, paymentLifecycleRequest{subscriptionID: sub.ID, asOf: time.Now().UTC()}); err != nil {
		return err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	return s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		discovered, err := s.lifecycleSubscription(tx, sub.ID)
		if err != nil {
			return err
		}
		current, participants, err := s.lockPaymentSubscriptions(tx, discovered, nested)
		if err != nil {
			return err
		}
		if current.SubscriptionStatus == types.SubscriptionStatusActive {
			return nil
		}
		if !checkoutDraftEligible(current, time.Now().UTC()) {
			return ierr.NewError("checkout subscription is no longer an eligible draft").Mark(ierr.ErrVersionConflict)
		}
		var activated []*subscription.Subscription
		for _, participant := range participants {
			if participant.ID != current.ID && lo.FromPtr(participant.ParentSubscriptionID) != current.ID {
				continue
			}
			if !checkoutDraftEligible(participant, time.Now().UTC()) {
				continue
			}
			updated := subscription.NewSubscriptionBuilder(participant).WithStatus(types.SubscriptionStatusActive).Build()
			if err := s.SubRepo.Update(activity.WithAction(tx, updated.ID, activity.SubscriptionActivated), updated); err != nil {
				return err
			}
			activated = append(activated, updated)
		}
		notify := func() {
			detached := types.WithoutDBTransaction(context.WithoutCancel(tx))
			detached, cancel := context.WithTimeout(detached, 10*time.Second)
			defer cancel()
			for _, owner := range activated {
				if owner.ID == sub.ID {
					*sub = *owner
				}
				if err := s.processPendingCreditGrantsForSubscription(detached, owner); err != nil {
					s.Logger.Error(detached, "checkout activation grants deferred to recovery", "error", err, "subscription_id", owner.ID)
				}
				s.publishSystemEvent(detached, types.WebhookEventSubscriptionActivated, owner.ID)
				NotifySubscriptionChanged(detached, s.ServiceParams, owner.ID)
			}
		}
		if !types.RegisterPostCommit(tx, notify) {
			return ierr.NewError("checkout activation requires an active postcommit queue").Mark(ierr.ErrInvalidOperation)
		}
		return nil
	})
}

func checkoutDraftEligible(sub *subscription.Subscription, asOf time.Time) bool {
	return sub.SubscriptionStatus == types.SubscriptionStatusDraft && sub.PauseStatus != types.PauseStatusActive && sub.ActivePauseID == nil && sub.CancelledAt == nil &&
		(sub.EndDate == nil || sub.EndDate.After(asOf)) && (sub.CancelAt == nil || sub.CancelAt.After(asOf)) && (!sub.CancelAtPeriodEnd || sub.CurrentPeriodEnd.After(asOf))
}

func (s *subscriptionService) archiveDraftCheckoutSubscription(ctx context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return nil
	}
	if err := s.validateSubscriptionLifecycle(ctx, subscriptionID); err != nil {
		return err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	return s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		discovered, err := s.lifecycleSubscription(tx, subscriptionID)
		if err != nil {
			return err
		}
		current, participants, err := s.lockPaymentSubscriptions(tx, discovered, nested)
		if err != nil {
			return err
		}
		if current.SubscriptionStatus != types.SubscriptionStatusDraft {
			return ierr.NewError("checkout subscription is no longer draft").WithHint("subscription was already activated").Mark(ierr.ErrAlreadyExists)
		}
		var drafts []*subscription.Subscription
		for _, participant := range participants {
			if participant.ID != current.ID && lo.FromPtr(participant.ParentSubscriptionID) != current.ID {
				continue
			}
			// A grouped seat can begin its trial before the parent's checkout completes.
			if participant.SubscriptionStatus != types.SubscriptionStatusDraft && participant.SubscriptionStatus != types.SubscriptionStatusTrialing {
				return ierr.NewError("checkout child has left its initial draft or trial").Mark(ierr.ErrVersionConflict)
			}
			drafts = append(drafts, participant)
		}
		for _, draft := range drafts {
			if err := s.archiveDraftSubscriptionDependencies(tx, draft.ID); err != nil {
				return err
			}
		}
		for _, draft := range drafts {
			if err := s.SubRepo.Delete(tx, draft.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *subscriptionService) archiveDraftSubscriptionDependencies(ctx context.Context, subscriptionID string) error {
	if err := s.archiveDraftCommitments(ctx, subscriptionID); err != nil {
		return err
	}
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

func (s *subscriptionService) archiveDraftCommitments(ctx context.Context, subscriptionID string) error {
	commitments := newCommitmentService(s.ServiceParams)
	ctx, err := commitments.lockCollectionOwner(ctx, subscriptionID)
	if err != nil {
		return err
	}
	if err := commitments.lockCommitmentKey(ctx, "commitments:"+subscriptionID); err != nil {
		return err
	}
	rows, err := s.CommitmentRepo.List(ctx, &commitment.Filter{
		QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(501), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionIDs: []string{subscriptionID},
	})
	if err != nil {
		return err
	}
	if len(rows) > 500 {
		return ierr.Validation("checkout cleanup supports at most 500 commitments")
	}
	for _, owner := range rows {
		if owner == nil || !commitmentTermsScope(ctx, owner.TenantID, owner.EnvironmentID, owner.Status) || owner.SubscriptionID != subscriptionID {
			return ierr.NewError("checkout cleanup returned invalid commitment scope").Mark(ierr.ErrDatabase)
		}
	}
	slices.SortFunc(rows, func(a, b *commitment.Commitment) int { return strings.Compare(a.ID, b.ID) })
	for _, owner := range rows {
		if err := commitments.lockCommitment(ctx, owner.ID); err != nil {
			return err
		}
		if commitmentUsesTerms(owner) {
			if s.BillingTerms == nil {
				return ierr.NewError("checkout commitment cleanup is unavailable").Mark(ierr.ErrServiceUnavailable)
			}
			state, err := s.BillingTerms.Get(ctx, commitmentBillingOwner(owner.ID))
			if err != nil {
				return err
			}
			if state.Summary().AssignedAmount.IsPositive() {
				return ierr.NewError("checkout commitment already has assigned invoice principal").Mark(ierr.ErrVersionConflict)
			}
			if _, _, err := commitments.settleCommitment(ctx, owner.ID, owner.StartDate, nil, true); err != nil {
				return err
			}
		}
		if err := s.CommitmentRepo.Delete(ctx, owner.ID); err != nil {
			return err
		}
	}
	return nil
}
