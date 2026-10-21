package dto

import (
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// PauseSubscriptionRequest pauses an active subscription immediately.
// @Description Pauses an active subscription now. Usage and arrear fixed charges up to the pause are billed on a closing invoice.
type PauseSubscriptionRequest struct {
	// ProrationBehavior controls proration on the closing invoice. Defaults to none.
	// @Description none bills arrear fixed charges for the full period and gives no credit. create_prorations bills arrear fixed charges for the used part of the period and credits the unused part of advance fixed charges to the wallet.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

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
// @Description Resumes a paused subscription now. The billing anchor is kept, so the first period runs from the resume time to the next billing date.
type ResumeSubscriptionRequest struct {
	// ProrationBehavior, when sent, is saved on the subscription and prices the period that starts at the resume.
	// @Description none bills fixed charges of the first period at the full price. create_prorations bills them for the remaining part of the period. When omitted, the subscription's current proration_behavior is used.
	ProrationBehavior *types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// Metadata merged into the pause
	// @Example {"resumed_by": "admin"}
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the resume subscription request
func (r *ResumeSubscriptionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if r.ProrationBehavior != nil {
		return r.ProrationBehavior.Validate()
	}
	return nil
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
