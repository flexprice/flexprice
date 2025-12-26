package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Default threshold for wallet alerts
// const WalletBalanceAlertThreshold = 0.5

// AlertState represents the current state of a wallet alert
type AlertState string

const (
	AlertStateOk      AlertState = "ok"
	AlertStateInfo    AlertState = "info"
	AlertStateWarning AlertState = "warning"
	AlertStateInAlarm AlertState = "in_alarm"
)

type AlertType string

const (
	AlertTypeLowOngoingBalance    AlertType = "low_ongoing_balance"
	AlertTypeLowCreditBalance     AlertType = "low_credit_balance"
	AlertTypeFeatureWalletBalance AlertType = "feature_wallet_balance"
)

// AlertEntityType represents the type of entity for alerts
type AlertEntityType string

const (
	AlertEntityTypeWallet  AlertEntityType = "wallet"
	AlertEntityTypeFeature AlertEntityType = "feature"
)

func (aet AlertEntityType) Validate() error {
	allowedTypes := []AlertEntityType{
		AlertEntityTypeWallet,
		AlertEntityTypeFeature,
	}
	if !lo.Contains(allowedTypes, aet) {
		return ierr.NewError("invalid alert entity type").
			WithHint("Please provide a valid alert entity type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AlertThresholdType represents the type of threshold for alerts
type AlertThresholdType string

const (
	AlertThresholdTypeAmount AlertThresholdType = "amount"
)

func (att AlertThresholdType) Validate() error {
	allowedTypes := []AlertThresholdType{
		AlertThresholdTypeAmount,
	}
	if !lo.Contains(allowedTypes, att) {
		return ierr.NewError("invalid alert threshold type").
			WithHint("Please provide a valid alert threshold type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (at AlertType) Validate() error {
	allowedTypes := []AlertType{
		AlertTypeLowOngoingBalance,
		AlertTypeLowCreditBalance,
		AlertTypeFeatureWalletBalance,
	}
	if !lo.Contains(allowedTypes, at) {
		return ierr.NewError("invalid alert type").
			WithHint("Please provide a valid alert type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type AlertInfo struct {
	AlertSettings *AlertSettings  `json:"alert_settings,omitempty"`
	ValueAtTime   decimal.Decimal `json:"value_at_time"`
	Timestamp     time.Time       `json:"timestamp"`
}

// AlertConfig represents the configuration for wallet alerts
type AlertConfig struct {
	Threshold *WalletAlertThreshold `json:"threshold,omitempty"`
}

// Validate implements SettingConfig interface
func (c AlertConfig) Validate() error {
	// Validate that threshold exists and is valid
	if c.Threshold == nil {
		return ierr.NewError("threshold is required").
			WithHint("Please provide a threshold").
			Mark(ierr.ErrValidation)
	}
	// Validate threshold type
	if err := c.Threshold.Type.Validate(); err != nil {
		return err
	}
	return nil
}

// WalletAlertThreshold represents the threshold configuration for wallet alerts
type WalletAlertThreshold struct {
	Type  AlertThresholdType `json:"type"` // amount
	Value decimal.Decimal    `json:"value"`
}

// AlertLogFilter represents filters for alert log queries
type AlertLogFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters     []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort        []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	EntityType  AlertEntityType    `json:"entity_type,omitempty" form:"entity_type" validate:"omitempty"`
	EntityID    string             `json:"entity_id,omitempty" form:"entity_id" validate:"omitempty"`
	AlertType   AlertType          `json:"alert_type,omitempty" form:"alert_type" validate:"omitempty"`
	AlertStatus AlertState         `json:"alert_status,omitempty" form:"alert_status" validate:"omitempty"`
	CustomerID  string             `json:"customer_id,omitempty" form:"customer_id" validate:"omitempty"`
	Expand      *string            `json:"expand,omitempty" form:"expand" validate:"omitempty"`
}

// NewDefaultAlertLogFilter creates a new AlertLogFilter with default values
func NewDefaultAlertLogFilter() *AlertLogFilter {
	return &AlertLogFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitAlertLogFilter creates a new AlertLogFilter with no pagination limits
func NewNoLimitAlertLogFilter() *AlertLogFilter {
	return &AlertLogFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the alert log filter
func (f *AlertLogFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if f.AlertType != "" {
		if err := f.AlertType.Validate(); err != nil {
			return err
		}
	}

	if f.EntityType != "" {
		if err := f.EntityType.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *AlertLogFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *AlertLogFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetExpand returns the Expand for the filter
func (f *AlertLogFilter) GetExpand() Expand {
	if f.Expand == nil || *f.Expand == "" {
		return NewExpand("")
	}
	return NewExpand(*f.Expand)
}

type AlertSettings struct {
	Critical     *AlertThreshold `json:"critical"`
	Warning      *AlertThreshold `json:"warning"`
	Info         *AlertThreshold `json:"info"`
	AlertEnabled *bool           `json:"alert_enabled"`
}

func (at *AlertSettings) Validate() error {
	// If alert_enabled is true, at least one threshold must be provided
	if at.AlertEnabled != nil && *at.AlertEnabled {
		if at.Critical == nil && at.Warning == nil && at.Info == nil {
			return ierr.NewError("at least one threshold (critical, warning, or info) is required when alert_enabled is true").
				WithHint("Please provide at least one threshold configuration").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate critical threshold if provided
	if at.Critical != nil {
		// critical condition must be provided either above or below
		if at.Critical.Condition != AlertConditionAbove && at.Critical.Condition != AlertConditionBelow {
			return ierr.NewError("critical threshold condition must be either above or below").
				WithHint("Please provide a valid critical threshold condition").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate warning threshold if provided
	if at.Warning != nil {
		// If warning is provided, critical must also be provided for validation
		if at.Critical == nil {
			return ierr.NewError("critical threshold is required when warning threshold is provided").
				WithHint("Please provide a critical threshold").
				Mark(ierr.ErrValidation)
		}
		switch at.Critical.Condition {
		case AlertConditionAbove:
			// warning threshold must be less than critical threshold
			if at.Warning.Threshold.GreaterThan(at.Critical.Threshold) {
				return ierr.NewError("warning threshold must be less than critical threshold").
					WithHint("Please provide a valid warning threshold").
					Mark(ierr.ErrValidation)
			}
			// warning condition must be same as critical condition
			if at.Warning.Condition != at.Critical.Condition {
				return ierr.NewError("warning condition must be same as critical condition").
					WithHint("Please provide a valid warning condition").
					Mark(ierr.ErrValidation)
			}
		case AlertConditionBelow:
			// warning threshold must be greater than critical threshold
			if at.Warning.Threshold.LessThan(at.Critical.Threshold) {
				return ierr.NewError("warning threshold must be greater than critical threshold").
					WithHint("Please provide a valid warning threshold").
					Mark(ierr.ErrValidation)
			}
			// warning condition must be same as critical condition
			if at.Warning.Condition != at.Critical.Condition {
				return ierr.NewError("warning condition must be same as critical condition").
					WithHint("Please provide a valid warning condition").
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate info threshold if provided - Info can stand alone!
	if at.Info != nil {
		// Info condition must be valid
		if at.Info.Condition != AlertConditionAbove && at.Info.Condition != AlertConditionBelow {
			return ierr.NewError("info threshold condition must be either above or below").
				WithHint("Please provide a valid info threshold condition").
				Mark(ierr.ErrValidation)
		}

		// If warning exists, validate info against warning
		if at.Warning != nil {
			// Both must use the same condition
			if at.Info.Condition != at.Warning.Condition {
				return ierr.NewError("info condition must be same as warning condition").
					WithHint("Please provide a valid info condition").
					Mark(ierr.ErrValidation)
			}

			switch at.Warning.Condition {
			case AlertConditionAbove:
				// info threshold must be less than warning threshold
				if at.Info.Threshold.GreaterThan(at.Warning.Threshold) {
					return ierr.NewError("info threshold must be less than warning threshold").
						WithHint("Please provide a valid info threshold").
						Mark(ierr.ErrValidation)
				}
			case AlertConditionBelow:
				// info threshold must be greater than warning threshold
				if at.Info.Threshold.LessThan(at.Warning.Threshold) {
					return ierr.NewError("info threshold must be greater than warning threshold").
						WithHint("Please provide a valid info threshold").
						Mark(ierr.ErrValidation)
				}
			}
		} else if at.Critical != nil {
			// If critical exists but no warning, validate info against critical
			if at.Info.Condition != at.Critical.Condition {
				return ierr.NewError("info condition must be same as critical condition").
					WithHint("Please provide a valid info condition").
					Mark(ierr.ErrValidation)
			}

			switch at.Critical.Condition {
			case AlertConditionAbove:
				// info threshold must be less than critical threshold
				if at.Info.Threshold.GreaterThan(at.Critical.Threshold) {
					return ierr.NewError("info threshold must be less than critical threshold").
						WithHint("Please provide a valid info threshold").
						Mark(ierr.ErrValidation)
				}
			case AlertConditionBelow:
				// info threshold must be greater than critical threshold
				if at.Info.Threshold.LessThan(at.Critical.Threshold) {
					return ierr.NewError("info threshold must be greater than critical threshold").
						WithHint("Please provide a valid info threshold").
						Mark(ierr.ErrValidation)
				}
			}
		}
		// If neither critical nor warning exist, info can stand alone - no validation needed
	}

	return nil
}

type AlertThreshold struct {
	Threshold decimal.Decimal `json:"threshold"`
	Condition AlertCondition  `json:"condition"`
}

func (at *AlertThreshold) Validate() error {
	if at.Condition == "" {
		return ierr.NewError("alert threshold condition is required").
			WithHint("Please provide a valid alert threshold condition").
			Mark(ierr.ErrValidation)
	}
	if at.Condition != AlertConditionAbove && at.Condition != AlertConditionBelow {
		return ierr.NewError("alert threshold condition must be either above or below").
			WithHint("Please provide a valid alert threshold condition").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type AlertCondition string

const (
	AlertConditionAbove AlertCondition = "above"
	AlertConditionBelow AlertCondition = "below"
)

func (ac AlertCondition) Validate() error {
	allowedConditions := []AlertCondition{
		AlertConditionAbove,
		AlertConditionBelow,
	}
	if !lo.Contains(allowedConditions, ac) {
		return ierr.NewError("invalid alert condition").
			WithHint("Please provide a valid alert condition").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AlertStatus determines the alert status based on ongoing balance vs alert settings
func (At *AlertSettings) AlertState(ongoingBalance decimal.Decimal) (AlertState, error) {
	// Determine which condition to use (check critical first, then warning, then info)
	var condition AlertCondition
	if At.Critical != nil {
		condition = At.Critical.Condition
	} else if At.Warning != nil {
		condition = At.Warning.Condition
	} else if At.Info != nil {
		condition = At.Info.Condition
	} else {
		return "", ierr.NewError("Alert State determination failed - no thresholds configured").
			WithHint("Please provide at least one threshold").
			Mark(ierr.ErrValidation)
	}

	switch condition {
	case AlertConditionAbove:
		// Check in order of severity: critical > warning > info > ok
		if At.Critical != nil && ongoingBalance.GreaterThanOrEqual(At.Critical.Threshold) {
			return AlertStateInAlarm, nil
		}
		if At.Warning != nil && ongoingBalance.GreaterThanOrEqual(At.Warning.Threshold) {
			return AlertStateWarning, nil
		}
		if At.Info != nil && ongoingBalance.GreaterThanOrEqual(At.Info.Threshold) {
			return AlertStateInfo, nil
		}
		return AlertStateOk, nil

	case AlertConditionBelow:
		// Check in order of severity: critical > warning > info > ok
		if At.Critical != nil && ongoingBalance.LessThanOrEqual(At.Critical.Threshold) {
			return AlertStateInAlarm, nil
		}
		if At.Warning != nil && ongoingBalance.LessThanOrEqual(At.Warning.Threshold) {
			return AlertStateWarning, nil
		}
		if At.Info != nil && ongoingBalance.LessThanOrEqual(At.Info.Threshold) {
			return AlertStateInfo, nil
		}
		return AlertStateOk, nil
	}

	return "", ierr.NewError("Alert State determination failed").
		WithHint("Please provide a valid alert settings").
		Mark(ierr.ErrValidation)
}

func (at *AlertSettings) IsAlertEnabled() bool {
	return at.AlertEnabled != nil && *at.AlertEnabled
}
