package selfbilling

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// alertSink is a Zapier catch hook that records every alert posted to it.
type alertSink struct {
	mu     sync.Mutex
	alerts []alert
}

// newAlertSink points the service's Zapier webhook at a recording test server.
func newAlertSink(t *testing.T, s *selfBillingService) *alertSink {
	t.Helper()
	sink := &alertSink{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a alert
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		sink.mu.Lock()
		sink.alerts = append(sink.alerts, a)
		sink.mu.Unlock()
	}))
	t.Cleanup(srv.Close)

	s.Config.Billing.ZapierWebhookURL = srv.URL
	return sink
}

func (k *alertSink) received() []alert {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]alert(nil), k.alerts...)
}

// waitFor waits until want alerts arrived and no more follow.
func (k *alertSink) waitFor(t *testing.T, want int) []alert {
	t.Helper()
	require.Eventually(t, func() bool { return len(k.received()) >= want }, 2*time.Second, 5*time.Millisecond)
	assert.Never(t, func() bool { return len(k.received()) > want }, 50*time.Millisecond, 10*time.Millisecond)
	return k.received()
}

func TestNotify_EmptyURLSendsNothing(t *testing.T) {
	fc := newFakeClient()
	s, _ := newTestService(fc)
	sink := newAlertSink(t, s)
	s.Config.Billing.ZapierWebhookURL = ""

	require.NoError(t, s.provision(context.Background(), "t1", "Acme", "a@acme.io"))

	assert.Equal(t, 1, fc.trials())
	assert.Empty(t, sink.waitFor(t, 0))
}

func TestNotify_RejectedAlertNeverFailsCaller(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	s, _ := newTestService(newFakeClient())
	s.Config.Billing.ZapierWebhookURL = srv.URL

	require.NoError(t, s.provision(context.Background(), "t1", "Acme", "a@acme.io"))
	require.Eventually(t, func() bool { return hits.Load() == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestNotify_FailureLogsNeverCarryTheHookURL(t *testing.T) {
	const secretPath = "/hooks/catch/123/s3cr3t/"

	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(rejecting.Close)

	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()

	tests := []struct {
		name    string
		hookURL string
	}{
		{"rejected_by_zapier", rejecting.URL + secretPath},
		{"transport_failure", unreachable.URL + secretPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			s, _ := newTestService(newFakeClient())
			s.Logger = logger.NewFromSugared(zap.New(core).Sugar())
			s.Config.Billing.ZapierWebhookURL = tt.hookURL

			s.notify(context.Background(), alert{Event: alertTrialStarted, TenantID: "t1"})

			require.Eventually(t, func() bool { return logs.Len() == 1 }, 5*time.Second, 5*time.Millisecond)
			entry := logs.All()[0]
			logged := entry.Message + fmt.Sprint(entry.ContextMap())
			assert.NotContains(t, logged, "s3cr3t")
			assert.Contains(t, entry.ContextMap(), "error")
		})
	}
}

func TestNotify_InsecureHookIsRefused(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	s, _ := newTestService(newFakeClient())
	s.Logger = logger.NewFromSugared(zap.New(core).Sugar())
	s.Config.Billing.ZapierWebhookURL = "http://hooks.example.com/hooks/catch/123/s3cr3t/"

	s.notify(context.Background(), alert{Event: alertTrialStarted, TenantID: "t1"})

	require.Equal(t, 1, logs.Len(), "refused synchronously, before any request")
	entry := logs.All()[0]
	assert.NotContains(t, entry.Message+fmt.Sprint(entry.ContextMap()), "s3cr3t")
}
