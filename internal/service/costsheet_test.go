package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainCostSheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CostSheetServiceSuite struct {
	suite.Suite
	ctx              context.Context
	costSheetService CostSheetService
	costSheetRepo    *testutil.InMemoryCostSheetStore
	subscriptionRepo *testutil.InMemorySubscriptionStore
	logger           *logger.Logger
}

func TestCostSheetService(t *testing.T) {
	suite.Run(t, new(CostSheetServiceSuite))
}

func (s *CostSheetServiceSuite) SetupTest() {
	// Initialize context with tenant and environment IDs
	s.ctx = context.Background()
	s.ctx = context.WithValue(s.ctx, types.CtxTenantID, "test-tenant")
	s.ctx = context.WithValue(s.ctx, types.CtxEnvironmentID, "test-env")

	// Initialize config and logger
	cfg, err := config.NewConfig()
	s.Require().NoError(err)
	s.logger, err = logger.NewLogger(cfg)
	s.Require().NoError(err)

	// Initialize repositories
	s.costSheetRepo = testutil.NewInMemoryCostSheetStore()
	s.subscriptionRepo = testutil.NewInMemorySubscriptionStore()

	// Initialize service
	s.costSheetService = NewCostSheetService(ServiceParams{
		Logger:        s.logger,
		CostSheetRepo: s.costSheetRepo,
		SubRepo:       s.subscriptionRepo,
	})
}

func (s *CostSheetServiceSuite) TestCalculateMargin() {
	testCases := []struct {
		name        string
		cost        decimal.Decimal
		revenue     decimal.Decimal
		wantMargin  decimal.Decimal
		description string
	}{
		{
			name:        "Cost greater than revenue",
			cost:        decimal.NewFromInt(100),
			revenue:     decimal.NewFromInt(50),
			wantMargin:  decimal.NewFromFloat(-1.0), // (50-100)/50 = -1.0
			description: "Should return negative margin when cost exceeds revenue",
		},
		{
			name:        "Revenue greater than cost",
			cost:        decimal.NewFromInt(100),
			revenue:     decimal.NewFromInt(150),
			wantMargin:  decimal.NewFromFloat(0.3333333333333333), // (150-100)/150 = 0.3333...
			description: "Should return positive margin when revenue exceeds cost",
		},
		{
			name:        "Revenue equals cost",
			cost:        decimal.NewFromInt(100),
			revenue:     decimal.NewFromInt(100),
			wantMargin:  decimal.Zero, // (100-100)/100 = 0
			description: "Should return zero margin when revenue equals cost",
		},
		{
			name:        "Both cost and revenue zero",
			cost:        decimal.Zero,
			revenue:     decimal.Zero,
			wantMargin:  decimal.Zero,
			description: "Should return zero margin when both inputs are zero",
		},
		{
			name:        "Revenue positive, cost zero",
			cost:        decimal.Zero,
			revenue:     decimal.NewFromInt(100),
			wantMargin:  decimal.NewFromInt(1), // (100-0)/100 = 1.0
			description: "Should return 100% margin when cost is zero",
		},
		{
			name:        "Cost positive, revenue zero",
			cost:        decimal.NewFromInt(100),
			revenue:     decimal.Zero,
			wantMargin:  decimal.Zero, // Revenue is zero, so return zero to avoid division by zero
			description: "Should return zero margin when revenue is zero (division by zero protection)",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			gotMargin := s.costSheetService.CalculateMargin(tc.cost, tc.revenue)
			s.Equal(tc.wantMargin.String(), gotMargin.String(), tc.description)
		})
	}
}

