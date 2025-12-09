package models

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// Workflow types and actions - domain models
type WorkflowType string

const (
	WorkflowTypeCustomerOnboarding WorkflowType = "customer_onboarding"
)

type WorkflowAction string

const (
	WorkflowActionCreateSubscription WorkflowAction = "create_subscription"
	WorkflowActionCreateWallet       WorkflowAction = "create_wallet"
)

// WorkflowActionConfig is an interface for workflow action configurations
type WorkflowActionConfig interface {
	Validate() error
	GetAction() WorkflowAction
	// Convert to DTO using flexible parameters - implementations can type assert what they need
	ToDTO(params interface{}) (interface{}, error)
}

// WorkflowActionParams contains common parameters that actions might need
type WorkflowActionParams struct {
	CustomerID string
	Currency   string
	// Add more fields as needed for different action types
}

// WorkflowConfig represents a workflow configuration
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
		// Convert json.RawMessage to map[string]interface{}
		var actionMap map[string]interface{}
		if err := json.Unmarshal(actionData, &actionMap); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to unmarshal action data to map").
				Mark(ierr.ErrValidation)
		}

		// Get action type from map
		actionTypeStr, ok := actionMap["action"].(string)
		if !ok {
			return ierr.NewError("action field is required and must be a string").
				WithHint("Please provide a valid action type").
				Mark(ierr.ErrValidation)
		}
		actionType := WorkflowAction(actionTypeStr)

		// Use utils.ToStruct to unmarshal into the appropriate concrete type
		var action WorkflowActionConfig
		switch actionType {
		case WorkflowActionCreateWallet:
			walletAction, err := utils.ToStruct[CreateWalletActionConfig](actionMap)
			if err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to convert create_wallet action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &walletAction

		case WorkflowActionCreateSubscription:
			subAction, err := utils.ToStruct[CreateSubscriptionActionConfig](actionMap)
			if err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to convert create_subscription action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &subAction

		default:
			return ierr.NewErrorf("unknown action type: %s", actionType).
				WithHint("Please provide a valid action type").
				WithReportableDetails(map[string]any{
					"action": actionType,
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
			actionJSON, err := json.Marshal(action)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to marshal action to JSON").
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

func (c WorkflowConfig) Validate() error {
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

// ToDTO converts the action config directly to CreateWalletRequest DTO
func (c *CreateWalletActionConfig) ToDTO(params interface{}) (interface{}, error) {
	// Type assert to get the parameters we need
	actionParams, ok := params.(*WorkflowActionParams)
	if !ok {
		return nil, ierr.NewError("invalid parameters for create_wallet action").
			WithHint("Expected WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	// Set default conversion rate if not provided
	conversionRate := c.ConversionRate
	if conversionRate.IsZero() {
		conversionRate = decimal.NewFromInt(1)
	}

	return &dto.CreateWalletRequest{
		CustomerID:     actionParams.CustomerID,
		Currency:       c.Currency,
		ConversionRate: conversionRate,
	}, nil
}

// CreateSubscriptionActionConfig represents configuration for creating a subscription action
type CreateSubscriptionActionConfig struct {
	Action       WorkflowAction `json:"action"`
	PlanID       string         `json:"plan_id,omitempty"`
	BillingCycle string         `json:"billing_cycle,omitempty"`
}

func (c *CreateSubscriptionActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	if c.PlanID == "" {
		return ierr.NewError("plan_id is required for create_subscription action").
			WithHint("Please provide a plan_id").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (c *CreateSubscriptionActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateSubscription
}

// ToDTO converts the action config directly to CreateSubscriptionRequest DTO
func (c *CreateSubscriptionActionConfig) ToDTO(params interface{}) (interface{}, error) {
	// Type assert to get the parameters we need
	actionParams, ok := params.(*WorkflowActionParams)
	if !ok {
		return nil, ierr.NewError("invalid parameters for create_subscription action").
			WithHint("Expected WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	// Parse billing cycle - default to anniversary if not provided
	billingCycle := types.BillingCycleAnniversary
	if c.BillingCycle != "" {
		billingCycle = types.BillingCycle(c.BillingCycle)
		if err := billingCycle.Validate(); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid billing_cycle value").
				WithReportableDetails(map[string]interface{}{
					"billing_cycle": c.BillingCycle,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Default start date to current time
	now := time.Now().UTC()
	startDate := &now

	return &dto.CreateSubscriptionRequest{
		CustomerID:         actionParams.CustomerID,
		PlanID:             c.PlanID,
		Currency:           actionParams.Currency,
		StartDate:          startDate,
		BillingCadence:     types.BILLING_CADENCE_RECURRING, // Default to recurring
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,    // Default to monthly
		BillingPeriodCount: 1,                               // Default to 1
		BillingCycle:       billingCycle,
	}, nil
}
