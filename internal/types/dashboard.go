package types

import (
	"time"
)

const (
	// DefaultWindowSize is the default window size for revenue trend
	DefaultWindowSize = WindowSizeMonth
	// DefaultWindowCount is the default window count for revenue trend
	DefaultWindowCount = 3
)

// RevenueTrendWindow represents a revenue window result from repository
type RevenueTrendWindow struct {
	WindowIndex int
	WindowStart time.Time
	WindowEnd   time.Time
	Revenue     string
	Currency    string
}

// InvoicePaymentStatus represents invoice payment status counts from repository
type InvoicePaymentStatus struct {
	Pending   int
	Succeeded int
	Failed    int
}

// RevenueWindow represents a single time window with revenue data
type RevenueWindow struct {
	WindowStart  time.Time `json:"window_start"`  // Window start time (matches existing pattern)
	WindowEnd    time.Time `json:"window_end"`    // Window end time
	WindowLabel  string    `json:"window_label"`  // Human-readable label
	TotalRevenue string    `json:"total_revenue"` // Total revenue for this window
}

// SubscriptionPlanCount represents subscription count for a plan
type SubscriptionPlanCount struct {
	PlanID   string `json:"plan_id"`
	PlanName string `json:"plan_name"`
	Count    int    `json:"count"`
}
