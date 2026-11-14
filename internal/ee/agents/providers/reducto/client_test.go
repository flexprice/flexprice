package reducto

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

func newTestClient(t *testing.T, cfg config.ReductoConfig, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	return newClient(cfg, httpclient.NewClientWithConfig(httpclient.ClientConfig{Timeout: 5 * time.Second}), logger.NewNoopLogger())
}

func TestUpload_SendsMultipartFile(t *testing.T) {
	c := newTestClient(t, config.ReductoConfig{APIKey: "rk"}, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/upload", r.URL.Path)
		require.Equal(t, "Bearer rk", r.Header.Get("Authorization"))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		data, _ := io.ReadAll(file)
		require.Equal(t, "msa.pdf", header.Filename)
		require.Equal(t, "application/pdf", header.Header.Get("Content-Type"))
		require.Equal(t, "%PDF-1.7", string(data))
		_, _ = w.Write([]byte(`{"file_id":"reducto://abc.pdf","presigned_url":"https://x"}`))
	})

	resp, err := c.Upload(context.Background(), "msa.pdf", "application/pdf", []byte("%PDF-1.7"))
	require.NoError(t, err)
	require.Equal(t, "reducto://abc.pdf", resp.FileID)
}

func TestExtract_SendsSchemaAndParsesFields(t *testing.T) {
	var got ExtractRequest
	c := newTestClient(t, config.ReductoConfig{APIKey: "rk"}, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/extract", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &got))
		_, _ = w.Write([]byte(`{"response_type":"v3_extract","job_id":"job-1","duration":7.9,
			"usage":{"num_pages":4,"num_fields":61,"credits":null,"extract_mode":"extract"},
			"result":{"name":{"value":"Acme","citations":[{"type":"Text","content":"Acme Inc.","confidence":"high",
				"bbox":{"left":0.1,"top":0.2,"width":0.3,"height":0.01,"page":2},"granular_confidence":{"extract_confidence":0.97,"parse_confidence":1}}]}}}`))
	})

	resp, err := c.Extract(context.Background(), &ExtractRequest{
		Input:        "reducto://abc.pdf",
		Instructions: Instructions{Schema: json.RawMessage(`{"type":"object"}`), SystemPrompt: "be precise"},
		Settings:     &ExtractSettings{Citations: &CitationSettings{Enabled: true}},
	})
	require.NoError(t, err)
	require.Equal(t, "reducto://abc.pdf", got.Input)
	require.JSONEq(t, `{"type":"object"}`, string(got.Instructions.Schema))
	require.True(t, got.Settings.Citations.Enabled)
	require.Equal(t, "job-1", resp.JobID)
	require.Equal(t, 4, resp.Usage.NumPages)

	var result map[string]Field
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	f := result["name"]
	require.JSONEq(t, `"Acme"`, string(f.Value))
	require.Equal(t, 2, f.Citations[0].BBox.Page)
	require.Equal(t, 0.97, *f.Citations[0].GranularConfidence.ExtractConfidence)
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.ReductoConfig
		status  int
		body    string
		call    func(c *Client) error
		matches func(error) bool
	}{
		{name: "missing api key", cfg: config.ReductoConfig{}, call: extractCall, matches: ierr.IsServiceUnavailable},
		{name: "empty upload", cfg: config.ReductoConfig{APIKey: "k"}, call: func(c *Client) error {
			_, err := c.Upload(context.Background(), "x.pdf", "application/pdf", nil)
			return err
		}, matches: ierr.IsValidation},
		{name: "missing input", cfg: config.ReductoConfig{APIKey: "k"}, call: func(c *Client) error {
			_, err := c.Extract(context.Background(), &ExtractRequest{})
			return err
		}, matches: ierr.IsValidation},
		{name: "rate limited", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusTooManyRequests, body: `{}`, call: extractCall, matches: ierr.IsTooManyRequests},
		{name: "no credits", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusPaymentRequired, body: `{}`, call: extractCall, matches: ierr.IsServiceUnavailable},
		{name: "bad key", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusUnauthorized, body: `{"detail":"Invalid access token"}`, call: extractCall, matches: ierr.IsServiceUnavailable},
		{name: "unprocessable", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusUnprocessableEntity, body: `{"detail":[{"msg":"bad"}]}`, call: extractCall, matches: ierr.IsInvalidOperation},
		{name: "server error", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusBadGateway, body: `{}`, call: extractCall, matches: ierr.IsHTTPClient},
		{name: "empty result", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusOK, body: `{"job_id":"j"}`, call: extractCall, matches: ierr.IsHTTPClient},
		{name: "upload without file id", cfg: config.ReductoConfig{APIKey: "k"}, status: http.StatusOK, body: `{}`, call: func(c *Client) error {
			_, err := c.Upload(context.Background(), "x.pdf", "application/pdf", []byte("x"))
			return err
		}, matches: ierr.IsHTTPClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.cfg, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			err := tt.call(c)
			require.Error(t, err)
			require.True(t, tt.matches(err), "unexpected error kind: %v", err)
		})
	}
}

func TestTimeoutHasOwnHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	c := newClient(config.ReductoConfig{APIKey: "k", BaseURL: srv.URL},
		httpclient.NewClientWithConfig(httpclient.ClientConfig{Timeout: 50 * time.Millisecond}), logger.NewNoopLogger())

	err := extractCall(c)
	require.Error(t, err)
	require.Equal(t, []string{"The document service took too long to respond. Try again."}, cerrors.GetAllHints(err))
}

func extractCall(c *Client) error {
	_, err := c.Extract(context.Background(), &ExtractRequest{Input: "reducto://abc.pdf", Instructions: Instructions{Schema: json.RawMessage(`{}`)}})
	return err
}
