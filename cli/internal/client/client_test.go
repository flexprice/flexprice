package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClient_SendsAPIKeyHeader(t *testing.T) {
	var gotKey, gotEnv string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotEnv = r.Header.Get("x-environment-id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "sk_test_123", Version: "1.2.3"})
	if _, err := c.Do(context.Background(), http.MethodGet, "/customers", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotKey != "sk_test_123" {
		t.Errorf("x-api-key = %q, want %q", gotKey, "sk_test_123")
	}
	// API keys are environment-scoped; the CLI must never send this header. Design doc §6.
	if gotEnv != "" {
		t.Errorf("x-environment-id = %q, want empty", gotEnv)
	}
}

func TestClient_SendsIdentifiableUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "1.2.3"})
	if _, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !strings.HasPrefix(ua, "flexprice-cli/1.2.3") {
		t.Errorf("User-Agent = %q, want prefix flexprice-cli/1.2.3", ua)
	}
}

func TestClient_ErrorStatusReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// The real envelope shape, verified against the live API.
		_, _ = w.Write([]byte(`{"code":"not_found","message":"Customer with ID c was not found",` +
			`"http_status_code":404,"details":{"customer_id":"c"}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, err := c.Do(context.Background(), http.MethodGet, "/missing", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "was not found") {
		t.Errorf("Error() = %q, want the API message surfaced", apiErr.Error())
	}
}

// A 5xx is retried; a 4xx is not, because retrying a client error just wastes time.
func TestClient_RetriesServerErrorsNotClientErrors(t *testing.T) {
	var serverHits, clientHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			serverHits++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		clientHits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"validation_error","message":"bad"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, _ = c.Do(context.Background(), http.MethodGet, "/boom", nil, nil)
	_, _ = c.Do(context.Background(), http.MethodGet, "/bad", nil, nil)

	if serverHits < 2 {
		t.Errorf("server error hit %d times, want retries", serverHits)
	}
	if clientHits != 1 {
		t.Errorf("client error hit %d times, want exactly 1 (no retry)", clientHits)
	}
}

func TestRedact_IsAllowlistBased(t *testing.T) {
	in := map[string]any{
		"id":            "cust_1",
		"email":         "a@b.com",
		"secret_token":  "shhh",
		"something_new": "unanticipated",
	}
	out := Redact(in)
	if out["id"] != "cust_1" {
		t.Errorf("id was redacted, want preserved")
	}
	// Anything not on the allowlist is redacted, so unanticipated fields fail closed.
	if out["something_new"] != redacted {
		t.Errorf("something_new = %v, want %q", out["something_new"], redacted)
	}
	if out["secret_token"] != redacted {
		t.Errorf("secret_token = %v, want %q", out["secret_token"], redacted)
	}
}

// Diagnostics must never reach stdout, even under --debug, so
// `--output json > file.json` stays clean.
func TestClient_DebugOutputNeverReachesStdout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cust_1","secret_token":"shhh"}`))
	}))
	defer srv.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	var debugBuf bytes.Buffer
	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t", Debug: true, DebugOut: &debugBuf})
	if _, err := c.Do(context.Background(), http.MethodPost, "/customers", nil, map[string]string{"name": "a"}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stdout = origStdout

	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if len(captured) != 0 {
		t.Errorf("stdout captured %q, want empty — diagnostics must go to debugW, not stdout", captured)
	}
	if debugBuf.Len() == 0 {
		t.Fatal("debugW is empty, want request/response dumps — test didn't actually exercise debug output")
	}
}

// A retry must re-send the body, not an already-drained reader. Safe only
// because retryablehttp snapshots the concrete *bytes.Reader it is given.
func TestClient_RetriedPUTResendsSameBody(t *testing.T) {
	var attempt int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		n := atomic.AddInt32(&attempt, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, err := c.Do(context.Background(), http.MethodPut, "/customers", nil, map[string]string{"name": "acme"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2 (one retry)", len(bodies))
	}
	want := `{"name":"acme"}`
	for i, b := range bodies {
		if b != want {
			t.Errorf("attempt %d body = %q, want %q", i+1, b, want)
		}
	}
}

// httptest's URL has an empty path, so other tests can't catch a join bug
// against a base URL that itself carries one.
func TestClient_JoinsPathAgainstBaseURLWithPath(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL + "/v1", APIKey: "k", Version: "t"})
	q := url.Values{"limit": []string{"10"}, "external_id": []string{"a b"}}
	if _, err := c.Do(context.Background(), http.MethodGet, "/customers/cust_1", q, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if gotPath != "/v1/customers/cust_1" {
		t.Errorf("path = %q, want %q", gotPath, "/v1/customers/cust_1")
	}
	wantQuery := url.Values{"limit": []string{"10"}, "external_id": []string{"a b"}}.Encode()
	if gotQuery != wantQuery {
		t.Errorf("query = %q, want %q", gotQuery, wantQuery)
	}
}

// The server may have committed before failing, so a retried POST can duplicate
// a subscription, invoice or payment.
func TestClient_DoesNotRetryPOSTOnServerError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, _ = c.Do(context.Background(), http.MethodPost, "/subscriptions", nil, map[string]any{"plan_id": "p"})

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("POST hit the server %d times, want exactly 1 (no retry)", got)
	}
}

// 429 is retried for any method: the server is explicitly saying it did not
// process the request.
func TestClient_RetriesPOSTOnRateLimit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	if _, err := c.Do(context.Background(), http.MethodPost, "/events", nil, map[string]any{"n": 1}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Errorf("POST hit the server %d times, want a retry after 429", got)
	}
}

func TestNew_MalformedBaseURLFailsWithAClearMessage(t *testing.T) {
	c := New(Options{BaseURL: "not-a-url", APIKey: "k", Version: "t"})
	_, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("want an error for a malformed base URL")
	}
	if !strings.Contains(err.Error(), "base URL") {
		t.Errorf("err = %q, want it to name the base URL as the cause", err)
	}
}

// Free-text fields must not be allowlisted: a server interpolates customer data
// into them.
func TestRedact_DoesNotAllowlistFreeTextFields(t *testing.T) {
	out := Redact(map[string]any{
		"id":      "cust_1",
		"message": "Customer ada@example.com owes 500 on card 4111111111111111",
		"name":    "Ada Lovelace",
	})
	if out["id"] != "cust_1" {
		t.Errorf("id was redacted, want preserved")
	}
	for _, k := range []string{"message", "name"} {
		if out[k] != redacted {
			t.Errorf("%s = %v, want redacted", k, out[k])
		}
	}
}
