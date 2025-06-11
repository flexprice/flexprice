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
		// For now, apply during trial. This could be configurable per grant
		return StateActionApply, "trial_active"

	case types.SubscriptionStatusPastDue:
		return StateActionDefer, "subscription_past_due"

	case types.SubscriptionStatusUnpaid:
		return StateActionDefer, "subscription_unpaid"

	case types.SubscriptionStatusCancelled:
		return StateActionCancel, "subscription_cancelled"

	case types.SubscriptionStatusIncomplete:
		return StateActionDefer, "subscription_incomplete"

	case types.SubscriptionStatusIncompleteExpired:
		return StateActionCancel, "subscription_incomplete_expired"

	case types.SubscriptionStatusPaused:
		return StateActionDefer, "subscription_paused"

	default:
		return StateActionSkip, "unknown_subscription_status"
	}
}