func (s *CostSheetServiceSuite) TestROICalculationComponents() {
	// Test the ROI calculation components directly
	testCases := []struct {
		name              string
		cost              decimal.Decimal
		revenue           decimal.Decimal
		wantNetRevenue    decimal.Decimal
		wantNetMargin     decimal.Decimal
		wantMarginPercent decimal.Decimal
	}{
		{
			name:              "Profitable ROI",
			cost:              decimal.NewFromInt(100),
			revenue:           decimal.NewFromInt(150),
			wantNetRevenue:    decimal.NewFromInt(50),                   // 150 - 100
			wantNetMargin:     decimal.NewFromFloat(0.3333333333333333), // (150-100)/150 = 0.3333...
			wantMarginPercent: decimal.NewFromFloat(33.33),              // 0.3333... * 100 rounded to 2 decimals
		},
		{
			name:              "Loss ROI",
			cost:              decimal.NewFromInt(150),
			revenue:           decimal.NewFromInt(100),
			wantNetRevenue:    decimal.NewFromInt(-50),      // 100 - 150
			wantNetMargin:     decimal.NewFromFloat(-0.5),   // (100-150)/100 = -0.5
			wantMarginPercent: decimal.NewFromFloat(-50.00), // -0.5 * 100 = -50%
		},
		{
			name:              "Break-even ROI",
			cost:              decimal.NewFromInt(100),
			revenue:           decimal.NewFromInt(100),
			wantNetRevenue:    decimal.Zero,
			wantNetMargin:     decimal.Zero,
			wantMarginPercent: decimal.Zero,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Calculate net revenue
			netRevenue := tc.revenue.Sub(tc.cost)
			s.Equal(tc.wantNetRevenue.String(), netRevenue.String(), "Net revenue calculation")

			// Calculate margin
			margin := s.costSheetService.CalculateMargin(tc.cost, tc.revenue)
			s.Equal(tc.wantNetMargin.String(), margin.String(), "Net margin calculation")

			// Calculate margin percentage
			marginPercent := margin.Mul(decimal.NewFromInt(100)).Round(2)
			s.Equal(tc.wantMarginPercent.String(), marginPercent.String(), "Margin percentage calculation")
		})
	}
}

// CRUD Operations
// CreateCostSheet(ctx context.Context, req *dto.CreateCostSheetRequest) (*dto.CostSheetResponse, error)
// GetCostSheet(ctx context.Context, id string) (*dto.CostSheetResponse, error)
// ListCostSheets(ctx context.Context, filter *domainCostSheet.Filter) (*dto.ListCostSheetsResponse, error)
// UpdateCostSheet(ctx context.Context, req *dto.UpdateCostSheetRequest) (*dto.CostSheetResponse, error)
// DeleteCostSheet(ctx context.Context, id string) error
func (s *CostSheetServiceSuite) TestCreateCostSheet() {
	// Test case: Successfully create a cost sheet
	req := &dto.CreateCostSheetRequest{
		MeterID: "meter-1",
		PriceID: "price-1",
	}

	resp, err := s.costSheetService.CreateCostSheet(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.ID)
	s.Equal(req.MeterID, resp.MeterID)
	s.Equal(req.PriceID, resp.PriceID)
	s.Equal(types.StatusPublished, resp.Status) // New costsheets start as published by default

	// Test case: Create with invalid data (missing required fields)
	invalidReq := &dto.CreateCostSheetRequest{}
	resp, err = s.costSheetService.CreateCostSheet(s.ctx, invalidReq)
	s.Error(err)
	s.Nil(resp)
}

func (s *CostSheetServiceSuite) TestGetCostSheet() {
	// Setup: Create a costsheet first
	createReq := &dto.CreateCostSheetRequest{
		MeterID: "meter-1",
		PriceID: "price-1",
	}
	created, err := s.costSheetService.CreateCostSheet(s.ctx, createReq)
	s.Require().NoError(err)
	s.Require().NotNil(created)

	// Test case: Successfully get existing costsheet
	resp, err := s.costSheetService.GetCostSheet(s.ctx, created.ID)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(created.ID, resp.ID)
	s.Equal(created.MeterID, resp.MeterID)
	s.Equal(created.PriceID, resp.PriceID)

	// Test case: Get non-existent costsheet
	resp, err = s.costSheetService.GetCostSheet(s.ctx, "non-existent-id")
	s.Error(err)
	s.Nil(resp)
}

