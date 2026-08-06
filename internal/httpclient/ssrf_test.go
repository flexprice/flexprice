package httpclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", false},
		{"loopback v6", "::1", false},
		{"link-local metadata", "169.254.169.254", false},
		{"link-local v6", "fe80::1", false},
		{"private 10/8", "10.0.0.5", false},
		{"private 172.16/12", "172.16.5.5", false},
		{"private 192.168/16", "192.168.1.1", false},
		{"unique local v6", "fc00::1", false},
		{"unspecified v4", "0.0.0.0", false},
		{"unspecified v6", "::", false},
		{"multicast v4", "224.0.0.1", false},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", false},
		{"shared address space (CGNAT)", "100.64.0.1", false},
		{"documentation TEST-NET-1", "192.0.2.1", false},
		{"documentation TEST-NET-2", "198.51.100.1", false},
		{"documentation TEST-NET-3", "203.0.113.1", false},
		{"benchmarking", "198.18.0.1", false},
		{"limited broadcast", "255.255.255.255", false},
		{"ipv6 documentation", "2001:db8::1", false},
		{"this-network 0/8", "0.1.2.3", false},
		{"class E reserved", "240.0.0.1", false},
		{"nat64-encoded metadata IP", "64:ff9b::a9fe:a9fe", false},
		{"nat64-encoded public IP", "64:ff9b::0808:0808", true},
		{"6to4-encoded loopback", "2002:7f00:1::", false},
		{"6to4-encoded public IP", "2002:0808:0808::", true},
		{"public v4", "8.8.8.8", true},
		{"public v6", "2606:4700:4700::1111", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", tt.ip)
			}
			if got := IsPublicIP(ip); got != tt.want {
				t.Errorf("IsPublicIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestSSRFSafeTransport_IgnoresProxyEnv(t *testing.T) {
	// http.DefaultTransport (which SSRFSafeTransport clones) defaults Proxy to
	// ProxyFromEnvironment. If left in place, an HTTP_PROXY/HTTPS_PROXY env var
	// would make the dial guard validate only the proxy's IP while the proxy
	// itself forwards to whatever internal address the caller-supplied URL
	// names -- bypassing the guard entirely.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")

	if SSRFSafeTransport().Proxy != nil {
		t.Fatal("expected SSRFSafeTransport to disable proxying so the dial guard always sees the real destination")
	}
}

func TestSSRFSafeTransport_BlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: SSRFSafeTransport()}
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected request to loopback test server to be blocked, got nil error")
	}
}
