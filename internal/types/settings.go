package types

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SettingConfig defines the interface for setting configuration validation
type SettingConfig interface {
	Validate() error
}

type SettingKey string

const (
	SettingKeyInvoiceConfig            SettingKey = "invoice_config"
	SettingKeySubscriptionConfig       SettingKey = "subscription_config"
	SettingKeyInvoicePDFConfig         SettingKey = "invoice_pdf_config"
	SettingKeyTenantConfig             SettingKey = "tenant_config"
	SettingKeyCustomerOnboarding       SettingKey = "customer_onboarding"
	SettingKeyWalletBalanceAlertConfig SettingKey = "wallet_balance_alert_config"
	SettingKeyPrepareProcessedEvents   SettingKey = "prepare_processed_events_config"
	SettingKeyCustomAnalytics          SettingKey = "custom_analytics_config"
	SettingKeyCustomerPortalConfig     SettingKey = "customer_portal_config"
	SettingKeyEventIngestionFilter     SettingKey = "event_ingestion_filter"
	SettingKeyBonusCreditsTopupConfig  SettingKey = "bonus_credits_topup_config"
	SettingKeyPaymentMandateLimits     SettingKey = "payment_mandate_limits"
)

func (s *SettingKey) Validate() error {

	allowedKeys := []SettingKey{
		SettingKeyInvoiceConfig,
		SettingKeySubscriptionConfig,
		SettingKeyInvoicePDFConfig,
		SettingKeyTenantConfig,
		SettingKeyCustomerOnboarding,
		SettingKeyWalletBalanceAlertConfig,
		SettingKeyPrepareProcessedEvents,
		SettingKeyCustomAnalytics,
		SettingKeyCustomerPortalConfig,
		SettingKeyEventIngestionFilter,
		SettingKeyBonusCreditsTopupConfig,
		SettingKeyPaymentMandateLimits,
	}

	if !lo.Contains(allowedKeys, *s) {
		return ierr.NewErrorf("invalid setting key: %s", *s).
			WithHint("Please provide a valid setting key").
			WithReportableDetails(map[string]any{
				"allowed": allowedKeys,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// DefaultSettingValue represents a default setting configuration
type DefaultSettingValue struct {
	Key          SettingKey             `json:"key"`
	DefaultValue map[string]interface{} `json:"default_value"`
	Description  string                 `json:"description"`
}

// SubscriptionConfig represents the configuration for subscription auto-cancellation
type SubscriptionConfig struct {
	GracePeriodDays         int  `json:"grace_period_days" validate:"required,min=1"`
	AutoCancellationEnabled bool `json:"auto_cancellation_enabled"`
}

// Validate implements SettingConfig interface
func (c SubscriptionConfig) Validate() error {
	return validator.ValidateRequest(c)
}

// InvoicePDFConfig represents configuration for invoice PDF generation
type InvoicePDFConfig struct {
	TemplateName TemplateName `json:"template_name" validate:"required"`
	GroupBy      []string     `json:"group_by" validate:"omitempty,dive,required"`
}

// Validate implements SettingConfig interface
func (c InvoicePDFConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	// Additional validation for TemplateName enum
	return c.TemplateName.Validate()
}

// TenantConfig represents environment creation limits and user limit configuration
type TenantConfig struct {
	Production  int `json:"production" validate:"omitempty,min=0"`
	Development int `json:"development" validate:"omitempty,min=0"`
	MaxUsers    int `json:"max_users" validate:"omitempty,min=1"`
}

// Validate implements SettingConfig interface
func (c TenantConfig) Validate() error {
	return validator.ValidateRequest(c)
}

// TenantEnvConfig represents a generic configuration for a specific tenant and environment
type TenantEnvConfig struct {
	TenantID      string                 `json:"tenant_id"`
	EnvironmentID string                 `json:"environment_id"`
	Config        map[string]interface{} `json:"config"`
}

// TenantSubscriptionConfig represents subscription configuration for a specific tenant and environment
type TenantEnvSubscriptionConfig struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	*SubscriptionConfig
}

// PrepareProcessedEventsConfig is DEPRECATED - settings now use WorkflowConfig
// This struct is kept only for backward compatibility in ValidateSettingValue
// The RolloutSubscriptions field is removed - use rollout_to_subscriptions action instead
type PrepareProcessedEventsConfig struct {
	FeatureType FeatureType                       `json:"feature_type,omitempty"`
	Meter       PrepareProcessedEventsMeterConfig `json:"meter"`
	Price       PrepareProcessedEventsPriceConfig `json:"price"`
	PlanID      string                            `json:"plan_id,omitempty"`
}

type PrepareProcessedEventsMeterConfig struct {
	AggregationType  AggregationType `json:"aggregation_type,omitempty"`
	AggregationField string          `json:"aggregation_field,omitempty"`
	ResetUsage       ResetUsage      `json:"reset_usage,omitempty"`
}

type PrepareProcessedEventsPriceConfig struct {
	BillingCadence     BillingCadence  `json:"billing_cadence,omitempty"`
	BillingPeriod      BillingPeriod   `json:"billing_period,omitempty"`
	BillingModel       BillingModel    `json:"billing_model,omitempty"`
	Currency           string          `json:"currency,omitempty"`
	EntityType         PriceEntityType `json:"entity_type,omitempty"`
	InvoiceCadence     InvoiceCadence  `json:"invoice_cadence,omitempty"`
	PriceUnitType      PriceUnitType   `json:"price_unit_type,omitempty"`
	Type               PriceType       `json:"type,omitempty"`
	Amount             decimal.Decimal `json:"amount,omitempty"`
	BillingPeriodCount int             `json:"billing_period_count,omitempty"`
}

// Validate implements SettingConfig interface
func (c PrepareProcessedEventsConfig) Validate() error {
	// Follow existing settings pattern:
	// - Defaults are provided by GetDefaultSettings()
	// - Validate() only validates provided fields (no mutation, no required plan_id here)

	if c.FeatureType != "" {
		if err := c.FeatureType.Validate(); err != nil {
			return err
		}
	}

	// Meter validation (only when fields are provided)
	if c.Meter.AggregationType != "" {
		if !c.Meter.AggregationType.Validate() {
			return ierr.NewError("invalid aggregation type").
				WithHint("Provide a valid aggregation type for meter").
				WithReportableDetails(map[string]any{"aggregation_type": c.Meter.AggregationType}).
				Mark(ierr.ErrValidation)
		}
		if c.Meter.AggregationType.RequiresField() && strings.TrimSpace(c.Meter.AggregationField) == "" {
			return ierr.NewError("aggregation_field is required for the configured aggregation type").
				WithHint("Provide aggregation_field (e.g. \"value\")").
				WithReportableDetails(map[string]any{"aggregation_type": c.Meter.AggregationType}).
				Mark(ierr.ErrValidation)
		}
	}
	if c.Meter.ResetUsage != "" {
		if err := c.Meter.ResetUsage.Validate(); err != nil {
			return err
		}
	}

	// Price validation (only when fields are provided)
	if c.Price.BillingCadence != "" {
		if err := c.Price.BillingCadence.Validate(); err != nil {
			return err
		}
	}
	if c.Price.BillingPeriod != "" {
		if err := c.Price.BillingPeriod.Validate(); err != nil {
			return err
		}
	}
	if c.Price.BillingModel != "" {
		if err := c.Price.BillingModel.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Price.Currency) != "" && len(strings.TrimSpace(c.Price.Currency)) != 3 {
		return ierr.NewError("currency must be a 3-letter code").
			WithHint("Provide a valid 3-letter currency code (e.g. USD)").
			WithReportableDetails(map[string]any{"currency": c.Price.Currency}).
			Mark(ierr.ErrValidation)
	}
	if c.Price.EntityType != "" {
		if err := c.Price.EntityType.Validate(); err != nil {
			return err
		}
		// This workflow only supports PLAN-scoped prices
		if c.Price.EntityType != PRICE_ENTITY_TYPE_PLAN {
			return ierr.NewError("entity_type must be PLAN for prepare_processed_events_config").
				WithHint("Set entity_type to PLAN").
				WithReportableDetails(map[string]any{"entity_type": c.Price.EntityType}).
				Mark(ierr.ErrValidation)
		}
	}
	if c.Price.InvoiceCadence != "" {
		if err := c.Price.InvoiceCadence.Validate(); err != nil {
			return err
		}
	}
	if c.Price.PriceUnitType != "" {
		if err := c.Price.PriceUnitType.Validate(); err != nil {
			return err
		}
	}
	if c.Price.Type != "" {
		if err := c.Price.Type.Validate(); err != nil {
			return err
		}
	}
	if c.Price.Amount.IsNegative() {
		return ierr.NewError("amount cannot be negative").
			WithHint("Provide a non-negative amount").
			WithReportableDetails(map[string]any{"amount": c.Price.Amount.String()}).
			Mark(ierr.ErrValidation)
	}
	if c.Price.BillingPeriodCount < 0 {
		return ierr.NewError("billing_period_count cannot be negative").
			WithHint("Provide a billing_period_count >= 1").
			WithReportableDetails(map[string]any{"billing_period_count": c.Price.BillingPeriodCount}).
			Mark(ierr.ErrValidation)
	}
	if c.Price.BillingPeriodCount > 0 && c.Price.BillingPeriodCount < 1 {
		return ierr.NewError("billing_period_count must be greater than 0").
			WithHint("Set billing_period_count to 1 or more").
			WithReportableDetails(map[string]any{"billing_period_count": c.Price.BillingPeriodCount}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// CustomAnalyticsRuleID represents the type of calculation to perform
type CustomAnalyticsRuleID string

const (
	// CustomAnalyticsRuleRevenuePerMinute calculates revenue per minute from millisecond usage
	// Formula: total_cost / (total_usage / 60000)
	// Can be applied to any feature that tracks usage in milliseconds
	CustomAnalyticsRuleRevenuePerMinute CustomAnalyticsRuleID = "revenue-per-minute"
)

// CustomAnalyticsConfig represents configuration for custom analytics calculations
type CustomAnalyticsConfig struct {
	Rules []CustomAnalyticsRule `json:"rules" validate:"dive"`
}

// CustomAnalyticsRule represents a single custom analytics rule
type CustomAnalyticsRule struct {
	ID         string `json:"id" validate:"required"`
	TargetType string `json:"target_type" validate:"required,oneof=feature meter event_name"`
	TargetID   string `json:"target_id" validate:"required"`
}

// Validate implements SettingConfig interface
func (c CustomAnalyticsConfig) Validate() error {
	return validator.ValidateRequest(c)
}

// CustomerPortalConfig is the top-level configuration for the customer self-service portal.
// It controls branding, section layout, and per-tab behaviour.
type CustomerPortalConfig struct {
	// Version is a user-managed schema version string (e.g. "1.0")
	Version string `json:"version,omitempty"`

	// Theme holds the visual branding colours for the portal
	Theme CustomerPortalTheme `json:"theme,omitempty"`

	// Sections defines the ordered list of navigation sections shown in the portal
	Sections []CustomerPortalSection `json:"sections,omitempty" validate:"omitempty,dive"`
}

// Validate implements SettingConfig interface
func (c CustomerPortalConfig) Validate() error {
	return validator.ValidateRequest(c)
}

// CustomerPortalTheme holds brand colour tokens used by the portal UI
type CustomerPortalTheme struct {
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
	TertiaryColor  string `json:"tertiary_color,omitempty"`
}

// CustomerPortalSection represents a top-level navigation section in the portal
type CustomerPortalSection struct {
	ID      string              `json:"id" validate:"required"`
	Label   string              `json:"label,omitempty"`
	Enabled bool                `json:"enabled"`
	Order   int                 `json:"order,omitempty"`
	Tabs    []CustomerPortalTab `json:"tabs,omitempty" validate:"omitempty,dive"`
}

// CustomerPortalTab represents a single tab within a portal section
type CustomerPortalTab struct {
	ID          string                     `json:"id" validate:"required"`
	Type        string                     `json:"type" validate:"required"`
	Enabled     bool                       `json:"enabled"`
	Order       int                        `json:"order,omitempty"`
	UsageGraph  *CustomerPortalUsageGraph  `json:"usage_graph,omitempty"`
	MetricCards *CustomerPortalMetricCards `json:"metric_cards,omitempty"`
}

// CustomerPortalUsageGraph holds configuration for usage_graph tab types
type CustomerPortalUsageGraph struct {
	DatePresets          []string `json:"date_presets,omitempty"`
	DefaultPreset        string   `json:"default_preset,omitempty"`
	AllowCustomDateRange bool     `json:"allow_custom_date_range"`
	FeatureFilterMode    string   `json:"feature_filter_mode,omitempty"`
}

// CustomerPortalMetricCards holds configuration for metric_cards tab types
type CustomerPortalMetricCards struct {
	ShowCostMetrics   bool `json:"show_cost_metrics"`
	ShowCustomMetrics bool `json:"show_custom_metrics"`
	ShowRevenueMetric bool `json:"show_revenue_metric"`
}

// EventIngestionFilterConfig controls which external customer IDs are allowed through
// the raw-event → events transformation pipeline. When Enabled is true, only events
// whose ExternalCustomerID appears in AllowedExternalCustomerIDs are forwarded; all
// others are stored in raw_events but silently dropped at the enqueue step.
// This is useful for large-volume pilots where only a subset of customers need live
// billing (e.g. 400 out of 60 000 VAPI orgs).
type EventIngestionFilterConfig struct {
	Enabled                    bool     `json:"enabled"`
	AllowedExternalCustomerIDs []string `json:"allowed_external_customer_ids"`
}

// Validate implements SettingConfig interface.
// It rejects the combination of Enabled=true with an empty allowlist because
// that would silently drop every event — an almost certainly unintentional
// configuration. Use Enabled=false to disable filtering entirely.
func (c EventIngestionFilterConfig) Validate() error {
	if c.Enabled && len(c.AllowedExternalCustomerIDs) == 0 {
		return ierr.NewError("event_ingestion_filter: enabled is true but allowed_external_customer_ids is empty — this would block all events; set enabled=false to disable filtering").
			Mark(ierr.ErrValidation)
	}
	return validator.ValidateRequest(c)
}

// BonusValueType controls whether a slab's bonus is a fixed credit amount or a percentage of
// the credits being purchased.
type BonusValueType string

const (
	BonusValueTypeFlat       BonusValueType = "flat"
	BonusValueTypePercentage BonusValueType = "percentage"
)

// BonusValue is the bonus a matched slab grants.
type BonusValue struct {
	Type  BonusValueType  `json:"type" validate:"required"`
	Value decimal.Decimal `json:"value" validate:"required"`
}

// BonusCreditsSlab: if credits_to_add <Operator> Threshold, grant Bonus. Slabs are evaluated in
// list order and the FIRST match wins, so they must be stored sorted DESCENDING by Threshold —
// the highest bracket a purchase clears is the one that applies.
//
// Operator reuses the existing FilterOperatorType instead of minting a new type. Only
// GREATER_THAN_EQUAL is accepted for now, enforced in BonusCreditsTopupConfig.Validate against
// the actual constant (not a duplicated string literal in a struct tag).
type BonusCreditsSlab struct {
	Threshold decimal.Decimal    `json:"threshold" validate:"required"`
	Operator  FilterOperatorType `json:"operator" validate:"required"`
	Bonus     BonusValue         `json:"bonus" validate:"required"`
}

// BonusCreditsTopupConfig defines slab-based bonus-credit rules applied to a purchased wallet top-up.
type BonusCreditsTopupConfig struct {
	Enabled bool               `json:"enabled"`
	Slabs   []BonusCreditsSlab `json:"slabs" validate:"omitempty,dive"`
}

// Validate implements SettingConfig interface.
func (c BonusCreditsTopupConfig) Validate() error {
	if c.Enabled && len(c.Slabs) == 0 {
		return ierr.NewError("bonus_credits_topup_config: enabled is true but slabs is empty").
			WithHint("Add at least one slab, or set enabled=false").
			Mark(ierr.ErrValidation)
	}
	for i, slab := range c.Slabs {
		if slab.Operator != GREATER_THAN_EQUAL {
			return ierr.NewErrorf("bonus_credits_topup_config: slab operator must be %s", GREATER_THAN_EQUAL).
				WithHint("Only the gte operator is supported for bonus credit slabs").
				WithReportableDetails(map[string]any{
					"operator": slab.Operator,
				}).
				Mark(ierr.ErrValidation)
		}
		if slab.Threshold.LessThan(decimal.Zero) {
			return ierr.NewError("bonus_credits_topup_config: slab threshold cannot be negative").
				WithHint("Threshold must be zero or positive").
				WithReportableDetails(map[string]any{
					"threshold": slab.Threshold,
				}).
				Mark(ierr.ErrValidation)
		}
		if slab.Bonus.Type != BonusValueTypeFlat && slab.Bonus.Type != BonusValueTypePercentage {
			return ierr.NewErrorf("bonus_credits_topup_config: bonus type must be %s or %s", BonusValueTypeFlat, BonusValueTypePercentage).
				WithHint("Only flat or percentage bonus types are supported").
				WithReportableDetails(map[string]any{
					"bonus_type": slab.Bonus.Type,
				}).
				Mark(ierr.ErrValidation)
		}
		if slab.Bonus.Value.LessThan(decimal.Zero) {
			return ierr.NewError("bonus_credits_topup_config: bonus value cannot be negative").
				WithHint("Bonus value must be zero or positive").
				WithReportableDetails(map[string]any{
					"bonus_value": slab.Bonus.Value,
				}).
				Mark(ierr.ErrValidation)
		}
		if i > 0 && !c.Slabs[i-1].Threshold.GreaterThan(slab.Threshold) {
			return ierr.NewError("bonus_credits_topup_config: slabs must be sorted descending by threshold").
				WithHint("Slabs must be sorted in strictly descending order by threshold, the highest bracket first").
				WithReportableDetails(map[string]any{
					"index":     i,
					"threshold": slab.Threshold,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return validator.ValidateRequest(c)
}

// PaymentMandateLimits holds per-rail auto-charge ceilings (keyed by PaymentMethodType).
// This is a safety ceiling, not the auto-charge opt-in — that lives in the checkout request.
type PaymentMandateLimits struct {
	MandateLimits map[PaymentMethodType]MandateLimit `json:"mandate_limits"`
}

type MandateLimit struct {
	MaxAmount decimal.Decimal `json:"max_amount" swaggertype:"string"`
	Currency  string          `json:"currency,omitempty"`
}

// Validate implements SettingConfig interface
func (c PaymentMandateLimits) Validate() error {
	for rail, limit := range c.MandateLimits {
		if limit.MaxAmount.IsNegative() {
			return ierr.NewErrorf("max_amount for rail %q must not be negative", rail).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetDefaultSettings returns the default settings configuration for all setting keys
// Uses typed structs and converts them to maps using ToMap utility from conversion.go
func GetDefaultSettings() (map[SettingKey]DefaultSettingValue, error) {
	// Define defaults as typed structs
	defaultInvoiceConfig := InvoiceConfig{
		InvoiceNumberPrefix:                    "INV",
		InvoiceNumberFormat:                    InvoiceNumberFormatYYYYMM,
		InvoiceNumberStartSequence:             1,
		InvoiceNumberTimezone:                  DefaultTimezone,
		InvoiceNumberSeparator:                 "-",
		InvoiceNumberSuffixLength:              5,
		DueDateDays:                            lo.ToPtr(1),
		AutoCompletePurchasedCreditTransaction: false,
		FinalizationDelaySeconds:               7200, // 2 hours
	}

	defaultSubscriptionConfig := SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	defaultInvoicePDFConfig := InvoicePDFConfig{
		TemplateName: TemplateInvoiceDefault,
		GroupBy:      []string{},
	}

	defaultTenantConfig := TenantConfig{
		Production:  0,
		Development: 2,
		MaxUsers:    10,
	}

	// Note: WorkflowConfig is now defined in service package to avoid import cycles
	// We'll use a map for the default config to avoid importing service package here
	defaultCustomerOnboardingConfig := map[string]interface{}{
		"workflow_type": "customer_onboarding",
		"actions":       []interface{}{},
	}

	defaultWalletBalanceAlertConfig := AlertSettings{
		Critical: &AlertThreshold{
			Threshold: decimal.NewFromFloat(0.0),
			Condition: AlertConditionBelow,
		},
		AlertEnabled: lo.ToPtr(false), // Disabled by default, users must explicitly enable
	}

	// Defaults for prepare_processed_events_config (plan_id intentionally omitted from action)
	// Use map like customer_onboarding to avoid import cycles
	defaultPrepareProcessedEventsConfig := map[string]interface{}{
		"workflow_type": "prepare_processed_events",
		"actions":       []interface{}{},
	}

	// Convert typed structs to maps using centralized utility
	invoiceConfigMap, err := utils.ToMap(defaultInvoiceConfig)
	if err != nil {
		return nil, err
	}
	subscriptionConfigMap, err := utils.ToMap(defaultSubscriptionConfig)
	if err != nil {
		return nil, err
	}
	invoicePDFConfigMap, err := utils.ToMap(defaultInvoicePDFConfig)
	if err != nil {
		return nil, err
	}
	tenantConfigMap, err := utils.ToMap(defaultTenantConfig)
	if err != nil {
		return nil, err
	}
	// Already a map, no conversion needed
	customerOnboardingConfigMap := defaultCustomerOnboardingConfig

	defaultWalletBalanceAlertConfigMap, err := utils.ToMap(defaultWalletBalanceAlertConfig)
	if err != nil {
		return nil, err
	}

	// Already a map, no conversion needed
	defaultPrepareProcessedEventsConfigMap := defaultPrepareProcessedEventsConfig

	defaultCustomerPortalConfig := CustomerPortalConfig{
		Version: "1.0",
		Theme:   CustomerPortalTheme{},
		Sections: []CustomerPortalSection{
			{
				ID: "usage", Label: "Usage", Enabled: true, Order: 1,
				Tabs: []CustomerPortalTab{
					{
						ID: "1", Type: "metric_cards", Order: 1, Enabled: true,
						MetricCards: &CustomerPortalMetricCards{
							ShowCostMetrics:   false,
							ShowCustomMetrics: true,
							ShowRevenueMetric: true,
						},
					},
					{
						ID: "2", Type: "usage_graph", Order: 2, Enabled: true,
						UsageGraph: &CustomerPortalUsageGraph{
							DatePresets:          []string{"today", "last_7_days", "last_30_days", "current_month", "last_month"},
							DefaultPreset:        "last_7_days",
							FeatureFilterMode:    "inc",
							AllowCustomDateRange: true,
						},
					},
					{ID: "4", Type: "current_usage", Order: 3, Enabled: true},
					{ID: "3", Type: "usage_breakdown", Order: 4, Enabled: true},
				},
			},
			{
				ID: "credits", Label: "Credits", Enabled: true, Order: 2,
				Tabs: []CustomerPortalTab{
					{ID: "6", Type: "wallet_balance", Order: 1, Enabled: true},
					{ID: "7", Type: "wallet_transactions", Order: 2, Enabled: true},
				},
			},
			{
				ID: "invoices", Label: "Invoices", Enabled: true, Order: 3,
				Tabs: []CustomerPortalTab{
					{ID: "8", Type: "invoices", Order: 1, Enabled: true},
				},
			},
			{
				ID: "overview", Label: "Overview", Enabled: true, Order: 4,
				Tabs: []CustomerPortalTab{
					{ID: "9", Type: "wallet_balance", Order: 1, Enabled: true},
					{ID: "10", Type: "subscriptions", Order: 2, Enabled: true},
					{
						ID: "11", Type: "usage_graph", Order: 3, Enabled: true,
						UsageGraph: &CustomerPortalUsageGraph{
							DatePresets:          []string{"last_7_days", "last_30_days"},
							DefaultPreset:        "last_7_days",
							FeatureFilterMode:    "all",
							AllowCustomDateRange: true,
						},
					},
				},
			},
		},
	}
	defaultCustomerPortalConfigMap, err := utils.ToMap(defaultCustomerPortalConfig)
	if err != nil {
		return nil, err
	}

	defaultEventIngestionFilterConfig := EventIngestionFilterConfig{
		Enabled:                    false,
		AllowedExternalCustomerIDs: []string{},
	}
	defaultEventIngestionFilterConfigMap, err := utils.ToMap(defaultEventIngestionFilterConfig)
	if err != nil {
		return nil, err
	}

	defaultBonusCreditsTopupConfig := BonusCreditsTopupConfig{
		Enabled: false,
		Slabs:   []BonusCreditsSlab{},
	}
	defaultBonusCreditsTopupConfigMap, err := utils.ToMap(defaultBonusCreditsTopupConfig)
	if err != nil {
		return nil, err
	}

	defaultPaymentMandateLimits := PaymentMandateLimits{
		MandateLimits: map[PaymentMethodType]MandateLimit{
			PaymentMethodTypeUPI: {MaxAmount: decimal.NewFromInt(100000), Currency: "INR"},
		},
	}
	defaultPaymentMandateLimitsMap, err := utils.ToMap(defaultPaymentMandateLimits)
	if err != nil {
		return nil, err
	}

	return map[SettingKey]DefaultSettingValue{
		SettingKeyInvoiceConfig: {
			Key:          SettingKeyInvoiceConfig,
			DefaultValue: invoiceConfigMap,
			Description:  "Default configuration for invoice generation and management",
		},
		SettingKeySubscriptionConfig: {
			Key:          SettingKeySubscriptionConfig,
			DefaultValue: subscriptionConfigMap,
			Description:  "Default configuration for subscription auto-cancellation (grace period and enabled flag)",
		},
		SettingKeyInvoicePDFConfig: {
			Key:          SettingKeyInvoicePDFConfig,
			DefaultValue: invoicePDFConfigMap,
			Description:  "Default configuration for invoice PDF generation",
		},
		SettingKeyTenantConfig: {
			Key:          SettingKeyTenantConfig,
			DefaultValue: tenantConfigMap,
			Description:  "Default configuration for tenant (environment creation limits, production and sandbox)",
		},
		SettingKeyCustomerOnboarding: {
			Key:          SettingKeyCustomerOnboarding,
			DefaultValue: customerOnboardingConfigMap,
			Description:  "Default configuration for customer onboarding workflow",
		},
		SettingKeyWalletBalanceAlertConfig: {
			Key:          SettingKeyWalletBalanceAlertConfig,
			DefaultValue: defaultWalletBalanceAlertConfigMap,
			Description:  "Default configuration for wallet balance alert configuration",
		},
		SettingKeyPrepareProcessedEvents: {
			Key:          SettingKeyPrepareProcessedEvents,
			DefaultValue: defaultPrepareProcessedEventsConfigMap,
			Description:  "Configuration for preparing processed events (auto-create missing feature/meter/price and optional subscription rollout)",
		},
		SettingKeyCustomAnalytics: {
			Key: SettingKeyCustomAnalytics,
			DefaultValue: map[string]interface{}{
				"rules": []interface{}{},
			},
			Description: "Configuration for custom analytics calculations (e.g., revenue per minute)",
		},
		SettingKeyCustomerPortalConfig: {
			Key:          SettingKeyCustomerPortalConfig,
			DefaultValue: defaultCustomerPortalConfigMap,
			Description:  "Configuration for the customer self-service portal (branding, allowed sections, permissions)",
		},
		SettingKeyEventIngestionFilter: {
			Key:          SettingKeyEventIngestionFilter,
			DefaultValue: defaultEventIngestionFilterConfigMap,
			Description:  "Controls which external customer IDs are forwarded from raw events to the events pipeline (pilot allowlist)",
		},
		SettingKeyBonusCreditsTopupConfig: {
			Key:          SettingKeyBonusCreditsTopupConfig,
			DefaultValue: defaultBonusCreditsTopupConfigMap,
			Description:  "Slab-based bonus credit rules applied automatically to purchased wallet top-ups",
		},
		SettingKeyPaymentMandateLimits: {
			Key:          SettingKeyPaymentMandateLimits,
			DefaultValue: defaultPaymentMandateLimitsMap,
			Description:  "Per-rail auto-charge ceilings (e.g. UPI Autopay) used to cap mandate amounts; not an opt-in switch",
		},
	}, nil
}

// IsValidSettingKey checks if a setting key is valid
func IsValidSettingKey(key string) bool {
	defaults, err := GetDefaultSettings()
	if err != nil {
		return false
	}
	_, exists := defaults[SettingKey(key)]
	return exists
}

// ValidateSettingValue validates a setting value based on its key
// Uses centralized conversion (inline to avoid import cycle)
func ValidateSettingValue(key SettingKey, value map[string]interface{}) error {
	if err := key.Validate(); err != nil {
		return err
	}

	if value == nil {
		return ierr.NewErrorf("value cannot be nil").
			WithHint("Please provide a valid setting value").
			Mark(ierr.ErrValidation)
	}

	// Use ToStruct from conversion.go (same package, no import cycle)
	switch key {
	case SettingKeyInvoiceConfig:
		config, err := utils.ToStruct[InvoiceConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeySubscriptionConfig:
		config, err := utils.ToStruct[SubscriptionConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyInvoicePDFConfig:
		config, err := utils.ToStruct[InvoicePDFConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyTenantConfig:
		config, err := utils.ToStruct[TenantConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyCustomerOnboarding:
		// WorkflowConfig validation is handled in the service layer
		// Here we just do basic structure validation
		if _, ok := value["workflow_type"]; !ok {
			return ierr.NewError("workflow_type is required").
				WithHint("Please provide a workflow_type").
				Mark(ierr.ErrValidation)
		}
		if _, ok := value["actions"]; !ok {
			return ierr.NewError("actions field is required").
				WithHint("Please provide an actions array").
				Mark(ierr.ErrValidation)
		}
		return nil

	case SettingKeyWalletBalanceAlertConfig:
		config, err := utils.ToStruct[AlertSettings](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyPrepareProcessedEvents:
		config, err := utils.ToStruct[PrepareProcessedEventsConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyCustomAnalytics:
		config, err := utils.ToStruct[CustomAnalyticsConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyCustomerPortalConfig:
		config, err := utils.ToStruct[CustomerPortalConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyEventIngestionFilter:
		config, err := utils.ToStruct[EventIngestionFilterConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyBonusCreditsTopupConfig:
		config, err := utils.ToStruct[BonusCreditsTopupConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyPaymentMandateLimits:
		config, err := utils.ToStruct[PaymentMandateLimits](value)
		if err != nil {
			return err
		}
		return config.Validate()

	default:
		return ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// DefaultTimezone is the fallback IANA timezone used when a customer has no
// timezone set. All billing-period math and reset-window math degrade to UTC
// when the timezone is empty or equal to this value.
const DefaultTimezone = "UTC"

// timezoneAbbreviationMap maps common three-letter timezone abbreviations to IANA timezone identifiers
var timezoneAbbreviationMap = map[string]string{
	// Indian Standard Time
	"IST": "Asia/Kolkata",

	// US Timezones
	"EST":  "America/New_York",    // Eastern Standard Time
	"CST":  "America/Chicago",     // Central Standard Time
	"MST":  "America/Denver",      // Mountain Standard Time
	"PST":  "America/Los_Angeles", // Pacific Standard Time
	"HST":  "Pacific/Honolulu",    // Hawaii Standard Time
	"AKST": "America/Anchorage",   // Alaska Standard Time

	// European Timezones
	"GMT": "Europe/London", // Greenwich Mean Time
	"CET": "Europe/Berlin", // Central European Time
	"EET": "Europe/Athens", // Eastern European Time
	"WET": "Europe/Lisbon", // Western European Time
	"BST": "Europe/London", // British Summer Time

	// Asia Pacific
	"JST":  "Asia/Tokyo",       // Japan Standard Time
	"KST":  "Asia/Seoul",       // Korea Standard Time
	"CCT":  "Asia/Shanghai",    // China Coast Time (avoiding CST conflict)
	"AEST": "Australia/Sydney", // Australian Eastern Standard Time
	"AWST": "Australia/Perth",  // Australian Western Standard Time

	// Others
	"MSK": "Europe/Moscow",  // Moscow Standard Time
	"CAT": "Africa/Harare",  // Central Africa Time
	"EAT": "Africa/Nairobi", // East Africa Time
	"WAT": "Africa/Lagos",   // West Africa Time
}

// ResolveTimezone converts timezone abbreviation to IANA identifier or returns the input if it's already valid
func ResolveTimezone(timezone string) string {
	// First check if it's a known abbreviation
	if ianaName, exists := timezoneAbbreviationMap[strings.ToUpper(timezone)]; exists {
		return ianaName
	}

	// If not an abbreviation, return as-is (might be IANA name already)
	return timezone
}

// ValidateTimezone validates a timezone by converting abbreviations and checking with time.LoadLocation
func ValidateTimezone(timezone string) error {
	resolvedTimezone := ResolveTimezone(timezone)
	_, err := time.LoadLocation(resolvedTimezone)
	return err
}
