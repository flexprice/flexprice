package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ErrURLNotPublic is returned when a URL resolves to an address that is not
// publicly routable. Callers may match on it to distinguish an SSRF rejection
// from a malformed URL.
var ErrURLNotPublic = errors.New("url must point to a publicly routable host")

// StubResolverForTest makes ValidateOutboundURL resolve every hostname to the
// given addresses, and restores the real resolver when the test finishes.
// Tests that exercise validation of public hostnames must use this: without it
// they perform live DNS lookups and fail wherever the network is unavailable.
func StubResolverForTest(t interface{ Cleanup(func()) }, ips ...net.IP) {
	previous := resolveHost
	t.Cleanup(func() { resolveHost = previous })
	resolveHost = func(string) ([]net.IP, error) { return ips, nil }
}

// dnsLookupTimeout bounds the resolution below. This runs on the request path,
// so an unresponsive DNS server must not hold the goroutine for the resolver's
// own default timeout.
const dnsLookupTimeout = 5 * time.Second

// resolveHost is swapped out in tests via StubResolverForTest. Production
// always resolves via DNS.
var resolveHost = func(host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

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
		if !IsPublicIP(ip) {
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
		if !IsPublicIP(ip) {
			return fmt.Errorf("%w: %s", ErrURLNotPublic, hostname)
		}
	}

	return nil
}

// IsPublicIP reports whether an address is publicly routable, and therefore not
// somewhere an outbound request could be used to reach internal infrastructure.
//
// Exported so the outbound HTTP transport can apply the same rule at dial time,
// which is what actually closes DNS rebinding: this function checks the
// addresses a name resolves to now, and the address dialed later may differ.
func IsPublicIP(ip net.IP) bool {
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

	// The RFC 8215 local-use translation range carries an IPv4 destination that
	// cannot be recovered without knowing the operator's prefix length, so it is
	// refused rather than decoded. See isNAT64LocalUsePrefix.
	if ip16 := ip.To16(); ip16 != nil && ip.To4() == nil && isNAT64LocalUsePrefix(ip16) {
		return false
	}

	// An IPv6 address can carry an IPv4 destination inside it. To4 only unwraps
	// the IPv4-mapped and IPv4-compatible forms, so NAT64 and 6to4 addresses
	// reach the IPv6 path with their real target hidden: 64:ff9b::a00:5 routes to
	// 10.0.0.5 and 2002:a9fe:a9fe:: routes to the 169.254.169.254 metadata
	// address. Judge those by the address they actually reach.
	if embedded := embeddedIPv4(ip); embedded != nil {
		return IsPublicIP(embedded)
	}

	if ip4 := ip.To4(); ip4 != nil {
		return isPublicIPv4(ip4)
	}

	return true
}

// isPublicIPv4 rejects the IPv4 ranges that are not publicly routable but are
// not covered by the net.IP classification helpers.
func isPublicIPv4(ip4 net.IP) bool {
	switch {
	// Carrier-grade NAT (100.64.0.0/10). Routable inside cloud networks and used
	// by some metadata proxies.
	case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127:
		return false
	// 192.0.0.0/24 holds IETF protocol assignments, including the 192.0.0.192
	// metadata address used by some providers.
	case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0:
		return false
	// 192.0.2.0/24 (TEST-NET-1), 198.51.100.0/24 (TEST-NET-2) and
	// 203.0.113.0/24 (TEST-NET-3) are documentation ranges, never a real
	// destination.
	case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2,
		ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100,
		ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113:
		return false
	// 198.18.0.0/15 is reserved for inter-network benchmarking.
	case ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19):
		return false
	// 240.0.0.0/4 is reserved for future use, and includes the
	// 255.255.255.255 broadcast address.
	case ip4[0] >= 240:
		return false
	}

	return true
}

// embeddedIPv4 returns the IPv4 address carried inside an IPv6 address for the
// translation schemes that To4 does not unwrap, or nil when there is none.
func embeddedIPv4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}

	// NAT64 well-known prefix 64:ff9b::/96 carries the IPv4 address in the last
	// four bytes. Bytes 4-11 must be zero for the prefix to be the /96 one:
	// matching on the first four bytes alone would also catch 64:ff9b:1::/48,
	// whose embedded address RFC 6052 splits around the u-octet rather than
	// leaving in the final four bytes. Reading those bytes for a /48 address
	// yields the wrong IPv4, so 64:ff9b:1::/48 is rejected outright below rather
	// than decoded here.
	if isNAT64WellKnownPrefix(ip16) {
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	}

	// 6to4 (2002::/16) encodes the IPv4 address in bytes 2-5.
	if ip16[0] == 0x20 && ip16[1] == 0x02 {
		return net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5])
	}

	return nil
}

// isNAT64WellKnownPrefix reports whether the address uses the 64:ff9b::/96
// well-known prefix, which is the only NAT64 prefix whose embedded IPv4 address
// sits in the final four bytes.
func isNAT64WellKnownPrefix(ip16 net.IP) bool {
	if ip16[0] != 0x00 || ip16[1] != 0x64 || ip16[2] != 0xff || ip16[3] != 0x9b {
		return false
	}
	for _, b := range ip16[4:12] {
		if b != 0x00 {
			return false
		}
	}
	return true
}

// isNAT64LocalUsePrefix reports whether the address falls in 64:ff9b:1::/48,
// the RFC 8215 local-use translation range.
//
// RFC 6052 encodes the embedded IPv4 address differently for each prefix length
// and splits it around the reserved u-octet for prefixes shorter than /96, so
// the destination cannot be recovered without knowing which length the operator
// configured. RFC 8215 says applications must not assume the embedded-address
// syntax here. Since the real target is unknowable, the whole range is refused
// rather than decoded: 64:ff9b:1:a00:0:5:808:808 translates to 10.0.0.5 while
// its final four bytes read as 8.8.8.8.
func isNAT64LocalUsePrefix(ip16 net.IP) bool {
	return ip16[0] == 0x00 && ip16[1] == 0x64 &&
		ip16[2] == 0xff && ip16[3] == 0x9b &&
		ip16[4] == 0x00 && ip16[5] == 0x01
}
