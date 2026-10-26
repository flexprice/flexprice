package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type stubCommitmentService struct {
	err          error
	gotID        string
	gotTerminate dto.TerminateCommitmentRequest
}

func (s *stubCommitmentService) Preview(context.Context, dto.CreateCommitmentRequest) (*dto.CommitmentResponse, error) {
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) Create(context.Context, dto.CreateCommitmentRequest) (*dto.CommitmentResponse, error) {
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) Get(_ context.Context, id string) (*dto.CommitmentResponse, error) {
	s.gotID = id
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) List(context.Context, *commitment.Filter) (*dto.ListCommitmentsResponse, error) {
	return &dto.ListCommitmentsResponse{}, s.err
}

func (s *stubCommitmentService) Update(_ context.Context, id string, _ dto.UpdateCommitmentRequest) (*dto.CommitmentResponse, error) {
	s.gotID = id
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) Terminate(_ context.Context, id string, req dto.TerminateCommitmentRequest) (*dto.CommitmentResponse, error) {
	s.gotID = id
	s.gotTerminate = req
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) TerminateForSubscription(context.Context, string, time.Time) error {
	return s.err
}

func (s *stubCommitmentService) IsEnabled(context.Context) (bool, error) { return true, s.err }

func (s *stubCommitmentService) ActiveForPeriod(context.Context, string, time.Time, time.Time) ([]*commitment.Commitment, error) {
	return nil, s.err
}

func (s *stubCommitmentService) CheckPriceAttach(context.Context, *subscription.Subscription, *price.Price, time.Time, *time.Time) error {
	return s.err
}

func setupCommitmentRouter(t *testing.T, svc *stubCommitmentService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log, err := logger.NewLogger(&config.Configuration{})
	require.NoError(t, err)

	h := NewCommitmentHandler(svc, log)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/v1/commitments", h.CreateCommitment)
	router.POST("/v1/commitments/preview", h.PreviewCommitment)
	router.POST("/v1/commitments/search", h.QueryCommitments)
	router.GET("/v1/commitments/:id", h.GetCommitment)
	router.PUT("/v1/commitments/:id", h.UpdateCommitment)
	router.POST("/v1/commitments/:id/terminate", h.TerminateCommitment)
	return router
}

func doCommitmentRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestCommitmentHandler(t *testing.T) {
	tests := []struct {
		name   string
		svcErr error
		method string
		path   string
		body   string
		want   int
		wantID string
	}{
		{"create ok", nil, http.MethodPost, "/v1/commitments", `{}`, http.StatusCreated, ""},
		{"create bad json", nil, http.MethodPost, "/v1/commitments", `{`, http.StatusBadRequest, ""},
		{"preview ok", nil, http.MethodPost, "/v1/commitments/preview", `{}`, http.StatusOK, ""},
		{"preview bad json", nil, http.MethodPost, "/v1/commitments/preview", `not json`, http.StatusBadRequest, ""},
		{"preview disabled", ierr.NewError("off").
			Mark(ierr.ErrInvalidOperation), http.MethodPost, "/v1/commitments/preview", `{}`, http.StatusBadRequest, ""},
		{"search ok", nil, http.MethodPost, "/v1/commitments/search", `{}`, http.StatusOK, ""},
		{"get ok", nil, http.MethodGet, "/v1/commitments/cmt_1", ``, http.StatusOK, "cmt_1"},
		{"update ok", nil, http.MethodPut, "/v1/commitments/cmt_2", `{"name":"Renamed"}`, http.StatusOK, "cmt_2"},
		{"get not found", ierr.NewError("missing").
			Mark(ierr.ErrNotFound), http.MethodGet, "/v1/commitments/cmt_1", ``, http.StatusNotFound, ""},
		{"terminate empty body", nil, http.MethodPost, "/v1/commitments/cmt_1/terminate", ``, http.StatusOK, "cmt_1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubCommitmentService{err: tc.svcErr}
			router := setupCommitmentRouter(t, svc)
			w := doCommitmentRequest(router, tc.method, tc.path, tc.body)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.wantID != "" {
				require.Equal(t, tc.wantID, svc.gotID)
			}
		})
	}
}

func TestCommitmentHandler_TerminateEmptyBodyOfUnknownLength(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/commitments/cmt_3/terminate", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "cmt_3", svc.gotID)
}

func TestCommitmentHandler_TerminatePassesWholeRequestToService(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	body := `{"effective_at":"2027-04-01T00:00:00Z","cancellation_fee":"5"}`
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_4/terminate", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "cmt_4", svc.gotID)
	require.NotNil(t, svc.gotTerminate.EffectiveAt)
	require.True(t, svc.gotTerminate.EffectiveAt.Equal(time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)))
	require.NotNil(t, svc.gotTerminate.CancellationFee)
	require.True(t, svc.gotTerminate.CancellationFee.Equal(decimal.NewFromInt(5)))
}

func TestCommitmentHandler_TerminateFeeOnNonBindingIsBadRequest(t *testing.T) {
	svcErr := ierr.NewError("a cancellation fee requires a binding commitment").
		WithHint("Only binding commitments can be charged on termination").
		Mark(ierr.ErrValidation)
	svc := &stubCommitmentService{err: svcErr}
	router := setupCommitmentRouter(t, svc)
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_6/terminate", `{"cancellation_fee":"5"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.NotNil(t, svc.gotTerminate.CancellationFee)
	require.Contains(t, w.Body.String(), "binding")
}
