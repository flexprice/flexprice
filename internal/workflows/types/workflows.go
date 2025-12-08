package workflows

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type WorkflowType string

const (
	WorkflowTypeCustomerOnboarding WorkflowType = "customer_onboarding"
)

type WorkflowAction string

const (
	WorkflowActionCreateSubscription WorkflowAction = "create_subscription"
	WorkflowActionCreateWallet       WorkflowAction = "create_wallet"
)

type WorkflowConfig struct {
	WorkflowType WorkflowType           `json:"workflow_type" binding:"required"`
	Actions      []WorkflowActionConfig `json:"actions" binding:"required"`
}

func (c *WorkflowConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	for _, action := range c.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// WorkflowActionConfig is a union type that can represent any workflow action
// Instead of using an interface, we use a struct with all possible fields
// The "action" field determines which fields are valid
type WorkflowActionConfig struct {
	// Type discriminator - determines which action type this is
	Action WorkflowAction `json:"action" binding:"required"`

	// Fields for create_wallet action
	Currency       string          `json:"currency,omitempty"`
	ConversionRate decimal.Decimal `json:"conversion_rate,omitempty"`

	// Fields for create_subscription action
	PlanID        string `json:"plan_id,omitempty"`
	PlanLookupKey string `json:"plan_lookup_key,omitempty"`
	BillingCycle  string `json:"billing_cycle,omitempty"`
	StartDate     string `json:"start_date,omitempty"` // ISO 8601 date string
}

// Validate validates the workflow action config based on its action type
func (c *WorkflowActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	switch c.Action {
	case WorkflowActionCreateWallet:
		if c.Currency == "" {
			return ierr.NewError("currency is required for create_wallet action").
				WithHint("Please provide a currency").
				Mark(ierr.ErrValidation)
		}
		return nil

	case WorkflowActionCreateSubscription:
		if c.PlanID == "" && c.PlanLookupKey == "" {
			return ierr.NewError("plan_id or plan_lookup_key is required for create_subscription action").
				WithHint("Please provide either plan_id or plan_lookup_key").
				Mark(ierr.ErrValidation)
		}
		return nil

	default:
		return ierr.NewErrorf("unknown action type: %s", c.Action).
			WithHint("Please provide a valid action type").
			WithReportableDetails(map[string]any{
				"action": c.Action,
				"allowed": []WorkflowAction{
					WorkflowActionCreateWallet,
					WorkflowActionCreateSubscription,
				},
			}).
			Mark(ierr.ErrValidation)
	}
}

// GetAction returns the action type
func (c *WorkflowActionConfig) GetAction() WorkflowAction {
	return c.Action
}
