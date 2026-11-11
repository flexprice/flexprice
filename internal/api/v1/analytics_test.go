package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// stubAnalyticsService is a minimal service.AnalyticsService double that lets
// each test control exactly what the handler receives back, isolating the
// HTTP layer (routing, DTO binding, error->status mapping, JSON encoding)
// from the real translate/execute/shape pipeline already covered by
// internal/ee/service/analytics_test.go.
type stubAnalyticsService struct {
	result  *dto.AnalyticsQueryResult
	err     error
	catalog *dto.ReportCatalogResponse
	lastKey string
	lastReq *dto.ReportQueryRequest

	views    []*analytics.View
	lastView *analytics.View
	lastID   string
}

func (s *stubAnalyticsService) ListViews(_ context.Context, _ types.Family) ([]*analytics.View, error) {
	return s.views, s.err
}

func (s *stubAnalyticsService) GetView(_ context.Context, _ string) (*analytics.View, error) {
	if len(s.views) == 0 {
		return nil, s.err
	}
	return s.views[0], s.err
}

func (s *stubAnalyticsService) UpdateView(_ context.Context, v *analytics.View) error {
	s.lastView = v
	return s.err
}

func (s *stubAnalyticsService) DeleteView(_ context.Context, id string) error {
	s.lastID = id
	return s.err
}

func (s *stubAnalyticsService) ListReports(_ context.Context) (*dto.ReportCatalogResponse, error) {
	return s.catalog, s.err
}

func (s *stubAnalyticsService) RunReport(_ context.Context, key string, req *dto.ReportQueryRequest) (*dto.AnalyticsQueryResult, error) {
	s.lastKey, s.lastReq = key, req
	return s.result, s.err
}

func (s *stubAnalyticsService) ExecuteView(_ context.Context, _ *analytics.ViewDefinition, _ map[string][]string) (*dto.AnalyticsQueryResult, error) {
	return s.result, s.err
}

func (s *stubAnalyticsService) CreateView(_ context.Context, _ *analytics.View) error {
	return s.err
}

func (s *stubAnalyticsService) QueryView(_ context.Context, _ string, _ map[string][]string) (*dto.AnalyticsQueryResult, error) {
	return s.result, s.err
}

// canned AnalyticsQueryResult mirroring what shapeBreakdown produces for a
// properties.region dimension plus a usage_quantity metric.
func cannedBreakdownResult() *dto.AnalyticsQueryResult {
	return &dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{
			{Name: "region", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
			{Name: "usage_quantity", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
		},
		Rows: [][]string{
			{"us-east", "42"},
		},
		Meta: dto.AnalyticsQueryMeta{QuerySource: "meter_usage"},
	}
}

func setupAnalyticsRouter(t *testing.T, svc service.AnalyticsService) *gin.Engine {
	t.Helper()

	cfg := &config.Configuration{}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	handler := NewAnalyticsHandler(svc, nil, log)

	router := gin.New()
	// ErrorHandler mirrors router.go's wiring: handlers call c.Error(err) and
	// rely on this middleware to resolve it into a status code + JSON body.
	router.Use(middleware.ErrorHandler())
	router.POST("/v1/analytics/query", handler.Query)
	router.POST("/v1/analytics/views", handler.CreateView)
	router.GET("/v1/analytics/views", handler.ListViews)
	router.GET("/v1/analytics/views/:id", handler.GetView)
	router.PUT("/v1/analytics/views/:id", handler.UpdateView)
	router.DELETE("/v1/analytics/views/:id", handler.DeleteView)
	router.GET("/v1/analytics/reports", handler.ListReports)
	router.POST("/v1/analytics/reports/:key/query", handler.RunReport)

	return router
}

func doCreateAnalyticsView(router *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/analytics/views", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doAnalyticsQuery(router *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/analytics/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestAnalyticsQuery_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupAnalyticsRouter(t, &stubAnalyticsService{result: cannedBreakdownResult()})

	body := `{
		"definition": {
			"name": "usage-by-region",
			"shape": "breakdown",
			"metrics": ["usage_quantity"],
			"dimensions": ["properties.region"],
			"time": {"range": "last_7_days", "grain": "day"}
		},
		"variables": {}
	}`

	w := doAnalyticsQuery(router, body)

	require.Equal(t, http.StatusOK, w.Code)

	var result dto.AnalyticsQueryResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result), "response body is not valid JSON: %q", w.Body.String())

	require.Len(t, result.Columns, 2)
	require.Equal(t, types.ColumnRoleDimension, result.Columns[0].Role)
	require.Equal(t, types.ColumnRoleMetric, result.Columns[1].Role)
	require.Equal(t, [][]string{{"us-east", "42"}}, result.Rows)
}

