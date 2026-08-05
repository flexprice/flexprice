package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// IsPublicIP reports whether ip is a globally routable address -- i.e. not
// loopback, link-local (including the 169.254.169.254 cloud metadata
// address), private (RFC1918/RFC4193), unspecified, or multicast. It
// correctly handles IPv4-mapped IPv6 forms since net.IP's classifiers
// normalize via To4() internally.
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

// controlBlockNonPublic is a net.Dialer.Control callback. The Go runtime
// invokes it on the actual resolved address immediately before the TCP
// connect() syscall -- for the initial connection, every redirect hop, and
// every candidate IP a hostname resolves to -- so validating here can't be
// bypassed by a check-then-connect DNS-rebinding race.
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

// SSRFSafeTransport returns an *http.Transport that refuses to connect to
// non-public IP addresses. Use it as the base transport (wrapped with
// OtelTransport) for any HTTP client that fetches a caller-supplied URL.
func SSRFSafeTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlBlockNonPublic,
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	return transport
}
