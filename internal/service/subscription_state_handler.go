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
		return StateActionApply, "subscription is active"

	case types.SubscriptionStatusTrialing:
		// Check if grant should apply during trial period
		if h.shouldApplyDuringTrial() {
			return StateActionApply, "subscription is in trial and credits are enabled for trial period"
		}
		return StateActionDefer, "subscription is in trial and credits are not enabled for trial period"

	case types.SubscriptionStatusPastDue:
		// For past due, we defer rather than skip to allow recovery
		return StateActionDefer, "subscription is past due, deferring until payment is resolved"

	case types.SubscriptionStatusUnpaid:
		// Similar to past due - defer for potential recovery
		return StateActionDefer, "subscription is unpaid, deferring until payment is resolved"

	case types.SubscriptionStatusCancelled:
		// Cancelled subscriptions should not receive new credits
		return StateActionCancel, "subscription is cancelled"

	case types.SubscriptionStatusIncomplete:
		// Defer incomplete subscriptions as they might become active
		return StateActionDefer, "subscription is incomplete, deferring until completion"

	case types.SubscriptionStatusIncompleteExpired:
		// Expired incomplete subscriptions should be cancelled
		return StateActionCancel, "subscription is incomplete and expired"

	case types.SubscriptionStatusPaused:
		// Paused subscriptions should defer credits until resumed
		return StateActionDefer, "subscription is paused, deferring until resumed"

	default:
		// Unknown status - skip for safety
		return StateActionSkip, "unknown subscription status: " + string(h.subscription.SubscriptionStatus)
	}
}

// shouldApplyDuringTrial determines if credits should be applied during trial period
// This could be configurable per grant in the future
func (h *SubscriptionStateHandler) shouldApplyDuringTrial() bool {
	// For now, apply credits during trial unless explicitly configured otherwise
	// Future enhancement: Add trial_credits_enabled field to CreditGrant
	return true
}
