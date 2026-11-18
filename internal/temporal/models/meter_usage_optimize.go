package models

// MeterUsageOptimizeInput is the input for MeterUsageOptimizeWorkflow.
type MeterUsageOptimizeInput struct{}

// MeterUsageOptimizeResult counts partitions by outcome.
type MeterUsageOptimizeResult struct {
	Optimized int `json:"optimized"`
	Failed    int `json:"failed"`
}
