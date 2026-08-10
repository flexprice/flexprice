package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateSubscriptionPhaseRequest represents the request to create a subscription phase
type CreateSubscriptionPhaseRequest struct {
	// subscription_id is the identifier for the subscription
	SubscriptionID string `json:"subscription_id" validate:"required"`

	// start_date is when the phase starts (required)
	StartDate *time.Time `json:"start_date" validate:"required"`

	// end_date is when the phase ends (nil if phase is indefinite)
	EndDate *time.Time `json:"end_date,omitempty"`

	// metadata contains additional key-value pairs
	Metadata types.Metadata `json:"metadata,omitempty"`
}

// Validate validates the CreateSubscriptionPhaseRequest
func (r *CreateSubscriptionPhaseRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}

	if r.StartDate == nil {
		return ierr.NewError("start_date is required").
			WithHint("Please provide a valid start date for the subscription phase").
			Mark(ierr.ErrValidation)
	}

	if r.StartDate != nil && r.EndDate != nil && r.EndDate.Before(*r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("Ensure the phase end date is on or after the start date").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToSubscriptionPhase converts the request to a domain SubscriptionPhase
func (r *CreateSubscriptionPhaseRequest) ToSubscriptionPhase(ctx context.Context) *subscription.SubscriptionPhase {
	// StartDate is required, so it should never be nil after validation
	var startDate time.Time
	if r.StartDate != nil {
		startDate = *r.StartDate
	}

	return &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: r.SubscriptionID,
		StartDate:      startDate,
		EndDate:        r.EndDate,
		Metadata:       r.Metadata,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
}

// UpdateSubscriptionPhaseRequest represents the request to update a subscription phase
// Only metadata can be updated - start_date and end_date are immutable
type UpdateSubscriptionPhaseRequest struct {
	// metadata contains additional key-value pairs
	Metadata *types.Metadata `json:"metadata,omitempty"`
}

// Validate validates the UpdateSubscriptionPhaseRequest
func (r *UpdateSubscriptionPhaseRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// SubscriptionPhaseResponse represents the response for subscription phase operations
type SubscriptionPhaseResponse struct {
	*subscription.SubscriptionPhase
}

// ListSubscriptionPhasesResponse represents the response for listing subscription phases
type ListSubscriptionPhasesResponse = types.ListResponse[*SubscriptionPhaseResponse]