func (s *CostSheetServiceSuite) TestListCostSheets() {
	// Setup: Create multiple costsheets
	costsheets := []struct {
		meterID string
		priceID string
	}{
		{"meter-1", "price-1"},
		{"meter-2", "price-2"},
	}

	createdIDs := make([]string, 0, len(costsheets))
	for _, cs := range costsheets {
		req := &dto.CreateCostSheetRequest{
			MeterID: cs.meterID,
			PriceID: cs.priceID,
		}
		created, err := s.costSheetService.CreateCostSheet(s.ctx, req)
		s.Require().NoError(err)
		createdIDs = append(createdIDs, created.ID)
	}

	// Test case: List all costsheets
	filter := &domainCostSheet.Filter{
		QueryFilter: types.NewDefaultQueryFilter(),
	}
	resp, err := s.costSheetService.ListCostSheets(s.ctx, filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(len(costsheets), resp.Total)
	s.Equal(len(costsheets), len(resp.Items))

	// Test case: List with pagination - let's be more explicit about what we expect
	filter.QueryFilter.Limit = lo.ToPtr(1)
	filter.QueryFilter.Offset = lo.ToPtr(0)
	resp, err = s.costSheetService.ListCostSheets(s.ctx, filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(1, len(resp.Items)) // Should return exactly 1 item
}

func (s *CostSheetServiceSuite) TestUpdateCostSheet() {
	// Setup: Create a costsheet first
	createReq := &dto.CreateCostSheetRequest{
		MeterID: "meter-1",
		PriceID: "price-1",
	}
	created, err := s.costSheetService.CreateCostSheet(s.ctx, createReq)
	s.Require().NoError(err)
	s.Require().NotNil(created)

	// Test case: Successfully update costsheet status
	updateReq := &dto.UpdateCostSheetRequest{
		ID:     created.ID,
		Status: string(types.StatusArchived),
	}
	resp, err := s.costSheetService.UpdateCostSheet(s.ctx, updateReq)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(created.ID, resp.ID)
	s.Equal(types.StatusArchived, resp.Status)

	// Test case: Update non-existent costsheet
	updateReq.ID = "non-existent-id"
	resp, err = s.costSheetService.UpdateCostSheet(s.ctx, updateReq)
	s.Error(err)
	s.Nil(resp)
}

func (s *CostSheetServiceSuite) TestDeleteCostSheet() {
	// Setup: Create a costsheet first
	createReq := &dto.CreateCostSheetRequest{
		MeterID: "meter-1",
		PriceID: "price-1",
	}
	created, err := s.costSheetService.CreateCostSheet(s.ctx, createReq)
	s.Require().NoError(err)
	s.Require().NotNil(created)

	// Test case: Successfully delete costsheet
	err = s.costSheetService.DeleteCostSheet(s.ctx, created.ID)
	s.NoError(err)

	// Verify deletion
	_, err = s.costSheetService.GetCostSheet(s.ctx, created.ID)
	s.Error(err)

	// Test case: Delete non-existent costsheet
	err = s.costSheetService.DeleteCostSheet(s.ctx, "non-existent-id")
	s.Error(err)
}

// Helper Operations
// GetSubscriptionDetails(ctx context.Context, subscriptionID string) (*domainSubscription.Subscription, error)
func (s *CostSheetServiceSuite) TestGetSubscriptionDetails() {
	// This test verifies the service method exists and handles errors correctly
	// We'll use a mock/simple approach since the service calls other complex services

	// Test case: Get non-existent subscription (should return error)
	sub, err := s.costSheetService.GetSubscriptionDetails(s.ctx, "nonexistent-id")
	s.Error(err)
	s.Nil(sub)

	// Note: Testing successful case would require setting up plan service,
	// customer service, etc. which is beyond the scope of this unit test.
	// Integration tests would be more appropriate for testing the full flow.
}
