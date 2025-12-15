package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CustomerOnboardingWorkflowInput represents the input for the customer onboarding workflow
type CustomerOnboardingWorkflowInput struct {
	CustomerID     string         `json:"customer_id" validate:"required"`
	TenantID       string         `json:"tenant_id" validate:"required"`
	EnvironmentID  string         `json:"environment_id" validate:"required"`
	UserID         string         `json:"user_id" validate:"required"`
	WorkflowConfig WorkflowConfig `json:"workflow_config" validate:"required"`
}

// Validate validates the customer onboarding workflow input
func (c *CustomerOnboardingWorkflowInput) Validate() error {
	if c.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation)
	}
	if c.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Please provide a valid tenant ID").
			Mark(ierr.ErrValidation)
	}
	if c.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Please provide a valid environment ID").
			Mark(ierr.ErrValidation)
	}
	if c.UserID == "" {
		return ierr.NewError("user_id is required").
			WithHint("Please provide a valid user ID").
			Mark(ierr.ErrValidation)
	}
	return c.WorkflowConfig.Validate()
}

// CustomerOnboardingWorkflowResult represents the result of the customer onboarding workflow
type CustomerOnboardingWorkflowResult struct {
	CustomerID      string                           `json:"customer_id"`
	Status          string                           `json:"status"`
	CompletedAt     time.Time                        `json:"completed_at"`
	ActionsExecuted int                              `json:"actions_executed"`
	Results         []CustomerOnboardingActionResult `json:"results"`
	ErrorSummary    *string                          `json:"error_summary,omitempty"`
}

// CustomerOnboardingActionResult represents the result of a single action in the workflow
type CustomerOnboardingActionResult struct {
	ActionType   WorkflowAction `json:"action_type"`
	ActionIndex  int            `json:"action_index"`
	Status       string         `json:"status"`
	ResourceID   string         `json:"resource_id,omitempty"`
	ResourceType string         `json:"resource_type,omitempty"`
	Error        *string        `json:"error,omitempty"`
}

// CreateWalletActivityInput represents the input for the create wallet activity
type CreateWalletActivityInput struct {
	CustomerID    string                    `json:"customer_id" validate:"required"`
	TenantID      string                    `json:"tenant_id" validate:"required"`
	EnvironmentID string                    `json:"environment_id" validate:"required"`
	UserID        string                    `json:"user_id" validate:"required"`
	WalletConfig  *CreateWalletActionConfig `json:"wallet_config" validate:"required"`
}

// Validate validates the create wallet activity input
func (c *CreateWalletActivityInput) Validate() error {
	if c.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation)
	}
	if c.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Please provide a valid tenant ID").
			Mark(ierr.ErrValidation)
	}
	if c.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Please provide a valid environment ID").
			Mark(ierr.ErrValidation)
	}
	if c.UserID == "" {
		return ierr.NewError("user_id is required").
			WithHint("Please provide a valid user ID").
			Mark(ierr.ErrValidation)
	}
	if c.WalletConfig == nil {
		return ierr.NewError("wallet_config is required").
			WithHint("Please provide wallet configuration").
			Mark(ierr.ErrValidation)
	}
	return c.WalletConfig.Validate()
}

// CreateWalletActivityResult represents the result of the create wallet activity
type CreateWalletActivityResult struct {
	WalletID       string             `json:"wallet_id"`
	CustomerID     string             `json:"customer_id"`
	Currency       string             `json:"currency"`
	ConversionRate string             `json:"conversion_rate"`
	WalletType     types.WalletType   `json:"wallet_type"`
	Status         types.WalletStatus `json:"status"`
}

// CreateSubscriptionActivityInput represents the input for the create subscription activity
type CreateSubscriptionActivityInput struct {
	CustomerID         string                          `json:"customer_id" validate:"required"`
	TenantID           string                          `json:"tenant_id" validate:"required"`
	EnvironmentID      string                          `json:"environment_id" validate:"required"`
	UserID             string                          `json:"user_id" validate:"required"`
	SubscriptionConfig *CreateSubscriptionActionConfig `json:"subscription_config" validate:"required"`
}

// Validate validates the create subscription activity input
func (c *CreateSubscriptionActivityInput) Validate() error {
	if c.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation)
	}
	if c.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Please provide a valid tenant ID").
			Mark(ierr.ErrValidation)
	}
	if c.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Please provide a valid environment ID").
			Mark(ierr.ErrValidation)
	}
	if c.UserID == "" {
		return ierr.NewError("user_id is required").
			WithHint("Please provide a valid user ID").
			Mark(ierr.ErrValidation)
	}
	if c.SubscriptionConfig == nil {
		return ierr.NewError("subscription_config is required").
			WithHint("Please provide subscription configuration").
			Mark(ierr.ErrValidation)
	}
	return c.SubscriptionConfig.Validate()
}

// CreateSubscriptionActivityResult represents the result of the create subscription activity
type CreateSubscriptionActivityResult struct {
	SubscriptionID string                   `json:"subscription_id"`
	CustomerID     string                   `json:"customer_id"`
	PlanID         string                   `json:"plan_id"`
	Currency       string                   `json:"currency"`
	Status         types.SubscriptionStatus `json:"status"`
	BillingCycle   types.BillingCycle       `json:"billing_cycle"`
	StartDate      *time.Time               `json:"start_date"`
}
