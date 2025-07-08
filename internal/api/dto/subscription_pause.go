package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PauseSubscriptionRequest represents a request to pause a subscription
type PauseSubscriptionRequest struct {
	PauseMode  types.PauseMode   `json:"pause_mode" validate:"required"`
	PauseStart *time.Time        `json:"pause_start,omitempty" validate:"omitempty"`
	PauseEnd   *time.Time        `json:"pause_end,omitempty" validate:"omitempty,gtfield=PauseStart"`
	PauseDays  *int              `json:"pause_days,omitempty" validate:"omitempty,gt=0"`
	Reason     string            `json:"reason,omitempty" validate:"omitempty,max=255"`
	DryRun     bool              `json:"dry_run,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
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
type PauseSubscriptionResponse struct {
	Subscription  *SubscriptionResponse       `json:"subscription,omitempty"`
	Pause         *SubscriptionPauseResponse  `json:"pause,omitempty"`
	BillingImpact *types.BillingImpactDetails `json:"billing_impact"`
	DryRun        bool                        `json:"dry_run"`
}

// ResumeSubscriptionRequest represents a request to resume a subscription
type ResumeSubscriptionRequest struct {
	ResumeMode types.ResumeMode  `json:"resume_mode" validate:"required"`
	DryRun     bool              `json:"dry_run,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Validate validates the resume subscription request
func (r *ResumeSubscriptionRequest) Validate() error {
	if err := r.ResumeMode.Validate(); err != nil {
		return err
	}

	return nil
}

// SubscriptionPauseResponse represents a subscription pause in API responses
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
type ResumeSubscriptionResponse struct {
	// Only included if not a dry run
	Subscription *SubscriptionResponse      `json:"subscription,omitempty"`
	Pause        *SubscriptionPauseResponse `json:"pause,omitempty"`

	// Billing impact details
	BillingImpact *types.BillingImpactDetails `json:"billing_impact"`

	// Whether this was a dry run
	DryRun bool `json:"dry_run"`
}

// ListSubscriptionPausesResponse represents a list of subscription pauses in API responses
type ListSubscriptionPausesResponse struct {
	Items []*SubscriptionPauseResponse `json:"items"`
	Total int                          `json:"total"`
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
