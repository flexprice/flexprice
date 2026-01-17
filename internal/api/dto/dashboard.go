package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// DashboardRevenuesRequest represents the request for dashboard revenues API
type DashboardRevenuesRequest struct {
	RevenueTrend *RevenueTrendRequest `json:"revenue_trend,omitempty"`
}

// Validate validates the dashboard revenues request
func (r *DashboardRevenuesRequest) Validate() error {
	// Initialize RevenueTrend if nil
	if r.RevenueTrend == nil {
		r.RevenueTrend = &RevenueTrendRequest{}
	}
	return r.RevenueTrend.Validate()
}

// RevenueTrendRequest represents parameters for revenue trend section
type RevenueTrendRequest struct {
	WindowCount *int `json:"window_count,omitempty"` //by default, it's 3
}

// Validate validates the revenue trend request
func (r *RevenueTrendRequest) Validate() error {
	// Validate window_count if provided
	if r.WindowCount != nil && *r.WindowCount <= 0 {
		return ierr.NewError("window_count must be >= 1").
			WithHint("window_count must be a positive integer").
			WithReportableDetails(map[string]interface{}{
				"window_count": *r.WindowCount,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.WindowCount == nil {
		r.WindowCount = lo.ToPtr(types.DefaultWindowCount)
	}

	return nil
}

// DashboardRevenuesResponse represents the response for dashboard revenues API
type DashboardRevenuesResponse struct {
	RevenueTrend         *RevenueTrendResponse         `json:"revenue_trend,omitempty"`
	RecentSubscriptions  *RecentSubscriptionsResponse  `json:"recent_subscriptions,omitempty"`
	InvoicePaymentStatus *InvoicePaymentStatusResponse `json:"invoice_payment_status,omitempty"`
}

// RevenueTrendResponse represents revenue trend data
type RevenueTrendResponse struct {
	Currency    map[string]CurrencyRevenueWindows `json:"currency_revenue_windows"`
	WindowSize  types.WindowSize                  `json:"window_size"`
	WindowCount int                               `json:"window_count"`
}

// CurrencyRevenueWindows represents windows for a specific currency
type CurrencyRevenueWindows struct {
	Windows []types.RevenueWindow `json:"windows"`
}

// RecentSubscriptionsResponse represents recent subscriptions data
type RecentSubscriptionsResponse struct {
	TotalCount  int                           `json:"total_count"`
	Plans       []types.SubscriptionPlanCount `json:"plans"`
	PeriodStart time.Time                     `json:"period_start"`
	PeriodEnd   time.Time                     `json:"period_end"`
}

// InvoicePaymentStatusResponse represents invoice payment status counts
// Note: Paid field maps to types.InvoicePaymentStatus.Succeeded for API consistency
type InvoicePaymentStatusResponse struct {
	Paid        int       `json:"paid"`    // Maps to InvoicePaymentStatus.Succeeded
	Pending     int       `json:"pending"` // Maps to InvoicePaymentStatus.Pending
	Failed      int       `json:"failed"`  // Maps to InvoicePaymentStatus.Failed
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}
