package models

// MeterUsageOptimizeInput is the input for MeterUsageOptimizeWorkflow.
type MeterUsageOptimizeInput struct{}

// MeterUsageOptimizeResult counts partitions by outcome.
type MeterUsageOptimizeResult struct {
	Optimized int `json:"optimized"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}
