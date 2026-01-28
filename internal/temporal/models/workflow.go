package models

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Workflow types and actions - domain models
type WorkflowType string

const (
	WorkflowTypeCustomerOnboarding     WorkflowType = "customer_onboarding"
	WorkflowTypePrepareProcessedEvents WorkflowType = "prepare_processed_events"
)

type WorkflowAction string

const (
	WorkflowActionCreateCustomer         WorkflowAction = "create_customer"
	WorkflowActionCreateSubscription     WorkflowAction = "create_subscription"
	WorkflowActionCreateWallet           WorkflowAction = "create_wallet"
	WorkflowActionCreateFeatureAndPrice  WorkflowAction = "create_feature_and_price"
	WorkflowActionRolloutToSubscriptions WorkflowAction = "rollout_to_subscriptions"
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
	CustomerID                  string
	Currency                    string
	EventTimestamp              *time.Time             // Optional - timestamp of the triggering event for subscription start date
	DefaultUserID               *string                // Optional - user_id from config for created_by/updated_by fields
	EventName                   string                 // Optional - event name for prepare processed events workflow
	EventProperties             map[string]interface{} // Optional - event properties for feature determination
	OnlyCreateAggregationFields []string               // Optional - when set, create only features for these aggregation fields (skip existing)
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
		case WorkflowActionCreateCustomer:
			customerAction, err := utils.ToStruct[CreateCustomerActionConfig](actionMap)
			if err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to convert create_customer action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &customerAction

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

		case WorkflowActionCreateFeatureAndPrice:
			featureAction, err := utils.ToStruct[CreateFeatureAndPriceActionConfig](actionMap)
			if err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to convert create_feature_and_price action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &featureAction

		case WorkflowActionRolloutToSubscriptions:
			rolloutAction, err := utils.ToStruct[RolloutToSubscriptionsActionConfig](actionMap)
			if err != nil {
				return ierr.WithError(err).
					WithHintf("Failed to convert rollout_to_subscriptions action: %v", err).
					Mark(ierr.ErrValidation)
			}
			action = &rolloutAction

		default:
			return ierr.NewErrorf("unknown action type: %s", actionType).
				WithHint("Please provide a valid action type").
				WithReportableDetails(map[string]any{
					"action": actionType,
					"allowed": []WorkflowAction{
						WorkflowActionCreateCustomer,
						WorkflowActionCreateWallet,
						WorkflowActionCreateSubscription,
						WorkflowActionCreateFeatureAndPrice,
						WorkflowActionRolloutToSubscriptions,
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

	// Validate each action
	for _, action := range c.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}

	// Enforce that create_customer action must be first if present
	for i, action := range c.Actions {
		if action.GetAction() == WorkflowActionCreateCustomer {
			if i != 0 {
				return ierr.NewError("create_customer action must be the first action in the workflow").
					WithHint("Move create_customer action to the beginning of the actions array").
					WithReportableDetails(map[string]interface{}{
						"current_position":  i,
						"required_position": 0,
					}).
					Mark(ierr.ErrValidation)
			}
			// Only one create_customer action is allowed
			break
		}
	}

	return nil
}

// CreateCustomerActionConfig represents configuration for creating a customer action
type CreateCustomerActionConfig struct {
	Action        WorkflowAction `json:"action"`                    // Type discriminator - automatically set to "create_customer"
	DefaultUserID *string        `json:"default_user_id,omitempty"` // Optional user_id to use for created_by/updated_by (defaults to NULL if not provided)
	// Name and ExternalID will be provided at runtime from the event context
	// Email is optional and left empty for auto-created customers
}

func (c *CreateCustomerActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	// No additional validation needed - name and external_id come from runtime context
	return nil
}

func (c *CreateCustomerActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateCustomer
}

// ToDTO converts the action config to CreateCustomerRequest DTO
func (c *CreateCustomerActionConfig) ToDTO(params interface{}) (interface{}, error) {
	// Type assert to get the parameters we need
	actionParams, ok := params.(*WorkflowActionParams)
	if !ok {
		return nil, ierr.NewError("invalid parameters for create_customer action").
			WithHint("Expected WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	// ExternalID must be provided in params
	if actionParams.CustomerID == "" {
		return nil, ierr.NewError("customer_id (external_id) is required for create_customer action").
			WithHint("Provide external customer ID in WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	return &dto.CreateCustomerRequest{
		ExternalID: actionParams.CustomerID,
		Name:       actionParams.CustomerID,
		Email:      "",
		Metadata: map[string]string{
			"created_by_workflow": "true",
			"workflow_type":       "customer_onboarding",
		},
		SkipOnboardingWorkflow: true,
	}, nil
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
	StartDate    *time.Time     `json:"start_date,omitempty"` // Optional start_date, if provided takes highest priority
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

	// Start date priority:
	// 1. Config start_date (if provided)
	// 2. Event timestamp (if provided)
	// 3. Current time (fallback)
	var startDate *time.Time
	if c.StartDate != nil {
		// Use config start_date (highest priority)
		startDate = c.StartDate
	} else if actionParams.EventTimestamp != nil {
		// Use event timestamp (second priority)
		startDate = actionParams.EventTimestamp
	} else {
		// Use current time (fallback)
		now := time.Now().UTC()
		startDate = &now
	}

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

// CreateFeatureAndPriceActionConfig represents configuration for creating a feature, meter, and price action
// Meter and price defaults come from GetDefaultSettings() - not stored in action config
type CreateFeatureAndPriceActionConfig struct {
	Action      WorkflowAction    `json:"action"` // Type discriminator - automatically set to "create_feature_and_price"
	PlanID      string            `json:"plan_id" binding:"required"`
	FeatureType types.FeatureType `json:"feature_type,omitempty"`
}

func (c *CreateFeatureAndPriceActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	if c.PlanID == "" {
		return ierr.NewError("plan_id is required for create_feature_and_price action").
			WithHint("Please provide a plan_id").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (c *CreateFeatureAndPriceActionConfig) GetAction() WorkflowAction {
	return WorkflowActionCreateFeatureAndPrice
}

// CreateFeatureAndPriceDTOs contains both feature and price DTOs
type CreateFeatureAndPriceDTOs struct {
	Feature *dto.CreateFeatureRequest
	Price   *dto.CreatePriceRequest
}

// EventPropertyKey represents event property keys used for feature determination
type EventPropertyKey string

const (
	// Token-related property keys (Case 1: 5 features)
	EventPropertyKeyPromptTokens             EventPropertyKey = "promptTokens"
	EventPropertyKeyCompletionTokens         EventPropertyKey = "completionTokens"
	EventPropertyKeyCachedPromptTokens       EventPropertyKey = "cachedPromptTokens"
	EventPropertyKeyCacheCreationInputTokens EventPropertyKey = "cacheCreationInputTokens"
	EventPropertyKeyCacheReadInputTokens     EventPropertyKey = "cacheReadInputTokens"

	// Audio/Text token property keys (Case 2: 6 features)
	EventPropertyKeyUncachedPromptAudioTokens EventPropertyKey = "uncachedPromptAudioTokens"
	EventPropertyKeyUncachedPromptTextTokens  EventPropertyKey = "uncachedPromptTextTokens"
	EventPropertyKeyCachedPromptAudioTokens   EventPropertyKey = "cachedPromptAudioTokens"
	EventPropertyKeyCachedPromptTextTokens    EventPropertyKey = "cachedPromptTextTokens"
	EventPropertyKeyCandidatesAudioTokens     EventPropertyKey = "candidatesAudioTokens"
	EventPropertyKeyCandidatesTextTokens      EventPropertyKey = "candidatesTextTokens"

	// Single feature property keys (Case 3)
	EventPropertyKeyNumCharacters EventPropertyKey = "numCharacters"
	EventPropertyKeyDurationMS    EventPropertyKey = "durationMS"

	// Property key for billable value (Case 3)
	EventPropertyKeyBillableValue EventPropertyKey = "billable_value"
)

// General enums for feature/meter (Case 3: numCharacters / durationMS)
const (

	// Unit Enums
	UnitSingularCharacter   = "character"
	UnitPluralCharacters    = "characters"
	UnitSingularMillisecond = "millisecond"
	UnitPluralMilliseconds  = "milliseconds"

	// Aggregation Field Enums
	AggregationFieldBillableValue = "value"
)

// FeatureSpec defines the specification for creating a feature
type FeatureSpec struct {
	Name             string // Feature name
	LookupKey        string // Feature lookup key
	AggregationField string // Meter aggregation field
	UnitSingular     string // Feature unit singular (optional)
	UnitPlural       string // Feature unit plural (optional)
}

// determineFeatureSpecs determines which features to create based on event properties
func determineFeatureSpecs(eventName string, eventProperties map[string]interface{}) []FeatureSpec {
	if eventProperties == nil {
		// Fallback to basic single feature creation
		return []FeatureSpec{
			{
				Name:             eventName,
				LookupKey:        eventName,
				AggregationField: AggregationFieldBillableValue,
			},
		}
	}

	// Case 1: Check for token-related fields (5 features)
	// Feature name = event.name-{AggregationField}, lookup key = feature.name, meter.name = feature.name
	tokenFields := []EventPropertyKey{
		EventPropertyKeyPromptTokens,
		EventPropertyKeyCompletionTokens,
		EventPropertyKeyCachedPromptTokens,
		EventPropertyKeyCacheCreationInputTokens,
		EventPropertyKeyCacheReadInputTokens,
	}
	hasTokenField := false
	for _, field := range tokenFields {
		if _, exists := eventProperties[string(field)]; exists {
			hasTokenField = true
			break
		}
	}

	if hasTokenField {
		specs := make([]FeatureSpec, 0, 5)
		for _, field := range tokenFields {
			aggField := string(field)
			featureName := eventName + "-" + aggField
			specs = append(specs, FeatureSpec{
				Name:             featureName,
				LookupKey:        featureName,
				AggregationField: aggField,
			})
		}
		return specs
	}

	// Case 2: Check for audio/text token fields (6 features)
	// Feature name = event_name-{AggregationField}, lookup key = feature name, meter.name = feature name
	audioTextFields := []EventPropertyKey{
		EventPropertyKeyUncachedPromptAudioTokens,
		EventPropertyKeyUncachedPromptTextTokens,
		EventPropertyKeyCachedPromptAudioTokens,
		EventPropertyKeyCachedPromptTextTokens,
		EventPropertyKeyCandidatesAudioTokens,
		EventPropertyKeyCandidatesTextTokens,
	}
	hasAudioTextField := false
	for _, field := range audioTextFields {
		if _, exists := eventProperties[string(field)]; exists {
			hasAudioTextField = true
			break
		}
	}

	if hasAudioTextField {
		specs := make([]FeatureSpec, 0, 6)
		for _, field := range audioTextFields {
			aggField := string(field)
			featureName := eventName + "-" + aggField
			specs = append(specs, FeatureSpec{
				Name:             featureName,
				LookupKey:        featureName,
				AggregationField: aggField,
			})
		}
		return specs
	}

	// Case 3: numCharacters or durationMS — single feature, feature name = event_name, lookup key = feature name
	if _, hasNumChars := eventProperties[string(EventPropertyKeyNumCharacters)]; hasNumChars {
		return []FeatureSpec{
			{
				Name:             eventName,
				LookupKey:        eventName,
				AggregationField: string(EventPropertyKeyBillableValue),
				UnitSingular:     UnitSingularCharacter,
				UnitPlural:       UnitPluralCharacters,
			},
		}
	}

	if _, hasDuration := eventProperties[string(EventPropertyKeyDurationMS)]; hasDuration {
		return []FeatureSpec{
			{
				Name:             eventName,
				LookupKey:        eventName,
				AggregationField: string(EventPropertyKeyBillableValue),
				UnitSingular:     UnitSingularMillisecond,
				UnitPlural:       UnitPluralMilliseconds,
			},
		}
	}

	// Fallback: basic single feature
	return []FeatureSpec{
		{
			Name:             eventName,
			LookupKey:        eventName,
			AggregationField: AggregationFieldBillableValue,
		},
	}
}

// RequiredAggregationFields returns the list of aggregation fields required for an event (same logic as determineFeatureSpecs).
// Used by feature usage tracking to know which meters to consider and which features to create when partially missing.
func RequiredAggregationFields(eventName string, eventProperties map[string]interface{}) []string {
	// Determine which set of features to create based on event properties
	specs := determineFeatureSpecs(eventName, eventProperties)
	fields := make([]string, 0, len(specs))
	for _, spec := range specs {
		fields = append(fields, spec.AggregationField)
	}
	return fields
}

// ToDTO converts the action config to both CreateFeatureRequest and CreatePriceRequest DTOs
// Returns a slice of DTOs - one for each feature that needs to be created based on event properties
func (c *CreateFeatureAndPriceActionConfig) ToDTO(params interface{}) (interface{}, error) {
	// Type assert to get the parameters we need
	actionParams, ok := params.(*WorkflowActionParams)
	if !ok {
		return nil, ierr.NewError("invalid parameters for create_feature_and_price action").
			WithHint("Expected WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	// EventName must be provided in params
	if actionParams.EventName == "" {
		return nil, ierr.NewError("event_name is required for create_feature_and_price action").
			WithHint("Provide event name in WorkflowActionParams").
			Mark(ierr.ErrValidation)
	}

	// Get event properties from params if available
	var eventProperties map[string]interface{}
	if actionParams.EventProperties != nil {
		eventProperties = actionParams.EventProperties
	}

	// Determine which features to create
	featureSpecs := determineFeatureSpecs(actionParams.EventName, eventProperties)

	// When OnlyCreateAggregationFields is set, create only features for those aggregation fields (skip existing)
	if len(actionParams.OnlyCreateAggregationFields) > 0 {
		allowedSet := actionParams.OnlyCreateAggregationFields
		featureSpecs = lo.Filter(featureSpecs, func(spec FeatureSpec, _ int) bool {
			return lo.Contains(allowedSet, spec.AggregationField)
		})
	}

	// Get defaults from settings
	defaults, err := types.GetDefaultSettings()
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get default settings").
			Mark(ierr.ErrInternal)
	}

	_, exists := defaults[types.SettingKeyPrepareProcessedEvents]
	if !exists {
		return nil, ierr.NewError("default settings not found for prepare_processed_events_config").
			WithHint("Default settings must be defined").
			Mark(ierr.ErrInternal)
	}

	// Defaults are applied here, not stored in action config (defaultSetting reserved for future extensibility)
	featureType := c.FeatureType
	if featureType == "" {
		featureType = types.FeatureTypeMetered
	}
	meterAggType := types.AggregationSum
	meterResetUsage := types.ResetUsageBillingPeriod
	priceBillingCadence := types.BILLING_CADENCE_RECURRING
	priceBillingPeriod := types.BILLING_PERIOD_MONTHLY
	priceBillingModel := types.BILLING_MODEL_FLAT_FEE
	priceCurrency := "USD"
	priceEntityType := types.PRICE_ENTITY_TYPE_PLAN
	priceInvoiceCadence := types.InvoiceCadenceArrear
	pricePriceUnitType := types.PRICE_UNIT_TYPE_FIAT
	priceType := types.PRICE_TYPE_USAGE
	priceAmount := decimal.NewFromFloat(0.0)
	priceBillingPeriodCount := 1

	// Create DTOs for each feature spec
	dtosList := make([]CreateFeatureAndPriceDTOs, 0, len(featureSpecs))
	for _, spec := range featureSpecs {
		// Create feature DTO
		featureReq := &dto.CreateFeatureRequest{
			Name:      spec.Name,
			LookupKey: spec.LookupKey,
			Type:      featureType,
			Meter: &dto.CreateMeterRequest{
				Name:      spec.Name,
				EventName: actionParams.EventName, // Original event name for meter
				Aggregation: meter.Aggregation{
					Type:  meterAggType,
					Field: spec.AggregationField,
				},
				Filters:    []meter.Filter{},
				ResetUsage: meterResetUsage,
			},
			Metadata: types.Metadata{
				"created_by_workflow": "true",
				"workflow_type":       "prepare_processed_events_workflow",
			},
		}

		// Set units if provided
		if spec.UnitSingular != "" && spec.UnitPlural != "" {
			featureReq.UnitSingular = spec.UnitSingular
			featureReq.UnitPlural = spec.UnitPlural
		}

		// Create price DTO (meter_id will be set after feature creation)
		priceReq := &dto.CreatePriceRequest{
			Amount:             &priceAmount,
			Currency:           priceCurrency,
			EntityType:         priceEntityType,
			EntityID:           c.PlanID,
			Type:               priceType,
			PriceUnitType:      pricePriceUnitType,
			BillingPeriod:      priceBillingPeriod,
			BillingPeriodCount: priceBillingPeriodCount,
			BillingModel:       priceBillingModel,
			BillingCadence:     priceBillingCadence,
			InvoiceCadence:     priceInvoiceCadence,
			// MeterID will be set after feature creation
			Metadata: map[string]string{
				"created_by_workflow": "true",
				"workflow_type":       "prepare_processed_events_workflow",
				"event_name":          actionParams.EventName,
				"feature_name":        spec.Name,
			},
		}

		dtosList = append(dtosList, CreateFeatureAndPriceDTOs{
			Feature: featureReq,
			Price:   priceReq,
		})
	}

	return dtosList, nil
}

// RolloutToSubscriptionsActionConfig represents configuration for rolling out plan prices to subscriptions
type RolloutToSubscriptionsActionConfig struct {
	Action WorkflowAction `json:"action"` // Type discriminator - automatically set to "rollout_to_subscriptions"
	PlanID string         `json:"plan_id" binding:"required"`
}

func (c *RolloutToSubscriptionsActionConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	if c.PlanID == "" {
		return ierr.NewError("plan_id is required for rollout_to_subscriptions action").
			WithHint("Please provide a plan_id").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (c *RolloutToSubscriptionsActionConfig) GetAction() WorkflowAction {
	return WorkflowActionRolloutToSubscriptions
}

// ToDTO converts the action config to DTO
// For rollout_to_subscriptions, we don't need a DTO conversion, but we implement it for interface compliance
func (c *RolloutToSubscriptionsActionConfig) ToDTO(params interface{}) (interface{}, error) {
	// This action doesn't need DTO conversion - it uses the plan_id directly
	// Return the config itself or nil - the workflow will extract plan_id directly
	return nil, nil
}
