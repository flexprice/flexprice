package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowConfig_CustomWorkflowsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"workflow_type": "customer_onboarding",
		"actions": [
			{"action": "create_wallet", "currency": "USD"}
		],
		"custom_workflows": {
			"enterprise": [
				{"action": "create_wallet", "currency": "EUR"},
				{"action": "create_subscription", "plan_id": "plan_enterprise"}
			]
		}
	}`)

	var cfg WorkflowConfig
	err := json.Unmarshal(raw, &cfg)
	require.NoError(t, err)

	assert.Equal(t, WorkflowTypeCustomerOnboarding, cfg.WorkflowType)
	require.Len(t, cfg.Actions, 1)
	assert.Equal(t, WorkflowActionCreateWallet, cfg.Actions[0].GetAction())
	require.Contains(t, cfg.CustomWorkflows, "enterprise")
	require.Len(t, cfg.CustomWorkflows["enterprise"], 2)
	assert.Equal(t, WorkflowActionCreateWallet, cfg.CustomWorkflows["enterprise"][0].GetAction())
	assert.Equal(t, WorkflowActionCreateSubscription, cfg.CustomWorkflows["enterprise"][1].GetAction())

	marshaled, err := json.Marshal(&cfg)
	require.NoError(t, err)

	var roundTripped WorkflowConfig
	err = json.Unmarshal(marshaled, &roundTripped)
	require.NoError(t, err)

	assert.Equal(t, cfg.WorkflowType, roundTripped.WorkflowType)
	require.Len(t, roundTripped.Actions, 1)
	assert.Equal(t, WorkflowActionCreateWallet, roundTripped.Actions[0].GetAction())
	require.Contains(t, roundTripped.CustomWorkflows, "enterprise")
	require.Len(t, roundTripped.CustomWorkflows["enterprise"], 2)
	assert.Equal(t, WorkflowActionCreateWallet, roundTripped.CustomWorkflows["enterprise"][0].GetAction())
	assert.Equal(t, WorkflowActionCreateSubscription, roundTripped.CustomWorkflows["enterprise"][1].GetAction())
}

func TestWorkflowConfig_ResolveOnboardingActions(t *testing.T) {
	defaultActions := []WorkflowActionConfig{
		&CreateWalletActionConfig{Action: WorkflowActionCreateWallet, Currency: "USD"},
	}
	customActions := []WorkflowActionConfig{
		&CreateWalletActionConfig{Action: WorkflowActionCreateWallet, Currency: "EUR"},
	}

	cfg := &WorkflowConfig{
		WorkflowType: WorkflowTypeCustomerOnboarding,
		Actions:      defaultActions,
		CustomWorkflows: map[string][]WorkflowActionConfig{
			"enterprise": customActions,
		},
	}

	t.Run("no key uses default", func(t *testing.T) {
		actions, found := cfg.ResolveOnboardingActions("")
		assert.True(t, found)
		assert.Equal(t, defaultActions, actions)
	})

	t.Run("known key uses custom", func(t *testing.T) {
		actions, found := cfg.ResolveOnboardingActions("enterprise")
		assert.True(t, found)
		assert.Equal(t, customActions, actions)
	})

	t.Run("unknown key falls back to default", func(t *testing.T) {
		actions, found := cfg.ResolveOnboardingActions("missing")
		assert.False(t, found)
		assert.Equal(t, defaultActions, actions)
	})
}

func TestWorkflowConfig_ValidateCustomWorkflows(t *testing.T) {
	cfg := WorkflowConfig{
		WorkflowType: WorkflowTypeCustomerOnboarding,
		Actions: []WorkflowActionConfig{
			&CreateWalletActionConfig{Action: WorkflowActionCreateWallet, Currency: "USD"},
		},
		CustomWorkflows: map[string][]WorkflowActionConfig{
			"bad": {
				&CreateWalletActionConfig{Action: WorkflowActionCreateWallet, Currency: "USD"},
				&CreateCustomerActionConfig{Action: WorkflowActionCreateCustomer},
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create_customer action must be the first action")
}
