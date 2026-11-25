package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesTheDashboardAtRootOnly(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/ui"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: status %d type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		body := rec.Body.String()
		for _, want := range []string{"e2eprobe", "/status", "/history"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page lacks %q", path, want)
			}
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nothing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown paths must not render the dashboard, got %d", rec.Code)
	}
}
