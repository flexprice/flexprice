package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/types"
)

// stubPlaybookService records what the handler passed and answers with err.
type stubPlaybookService struct {
	err       error
	gotID     string
	gotUpdate *dto.UpdatePlaybookRequest
	gotPause  *dto.PausePlaybookRunRequest
	gotList   *dto.ListPlaybooksRequest
	gotRuns   *dto.ListPlaybookRunsRequest
	gotTasks  *dto.ListCollectionTasksRequest
	gotOrder  *dto.ReorderPlaybooksRequest
	gotCreate *dto.CreatePlaybookRequest
	gotVer    *dto.PlaybookVersionRequest
}

func (s *stubPlaybookService) ListTemplates(context.Context) (*dto.ListPlaybookTemplatesResponse, error) {
	return &dto.ListPlaybookTemplatesResponse{}, s.err
}
func (s *stubPlaybookService) List(_ context.Context, req *dto.ListPlaybooksRequest) (*dto.ListPlaybooksResponse, error) {
	s.gotList = req
	return &dto.ListPlaybooksResponse{}, s.err
}
func (s *stubPlaybookService) Get(_ context.Context, id string) (*dto.PlaybookResponse, error) {
	s.gotID = id
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Create(_ context.Context, req *dto.CreatePlaybookRequest) (*dto.PlaybookResponse, error) {
	s.gotCreate = req
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Update(_ context.Context, id string, req *dto.UpdatePlaybookRequest) (*dto.PlaybookResponse, error) {
	s.gotID, s.gotUpdate = id, req
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Delete(_ context.Context, id string) error {
	s.gotID = id
	return s.err
}
func (s *stubPlaybookService) Duplicate(_ context.Context, id string) (*dto.PlaybookResponse, error) {
	s.gotID = id
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Activate(_ context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	s.gotID, s.gotVer = id, req
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Pause(_ context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	s.gotID, s.gotVer = id, req
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Resume(_ context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	s.gotID, s.gotVer = id, req
	return &dto.PlaybookResponse{}, s.err
}
func (s *stubPlaybookService) Reorder(_ context.Context, req *dto.ReorderPlaybooksRequest) (*dto.ListPlaybooksResponse, error) {
	s.gotOrder = req
	return &dto.ListPlaybooksResponse{}, s.err
}
func (s *stubPlaybookService) Preview(_ context.Context, id string) (*dto.PlaybookPreviewResponse, error) {
	s.gotID = id
	return &dto.PlaybookPreviewResponse{}, s.err
}
func (s *stubPlaybookService) ListRuns(_ context.Context, id string, req *dto.ListPlaybookRunsRequest) (*dto.ListPlaybookRunsResponse, error) {
	s.gotID, s.gotRuns = id, req
	return &dto.ListPlaybookRunsResponse{}, s.err
}
func (s *stubPlaybookService) PauseRun(_ context.Context, id string, req *dto.PausePlaybookRunRequest) (*dto.PlaybookRunResponse, error) {
	s.gotID, s.gotPause = id, req
	return &dto.PlaybookRunResponse{}, s.err
}
func (s *stubPlaybookService) ResumeRun(_ context.Context, id string) (*dto.PlaybookRunResponse, error) {
	s.gotID = id
	return &dto.PlaybookRunResponse{}, s.err
}
func (s *stubPlaybookService) PauseCustomer(_ context.Context, id string, req *dto.PausePlaybookRunRequest) (*dto.CustomerPlaybookRunsResponse, error) {
	s.gotID, s.gotPause = id, req
	return &dto.CustomerPlaybookRunsResponse{}, s.err
}
func (s *stubPlaybookService) ResumeCustomer(_ context.Context, id string) (*dto.CustomerPlaybookRunsResponse, error) {
	s.gotID = id
	return &dto.CustomerPlaybookRunsResponse{}, s.err
}
func (s *stubPlaybookService) ListTasks(_ context.Context, req *dto.ListCollectionTasksRequest) (*dto.ListCollectionTasksResponse, error) {
	s.gotTasks = req
	return &dto.ListCollectionTasksResponse{}, s.err
}
func (s *stubPlaybookService) CompleteTask(_ context.Context, id string) (*dto.CollectionTaskResponse, error) {
	s.gotID = id
	return &dto.CollectionTaskResponse{}, s.err
}
func (s *stubPlaybookService) ReopenTask(_ context.Context, id string) (*dto.CollectionTaskResponse, error) {
	s.gotID = id
	return &dto.CollectionTaskResponse{}, s.err
}
func (s *stubPlaybookService) RunPlaybooks(context.Context) error { return s.err }
func (s *stubPlaybookService) RunEnvironment(context.Context) (*dto.RunPlaybooksResponse, error) {
	return &dto.RunPlaybooksResponse{}, s.err
}

func setupPlaybookRouter(t *testing.T, svc *stubPlaybookService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log, err := logger.NewLogger(&config.Configuration{})
	require.NoError(t, err)
	h := NewPlaybookHandler(svc, log)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	g := r.Group("/v1/collections")
	g.GET("/playbook-templates", h.ListTemplates)
	g.GET("/playbooks", h.ListPlaybooks)
	g.POST("/playbooks", h.CreatePlaybook)
	g.PUT("/playbooks/reorder", h.ReorderPlaybooks)
	g.POST("/playbooks/run", h.RunPlaybooks)
	g.GET("/playbooks/:id", h.GetPlaybook)
	g.PUT("/playbooks/:id", h.UpdatePlaybook)
	g.DELETE("/playbooks/:id", h.DeletePlaybook)
	g.POST("/playbooks/:id/duplicate", h.DuplicatePlaybook)
	g.POST("/playbooks/:id/activate", h.ActivatePlaybook)
	g.POST("/playbooks/:id/pause", h.PausePlaybook)
	g.POST("/playbooks/:id/resume", h.ResumePlaybook)
	g.GET("/playbooks/:id/preview", h.PreviewPlaybook)
	g.GET("/playbooks/:id/runs", h.ListPlaybookRuns)
	g.POST("/playbook-runs/:id/pause", h.PausePlaybookRun)
	g.POST("/playbook-runs/:id/resume", h.ResumePlaybookRun)
	g.POST("/customers/:id/pause", h.PauseCustomerCollections)
	g.POST("/customers/:id/resume", h.ResumeCustomerCollections)
	g.GET("/tasks", h.ListCollectionTasks)
	g.POST("/tasks/:id/complete", h.CompleteCollectionTask)
	g.POST("/tasks/:id/reopen", h.ReopenCollectionTask)
	return r
}

func doPlaybookRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPlaybookHandler(t *testing.T) {
	notFound := ierr.NewError("missing").Mark(ierr.ErrNotFound)
	conflict := ierr.NewError("changed").Mark(ierr.ErrVersionConflict)
	notReady := ierr.NewError("not ready").Mark(ierr.ErrInvalidOperation)
	off := ierr.NewError("off").Mark(ierr.ErrPermissionDenied)
	tests := []struct {
		name   string
		svcErr error
		method string
		path   string
		body   string
		want   int
		wantID string
	}{
		{"templates", nil, http.MethodGet, "/v1/collections/playbook-templates", ``, http.StatusOK, ""},
		{"list", nil, http.MethodGet, "/v1/collections/playbooks?state=live", ``, http.StatusOK, ""},
		{"list collections off", off, http.MethodGet, "/v1/collections/playbooks", ``, http.StatusForbidden, ""},
		{"create", nil, http.MethodPost, "/v1/collections/playbooks", `{"name":"x","template_id":"standard"}`, http.StatusCreated, ""},
		{"create bad json", nil, http.MethodPost, "/v1/collections/playbooks", `{`, http.StatusBadRequest, ""},
		{"get", nil, http.MethodGet, "/v1/collections/playbooks/cpb_1", ``, http.StatusOK, "cpb_1"},
		{"get missing", notFound, http.MethodGet, "/v1/collections/playbooks/cpb_1", ``, http.StatusNotFound, ""},
		{"update", nil, http.MethodPut, "/v1/collections/playbooks/cpb_2", `{"version":3,"trigger":"overdue","steps":[{"id":"a","kind":"email","day":3,"tone":"firm"}]}`, http.StatusOK, "cpb_2"},
		{"update without version", nil, http.MethodPut, "/v1/collections/playbooks/cpb_2", `{"trigger":"overdue"}`, http.StatusBadRequest, ""},
		{"update stale", conflict, http.MethodPut, "/v1/collections/playbooks/cpb_2", `{"version":1,"trigger":"overdue"}`, http.StatusConflict, ""},
		{"delete", nil, http.MethodDelete, "/v1/collections/playbooks/cpb_3", ``, http.StatusNoContent, "cpb_3"},
		{"duplicate", nil, http.MethodPost, "/v1/collections/playbooks/cpb_4/duplicate", ``, http.StatusCreated, "cpb_4"},
		{"activate", nil, http.MethodPost, "/v1/collections/playbooks/cpb_5/activate", `{"version":2}`, http.StatusOK, "cpb_5"},
		{"activate without version", nil, http.MethodPost, "/v1/collections/playbooks/cpb_5/activate", `{}`, http.StatusBadRequest, ""},
		{"activate not ready", notReady, http.MethodPost, "/v1/collections/playbooks/cpb_5/activate", `{"version":2}`, http.StatusBadRequest, ""},
		{"pause", nil, http.MethodPost, "/v1/collections/playbooks/cpb_6/pause", `{"version":2}`, http.StatusOK, "cpb_6"},
		{"resume", nil, http.MethodPost, "/v1/collections/playbooks/cpb_7/resume", `{"version":2}`, http.StatusOK, "cpb_7"},
		{"reorder", nil, http.MethodPut, "/v1/collections/playbooks/reorder", `{"ids":["a","b"]}`, http.StatusOK, ""},
		{"reorder no ids", nil, http.MethodPut, "/v1/collections/playbooks/reorder", `{}`, http.StatusBadRequest, ""},
		{"run", nil, http.MethodPost, "/v1/collections/playbooks/run", ``, http.StatusOK, ""},
		{"preview", nil, http.MethodGet, "/v1/collections/playbooks/cpb_8/preview", ``, http.StatusOK, "cpb_8"},
		{"runs", nil, http.MethodGet, "/v1/collections/playbooks/cpb_9/runs?state=active&limit=10", ``, http.StatusOK, "cpb_9"},
		{"pause run empty body", nil, http.MethodPost, "/v1/collections/playbook-runs/cpr_1/pause", ``, http.StatusOK, "cpr_1"},
		{"pause run with reason", nil, http.MethodPost, "/v1/collections/playbook-runs/cpr_1/pause", `{"reason":"CFO call"}`, http.StatusOK, "cpr_1"},
		{"pause run bad json", nil, http.MethodPost, "/v1/collections/playbook-runs/cpr_1/pause", `{`, http.StatusBadRequest, ""},
		{"resume run not paused", conflict, http.MethodPost, "/v1/collections/playbook-runs/cpr_1/resume", ``, http.StatusConflict, ""},
		{"pause customer", nil, http.MethodPost, "/v1/collections/customers/cust_1/pause", ``, http.StatusOK, "cust_1"},
		{"resume customer", nil, http.MethodPost, "/v1/collections/customers/cust_1/resume", ``, http.StatusOK, "cust_1"},
		{"tasks", nil, http.MethodGet, "/v1/collections/tasks?state=open", ``, http.StatusOK, ""},
		{"complete task", nil, http.MethodPost, "/v1/collections/tasks/ctask_1/complete", ``, http.StatusOK, "ctask_1"},
		{"reopen task", nil, http.MethodPost, "/v1/collections/tasks/ctask_1/reopen", ``, http.StatusOK, "ctask_1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubPlaybookService{err: tc.svcErr}
			w := doPlaybookRequest(setupPlaybookRouter(t, svc), tc.method, tc.path, tc.body)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.wantID != "" {
				require.Equal(t, tc.wantID, svc.gotID)
			}
		})
	}
}

func TestPlaybookHandler_PassesRequestsThrough(t *testing.T) {
	svc := &stubPlaybookService{}
	r := setupPlaybookRouter(t, svc)

	w := doPlaybookRequest(r, http.MethodPut, "/v1/collections/playbooks/cpb_1",
		`{"version":4,"name":"High value","trigger":"overdue","audience":{"kind":"amount_over","amount":"10000","currency":"usd"},"min_gap_days":2,"skip_weekends":false,"steps":[{"id":"a","kind":"email","day":1,"tone":"friendly","subject":"s","body":"b","send_without_review":true}]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 4, svc.gotUpdate.Version)
	require.Equal(t, types.PlaybookAudienceAmountOver, svc.gotUpdate.Audience.Kind)
	require.Equal(t, "10000", svc.gotUpdate.Audience.Amount.String())
	require.Equal(t, 2, *svc.gotUpdate.MinGapDays)
	require.False(t, *svc.gotUpdate.SkipWeekends)
	require.True(t, svc.gotUpdate.Steps[0].SendWithoutReview)

	w = doPlaybookRequest(r, http.MethodGet, "/v1/collections/playbooks/cpb_1/runs?state=paused&limit=5&offset=10", ``)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, dto.ListPlaybookRunsRequest{State: "paused", Limit: 5, Offset: 10}, *svc.gotRuns)

	req := httptest.NewRequest(http.MethodPost, "/v1/collections/playbook-runs/cpr_2/pause", strings.NewReader(`{"reason":"Promised Friday"}`))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Promised Friday", svc.gotPause.Reason, "a body of unknown length is still read")
}
