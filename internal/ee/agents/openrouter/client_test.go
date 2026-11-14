package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, cfg config.OpenRouterConfig, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	return newClient(cfg, httpclient.NewClientWithConfig(httpclient.ClientConfig{Timeout: 5 * time.Second}), logger.NewNoopLogger())
}

func pdfRequest() *Request {
	return &Request{
		Model: "test/model",
		Messages: []Message{{
			Role: RoleUser,
			Content: []ContentPart{
				{Type: ContentTypeText, Text: "extract"},
				{Type: ContentTypeFile, File: &FilePart{Filename: "c.pdf", FileData: "data:application/pdf;base64,JVBERi0xLjQ="}},
			},
		}},
	}
}

func TestComplete_Success(t *testing.T) {
	var got Request
	var gotAuth string
	c := newTestClient(t, config.OpenRouterConfig{APIKey: "sk-test", ZDR: true, PDFEngine: PDFEngineMistralOCR},
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/chat/completions", r.URL.Path)
			gotAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(body, &got))
			_, _ = w.Write([]byte(`{"id":"gen-1","model":"test/model","choices":[{"finish_reason":"stop","message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"cost":0.0123}}`))
		})

	resp, err := c.Complete(context.Background(), pdfRequest())
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", gotAuth)
	require.Equal(t, `{"ok":true}`, resp.Content)
	require.Equal(t, &Usage{PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.0123}, resp.Usage)

	require.NotNil(t, got.Provider)
	require.Equal(t, dataCollectionDeny, got.Provider.DataCollection)
	require.True(t, got.Provider.RequireParameters)
	require.NotNil(t, got.Provider.ZDR)
	require.True(t, *got.Provider.ZDR)
	require.Equal(t, []Plugin{{ID: PluginFileParser, PDF: &PDFConfig{Engine: PDFEngineMistralOCR}}}, got.Plugins)
}

func TestComplete_NoPluginWhenEngineUnset(t *testing.T) {
	var got Request
	c := newTestClient(t, config.OpenRouterConfig{APIKey: "sk-test"}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &got))
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	})

	_, err := c.Complete(context.Background(), pdfRequest())
	require.NoError(t, err)
	require.Empty(t, got.Plugins)
	require.Nil(t, got.Provider.ZDR)
}

func TestComplete_Errors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.OpenRouterConfig
		status  int
		body    string
		req     *Request
		matches func(error) bool
	}{
		{name: "missing api key", cfg: config.OpenRouterConfig{}, req: pdfRequest(), matches: ierr.IsServiceUnavailable},
		{name: "missing model", cfg: config.OpenRouterConfig{APIKey: "k"}, req: &Request{}, matches: ierr.IsValidation},
		{name: "rate limited", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusTooManyRequests, body: `{}`, req: pdfRequest(), matches: ierr.IsTooManyRequests},
		{name: "no credits", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusPaymentRequired, body: `{}`, req: pdfRequest(), matches: ierr.IsServiceUnavailable},
		{name: "bad request", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusBadRequest, body: `{}`, req: pdfRequest(), matches: ierr.IsInvalidOperation},
		{name: "empty choices", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusOK, body: `{"choices":[]}`, req: pdfRequest(), matches: ierr.IsHTTPClient},
		{name: "error object", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusOK, body: `{"error":{"code":500,"message":"boom"}}`, req: pdfRequest(), matches: ierr.IsHTTPClient},
		{name: "refusal", cfg: config.OpenRouterConfig{APIKey: "k"}, status: http.StatusOK, body: `{"choices":[{"message":{"refusal":"no"}}]}`, req: pdfRequest(), matches: ierr.IsInvalidOperation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.cfg, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := c.Complete(context.Background(), tt.req)
			require.Error(t, err)
			require.True(t, tt.matches(err), "unexpected error kind: %v", err)
		})
	}
}

func TestComplete_TimeoutHasOwnHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	c := newClient(config.OpenRouterConfig{APIKey: "k", BaseURL: srv.URL},
		httpclient.NewClientWithConfig(httpclient.ClientConfig{Timeout: 50 * time.Millisecond}), logger.NewNoopLogger())

	_, err := c.Complete(context.Background(), pdfRequest())
	require.Error(t, err)
	require.True(t, ierr.IsHTTPClient(err))
	require.Equal(t, []string{"The AI model took too long to respond. Try again, or use a faster model."}, cerrors.GetAllHints(err))
}
