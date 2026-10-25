package models

import "time"

// RevenueRecognitionInput is the input for RevenueRecognitionWorkflow. Since overrides the billing window's start
// derived from the scheduled time, for manual reruns.
type RevenueRecognitionInput struct {
	Since *time.Time `json:"since,omitempty"`
}

// RecognizeRevenueActivityInput is one run: billing changed since Since, revenue recognized as of AsOf.
type RecognizeRevenueActivityInput struct {
	Since time.Time `json:"since"`
	AsOf  time.Time `json:"as_of"`
}

// RevenueRecognitionBackfillInput names the one environment RevenueRecognitionBackfillWorkflow rebuilds.
type RevenueRecognitionBackfillInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// BackfillRevenueActivityInput is the environment to rebuild, recognized as of AsOf.
type BackfillRevenueActivityInput struct {
	TenantID      string    `json:"tenant_id"`
	EnvironmentID string    `json:"environment_id"`
	AsOf          time.Time `json:"as_of"`
}
