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

func (f *fakeDLQService) Replay(_ context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error) {
	f.called = true
	return &admindto.ReplayDLQResponse{Scanned: 2}, f.err
}

func TestDLQReplayRoute(t *testing.T) {
	for _, tt := range []struct {
		name, secret, body string
		err                error
		status             int
		called             bool
	}{
		{name: "missing admin secret", body: `{}`, status: http.StatusUnauthorized},
		{name: "wrong admin secret", secret: "wrong", body: `{}`, status: http.StatusUnauthorized},
		{name: "invalid JSON", secret: "test-secret", body: `{`, status: http.StatusBadRequest},
		{name: "validation error", secret: "test-secret", body: `{}`, err: ierr.NewError("source required").Mark(ierr.ErrValidation), status: http.StatusBadRequest, called: true},
		{name: "success", secret: "test-secret", body: `{"source_topic":"events_dlq","dry_run":true}`, status: http.StatusOK, called: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeDLQService{err: tt.err}
			server := NewRouter(Handlers{Health: v1.NewHealthHandler(), DLQ: v1.NewDLQHandler(svc)}, nil, "test-secret")
			req := httptest.NewRequest(http.MethodPost, "/v1/dlq/replay", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Admin-Secret", tt.secret)
			req.Header.Set("X-API-Key", "tenant-key")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, tt.called, svc.called)
			if tt.status == http.StatusOK {
				require.Contains(t, rec.Body.String(), `"scanned":2`)
			}
		})
	}
}
