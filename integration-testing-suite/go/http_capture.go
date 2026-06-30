package main

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StackFrame is one frame from the Go call stack, trimmed to the file and
// function that live inside the integration test suite.
type StackFrame struct {
	File     string // basename only, e.g. "steps_subscription.go"
	Line     int
	Function string // short function name, e.g. "runSubscriptionSteps.func2"
}

// CallRecord captures one HTTP round-trip: the fully-enriched request (after
// RoutingCapture has added debug / pin headers) and the complete response.
type CallRecord struct {
	StepNumber int
	Method     string
	URL        string
	ReqHeaders http.Header
	ReqBody    []byte

	RespStatus  int
	RespHeaders http.Header
	RespBody    []byte
	Duration    time.Duration

	// Writer-pinning fields derived from the request/response headers.
	PinSent    bool  // X-Pin-To-Writer: true was on the outgoing request
	PinUntilMs int64 // X-Writer-Pinned-Until epoch-ms from response (0 = none)
	Routing    RoutingHeaders

	// TestStackTrace holds the frames from the integration-test source files
	// (steps_*.go, main.go, runner.go) that led to this HTTP call.
	// The outermost relevant frame is first.
	TestStackTrace []StackFrame
}

// TrafficLogger is an http.RoundTripper that sits between RoutingCapture and
// the real transport. It records every round-trip so the HTML report can show
// request/response detail per test step.
//
// Transport chain:
//
//	HTTP client → RoutingCapture → TrafficLogger → real transport → server
type TrafficLogger struct {
	inner       http.RoundTripper
	currentStep *atomic.Int32 // shared with SanityRunner; points to active step

	mu    sync.Mutex
	calls []CallRecord
}

// NewTrafficLogger creates a TrafficLogger. currentStep is an atomic that the
// runner keeps updated to the current step number.
func NewTrafficLogger(inner http.RoundTripper, currentStep *atomic.Int32) *TrafficLogger {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &TrafficLogger{inner: inner, currentStep: currentStep}
}

func (tl *TrafficLogger) RoundTrip(req *http.Request) (*http.Response, error) {
	step := int(tl.currentStep.Load())

	// Capture call stack now (before any awaiting), filter to test source files.
	stackFrames := captureTestStack()

	// Snapshot request body (stream can only be read once).
	var reqBody []byte
	if req.Body != nil && req.Body != http.NoBody {
		data, err := io.ReadAll(req.Body)
		if err == nil {
			reqBody = data
		}
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	pinSent := req.Header.Get("X-Pin-To-Writer") == "true"

	start := time.Now()
	resp, err := tl.inner.RoundTrip(req)
	dur := time.Since(start)

	rec := CallRecord{
		StepNumber:     step,
		Method:         req.Method,
		URL:            req.URL.String(),
		ReqHeaders:     req.Header.Clone(),
		ReqBody:        reqBody,
		Duration:       dur,
		PinSent:        pinSent,
		TestStackTrace: stackFrames,
	}

	if err != nil {
		// Still record the attempt.
		tl.mu.Lock()
		tl.calls = append(tl.calls, rec)
		tl.mu.Unlock()
		return nil, err
	}

	// Snapshot response body (consumers need to read it too).
	var respBody []byte
	if resp.Body != nil {
		data, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			respBody = data
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}

	rec.RespStatus = resp.StatusCode
	rec.RespHeaders = resp.Header.Clone()
	rec.RespBody = respBody
	rec.Routing = parseRoutingHeaders(resp.Header)

	if v := resp.Header.Get("X-Writer-Pinned-Until"); v != "" {
		// Parse manually to avoid strconv import cycle; reuse headerInt.
		rec.PinUntilMs = int64(headerInt(resp.Header, "X-Writer-Pinned-Until"))
	}

	tl.mu.Lock()
	tl.calls = append(tl.calls, rec)
	tl.mu.Unlock()

	return resp, nil
}

// Calls returns a copy of all recorded call records.
func (tl *TrafficLogger) Calls() []CallRecord {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]CallRecord, len(tl.calls))
	copy(out, tl.calls)
	return out
}

