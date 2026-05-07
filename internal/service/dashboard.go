package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/shopspring/decimal"
)

// DashboardService provides dashboard functionality
type DashboardService interface {
	GetRevenues(ctx context.Context, req dto.DashboardRevenuesRequest) (*dto.DashboardRevenuesResponse, error)
	GetRevenueDashboard(ctx context.Context, req dto.RevenueDashboardRequest) (*dto.RevenueDashboardResponse, error)
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
			s.Logger.ErrorwCtx(ctx, "failed to get revenue trend", "error", err)
			// Continue with other sections even if this fails
		} else {
			response.RevenueTrend = revenueTrend
		}
	}

	// Recent Subscriptions - always fetch
	recentSubs, err := s.getRecentSubscriptions(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get recent subscriptions", "error", err)
		// Continue with other sections even if this fails
	} else {
		response.RecentSubscriptions = recentSubs
	}

	// Invoice Payment Status - always fetch
	paymentStatus, err := s.getInvoicePaymentStatus(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get invoice payment status", "error", err)
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

// GetRevenueDashboard returns revenue analytics with summary tiles and per-customer breakdown.
func (s *dashboardService) GetRevenueDashboard(ctx context.Context, req dto.RevenueDashboardRequest) (*dto.RevenueDashboardResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to validate revenue dashboard request").
			Mark(ierr.ErrValidation)
	}

	// Step 1: Check custom analytics config for CPM / voice minutes
	meterID, hasCustomAnalytics := s.resolveVoiceMeterID(ctx)

	// Step 2: Fetch revenue data
	revenueRows, err := s.InvoiceLineItemRepo.GetRevenueByCustomer(ctx, req.PeriodStart, req.PeriodEnd, req.CustomerIDs)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to fetch revenue by customer").
			Mark(ierr.ErrDatabase)
	}

	var voiceRows []domaininvoice.VoiceMinutesRow
	if hasCustomAnalytics && meterID != "" {
		voiceRows, err = s.InvoiceLineItemRepo.GetVoiceMinutesByCustomer(ctx, req.PeriodStart, req.PeriodEnd, meterID, req.CustomerIDs)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("failed to fetch voice minutes by customer").
				Mark(ierr.ErrDatabase)
		}
	}

	// Step 3: Aggregate per-customer data
	type customerData struct {
		usageRevenue decimal.Decimal
		fixedRevenue decimal.Decimal
		voiceMs      decimal.Decimal
	}
	customerMap := make(map[string]*customerData)

	for _, row := range revenueRows {
		cd, ok := customerMap[row.CustomerID]
		if !ok {
			cd = &customerData{}
			customerMap[row.CustomerID] = cd
		}
		if row.PriceType == string(types.PRICE_TYPE_USAGE) {
			cd.usageRevenue = cd.usageRevenue.Add(row.Amount)
		} else {
			cd.fixedRevenue = cd.fixedRevenue.Add(row.Amount)
		}
	}

	// Merge voice minutes
	if hasCustomAnalytics {
		for _, row := range voiceRows {
			cd, ok := customerMap[row.CustomerID]
			if !ok {
				cd = &customerData{}
				customerMap[row.CustomerID] = cd
			}
			cd.voiceMs = cd.voiceMs.Add(row.UsageMs)
		}
	}

	// Step 4: Bulk-fetch customer details for enrichment
	uniqueCustomerIDs := make([]string, 0, len(customerMap))
	for custID := range customerMap {
		uniqueCustomerIDs = append(uniqueCustomerIDs, custID)
	}

	type customerInfo struct {
		Name       string
		ExternalID string
	}
	customerInfoMap := make(map[string]customerInfo, len(uniqueCustomerIDs))

	if len(uniqueCustomerIDs) > 0 {
		custFilter := &types.CustomerFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			CustomerIDs: uniqueCustomerIDs,
		}
		customers, err := s.CustomerRepo.List(ctx, custFilter)
		if err != nil {
			s.Logger.WarnwCtx(ctx, "failed to fetch customer details for revenue dashboard", "error", err)
			// Continue without customer details rather than failing the entire request
		} else {
			for _, c := range customers {
				customerInfoMap[c.ID] = customerInfo{
					Name:       c.Name,
					ExternalID: c.ExternalID,
				}
			}
		}
	}

	// Step 5: Build response — filter zero-revenue customers
	msPerMinute := decimal.NewFromInt(60000)

	var items []dto.RevenueDashboardCustomer
	summaryUsage := decimal.Zero
	summaryFixed := decimal.Zero
	summaryVoiceMs := decimal.Zero

	for custID, cd := range customerMap {
		totalRevenue := cd.usageRevenue.Add(cd.fixedRevenue)
		if totalRevenue.IsZero() {
			continue
		}

		summaryUsage = summaryUsage.Add(cd.usageRevenue)
		summaryFixed = summaryFixed.Add(cd.fixedRevenue)
		summaryVoiceMs = summaryVoiceMs.Add(cd.voiceMs)

		info := customerInfoMap[custID]
		cust := dto.RevenueDashboardCustomer{
			CustomerID:         custID,
			CustomerName:       info.Name,
			ExternalCustomerID: info.ExternalID,
			TotalRevenue:       totalRevenue,
			TotalUsageRevenue:  cd.usageRevenue,
			TotalFixedRevenue:  cd.fixedRevenue,
		}

		if hasCustomAnalytics {
			voiceMinutes := cd.voiceMs.Div(msPerMinute)
			cust.VoiceMinutes = &voiceMinutes

			if !cd.voiceMs.IsZero() {
				cpm := cd.usageRevenue.Div(voiceMinutes)
				cust.CPM = &cpm
			}
		}

		items = append(items, cust)
	}

	// Build summary
	summaryTotal := summaryUsage.Add(summaryFixed)
	summary := dto.RevenueDashboardSummary{
		TotalRevenue:      summaryTotal,
		TotalUsageRevenue: summaryUsage,
		TotalFixedRevenue: summaryFixed,
	}

	if hasCustomAnalytics {
		voiceMinutes := summaryVoiceMs.Div(msPerMinute)
		summary.VoiceMinutes = &voiceMinutes

		if !summaryVoiceMs.IsZero() {
			cpm := summaryUsage.Div(voiceMinutes)
			summary.CPM = &cpm
		}
	}

	// Sort by total revenue descending so highest-revenue customers appear first
	sort.Slice(items, func(i, j int) bool {
		return items[i].TotalRevenue.GreaterThan(items[j].TotalRevenue)
	})

	if items == nil {
		items = []dto.RevenueDashboardCustomer{}
	}

	var graph *dto.RevenueDashboardGraph
	if hasCustomAnalytics && meterID != "" {
		const dateTruncMonth = "month"

		revenueTS, tsErr := s.InvoiceLineItemRepo.GetRevenueTimeSeries(ctx, req.PeriodStart, req.PeriodEnd, dateTruncMonth, req.CustomerIDs)
		if tsErr != nil {
			return nil, ierr.WithError(tsErr).
				WithHint("failed to fetch revenue time series for graph").
				Mark(ierr.ErrDatabase)
		}

		voiceTS, vErr := s.InvoiceLineItemRepo.GetVoiceMinutesTimeSeries(ctx, req.PeriodStart, req.PeriodEnd, meterID, dateTruncMonth, req.CustomerIDs)
		if vErr != nil {
			return nil, ierr.WithError(vErr).
				WithHint("failed to fetch voice minutes time series for graph").
				Mark(ierr.ErrDatabase)
		}

		revenueByWindow := aggregateRevenueDashboardByWindow(revenueTS)
		graph = &dto.RevenueDashboardGraph{
			TotalRevenue: buildRevenueDashboardGraphPoints(revenueByWindow),
			VoiceMinutes: buildVoiceMinutesDashboardGraphPoints(voiceTS),
		}
	}

	return &dto.RevenueDashboardResponse{
		Summary: summary,
		Items:   items,
		Graph:   graph,
	}, nil
}

