package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PauseSubscriptionRequest represents a request to pause a subscription
// @Description Request object for pausing an active subscription with various pause modes and options
type PauseSubscriptionRequest struct {
	// Mode for pausing the subscription
	// @Description Determines when the pause takes effect. "immediate" pauses right away, "scheduled" pauses at a specified time
	// @Enum immediate,scheduled
	PauseMode types.PauseMode `json:"pause_mode" validate:"required"`

	// Start date for the subscription pause
	// @Description ISO 8601 timestamp when the pause should begin. Required when pause_mode is "scheduled"
	// @Example "2024-01-15T00:00:00Z"
	PauseStart *time.Time `json:"pause_start,omitempty" validate:"omitempty"`

	// End date for the subscription pause
	// @Description ISO 8601 timestamp when the pause should end. Cannot be used together with pause_days. Must be after pause_start
	// @Example "2024-02-15T00:00:00Z"
	PauseEnd *time.Time `json:"pause_end,omitempty" validate:"omitempty,gtfield=PauseStart"`

	// Duration of the pause in days
	// @Description Number of days to pause the subscription. Cannot be used together with pause_end. Must be greater than 0
	// @Example 30
	PauseDays *int `json:"pause_days,omitempty" validate:"omitempty,gt=0"`

	// Reason for pausing the subscription
	// @Description Optional reason for the pause. Maximum 255 characters
	// @Example "Customer requested temporary suspension"
	Reason string `json:"reason,omitempty" validate:"omitempty,max=255"`

	// Whether to perform a dry run
	// @Description If true, validates the request and shows impact without actually pausing the subscription
	// @Example false
	DryRun bool `json:"dry_run,omitempty"`

	// Additional metadata as key-value pairs
	// @Description Optional metadata for storing additional information about the pause
	// @Example {"requested_by": "customer", "channel": "support_ticket"}
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the pause subscription request
func (r *PauseSubscriptionRequest) Validate() error {
	if err := r.PauseMode.Validate(); err != nil {
		return err
	}

	if r.PauseMode == types.PauseModeScheduled && r.PauseStart == nil {
		return ierr.NewError("pause_start is required when pause_mode is scheduled").
			WithHint("Please provide a valid date to start the pause").
			Mark(ierr.ErrValidation)
	}

	if r.PauseEnd != nil && r.PauseDays != nil {
		return ierr.NewError("invalid pause parameters").
			WithHint("Cannot specify both pause end date and number of pause days").
			WithReportableDetails(map[string]any{
				"pauseEnd":  r.PauseEnd,
				"pauseDays": r.PauseDays,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.PauseDays != nil && *r.PauseDays <= 0 {
		return ierr.NewError("invalid pause parameters").
			WithHint("Number of pause days must be a positive integer").
			WithReportableDetails(map[string]any{
				"pauseDays": r.PauseDays,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.PauseEnd != nil && r.PauseEnd.Before(time.Now().UTC()) {
		return ierr.NewError("invalid pause parameters").
			WithHint("Pause end date must be in the future").
			WithReportableDetails(map[string]any{
				"pauseEnd": r.PauseEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// PauseSubscriptionResponse represents the response to a pause subscription request
// @Description Response object containing the subscription, pause details, and billing impact information
type PauseSubscriptionResponse struct {
	// The subscription that was paused
	// @Description Updated subscription object after the pause operation
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`

	// Details of the pause operation
	// @Description Information about the subscription pause that was created
	Pause *SubscriptionPauseResponse `json:"pause,omitempty"`

	// Impact on billing and charges
	// @Description Details about how this pause affects billing, prorations, and upcoming charges
	BillingImpact *types.BillingImpactDetails `json:"billing_impact"`

	// Whether this was a dry run
	// @Description Indicates if this was a simulation (true) or actual pause (false)
	DryRun bool `json:"dry_run"`
}

// ResumeSubscriptionRequest represents a request to resume a subscription
// @Description Request object for resuming a paused subscription
type ResumeSubscriptionRequest struct {
	// Mode for resuming the subscription
	// @Description Determines how the subscription should be resumed
	// @Enum immediate,scheduled
	ResumeMode types.ResumeMode `json:"resume_mode" validate:"required"`

	// Whether to perform a dry run
	// @Description If true, validates the request and shows impact without actually resuming the subscription
	// @Example false
	DryRun bool `json:"dry_run,omitempty"`

	// Additional metadata as key-value pairs
	// @Description Optional metadata for storing additional information about the resume operation
	// @Example {"resumed_by": "admin", "reason": "issue_resolved"}
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the resume subscription request
func (r *ResumeSubscriptionRequest) Validate() error {
	if err := r.ResumeMode.Validate(); err != nil {
		return err
	}

	return nil
}

// SubscriptionPauseResponse represents a subscription pause in API responses
// @Description Response object containing subscription pause information
type SubscriptionPauseResponse struct {
	*subscription.SubscriptionPause
}

// NewSubscriptionPauseResponse creates a new subscription pause response
func NewSubscriptionPauseResponse(sub *subscription.Subscription, pause *subscription.SubscriptionPause) *PauseSubscriptionResponse {
	if pause == nil {
		return nil
	}

	return &PauseSubscriptionResponse{
		Subscription: &SubscriptionResponse{
			Subscription: sub,
		},
		Pause: &SubscriptionPauseResponse{
			SubscriptionPause: pause,
		},
		DryRun: false,
	}
}

// ResumeSubscriptionResponse represents the response to a resume subscription request
// @Description Response object containing the subscription, pause details, and billing impact after resuming
type ResumeSubscriptionResponse struct {
	// The subscription that was resumed
	// @Description Updated subscription object after the resume operation (only included if not a dry run)
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`

	// Details of the pause that was ended
	// @Description Information about the subscription pause that was terminated
	Pause *SubscriptionPauseResponse `json:"pause,omitempty"`

	// Impact on billing and charges
	// @Description Details about how resuming affects billing, prorations, and upcoming charges
	BillingImpact *types.BillingImpactDetails `json:"billing_impact"`

	// Whether this was a dry run
	// @Description Indicates if this was a simulation (true) or actual resume (false)
	DryRun bool `json:"dry_run"`
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
