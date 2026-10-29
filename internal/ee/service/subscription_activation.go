package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionService) ActivateDraftSubscription(ctx context.Context, id string, req dto.ActivateDraftSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.validateSubscriptionLifecycle(ctx, id); err != nil {
		return nil, err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	var response *dto.SubscriptionResponse
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		lifecycle, err := s.lockSubscriptionLifecycle(tx, id, nested)
		if err != nil {
			return err
		}
		tx = lifecycle.ctx
		current := lifecycle.current
		if !checkoutDraftEligible(current, time.Now().UTC()) {
			return ierr.NewError("subscription is not an eligible draft").WithHint("Only an unpaused draft that has not ended can be activated").Mark(ierr.ErrValidation)
		}
		_, lineItems, err := s.SubRepo.GetWithLineItems(tx, id)
		if err != nil {
			return err
		}
		current = subscription.NewSubscriptionBuilder(current).WithLineItems(lineItems).Build()
		response, err = s.activateLockedDraftSubscription(tx, current, req)
		return err
	})
	return response, err
}

func draftActivationStatus(sub *subscription.Subscription, invoice *dto.InvoiceResponse, trial bool) types.SubscriptionStatus {
	if sub.SubscriptionStatus != types.SubscriptionStatusDraft && sub.SubscriptionStatus != types.SubscriptionStatusIncomplete {
		return sub.SubscriptionStatus
	}
	if invoice != nil && invoice.PaymentStatus != types.PaymentStatusSucceeded {
		switch types.PaymentBehavior(sub.PaymentBehavior) {
		case types.PaymentBehaviorAllowIncomplete, types.PaymentBehaviorDefaultIncomplete, types.PaymentBehaviorErrorIfIncomplete:
			return types.SubscriptionStatusIncomplete
		default:
			return types.SubscriptionStatusActive
		}
	}
	if trial {
		return types.SubscriptionStatusTrialing
	}
	return types.SubscriptionStatusActive
}
