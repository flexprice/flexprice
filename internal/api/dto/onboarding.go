package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/go-playground/validator/v10"
)

// OnboardingEventsRequest represents the request to generate events for onboarding
type OnboardingEventsRequest struct {
	CustomerID     string `json:"customer_id" validate:"omitempty" example:"cus_01HKG8QWERTY123"`
	FeatureID      string `json:"feature_id" validate:"omitempty" example:"feat_01HKG8QWERTY123"`
	SubscriptionID string `json:"subscription_id" validate:"omitempty" example:"sub_01HKG8QWERTY123"`
	Duration       int    `json:"duration" validate:"omitempty,min=1,max=600" default:"60" example:"60"`
}

// Validate validates the OnboardingEventsRequest
func (r *OnboardingEventsRequest) Validate() error {
	// Either customer_id + feature_id OR subscription_id must be provided
	if (r.CustomerID == "" || r.FeatureID == "") && r.SubscriptionID == "" {
		return ierr.NewError("either customer_id + feature_id or subscription_id must be provided").
			WithHint("Please provide either a customer_id and feature_id or a subscription_id").
			Mark(ierr.ErrValidation)
	}

	// If both are provided, subscription_id takes precedence (we can add a warning log)
	return validator.New().Struct(r)
}

// OnboardingEventsResponse represents the response for onboarding events generation
type OnboardingEventsResponse struct {
	Message        string    `json:"message"`
	StartedAt      time.Time `json:"started_at"`
	Duration       int       `json:"duration"`
	Count          int       `json:"count"`
	CustomerID     string    `json:"customer_id"`
	FeatureID      string    `json:"feature_id,omitempty"`
	SubscriptionID string    `json:"subscription_id,omitempty"`
}
