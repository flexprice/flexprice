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
