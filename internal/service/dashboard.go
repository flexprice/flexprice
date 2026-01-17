package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// DashboardService provides dashboard functionality
type DashboardService interface {
	GetRevenues(ctx context.Context, req dto.DashboardRevenuesRequest) (*dto.DashboardRevenuesResponse, error)
}

type dashboardService struct {
	ServiceParams
}

// NewDashboardService creates a new dashboard service
func NewDashboardService(
	params ServiceParams,
) DashboardService {
	return &dashboardService{
		ServiceParams: params,
	}
}

// GetRevenues returns dashboard revenues data
func (s *dashboardService) GetRevenues(ctx context.Context, req dto.DashboardRevenuesRequest) (*dto.DashboardRevenuesResponse, error) {
	response := &dto.DashboardRevenuesResponse{}

	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to get dashboard revenues").
			Mark(ierr.ErrValidation)
	}

	// Revenue Trend
	if req.RevenueTrend != nil {
		revenueTrend, err := s.getRevenueTrend(ctx, req.RevenueTrend)
		if err != nil {
			s.Logger.Errorw("failed to get revenue trend", "error", err)
			// Continue with other sections even if this fails
		} else {
			response.RevenueTrend = revenueTrend
		}
	}

	// Recent Subscriptions - always fetch
	recentSubs, err := s.getRecentSubscriptions(ctx)
	if err != nil {
		s.Logger.Errorw("failed to get recent subscriptions", "error", err)
		// Continue with other sections even if this fails
	} else {
		response.RecentSubscriptions = recentSubs
	}

	// Invoice Payment Status - always fetch
	paymentStatus, err := s.getInvoicePaymentStatus(ctx)
	if err != nil {
		s.Logger.Errorw("failed to get invoice payment status", "error", err)
		// Continue with other sections even if this fails
	} else {
		response.InvoicePaymentStatus = paymentStatus
	}

	return response, nil
}

// getRevenueTrend calculates revenue trend data using repository
func (s *dashboardService) getRevenueTrend(ctx context.Context, req *dto.RevenueTrendRequest) (*dto.RevenueTrendResponse, error) {
	// Call repository method - always use MONTH window size
	windows, err := s.InvoiceRepo.GetRevenueTrend(ctx, *req.WindowCount)
	if err != nil {
		return nil, err
	}

	// Group by currency
	currencyMap := make(map[string][]types.RevenueWindow)
	for _, w := range windows {
		// Format label for MONTH window size
		label := w.WindowStart.Format("Jan 2006")
		revenueWindow := types.RevenueWindow{
			WindowStart:  w.WindowStart,
			WindowEnd:    w.WindowEnd,
			WindowLabel:  label,
			TotalRevenue: w.Revenue,
		}

		currency := strings.ToLower(strings.TrimSpace(w.Currency))
		if currency == "" {
			return nil, ierr.NewError("currency is missing for revenue data").
				WithHint("Revenue data must include currency information").
				Mark(ierr.ErrValidation)
		}
		currencyMap[currency] = append(currencyMap[currency], revenueWindow)
	}

	// Convert to response structure
	currencyRevenueWindows := make(map[string]dto.CurrencyRevenueWindows)
	for currency, windows := range currencyMap {
		currencyRevenueWindows[currency] = dto.CurrencyRevenueWindows{
			Windows: windows,
		}
	}

	return &dto.RevenueTrendResponse{
		Currency:    currencyRevenueWindows,
		WindowSize:  types.WindowSizeMonth,
		WindowCount: *req.WindowCount,
	}, nil
}

// getRecentSubscriptions gets recent subscriptions grouped by plan using repository
func (s *dashboardService) getRecentSubscriptions(ctx context.Context) (*dto.RecentSubscriptionsResponse, error) {
	// Call repository method
	planCounts, err := s.SubRepo.GetRecentSubscriptionsByPlan(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to DTO
	plans := make([]types.SubscriptionPlanCount, 0, len(planCounts))
	totalCount := 0
	for _, pc := range planCounts {
		plans = append(plans, types.SubscriptionPlanCount{
			PlanID:   pc.PlanID,
			PlanName: pc.PlanName,
			Count:    pc.Count,
		})
		totalCount += pc.Count
	}

	now := time.Now().UTC()
	periodStart := now.AddDate(0, 0, -7) // 7 days ago
	return &dto.RecentSubscriptionsResponse{
		TotalCount:  totalCount,
		Plans:       plans,
		PeriodStart: periodStart,
		PeriodEnd:   now,
	}, nil
}

// getInvoicePaymentStatus gets invoice payment status counts using repository
func (s *dashboardService) getInvoicePaymentStatus(ctx context.Context) (*dto.InvoicePaymentStatusResponse, error) {
	// Call repository method
	status, err := s.InvoiceRepo.GetInvoicePaymentStatus(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	periodStart := now.AddDate(0, 0, -7) // T-7 days from now
	return &dto.InvoicePaymentStatusResponse{
		Paid:        status.Succeeded,
		Pending:     status.Pending,
		Failed:      status.Failed,
		PeriodStart: periodStart,
		PeriodEnd:   now,
	}, nil
}
