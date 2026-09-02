package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The dial-time guard is what closes DNS rebinding: a URL validated as public
// when it was stored can resolve to an internal address by the time it is
// fetched, so the address actually dialed has to be checked.
func TestPublicOnlyDialContextRejectsInternalAddresses(t *testing.T) {
	blocked := []string{
		"169.254.169.254:80", // cloud metadata
		"127.0.0.1:80",       // loopback
		"[::1]:80",           // loopback v6
		"10.0.0.5:80",        // RFC1918
		"172.16.0.5:80",      // RFC1918
		"192.168.1.5:80",     // RFC1918
		"0.0.0.0:80",         // unspecified
		"100.64.0.1:80",      // carrier-grade NAT
		"192.0.0.192:80",     // IETF protocol assignment
	}

	for _, addr := range blocked {
		t.Run(addr, func(t *testing.T) {
			conn, err := publicOnlyDialContext(context.Background(), "tcp", addr)
			if err == nil {
				conn.Close()
				t.Fatalf("expected %s to be refused", addr)
			}
			if !errors.Is(err, ErrBlockedAddress) {
				t.Fatalf("expected ErrBlockedAddress for %s, got: %v", addr, err)
			}
		})
	}
}

// A local test server listens on loopback, so a client using the guarded
// transport must fail to reach it. This exercises the whole client path rather
// than the dialer alone.
func TestGuardedTransportBlocksLoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newGuardedTransport()}
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the guarded transport to refuse a loopback server")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got: %v", err)
	}
}

// The guard must not interfere with ordinary public destinations. The dial is
// cancelled immediately so the test neither reaches the network nor waits for a
// timeout; only the classification that precedes the dial is under test.
func TestPublicAddressIsNotBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := publicOnlyDialContext(ctx, "tcp", "93.184.216.34:1")
	if errors.Is(err, ErrBlockedAddress) {
		t.Fatal("a public literal address must not be refused by the guard")
	}
}

// A proxy would defeat the guard entirely: the dial goes to the proxy address,
// which is what gets classified, and the proxy then reaches the real destination
// unchecked. http.DefaultTransport carries ProxyFromEnvironment, so the clone
// must drop it rather than inherit it.
func TestGuardedTransportIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://10.0.0.5:3128")
	t.Setenv("HTTPS_PROXY", "http://10.0.0.5:3128")
	t.Setenv("ALL_PROXY", "http://10.0.0.5:3128")

	transport := newGuardedTransport()
	if transport.Proxy != nil {
		t.Fatal("the guarded transport must not use a proxy")
	}

	// With the proxy honoured this request would dial 10.0.0.5 and be reported
	// against that address; the guard must reject the loopback target itself.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: transport}
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the guarded transport to refuse a loopback server")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got: %v", err)
	}
}

func TestSplitHostPortErrorSurfaces(t *testing.T) {
	if _, err := publicOnlyDialContext(context.Background(), "tcp", "no-port-here"); err == nil {
		t.Fatal("expected an error for a malformed address")
	}
}
