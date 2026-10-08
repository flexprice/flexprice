package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newObservedLogger() (*logger.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.NewFromSugared(zap.New(core).Sugar()), logs
}

func TestProviderTransport_LogsOneLinePerCall(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		respBody     string
		respHeaders  map[string]string
		reqBody      string
		reqType      string
		wantLevel    zapcore.Level
		wantMessage  string
		wantRespBody string
		wantReqBody  string
	}{
		{
			name:        "success logs the request body but not the response body",
			status:      http.StatusOK,
			respBody:    `{"id":"cus_1","email":"a@b.com","status":"active"}`,
			respHeaders: map[string]string{"Request-Id": "req_123", "Content-Type": "application/json"},
			reqBody:     `{"email":"a@b.com","currency":"usd"}`,
			reqType:     "application/json",
			wantLevel:   zapcore.InfoLevel,
			wantMessage: "provider_call.completed",
			wantReqBody: `{"currency":"usd","email":"[redacted]"}`,
		},
		{
			name:         "4xx logs redacted bodies at info",
			status:       http.StatusPaymentRequired,
			respBody:     `{"error":{"code":"card_declined","decline_code":"insufficient_funds"}}`,
			respHeaders:  map[string]string{"Request-Id": "req_456", "Content-Type": "application/json"},
			reqBody:      "amount=100&card%5Bnumber%5D=4242424242424242&card%5Bcvc%5D=123",
			reqType:      "application/x-www-form-urlencoded",
			wantLevel:    zapcore.InfoLevel,
			wantMessage:  "provider_call.rejected",
			wantRespBody: `{"error":{"code":"card_declined","decline_code":"insufficient_funds"}}`,
			wantReqBody:  `{"amount":"100","card[cvc]":"[redacted]","card[number]":"[redacted]"}`,
		},
		{
			name:         "5xx logs at error",
			status:       http.StatusBadGateway,
			respBody:     `{"message":"upstream down"}`,
			respHeaders:  map[string]string{"Content-Type": "application/json"},
			reqBody:      `{"client_secret":"shh","amount":5}`,
			reqType:      "application/json",
			wantLevel:    zapcore.ErrorLevel,
			wantMessage:  "provider_call.failed",
			wantRespBody: `{"message":"upstream down"}`,
			wantReqBody:  `{"amount":5,"client_secret":"[redacted]"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.respHeaders {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.respBody)
			}))
			defer srv.Close()

			log, logs := newObservedLogger()
			client := NewProviderHTTPClient(0, log, "stripe")

			req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/charges?email=a@b.com", strings.NewReader(tt.reqBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", tt.reqType)

			resp, err := client.Do(req)
			require.NoError(t, err)
			got, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tt.respBody, string(got), "caller must still receive the full body")

			require.Equal(t, 1, logs.Len())
			entry := logs.All()[0]
			assert.Equal(t, tt.wantLevel, entry.Level)
			assert.Equal(t, tt.wantMessage, entry.Message)

			fields := entry.ContextMap()
			assert.Equal(t, "stripe", fields["provider"])
			assert.Equal(t, "/v1/charges", fields["http_path"], "query string must not be logged")
			assert.EqualValues(t, tt.status, fields["status_code"])
			assert.Equal(t, tt.respHeaders["Request-Id"], fields["provider_request_id"])
			if tt.wantRespBody == "" {
				assert.NotContains(t, fields, "response_body")
			} else {
				assert.Equal(t, tt.wantRespBody, fields["response_body"])
			}
			assert.Equal(t, tt.wantReqBody, fields["request_body"])
		})
	}
}

func TestProviderTransport_KillSwitch(t *testing.T) {
	tests := []struct {
		name         string
		log          *logger.Logger
		wantBodyRead bool
	}{
		{name: "nil logger", log: nil},
		{name: "provider_calls_enabled off", log: logger.NewNoopLogger()},
		{name: "provider_calls_enabled on", log: logger.NewFromSugared(zap.NewNop().Sugar()), wantBodyRead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			defer srv.Close()

			req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"a":1}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			bodyReads := 0
			getBody := req.GetBody
			req.GetBody = func() (io.ReadCloser, error) {
				bodyReads++
				return getBody()
			}

			resp, err := NewProviderHTTPClient(0, tt.log, "stripe").Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tt.wantBodyRead, bodyReads > 0, "the logging layer is the only reader of GetBody")
		})
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestProviderTransport_LogsTransportErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantLevel   zapcore.Level
		wantMessage string
	}{
		{name: "network error", err: errors.New("connection refused"), wantLevel: zapcore.ErrorLevel, wantMessage: "provider_call.failed"},
		{name: "caller canceled", err: fmt.Errorf("dial: %w", context.Canceled), wantLevel: zapcore.InfoLevel, wantMessage: "provider_call.canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logs := newObservedLogger()
			client := &http.Client{Transport: ProviderTransport(failingTransport{err: tt.err}, log, "razorpay")}

			req, err := http.NewRequest(http.MethodGet, "https://api.razorpay.com/v1/payments", nil)
			require.NoError(t, err)
			_, err = client.Do(req)
			require.Error(t, err)

			require.Equal(t, 1, logs.Len())
			assert.Equal(t, tt.wantLevel, logs.All()[0].Level)
			assert.Equal(t, tt.wantMessage, logs.All()[0].Message)
		})
	}
}

// Stripe's SDK builds requests with http.NewRequest(nil), then sets Body and GetBody,
// leaving ContentLength at 0.
func TestProviderTransport_LogsBodyOfUnknownLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/customers", nil)
	require.NoError(t, err)
	const body = "email=a%40b.com&currency=usd"
	req.Body = io.NopCloser(strings.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	log, logs := newObservedLogger()
	resp, err := NewProviderHTTPClient(0, log, "stripe").Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, `{"currency":"usd","email":"[redacted]"}`, logs.All()[0].ContextMap()["request_body"])
}

func TestProviderTransport_ReportsDetectedPII(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"id":"inv_1","footer":"Questions? Write to jane@acme.com"}`)
	}))
	defer srv.Close()

	log, logs := newObservedLogger()
	resp, err := NewProviderHTTPClient(0, log, "chargebee").Get(srv.URL + "/api/v2/invoices/inv_1")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, 2, logs.Len())
	call, detected := logs.All()[0], logs.All()[1]
	assert.Equal(t, `{"footer":"Questions? Write to [email]","id":"inv_1"}`, call.ContextMap()["response_body"])
	assert.Equal(t, "provider_call.pii_detected", detected.Message)
	assert.Equal(t, []any{"response.footer"}, detected.ContextMap()["pii_paths"])
	assert.NotContains(t, fmt.Sprint(detected.ContextMap()), "jane@acme.com", "the alert line must not carry values")
}

func TestProviderTransport_OmitsOversizedBodies(t *testing.T) {
	large := `{"pad":"` + strings.Repeat("x", maxParsedBodyBytes) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, large)
	}))
	defer srv.Close()

	log, logs := newObservedLogger()
	resp, err := NewProviderHTTPClient(0, log, "zoho_books").Get(srv.URL)
	require.NoError(t, err)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, large, string(got))
	assert.Equal(t, "[omitted: body over 64 KB]", logs.All()[0].ContextMap()["response_body"])
}

type ctxKey struct{}

// TestContextTransport goes through an http.Client with a Timeout, which wraps the request
// context before the transport runs.
func TestContextTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	var seen any
	spy := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Context().Value(ctxKey{})
		return http.DefaultTransport.RoundTrip(req)
	})
	bound := context.WithValue(context.Background(), ctxKey{}, "caller")
	client := &http.Client{Timeout: 100 * time.Millisecond, Transport: ContextTransport(bound, spy)}

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	start := time.Now()
	_, err = client.Do(req)

	require.Error(t, err, "the client timeout still applies")
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "caller", seen, "the request runs under the bound context")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
