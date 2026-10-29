package v1

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type costExplorerHandlerService struct {
	service.MeterUsageService
	request *dto.GetUsageAnalyticsRequest
}

func (s *costExplorerHandlerService) GetCostExplorer(_ context.Context, request *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	s.request = request
	return &dto.GetUsageAnalyticsResponse{}, nil
}

func TestEventsAnalytics_UsesCostExplorerAndPreservesPaging(t *testing.T) {
	svc := &costExplorerHandlerService{}
	handler := &EventsHandler{meterUsageService: svc}
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = httptest.NewRequest("POST", "/events/analytics", strings.NewReader(`{"external_customer_id":"customer","start_time":"2027-01-01T00:00:00Z","end_time":"2027-02-01T00:00:00Z","commitments_limit":25,"commitments_cursor":"opaque","include_children":true,"breakdown_bucket":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.GetUsageAnalytics(ctx)
	require.Empty(t, ctx.Errors)
	require.Equal(t, 200, writer.Code)
	require.NotNil(t, svc.request)
	require.Equal(t, 25, svc.request.CommitmentsLimit)
	require.Equal(t, "opaque", svc.request.CommitmentsCursor)
	require.True(t, svc.request.IncludeChildren)
	require.True(t, svc.request.BreakdownBucket)
}