// CallsForStep returns all recorded calls that happened during the given step.
func (tl *TrafficLogger) CallsForStep(step int) []CallRecord {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	var out []CallRecord
	for _, c := range tl.calls {
		if c.StepNumber == step {
			out = append(out, c)
		}
	}
	return out
}

// sensitiveHeaders contains header names that should be masked in the report.
var sensitiveHeaders = map[string]bool{
	"X-Api-Key":     true,
	"Authorization": true,
}

// maskedHeaders returns a copy of h with sensitive values redacted.
func maskedHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vs := range h {
		if sensitiveHeaders[http.CanonicalHeaderKey(k)] {
			out[k] = []string{"[REDACTED]"}
		} else {
			out[k] = vs
		}
	}
	return out
}

// routingHeaderKeys are the headers worth highlighting in the report.
var routingHeaderKeys = []string{
	"X-Debug-Db-Routing",
	"X-Pin-To-Writer",
	"X-Writer-Pinned-Until",
	"X-Db-Routing-Reader",
	"X-Db-Routing-Writer-Pinned",
	"X-Db-Routing-Writer-Tx",
	"X-Db-Routing-Writer-Forced",
	"X-Db-Routing-Writer-Calls",
}

// captureTestStack walks the current goroutine's call stack and returns frames
// that originate from integration-test source files (steps_*.go, runner.go,
// main.go, etc.). Frames from the Go runtime, net/http, and the SDK are
// filtered out, leaving only the lines of test code that caused the HTTP call.
// The outermost (highest in the call chain) relevant frame is first.
func captureTestStack() []StackFrame {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(3, pcs) // skip runtime.Callers + captureTestStack + RoundTrip
	if n == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs[:n])
	var out []StackFrame
	for {
		frame, more := frames.Next()
		file := frame.File
		base := filepath.Base(file)
		// Keep only frames from integration-test Go files.
		if isTestSourceFile(base) {
			fn := frame.Function
			// Trim package prefix (e.g. "main.(*SanityRunner).runSubscriptionSteps.func2")
			if idx := strings.LastIndex(fn, "/"); idx >= 0 {
				fn = fn[idx+1:]
			}
			out = append(out, StackFrame{
				File:     base,
				Line:     frame.Line,
				Function: fn,
			})
		}
		if !more {
			break
		}
	}
	// Reverse so that the most-specific (innermost) frame that called us is last,
	// meaning the step function closure is at the bottom — easier to read top-down.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// isTestSourceFile returns true for Go files that are part of the integration
// test suite (not the Go runtime, SDK, or stdlib).
func isTestSourceFile(base string) bool {
	if !strings.HasSuffix(base, ".go") {
		return false
	}
	// Exclude generated/infra files inside this package that aren't "test logic".
	skip := []string{"http_capture.go", "routing_headers.go", "http_client.go", "targets.go"}
	for _, s := range skip {
		if base == s {
			return false
		}
	}
	// Include steps_*.go, runner.go, main.go and anything else in the suite.
	return strings.HasPrefix(base, "steps_") ||
		base == "runner.go" ||
		base == "main.go"
}

// formatHeaders renders a header map as "Key: Value\n" lines.
func formatHeaders(h http.Header) string {
	var sb strings.Builder
	// Routing headers first for easy scanning.
	for _, key := range routingHeaderKeys {
		if vs := h[http.CanonicalHeaderKey(key)]; len(vs) > 0 {
			sb.WriteString(key + ": " + strings.Join(vs, ", ") + "\n")
		}
	}
	// Everything else.
	for k, vs := range h {
		isRouting := false
		for _, rk := range routingHeaderKeys {
			if strings.EqualFold(k, rk) {
				isRouting = true
				break
			}
		}
		if isRouting {
			continue
		}
		sb.WriteString(k + ": " + strings.Join(vs, ", ") + "\n")
	}
	return sb.String()
}
