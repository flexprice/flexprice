package service

import (
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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

func (h *SubscriptionStateHandler) DetermineAction() (StateAction, string) {
	switch h.subscription.SubscriptionStatus {
	case types.SubscriptionStatusActive:
		return StateActionApply, "subscription_active"

	case types.SubscriptionStatusTrialing:
		// Check if grant should apply during trial period
		if h.shouldApplyDuringTrial() {
			return StateActionApply, "trial_active_apply_enabled"
		}
		return StateActionDefer, "trial_active_apply_disabled"

	case types.SubscriptionStatusPastDue:
		// For past due, we defer rather than skip to allow recovery
		return StateActionDefer, "subscription_past_due"

	case types.SubscriptionStatusUnpaid:
		// Similar to past due - defer for potential recovery
		return StateActionDefer, "subscription_unpaid"

	case types.SubscriptionStatusCancelled:
		// Cancelled subscriptions should not receive new credits
		return StateActionCancel, "subscription_cancelled"

	case types.SubscriptionStatusIncomplete:
		// Defer incomplete subscriptions as they might become active
		return StateActionDefer, "subscription_incomplete"

	case types.SubscriptionStatusIncompleteExpired:
		// Expired incomplete subscriptions should be cancelled
		return StateActionCancel, "subscription_incomplete_expired"

	case types.SubscriptionStatusPaused:
		// Paused subscriptions should defer credits until resumed
		return StateActionDefer, "subscription_paused"

	default:
		// Unknown status - skip for safety
		return StateActionSkip, "unknown_subscription_status"
	}
}

// shouldApplyDuringTrial determines if credits should be applied during trial period
// This could be configurable per grant in the future
func (h *SubscriptionStateHandler) shouldApplyDuringTrial() bool {
	// For now, apply credits during trial unless explicitly configured otherwise
	// Future enhancement: Add trial_credits_enabled field to CreditGrant
	return true
}
