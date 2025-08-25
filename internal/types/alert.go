package types

type AlertState string

const (
	AlertStateOk      AlertState = "ok"
	AlertStateInAlarm AlertState = "in_alarm"
)

// AlertFilter represents the filter options for alerts
type AlertFilter struct {
	*QueryFilter
	AlertIDs     []string     `json:"alert_ids,omitempty" form:"alert_ids"`
	EntityTypes  []string     `json:"entity_types,omitempty" form:"entity_types"`
	EntityIDs    []string     `json:"entity_ids,omitempty" form:"entity_ids"`
	AlertMetrics []string     `json:"alert_metrics,omitempty" form:"alert_metrics"`
	AlertStates  []AlertState `json:"alert_states,omitempty" form:"alert_states"`
	AlertEnabled *bool        `json:"alert_enabled,omitempty" form:"alert_enabled"`
}

// NewAlertFilter creates a new alert filter with default values
func NewAlertFilter() *AlertFilter {
	return &AlertFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// Validate validates the alert filter
func (f *AlertFilter) Validate() error {
	if f.QueryFilter == nil {
		f.QueryFilter = NewDefaultQueryFilter()
	}
	return f.QueryFilter.Validate()
}

// Entity represents a generic entity that can be monitored
type Entity struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	AlertEnabled bool         `json:"alert_enabled"`
	AlertMetric  string       `json:"alert_metric"`
	AlertConfig  *AlertConfig `json:"alert_config,omitempty"`

	Data map[string]interface{} `json:"data,omitempty"`
}
