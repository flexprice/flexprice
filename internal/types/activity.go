package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ActivityType represents the type of activity
type ActivityType string

const (
	// Activity Types - must match the method names in activity structs
	ActivitySyncPlanPrices ActivityType = "SyncPlanPrices"
	ActivityFetchData      ActivityType = "FetchData"
	ActivityCalculate      ActivityType = "Calculate"
)

// Activity Names - using clean aliases for Temporal registration
const (
	// Plan Activities
	PlanActivitySyncPlanPrices = "PlanActivities.SyncPlanPrices"

	// Billing Activities
	BillingActivityFetchData = "BillingActivities.FetchDataActivity"
	BillingActivityCalculate = "BillingActivities.CalculateActivity"
)

// ActivityInfo holds information about an activity
type ActivityInfo struct {
	Name string // Fully qualified name (e.g., "PlanActivities.SyncPlanPrices")
	Type ActivityType
}

// String returns the string representation of the activity type
func (a ActivityType) String() string {
	return string(a)
}

// Validate validates the activity type
func (a ActivityType) Validate() error {
	switch a {
	case ActivitySyncPlanPrices, ActivityFetchData, ActivityCalculate:
		return nil
	default:
		return ierr.NewError("invalid activity type").
			WithHint("Activity type must be one of: SyncPlanPrices, FetchData, Calculate").
			Mark(ierr.ErrValidation)
	}
}

// QualifiedName returns the fully qualified activity name
func (a ActivityType) QualifiedName(prefix string) string {
	return prefix + "." + string(a)
}
