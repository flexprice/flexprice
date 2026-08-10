package validator

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrURLNotPublic is returned when a URL resolves to an address that is not
// publicly routable. Callers may match on it to distinguish an SSRF rejection
// from a malformed URL.
var ErrURLNotPublic = errors.New("url must point to a publicly routable host")

// resolveHost is swapped out in tests. Production always resolves via DNS.
var resolveHost = net.LookupIP

// ValidateOutboundURL checks that a user-supplied URL is safe to send a server
// side request to.
//
// Tenant-configured URLs (integration endpoints, OAuth servers, export targets)
// are chosen by whoever creates the connection, and we then send credentials to
// them. Unvalidated, such fields are SSRF sinks: they can name the cloud
// metadata service (169.254.169.254) or any other host inside the VPC.
//
// The URL must be absolute, https, carry no credentials, and resolve only to
// public addresses. Every resolved address is checked, not just the first, so a
// hostname that returns one public and one loopback address is rejected.
//
// This validates the URL at rest. It does not by itself defeat DNS rebinding or
// redirect-based bypasses; callers must also refuse redirects, which is what
// httpclient.RejectRedirects does.
func ValidateOutboundURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("url must not be empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return errors.New("url must be a valid URL")
	}

	if !u.IsAbs() {
		return errors.New("url must be absolute")
	}

	// Only https: http exposes credentials in transit, and other schemes
	// (file, gopher, ftp) are SSRF primitives rather than API endpoints.
	if u.Scheme != "https" {
		return errors.New("url must start with https://")
	}

	if u.User != nil {
		return errors.New("url must not contain credentials")
	}

	hostname := u.Hostname()
	if hostname == "" {
		return errors.New("url must have a valid host")
	}

	// A literal IP is checked directly; a hostname is resolved first so that a
	// name pointing at an internal address is rejected too.
	if ip := net.ParseIP(hostname); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("%w: %s", ErrURLNotPublic, hostname)
		}
		return nil
	}

	ips, err := resolveHost(hostname)
	if err != nil {
		return fmt.Errorf("url host could not be resolved: %s", hostname)
	}
	if len(ips) == 0 {
		return fmt.Errorf("url host did not resolve to any address: %s", hostname)
	}

	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("%w: %s", ErrURLNotPublic, hostname)
		}
	}

	return nil
}

// isPublicIP reports whether an address is publicly routable, and therefore not
// somewhere an outbound request could be used to reach internal infrastructure.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// Covers 127.0.0.0/8 and ::1, RFC1918 and unique-local, 0.0.0.0,
	// multicast, and link-local — which includes the 169.254.169.254 cloud
	// metadata endpoint and its IPv6 equivalent.
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}

	// Carrier-grade NAT (100.64.0.0/10). Not covered by IsPrivate, but routable
	// inside cloud networks and used by some metadata proxies.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
		// 192.0.0.0/24 holds IETF protocol assignments, including the
		// 192.0.0.192 metadata address used by some providers.
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0 {
			return false
		}
	}

	// IPv4-mapped and NAT64 addresses are re-checked as IPv4 so that
	// ::ffff:169.254.169.254 cannot slip past the checks above.
	if ip4 := ip.To4(); ip4 != nil && len(ip) == net.IPv6len {
		return isPublicIP(ip4)
	}

	return true
}
