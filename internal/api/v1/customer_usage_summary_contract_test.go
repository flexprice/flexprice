package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type customerUsageSummaryBillingStub struct {
	service.BillingService
	response *dto.CustomerUsageSummaryResponse
}

func (s customerUsageSummaryBillingStub) GetCustomerUsageSummary(_ context.Context, _ string, _ *dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error) {
	return s.response, nil
}

func TestGetCustomerUsageSummaryReportingUnitSDKContract(t *testing.T) {
	conversionRate := decimal.RequireFromString("0.001")
	response := &dto.CustomerUsageSummaryResponse{
		CustomerID: "cust_1",
		Features: []*dto.FeatureUsageSummary{{
			Feature: &dto.FeatureResponse{Feature: &feature.Feature{
				ID: "feat_1",
				ReportingUnit: &types.ReportingUnit{
					UnitSingular:   "second",
					UnitPlural:     "seconds",
					ConversionRate: &conversionRate,
				},
			}},
		}},
	}
	handler := &CustomerHandler{billing: customerUsageSummaryBillingStub{response: response}}
	router := gin.New()
	router.GET("/v1/customers/usage", handler.GetCustomerUsageSummary)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/customers/usage?customer_id=cust_1", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var decoded sdktypes.CustomerUsageSummaryResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &decoded))
	require.Len(t, decoded.Features, 1)
	require.NotNil(t, decoded.Features[0].Feature)
	require.NotNil(t, decoded.Features[0].Feature.ReportingUnit)
	require.NotNil(t, decoded.Features[0].Feature.ReportingUnit.UnitSingular)
	require.NotNil(t, decoded.Features[0].Feature.ReportingUnit.UnitPlural)
	require.Equal(t, "second", *decoded.Features[0].Feature.ReportingUnit.UnitSingular)
	require.Equal(t, "seconds", *decoded.Features[0].Feature.ReportingUnit.UnitPlural)
	require.NotNil(t, decoded.Features[0].Feature.ReportingUnit.ConversionRate)
	require.Equal(t, "0.001", *decoded.Features[0].Feature.ReportingUnit.ConversionRate)
}
