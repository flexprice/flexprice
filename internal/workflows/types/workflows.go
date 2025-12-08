package workflows

import (
	"encoding/json"

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

// UnmarshalJSON implements custom JSON unmarshaling to handle interface types
func (c *WorkflowConfig) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to get the raw data
	var temp struct {
		WorkflowType WorkflowType      `json:"workflow_type"`
		Actions      []json.RawMessage `json:"actions"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to unmarshal workflow config").
			Mark(ierr.ErrValidation)
	}

	c.WorkflowType = temp.WorkflowType
	c.Actions = make([]WorkflowActionConfig, 0, len(temp.Actions))

	// Unmarshal each action based on its "action" field
	for _, actionData := range temp.Actions {
		// First, check what type of action this is
		var actionType struct {
			Action WorkflowAction `json:"action"`
		}
		if err := json.Unmarshal(actionData, &actionType); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to determine action type").
				Mark(ierr.ErrValidation)
		}

		// Unmarshal into the appropriate concrete type
		var action WorkflowActionConfig
		switch actionType.Action {
		case WorkflowActionCreateWallet:
			var walletAction CreateWalletActionConfig
			if err := json.Unmarshal(actionData, &walletAction); err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to unmarshal create_wallet action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &walletAction

		case WorkflowActionCreateSubscription:
			var subAction CreateSubscriptionActionConfig
			if err := json.Unmarshal(actionData, &subAction); err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to unmarshal create_subscription action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &subAction

		default:
			return ierr.NewErrorf("unknown action type: %s", actionType.Action).
				WithHint("Please provide a valid action type").
				WithReportableDetails(map[string]any{
					"action": actionType.Action,
					"allowed": []WorkflowAction{
						WorkflowActionCreateWallet,
						WorkflowActionCreateSubscription,
					},
				}).
				Mark(ierr.ErrValidation)
		}

		c.Actions = append(c.Actions, action)
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling to include action type discriminator
func (c *WorkflowConfig) MarshalJSON() ([]byte, error) {
	if c == nil {
		return json.Marshal(nil)
	}

	// Initialize actionsData with proper capacity, handling nil Actions slice
	actionsLen := 0
	if c.Actions != nil {
		actionsLen = len(c.Actions)
	}
	actionsData := make([]json.RawMessage, 0, actionsLen)

	// Only iterate if Actions is not nil
	if c.Actions != nil {
		for _, action := range c.Actions {
			// Marshal the action to JSON
			actionJSON, err := json.Marshal(action)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to marshal action").
					Mark(ierr.ErrValidation)
			}
			actionsData = append(actionsData, actionJSON)
		}
	}

	// Create the final structure
	result := struct {
		WorkflowType WorkflowType      `json:"workflow_type"`
		Actions      []json.RawMessage `json:"actions"`
	}{
		WorkflowType: c.WorkflowType,
		Actions:      actionsData,
	}

	return json.Marshal(result)
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

// WorkflowActionConfig is an interface for workflow action configurations
type WorkflowActionConfig interface {
	Validate() error
	GetAction() WorkflowAction
}

// CreateWalletActionConfig represents configuration for creating a wallet action
type CreateWalletActionConfig struct {
	Action         WorkflowAction  `json:"action"` // Type discriminator - automatically set to "create_wallet"
	Currency       string          `json:"currency" binding:"required"`
	ConversionRate decimal.Decimal `json:"conversion_rate" default:"1"`
}

func (c *CreateWalletActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	if c.Currency == "" {
		return ierr.NewError("currency is required for create_wallet action").
			WithHint("Please provide a currency").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (c *CreateWalletActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateWallet
}

// CreateSubscriptionActionConfig represents configuration for creating a subscription action
type CreateSubscriptionActionConfig struct {
	Action        WorkflowAction `json:"action"` // Type discriminator - automatically set to "create_subscription"
	PlanID        string         `json:"plan_id,omitempty"`
	PlanLookupKey string         `json:"plan_lookup_key,omitempty"`
	BillingCycle  string         `json:"billing_cycle,omitempty"`
	StartDate     string         `json:"start_date,omitempty"` // ISO 8601 date string
}

func (c *CreateSubscriptionActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	// At least one of plan_id or plan_lookup_key must be provided
	if c.PlanID == "" && c.PlanLookupKey == "" {
		return ierr.NewError("plan_id or plan_lookup_key is required for create_subscription action").
			WithHint("Please provide either plan_id or plan_lookup_key").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (c *CreateSubscriptionActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateSubscription
}
