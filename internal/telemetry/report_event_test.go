package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

func newTestReporter(t *testing.T, host string) *Reporter {
	t.Helper()
	cfg := &config.Configuration{}
	cfg.Telemetry.Host = host
	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger.NewLogger: %v", err)
	}
	return NewReporter(cfg, log)
}

func TestReportEvent_OnlySendsAllowlistedNames(t *testing.T) {
	var mu sync.Mutex
	var sentEventNames []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			EventName string `json:"event_name"`
		}
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		sentEventNames = append(sentEventNames, decoded.EventName)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newTestReporter(t, srv.URL)
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant_01")

	input := []string{"customer.created", "feature.created", "wallet.created", "event.rejected"}
	for _, name := range input {
		r.ReportEvent(ctx, &types.WebhookEvent{EventName: types.WebhookEventName(name), Payload: []byte(`{}`)})
	}

	want := map[string]bool{"customer.created": true, "wallet.created": true}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(sentEventNames)
		mu.Unlock()
		if n >= len(want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, name := range sentEventNames {
		got[name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("expected exactly %v to be sent, got %v (raw: %v)", want, got, sentEventNames)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("expected %q to have been sent, it wasn't", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("expected %q to NOT have been sent, but it was", name)
		}
	}
}

func TestReportEvent_DisabledSendsNothing(t *testing.T) {
	var mu sync.Mutex
	hitCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hitCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newTestReporter(t, srv.URL)
	r.cfg.Telemetry.Disabled = true
	ctx := context.Background()

	r.ReportEvent(ctx, &types.WebhookEvent{EventName: "customer.created", Payload: []byte(`{}`)})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if hitCount != 0 {
		t.Fatalf("expected no requests while disabled, got %d", hitCount)
	}
}