// TestAnalyticsQuery_MalformedBody_ReturnsBadRequest covers the
// c.ShouldBindJSON rejection path with a table of malformed bodies: broken
// JSON syntax, and a field with the wrong JSON type. Note: the DTO's
// `validate:"required"` on Definition (a plain, non-pointer struct) is a
// go-playground/validator no-op — a present-but-empty `"definition": {}`
// binds and validates successfully, so it is deliberately not a case here;
// see the report's concerns section.
func TestAnalyticsQuery_MalformedBody_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "broken JSON syntax", body: `{not-json`},
		{name: "wrong JSON type for metrics field", body: `{"definition": {"shape": "breakdown", "metrics": "not-an-array"}, "variables": {}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Never reached: binding must reject the request before the
			// service is invoked. A non-nil result here would mask that.
			router := setupAnalyticsRouter(t, &stubAnalyticsService{result: cannedBreakdownResult()})

			w := doAnalyticsQuery(router, tt.body)

			require.Equal(t, http.StatusBadRequest, w.Code)

			var errResp ierr.ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp), "error response body is not valid JSON: %q", w.Body.String())
			require.NotEmpty(t, errResp.Message)
		})
	}
}

// Response must be the DTO.
func TestAnalyticsCreateView_ResponseIsDTONotDomainModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupAnalyticsRouter(t, &stubAnalyticsService{})

	body := `{
		"name": "usage-by-region",
		"definition": {
			"name": "usage-by-region",
			"shape": "breakdown",
			"metrics": ["usage_quantity"],
			"dimensions": ["properties.region"],
			"time": {"range": "last_7_days", "grain": "day"}
		}
	}`

	w := doCreateAnalyticsView(router, body)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "response body is not valid JSON: %q", w.Body.String())

	require.Contains(t, resp, "id")
	require.Contains(t, resp, "name")
	require.Contains(t, resp, "version")
	require.Contains(t, resp, "definition")

	require.NotContains(t, resp, "tenant_id")
	require.NotContains(t, resp, "status")
	require.Contains(t, resp, "created_at")
	require.Contains(t, resp, "updated_at")
	require.NotContains(t, resp, "updated_by")
}

func TestAnalyticsQuery_ServiceError_MapsToNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svcErr := ierr.NewError("view not found").
		WithHint("no such view").
		Mark(ierr.ErrNotFound)
	router := setupAnalyticsRouter(t, &stubAnalyticsService{err: svcErr})

	body := `{
		"definition": {
			"name": "usage-by-region",
			"shape": "breakdown",
			"metrics": ["usage_quantity"],
			"dimensions": ["properties.region"],
			"time": {"range": "last_7_days", "grain": "day"}
		},
		"variables": {}
	}`

	w := doAnalyticsQuery(router, body)

	require.Equal(t, http.StatusNotFound, w.Code)

	var errResp ierr.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	require.Equal(t, "no such view", errResp.Message)
}

func doRunReport(r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/analytics/reports/"+key+"/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListReportsHandler_OK(t *testing.T) {
	stub := &stubAnalyticsService{catalog: &dto.ReportCatalogResponse{Items: []*dto.ReportCatalogEntry{{Key: "invoice_total", Name: "Total billing"}}}}
	r := setupAnalyticsRouter(t, stub)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/analytics/reports", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"key":"invoice_total"`)
}

func TestRunReportHandler_PassesKeyAndBody(t *testing.T) {
	stub := &stubAnalyticsService{result: cannedBreakdownResult()}
	r := setupAnalyticsRouter(t, stub)
	w := doRunReport(r, "invoice_total", `{"shape":"timeseries","range":"2026-09-01..2026-10-01","grain":"month","compare":"previous_period"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "invoice_total", stub.lastKey)
	require.Equal(t, "previous_period", stub.lastReq.Compare)
}

func TestRunReportHandler_MissingShapeIs400(t *testing.T) {
	r := setupAnalyticsRouter(t, &stubAnalyticsService{result: cannedBreakdownResult()})
	w := doRunReport(r, "invoice_total", `{"range":"2026-09-01..2026-10-01"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRunReportHandler_NotFoundIs404(t *testing.T) {
	r := setupAnalyticsRouter(t, &stubAnalyticsService{err: ierr.NewError("report not found").Mark(ierr.ErrNotFound)})
	w := doRunReport(r, "nope", `{"shape":"scalar","range":"2026-09-01..2026-10-01"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestViewHandlers_ListGetUpdateDelete(t *testing.T) {
	v := &analytics.View{ID: "view_1", Name: "Acme", Version: 2, Definition: &analytics.ViewDefinition{Family: types.FamilyReport}}
	v.CreatedBy = "user_1"
	stub := &stubAnalyticsService{views: []*analytics.View{v}}
	r := setupAnalyticsRouter(t, stub)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/analytics/views?family=report", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"items":[{"id":"view_1"`)
	require.Contains(t, w.Body.String(), `"created_by":"user_1"`)
	require.NotContains(t, w.Body.String(), `tenant_id`)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/analytics/views/view_1", nil))
	require.Equal(t, http.StatusOK, w.Code)

	body := `{"name":"Renamed","definition":{"family":"report","shape":"scalar","metrics":["invoice_total"],"time":{"range":"this_month"}}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/analytics/views/view_1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "view_1", stub.lastView.ID)
	require.Equal(t, "Renamed", stub.lastView.Name)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/v1/analytics/views/view_1", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "view_1", stub.lastID)
}

func TestViewHandlers_Errors(t *testing.T) {
	r := setupAnalyticsRouter(t, &stubAnalyticsService{err: ierr.NewError("nope").Mark(ierr.ErrNotFound)})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/v1/analytics/views/x", nil))
	require.Equal(t, http.StatusNotFound, w.Code)

	r = setupAnalyticsRouter(t, &stubAnalyticsService{})
	long := strings.Repeat("a", 256)
	req := httptest.NewRequest(http.MethodPut, "/v1/analytics/views/x", strings.NewReader(`{"name":"`+long+`","definition":{"shape":"scalar","metrics":["invoice_total"]}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/analytics/views?family=bogus", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
