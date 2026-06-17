package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := config.GetDefaultConfig()
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}

func TestGeminiPricingService_ParsePricing_success(t *testing.T) {
	pricingJSON := `{"features":[],"plans":[{"name":"Pro","description":"","prices":[],"entitlements":[]}]}`
	geminiBody := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"parts": []any{
						map[string]any{"text": pricingJSON},
					},
				},
			},
		},
	}
	geminiBytes, err := json.Marshal(geminiBody)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.True(t, strings.Contains(r.URL.Path, "generateContent"))
		key := r.URL.Query().Get("key")
		require.Equal(t, "test-key", key)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(geminiBytes)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Configuration{
		Gemini: config.GeminiConfig{
			APIKey: "test-key",
			Model:  "gemini-test",
		},
	}
	stub := &stubHTTPClient{
		baseURL: strings.TrimPrefix(srv.URL, "http://"),
		scheme:  "http",
	}
	svc := newGeminiPricingService(cfg, stub, testLogger(t))

	schema := json.RawMessage(`{"type":"object","properties":{"plans":{"type":"array"}}}`)
	req := &dto.ParseGeminiPricingRequest{
		SystemPrompt:   "sys",
		UserPrompt:     "user",
		ResponseSchema: schema,
	}
	require.NoError(t, req.Validate())

	out, err := svc.ParsePricing(context.Background(), req)
	require.NoError(t, err)
	require.True(t, json.Valid(out))

	var round map[string]any
	require.NoError(t, json.Unmarshal(out, &round))
	require.Contains(t, round, "plans")
}

func TestGeminiPricingService_ParsePricing_missingAPIKey(t *testing.T) {
	cfg := &config.Configuration{Gemini: config.GeminiConfig{}}
	svc := newGeminiPricingService(cfg, &stubHTTPClient{}, testLogger(t))
	req := &dto.ParseGeminiPricingRequest{
		SystemPrompt:   "s",
		UserPrompt:     "u",
		ResponseSchema: json.RawMessage(`{}`),
	}
	_, err := svc.ParsePricing(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "gemini api key")
}

func TestGeminiPricingService_ParsePricing_upstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate"}}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Configuration{
		Gemini: config.GeminiConfig{APIKey: "k", Model: "m"},
	}
	stub := &stubHTTPClient{
		baseURL: strings.TrimPrefix(srv.URL, "http://"),
		scheme:  "http",
	}
	svc := newGeminiPricingService(cfg, stub, testLogger(t))
	req := &dto.ParseGeminiPricingRequest{
		SystemPrompt:   "s",
		UserPrompt:     "u",
		ResponseSchema: json.RawMessage(`{}`),
	}
	_, err := svc.ParsePricing(context.Background(), req)
	require.Error(t, err)
	require.True(t, ierr.IsTooManyRequests(err), "expected rate-limit error, got %v", err)
}

func TestGeminiPricingService_ParsePricing_fencedJSON_isAccepted(t *testing.T) {
	pricingJSON := "```json\n{\"features\":[],\"plans\":[]}\n```"
	geminiBody := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"parts": []any{
						map[string]any{"text": pricingJSON},
					},
				},
			},
		},
	}
	geminiBytes, err := json.Marshal(geminiBody)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(geminiBytes)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Configuration{
		Gemini: config.GeminiConfig{APIKey: "k", Model: "m"},
	}
	stub := &stubHTTPClient{
		baseURL: strings.TrimPrefix(srv.URL, "http://"),
		scheme:  "http",
	}
	svc := newGeminiPricingService(cfg, stub, testLogger(t))
	req := &dto.ParseGeminiPricingRequest{
		SystemPrompt:   "s",
		UserPrompt:     "u",
		ResponseSchema: json.RawMessage(`{}`),
	}

	out, err := svc.ParsePricing(context.Background(), req)
	require.NoError(t, err)
	require.JSONEq(t, `{"features":[],"plans":[]}`, string(out))
}

func TestGeminiPricingService_ParsePricing_retriesOnInvalidJSON(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"not json"}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"features\":[],\"plans\":[]}"}]}}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Configuration{
		Gemini: config.GeminiConfig{APIKey: "k", Model: "m"},
	}
	stub := &stubHTTPClient{
		baseURL: strings.TrimPrefix(srv.URL, "http://"),
		scheme:  "http",
	}
	svc := newGeminiPricingService(cfg, stub, testLogger(t))
	req := &dto.ParseGeminiPricingRequest{
		SystemPrompt:   "s",
		UserPrompt:     "u",
		ResponseSchema: json.RawMessage(`{}`),
	}

	out, err := svc.ParsePricing(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 2, callCount)
	require.JSONEq(t, `{"features":[],"plans":[]}`, string(out))
}

// stubHTTPClient rewrites requests to httptest.Server (GeminiPricingService builds googleapis URL).
type stubHTTPClient struct {
	baseURL string
	scheme  string
	inner   httpclient.Client
}

func (s *stubHTTPClient) Send(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
	if s.inner == nil {
		s.inner = httpclient.NewDefaultClient()
	}
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return nil, err
	}
	rewritten := *req
	rewritten.URL = s.scheme + "://" + s.baseURL + parsed.RequestURI()
	return s.inner.Send(ctx, &rewritten)
}
