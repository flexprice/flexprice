package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A redirect must never be followed: the redirect target has not been through
// the caller's URL validation (VAPT SFX-2026-0203-F16).
func TestSendDoesNotFollowRedirects(t *testing.T) {
	var internalHit atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHit.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secret-metadata"))
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClientWithConfig(ClientConfig{})
	resp, err := client.Send(context.Background(), &Request{
		Method: http.MethodGet,
		URL:    redirector.URL,
	})

	if err == nil {
		t.Fatalf("expected an error for a redirect response, got resp=%+v", resp)
	}
	if internalHit.Load() {
		t.Fatal("redirect was followed: the internal target received a request")
	}
}

// NewDefaultClient shares the redirect policy — a regression there would
// otherwise go unnoticed.
func TestNewDefaultClientDoesNotFollowRedirects(t *testing.T) {
	var internalHit atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	_, err := NewDefaultClient().Send(context.Background(), &Request{
		Method: http.MethodGet,
		URL:    redirector.URL,
	})
	if err == nil {
		t.Fatal("expected an error for a redirect response")
	}
	if internalHit.Load() {
		t.Fatal("redirect was followed: the internal target received a request")
	}
}

// NewOtelHTTPClient is used directly (not via Send) by the file-import download
// and HEAD paths, so its redirect policy must hold on the bare *http.Client.
func TestNewOtelHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var internalHit atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	resp, err := NewOtelHTTPClient(5 * time.Second).Get(redirector.URL)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	// CheckRedirect returns ErrUseLastResponse, so the 302 is surfaced as-is
	// rather than followed.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected the 302 to be returned unfollowed, got %d", resp.StatusCode)
	}
	if internalHit.Load() {
		t.Fatal("redirect was followed: the internal target received a request")
	}
}

// Non-redirect responses must still work normally.
func TestSendFollowsNoRedirectHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := NewClientWithConfig(ClientConfig{})
	resp, err := client.Send(context.Background(), &Request{
		Method: http.MethodGet,
		URL:    srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, resp.Body)
	}
}
