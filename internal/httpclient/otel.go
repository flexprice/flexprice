package httpclient

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OtelTransport wraps an http.RoundTripper with OpenTelemetry client-side
// instrumentation so that every outbound request emits a CLIENT span. These
// spans carry semantic HTTP attributes (http.request.method, server.address,
// url.full, ...) which power SigNoz's External API Monitoring view.
//
// It relies on the globally configured TracerProvider and propagator (set up in
// internal/tracing at startup). When tracing is disabled the global provider is
// a no-op, so the overhead is negligible and instrumentation can stay
// unconditional.
//
// If base is nil, http.DefaultTransport is used.
func OtelTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	return otelhttp.NewTransport(
		base,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			// e.g. "GET api.stripe.com" — readable and groups well by host.
			if r != nil && r.URL != nil {
				return r.Method + " " + r.URL.Host
			}
			return r.Method
		}),
	)
}

// NewOtelHTTPClient returns an *http.Client whose Transport is wrapped with
// OpenTelemetry client-side instrumentation. The provided timeout is preserved.
func NewOtelHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: OtelTransport(nil),
	}
}
