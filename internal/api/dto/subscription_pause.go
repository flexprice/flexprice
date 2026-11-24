package dto

import (
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// PauseSubscriptionRequest pauses an active subscription immediately.
// @Description Pauses an active subscription now. No invoice is raised: the period keeps its one invoice, billed at the period end or at the cancellation. Usage is billed for the time the subscription was active, and fixed charges for the paused days follow proration_behavior.
type PauseSubscriptionRequest struct {
	// ProrationBehavior decides whether this pause's days are taken off the fixed charges. Defaults to none.
	// @Description none bills fixed charges for the paused days. create_prorations takes the paused days off arrear fixed charges and credits them from advance fixed charges to the wallet when the period's invoice is finalized.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// PauseGracePeriodDays cancels the subscription if it is not resumed within these days.
	// @Description Must not go past the current period end. When omitted, the pause_grace_period_days of the subscription_config setting is used, capped at the period end, and without it the subscription is cancelled at the current period end if it is still paused.
	// @Example 15
	PauseGracePeriodDays *int `json:"pause_grace_period_days,omitempty" validate:"omitempty,min=1"`

	// Reason for pausing the subscription
	// @Description Optional reason for the pause. Maximum 255 characters
	// @Example "Customer requested temporary suspension"
	Reason string `json:"reason,omitempty" validate:"omitempty,max=255"`

	// Metadata stored on the pause
	// @Example {"requested_by": "customer", "channel": "support_ticket"}
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the pause subscription request
func (r *PauseSubscriptionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if r.ProrationBehavior != "" {
		return r.ProrationBehavior.Validate()
	}
	return nil
}

// ResumeSubscriptionRequest resumes a paused subscription immediately.
// @Description Resumes a paused subscription now. No invoice is raised: the period's invoice carries on and is billed at the period end.
type ResumeSubscriptionRequest struct {
	// Metadata merged into the pause
	// @Example {"resumed_by": "admin"}
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the resume subscription request
func (r *ResumeSubscriptionRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// SubscriptionPauseResponse represents a subscription pause in API responses
// @Description Response object containing subscription pause information
type SubscriptionPauseResponse struct {
	*subscription.SubscriptionPause
}

// PauseSubscriptionResponse is returned by the pause endpoint
// @Description The paused subscription and its pause
type PauseSubscriptionResponse struct {
	Subscription *SubscriptionResponse      `json:"subscription"`
	Pause        *SubscriptionPauseResponse `json:"pause"`
}

// ResumeSubscriptionResponse is returned by the resume endpoint
// @Description The resumed subscription and the pause it ended
type ResumeSubscriptionResponse struct {
	Subscription *SubscriptionResponse      `json:"subscription"`
	Pause        *SubscriptionPauseResponse `json:"pause"`
}

// ListSubscriptionPausesResponse represents a list of subscription pauses in API responses
// @Description Response object for listing subscription pauses with total count
type ListSubscriptionPausesResponse struct {
	// List of subscription pause objects
	// @Description Array of subscription pauses
	Items []*SubscriptionPauseResponse `json:"items"`

	// Total number of pauses
	// @Description Total count of subscription pauses in the response
	Total int `json:"total"`
}

// NewListSubscriptionPausesResponse creates a new list subscription pauses response
func NewListSubscriptionPausesResponse(pauses []*subscription.SubscriptionPause) *ListSubscriptionPausesResponse {
	items := make([]*SubscriptionPauseResponse, len(pauses))
	for i, pause := range pauses {
		items[i] = &SubscriptionPauseResponse{
			SubscriptionPause: pause,
		}
	}

	return &ListSubscriptionPausesResponse{
		Items: items,
		Total: len(items),
	}
}
