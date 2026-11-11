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
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type stubCommitmentService struct {
	err          error
	gotID        string
	gotTerminate dto.TerminateCommitmentRequest
	gotReplace   dto.ReplaceBillingTermsRequest
	gotStatuses  []types.InstalmentStatus

	gotInstalmentID string
	gotTrigger      dto.TriggerInstalmentRequest
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

func (s *stubCommitmentService) CancelTermination(_ context.Context, id string) (*dto.CommitmentResponse, error) {
	s.gotID = id
	return &dto.CommitmentResponse{Commitment: &commitment.Commitment{}}, s.err
}

func (s *stubCommitmentService) GetBillingTerms(_ context.Context, id string) (*dto.BillingTermsResponse, error) {
	s.gotID = id
	return &dto.BillingTermsResponse{}, s.err
}

func (s *stubCommitmentService) PreviewBillingTerms(_ context.Context, id string, req dto.ReplaceBillingTermsRequest) (*dto.BillingTermsResponse, error) {
	s.gotID, s.gotReplace = id, req
	return &dto.BillingTermsResponse{}, s.err
}

func (s *stubCommitmentService) ReplaceBillingTerms(_ context.Context, id string, req dto.ReplaceBillingTermsRequest) (*dto.BillingTermsResponse, error) {
	s.gotID, s.gotReplace = id, req
	return &dto.BillingTermsResponse{}, s.err
}

func (s *stubCommitmentService) ListInstalments(_ context.Context, id string, statuses []types.InstalmentStatus) (*dto.ListInstalmentsResponse, error) {
	s.gotID, s.gotStatuses = id, statuses
	return &dto.ListInstalmentsResponse{}, s.err
}

func (s *stubCommitmentService) TriggerInstalment(
	_ context.Context,
	id, instalmentID string,
	req dto.TriggerInstalmentRequest,
) (*billingterm.Instalment, error) {
	s.gotID, s.gotInstalmentID, s.gotTrigger = id, instalmentID, req
	return &billingterm.Instalment{}, s.err
}

func (s *stubCommitmentService) GetInstalment(context.Context, string) (*billingterm.Instalment, error) {
	return &billingterm.Instalment{}, s.err
}

func (s *stubCommitmentService) BillDueInstalments(context.Context) error { return s.err }

func (s *stubCommitmentService) TerminateForSubscription(context.Context, string, time.Time) error {
	return s.err
}

func (s *stubCommitmentService) IsEnabled(context.Context) (bool, error) { return true, s.err }

func (s *stubCommitmentService) ListOverlapping(context.Context, string, time.Time, time.Time) ([]*commitment.Commitment, error) {
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
	router.POST("/v1/commitments/:id/terminate/cancel", h.CancelTermination)
	router.GET("/v1/commitments/:id/billing-terms", h.GetBillingTerms)
	router.PUT("/v1/commitments/:id/billing-terms", h.ReplaceBillingTerms)
	router.POST("/v1/commitments/:id/billing-terms/preview", h.PreviewBillingTerms)
	router.GET("/v1/commitments/:id/instalments", h.ListInstalments)
	router.POST("/v1/commitments/:id/instalments/:instalment_id/trigger", h.TriggerInstalment)
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
		{"cancel termination ok", nil, http.MethodPost, "/v1/commitments/cmt_5/terminate/cancel", ``, http.StatusOK, "cmt_5"},
		{"cancel termination in effect", ierr.NewError("no pending termination").
			Mark(ierr.ErrInvalidOperation), http.MethodPost, "/v1/commitments/cmt_5/terminate/cancel", ``, http.StatusBadRequest, ""},
		{"cancel termination not found", ierr.NewError("missing").
			Mark(ierr.ErrNotFound), http.MethodPost, "/v1/commitments/cmt_5/terminate/cancel", ``, http.StatusNotFound, ""},
		{"get billing terms ok", nil, http.MethodGet, "/v1/commitments/cmt_1/billing-terms", ``, http.StatusOK, "cmt_1"},
		{"get billing terms not found", ierr.NewError("missing").
			Mark(ierr.ErrNotFound), http.MethodGet, "/v1/commitments/cmt_1/billing-terms", ``, http.StatusNotFound, ""},
		{"replace billing terms ok", nil, http.MethodPut, "/v1/commitments/cmt_2/billing-terms", `{"billing_terms":[{"kind":"recurring","value_type":"remainder"}]}`, http.StatusOK, "cmt_2"},
		{"replace billing terms bad json", nil, http.MethodPut, "/v1/commitments/cmt_2/billing-terms", `{`, http.StatusBadRequest, ""},
		{"replace billing terms inactive", ierr.NewError("terminated").
			Mark(ierr.ErrInvalidOperation), http.MethodPut, "/v1/commitments/cmt_2/billing-terms", `{"billing_terms":[]}`, http.StatusBadRequest, ""},
		{"preview billing terms ok", nil, http.MethodPost, "/v1/commitments/cmt_3/billing-terms/preview", `{"billing_terms":[{"kind":"once","value_type":"fixed","value":"30"}]}`, http.StatusOK, "cmt_3"},
		{"preview billing terms bad json", nil, http.MethodPost, "/v1/commitments/cmt_3/billing-terms/preview", `not json`, http.StatusBadRequest, ""},
		{"list instalments ok", nil, http.MethodGet, "/v1/commitments/cmt_4/instalments", ``, http.StatusOK, "cmt_4"},
		{"list instalments not found", ierr.NewError("missing").
			Mark(ierr.ErrNotFound), http.MethodGet, "/v1/commitments/cmt_4/instalments", ``, http.StatusNotFound, ""},
		{"list instalments with a status", nil, http.MethodGet, "/v1/commitments/cmt_4/instalments?status=cancelled", ``, http.StatusOK, "cmt_4"},
		{"list instalments with an unknown status", nil, http.MethodGet, "/v1/commitments/cmt_4/instalments?status=paid", ``, http.StatusBadRequest, ""},
		{"list instalments with an empty status", nil, http.MethodGet, "/v1/commitments/cmt_4/instalments?status=", ``, http.StatusBadRequest, ""},
		{"trigger instalment empty body", nil, http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", ``, http.StatusOK, "cmt_10"},
		{"trigger instalment empty object", nil, http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", `{}`, http.StatusOK, "cmt_10"},
		{"trigger instalment bad json", nil, http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", `not json`, http.StatusBadRequest, ""},
		{"trigger instalment not manual", ierr.NewError("date instalment").
			Mark(ierr.ErrInvalidOperation), http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", `{}`, http.StatusBadRequest, ""},
		{"trigger instalment outside the term", ierr.NewError("outside the term").
			Mark(ierr.ErrValidation), http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", `{"bill_on":"2099-01-01T00:00:00Z"}`, http.StatusBadRequest, ""},
		{"trigger instalment not found", ierr.NewError("missing").
			Mark(ierr.ErrNotFound), http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_1/trigger", `{}`, http.StatusNotFound, ""},
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
	body := `{"effective_at":"2025-04-01T00:00:00Z","cancellation_fee":{"type":"fixed","value":"5"}}`
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_4/terminate", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "cmt_4", svc.gotID)
	require.NotNil(t, svc.gotTerminate.EffectiveAt)
	require.True(t, svc.gotTerminate.EffectiveAt.Equal(time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)))
	require.NotNil(t, svc.gotTerminate.CancellationFee)
	require.Equal(t, types.CancellationFeeFixed, svc.gotTerminate.CancellationFee.Type)
	require.True(t, svc.gotTerminate.CancellationFee.Value.Equal(decimal.NewFromInt(5)))
}

func TestCommitmentHandler_TerminateBindsPercentageFee(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	body := `{"cancellation_fee":{"type":"percentage","value":"10"}}`
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_7/terminate", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, svc.gotTerminate.CancellationFee)
	require.Equal(t, types.CancellationFeePercentage, svc.gotTerminate.CancellationFee.Type)
	require.True(t, svc.gotTerminate.CancellationFee.Value.Equal(decimal.NewFromInt(10)))
}

func TestCommitmentHandler_TerminateRejectsAScalarFee(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_8/terminate", `{"cancellation_fee":"5"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Empty(t, svc.gotID, "the service is never called")
}

func TestCommitmentHandler_TerminateFeeOnNonBindingIsBadRequest(t *testing.T) {
	svcErr := ierr.NewError("a cancellation fee requires a binding commitment").
		WithHint("Only binding commitments can be charged on termination").
		Mark(ierr.ErrValidation)
	svc := &stubCommitmentService{err: svcErr}
	router := setupCommitmentRouter(t, svc)
	w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_6/terminate", `{"cancellation_fee":{"type":"fixed","value":"5"}}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.NotNil(t, svc.gotTerminate.CancellationFee)
	require.Contains(t, w.Body.String(), "binding")
}

func TestCommitmentHandler_ReplaceBillingTermsPassesTermsToService(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	body := `{"billing_terms":[{"kind":"once","value_type":"fixed","value":"30"},{"kind":"recurring","value_type":"remainder","invoice_cadence":"ARREAR"}]}`
	w := doCommitmentRequest(router, http.MethodPut, "/v1/commitments/cmt_5/billing-terms", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "cmt_5", svc.gotID)
	require.Len(t, svc.gotReplace.BillingTerms, 2)
	require.Equal(t, types.BillingTermKindOnce, svc.gotReplace.BillingTerms[0].Kind)
	require.True(t, svc.gotReplace.BillingTerms[0].Value.Equal(decimal.NewFromInt(30)))
	require.Equal(t, types.BillingTermValueRemainder, svc.gotReplace.BillingTerms[1].ValueType)
}

func TestCommitmentHandler_ListInstalmentsPassesStatusesToService(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []types.InstalmentStatus
	}{
		{"no filter", "/v1/commitments/cmt_7/instalments", []types.InstalmentStatus{}},
		{
			"repeated status",
			"/v1/commitments/cmt_7/instalments?status=scheduled&status=invoiced",
			[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusInvoiced},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubCommitmentService{}
			router := setupCommitmentRouter(t, svc)
			w := doCommitmentRequest(router, http.MethodGet, tc.path, ``)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "cmt_7", svc.gotID)
			require.Equal(t, tc.want, svc.gotStatuses)
		})
	}
}

func TestCommitmentHandler_TriggerInstalmentPassesRequestToService(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantBillOn *time.Time
	}{
		{"default date", ``, nil},
		{"explicit date", `{"bill_on":"2027-06-15T00:00:00Z"}`, lo.ToPtr(time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubCommitmentService{}
			router := setupCommitmentRouter(t, svc)
			w := doCommitmentRequest(router, http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_2/trigger", tc.body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "cmt_10", svc.gotID)
			require.Equal(t, "binst_2", svc.gotInstalmentID)
			if tc.wantBillOn == nil {
				require.Nil(t, svc.gotTrigger.BillOn)
				return
			}

			require.True(t, svc.gotTrigger.BillOn.Equal(*tc.wantBillOn))
		})
	}
}

func TestCommitmentHandler_TriggerInstalmentEmptyBodyOfUnknownLength(t *testing.T) {
	svc := &stubCommitmentService{}
	router := setupCommitmentRouter(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/commitments/cmt_10/instalments/binst_3/trigger", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "binst_3", svc.gotInstalmentID)
}
