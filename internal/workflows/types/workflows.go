package workflows

import (
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

type WorkflowActionConfig interface {
	Validate() error
	GetAction() WorkflowAction
}

type CreateWalletActionConfig struct {
	Currency       string          `json:"currency" binding:"required"`
	ConversionRate decimal.Decimal `json:"conversion_rate" default:"1"`
}

func (c *CreateWalletActionConfig) Validate() error {

	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	return nil
}

func (c *CreateWalletActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateWallet
}
