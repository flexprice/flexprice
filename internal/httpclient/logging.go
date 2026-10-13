package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

// providerRequestIDHeaders identify a call in the provider's own logs, in lookup order.
var providerRequestIDHeaders = []string{
	"Request-Id",
	"X-Request-Id",
	"Intuit_tid",
	"X-Hubspot-Correlation-Id",
}

type loggingTransport struct {
	base     http.RoundTripper
	logger   *logger.Logger
	provider string
}

// ProviderTransport adds an OTel span and, unless logging.provider_calls_enabled is off,
// one redacted log line per provider call.
func ProviderTransport(base http.RoundTripper, log *logger.Logger, provider string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	if !log.ProviderCallsEnabled() {
		return OtelTransport(base)
	}

	return OtelTransport(&loggingTransport{base: base, logger: log, provider: provider})
}

// NewProviderHTTPClient is NewOtelHTTPClient with provider call logging.
func NewProviderHTTPClient(timeout time.Duration, log *logger.Logger, provider string) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     ProviderTransport(nil, log, provider),
		CheckRedirect: RejectRedirects,
	}
}

// NewProviderClient is NewDefaultClient with provider call logging.
func NewProviderClient(log *logger.Logger, provider string) Client {
	return &DefaultClient{client: NewProviderHTTPClient(30*time.Second, log, provider)}
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	ctx := req.Context()
	durationMS := time.Since(start).Milliseconds()
	redact := &redactor{}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			t.logger.Info(ctx, "provider_call.canceled",
				"provider", t.provider,
				"http_method", req.Method,
				"http_host", req.URL.Host,
				"http_path", req.URL.Path,
				"duration_ms", durationMS,
			)
			return resp, err
		}
		t.logger.Error(ctx, "provider_call.failed",
			"error", err.Error(),
			"provider", t.provider,
			"http_method", req.Method,
			"http_host", req.URL.Host,
			"http_path", req.URL.Path,
			"duration_ms", durationMS,
			"request_body", requestBody(req, redact),
		)
		t.logDetected(ctx, req, redact)
		return resp, err
	}

	requestID := providerRequestID(resp.Header)
	reqBody := requestBody(req, redact)
	defer t.logDetected(ctx, req, redact)

	if resp.StatusCode < http.StatusBadRequest {
		t.logger.Info(ctx, "provider_call.completed",
			"provider", t.provider,
			"http_method", req.Method,
			"http_host", req.URL.Host,
			"http_path", req.URL.Path,
			"status_code", resp.StatusCode,
			"duration_ms", durationMS,
			"provider_request_id", requestID,
			"request_body", reqBody,
		)
		return resp, nil
	}

	// Response bodies are logged only on failure; successful ones are mostly noise.
	respBody := peekResponseBody(resp, redact)
	if resp.StatusCode >= http.StatusInternalServerError {
		t.logger.Error(ctx, "provider_call.failed",
			"error", fmt.Sprintf("%s returned HTTP %d", t.provider, resp.StatusCode),
			"provider", t.provider,
			"http_method", req.Method,
			"http_host", req.URL.Host,
			"http_path", req.URL.Path,
			"status_code", resp.StatusCode,
			"duration_ms", durationMS,
			"provider_request_id", requestID,
			"request_body", reqBody,
			"response_body", respBody,
		)
		return resp, nil
	}

	// Info, not Error: most 4xx are expected outcomes, and Error marks the span as failed.
	t.logger.Info(ctx, "provider_call.rejected",
		"provider", t.provider,
		"http_method", req.Method,
		"http_host", req.URL.Host,
		"http_path", req.URL.Path,
		"status_code", resp.StatusCode,
		"duration_ms", durationMS,
		"provider_request_id", requestID,
		"retry_after", resp.Header.Get("Retry-After"),
		"request_body", reqBody,
		"response_body", respBody,
	)
	return resp, nil
}

// logDetected reports fields where a pattern caught PII the key list missed.
func (t *loggingTransport) logDetected(ctx context.Context, req *http.Request, redact *redactor) {
	if len(redact.detected) == 0 {
		return
	}

	t.logger.Info(ctx, "provider_call.pii_detected",
		"provider", t.provider,
		"http_method", req.Method,
		"http_path", req.URL.Path,
		"pii_paths", redact.detected,
	)
}

func providerRequestID(h http.Header) string {
	for _, name := range providerRequestIDHeaders {
		if v := h.Get(name); v != "" {
			return v
		}
	}
	return ""
}

func requestBody(req *http.Request, redact *redactor) string {
	// Not ContentLength: Stripe's SDK leaves it 0 on requests with a body.
	if req.GetBody == nil || req.Body == nil || req.Body == http.NoBody {
		return ""
	}
	if reason := skipReason(req.Header); reason != "" {
		return reason
	}

	body, err := req.GetBody()
	if err != nil {
		return ""
	}
	defer body.Close()
	data, _ := io.ReadAll(io.LimitReader(body, maxParsedBodyBytes+1))

	return redact.body(data, req.Header.Get("Content-Type"), "request")
}

// peekResponseBody reads the body for logging and puts it back for the caller.
func peekResponseBody(resp *http.Response, redact *redactor) string {
	if resp.Body == nil || resp.Body == http.NoBody {
		return ""
	}
	if reason := skipReason(resp.Header); reason != "" {
		return reason
	}

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxParsedBodyBytes+1))
	resp.Body = &replayedBody{Reader: io.MultiReader(bytes.NewReader(data), resp.Body), Closer: resp.Body}
	return redact.body(data, resp.Header.Get("Content-Type"), "response")
}

type replayedBody struct {
	io.Reader
	io.Closer
}
