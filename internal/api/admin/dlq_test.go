package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/flexprice/flexprice/internal/api/admin/v1"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

type fakeDLQService struct {
	called bool
	err    error
}

func (f *fakeDLQService) Preview(_ context.Context, _ admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error) {
	f.called = true
	return &admindto.ReplayDLQResponse{Scanned: 2}, f.err
}

func (f *fakeDLQService) StartReplay(_ context.Context, _ admindto.ReplayDLQRequest) (*admindto.ReplayDLQRun, error) {
	f.called = true
	return &admindto.ReplayDLQRun{WorkflowID: "dlq-replay-events_dlq:dlq-replay-tool", Status: admindto.ReplayStatusQueued}, f.err
}

func (f *fakeDLQService) GetReplay(_ context.Context, workflowID, _ string) (*admindto.ReplayDLQRun, error) {
	f.called = true
	return &admindto.ReplayDLQRun{WorkflowID: workflowID, Status: admindto.ReplayStatusRunning}, f.err
}

func TestDLQReplayRoute(t *testing.T) {
	for _, tt := range []struct {
		name, secret, method, path, body, want string
		err                                    error
		status                                 int
		called                                 bool
	}{
		{name: "missing admin secret", body: `{}`, status: http.StatusUnauthorized},
		{name: "wrong admin secret", secret: "wrong", body: `{}`, status: http.StatusUnauthorized},
		{name: "invalid JSON", secret: "test-secret", body: `{`, status: http.StatusBadRequest},
		{name: "validation error", secret: "test-secret", body: `{}`, err: ierr.NewError("source required").Mark(ierr.ErrValidation), status: http.StatusBadRequest, called: true},
		{name: "preview", secret: "test-secret", body: `{"source_topic":"events_dlq","dry_run":true}`, status: http.StatusOK, called: true, want: `"scanned":2`},
		{name: "start replay", secret: "test-secret", body: `{"source_topic":"events_dlq","dry_run":false}`, status: http.StatusAccepted, called: true, want: `"status":"queued"`},
		{name: "replay already running", secret: "test-secret", body: `{"source_topic":"events_dlq"}`, err: ierr.NewError("running").Mark(ierr.ErrAlreadyExists), status: http.StatusConflict, called: true},
		{name: "replay status", secret: "test-secret", method: http.MethodGet, path: "/v1/dlq/replay/runs/dlq-replay-events_dlq%3Amine", status: http.StatusOK, called: true, want: `"workflow_id":"dlq-replay-events_dlq:mine"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeDLQService{err: tt.err}
			server := NewRouter(Handlers{Health: v1.NewHealthHandler(), DLQ: v1.NewDLQHandler(svc)}, nil, "test-secret")
			method, path := tt.method, tt.path
			if method == "" {
				method, path = http.MethodPost, "/v1/dlq/replay"
			}
			req := httptest.NewRequest(method, path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Admin-Secret", tt.secret)
			req.Header.Set("X-API-Key", "tenant-key")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, tt.called, svc.called)
			if tt.want != "" {
				require.Contains(t, rec.Body.String(), tt.want)
			}
		})
	}
}
