package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// IsPublicIP reports whether ip is globally routable (not loopback,
// link-local incl. 169.254.169.254, private, unspecified, or multicast).
func IsPublicIP(ip net.IP) bool {
	switch {
	case ip.IsLoopback(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsPrivate(),
		ip.IsUnspecified(),
		ip.IsMulticast():
		return false
	}
	return true
}

// controlBlockNonPublic runs on the resolved IP right before connect(), for
// every dial including redirects -- so it can't be bypassed by DNS rebinding.
func controlBlockNonPublic(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf guard: invalid address %q: %w", address, err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ssrf guard: unable to parse resolved address %q", host)
	}

	if !IsPublicIP(ip) {
		return fmt.Errorf("ssrf guard: connections to %s are not allowed", ip.String())
	}

	return nil
}

// SSRFSafeTransport blocks connections to non-public IPs. Use as the base
// transport (wrapped with OtelTransport) for clients fetching caller URLs.
func SSRFSafeTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlBlockNonPublic,
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	// Disable env-configured proxying: routing through a proxy would make the
	// dial guard validate the proxy's IP, not the caller-supplied destination.
	transport.Proxy = nil
	return transport
}