func aggregateRevenueDashboardByWindow(rows []domaininvoice.RevenueTimeSeriesRow) map[time.Time]decimal.Decimal {
	out := make(map[time.Time]decimal.Decimal)
	for _, row := range rows {
		out[row.WindowStart.UTC()] = out[row.WindowStart.UTC()].Add(row.Amount)
	}
	return out
}

func buildRevenueDashboardGraphPoints(agg map[time.Time]decimal.Decimal) []types.RevenueGraphPoint {
	if len(agg) == 0 {
		return []types.RevenueGraphPoint{}
	}
	keys := make([]time.Time, 0, len(agg))
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	points := make([]types.RevenueGraphPoint, 0, len(keys))
	for _, k := range keys {
		points = append(points, types.RevenueGraphPoint{
			Label: revenueDashboardGraphMonthLabel(k),
			Value: agg[k].String(),
		})
	}
	return points
}

func buildVoiceMinutesDashboardGraphPoints(rows []domaininvoice.VoiceMinutesTimeSeriesRow) []types.RevenueGraphPoint {
	if len(rows) == 0 {
		return []types.RevenueGraphPoint{}
	}
	msPerMinute := decimal.NewFromInt(60000)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].WindowStart.UTC().Before(rows[j].WindowStart.UTC())
	})
	points := make([]types.RevenueGraphPoint, 0, len(rows))
	for _, row := range rows {
		minutes := row.UsageMs.Div(msPerMinute)
		points = append(points, types.RevenueGraphPoint{
			Label: revenueDashboardGraphMonthLabel(row.WindowStart),
			Value: minutes.String(),
		})
	}
	return points
}

func revenueDashboardGraphMonthLabel(t time.Time) string {
	return t.UTC().Format("Jan 2006")
}

// resolveVoiceMeterID checks the custom_analytics_config setting for the
// "revenue-per-minute" rule and resolves the target feature to its meter ID.
// Returns (meterID, true) when custom analytics are active, ("", false) otherwise.
func (s *dashboardService) resolveVoiceMeterID(ctx context.Context) (string, bool) {
	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyCustomAnalytics)
	if err != nil || setting == nil || setting.Value == nil {
		return "", false
	}

	config, err := utils.ToStruct[types.CustomAnalyticsConfig](setting.Value)
	if err != nil {
		s.Logger.WarnwCtx(ctx, "failed to parse custom analytics config", "error", err)
		return "", false
	}

	// Find the revenue-per-minute rule
	for _, rule := range config.Rules {
		if types.CustomAnalyticsRuleID(rule.ID) != types.CustomAnalyticsRuleRevenuePerMinute {
			continue
		}

		if rule.TargetType == "feature" {
			feature, err := s.FeatureRepo.Get(ctx, rule.TargetID)
			if err != nil || feature == nil {
				s.Logger.WarnwCtx(ctx, "failed to resolve feature for revenue-per-minute rule",
					"error", err,
					"target_id", rule.TargetID,
				)
				return "", false
			}
			if feature.MeterID == "" {
				s.Logger.WarnwCtx(ctx, "feature has no meter_id for revenue-per-minute rule",
					"feature_id", feature.ID,
				)
				return "", false
			}
			return feature.MeterID, true
		}

		if rule.TargetType == "meter" {
			return rule.TargetID, true
		}

		return "", false
	}

	return "", false
}
