package service

import (
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type StateAction string

const (
	StateActionApply  StateAction = "apply"
	StateActionSkip   StateAction = "skip"
	StateActionDefer  StateAction = "defer"
	StateActionCancel StateAction = "cancel"
)

func NewSubscriptionStateHandler(subscription *subscription.Subscription, grant *creditgrant.CreditGrant) *SubscriptionStateHandler {
	return &SubscriptionStateHandler{
		subscription: subscription,
		grant:        grant,
	}
}

type SubscriptionStateHandler struct {
	subscription *subscription.Subscription
	grant        *creditgrant.CreditGrant
}

func (h *SubscriptionStateHandler) DetermineCreditGrantAction() (StateAction, error) {
	switch h.subscription.SubscriptionStatus {
	case types.SubscriptionStatusActive:
		return StateActionApply, nil

	case types.SubscriptionStatusTrialing:
		// Check if grant should apply during trial period
		if h.shouldApplyDuringTrial() {
			return StateActionApply, nil
		}
		return StateActionDefer, nil

	case types.SubscriptionStatusPastDue:
		// For past due, we defer rather than skip to allow recovery
		return StateActionDefer, nil

	case types.SubscriptionStatusUnpaid:
		// Similar to past due - defer for potential recovery
		return StateActionDefer, nil

	case types.SubscriptionStatusCancelled:
		// Cancelled subscriptions should not receive new credits
		return StateActionCancel, nil

	case types.SubscriptionStatusIncomplete:
		// Defer incomplete subscriptions as they might become active
		return StateActionDefer, nil

	case types.SubscriptionStatusIncompleteExpired:
		// Expired incomplete subscriptions should be cancelled
		return StateActionCancel, nil

	case types.SubscriptionStatusPaused:
		// Paused subscriptions should skip credits until resumed
		return StateActionSkip, nil

	default:
		// Unknown status - skip for safety
		return StateActionSkip, ierr.NewError("unknown subscription status").
			WithHint("Please provide a valid subscription status").
			WithReportableDetails(map[string]interface{}{
				"subscription_status": h.subscription.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}
}

// shouldApplyDuringTrial determines if credits should be applied during trial period
// This could be configurable per grant in the future
func (h *SubscriptionStateHandler) shouldApplyDuringTrial() bool {
	// For now, apply credits during trial unless explicitly configured otherwise
	// Future enhancement: Add trial_credits_enabled field to CreditGrant
	return true
}
