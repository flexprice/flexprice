package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// nonPublicNets covers reserved ranges net.IP's classifiers don't: "this
// network" (0.0.0.0/8), RFC6598 shared address space (CGNAT), RFC5737/
// RFC3849 documentation ranges, RFC2544 benchmarking, Class E reserved
// space, the limited broadcast address, and the RFC6052 local-use NAT64
// prefix. The local-use prefix is blocked outright rather than unwrapped
// like the well-known 64:ff9b::/96 prefix below: its /48 embedding splits
// the IPv4 payload around a reserved byte, so decoding it is unnecessary
// complexity for a prefix that isn't globally routable anyway.
var nonPublicNets = mustParseCIDRs(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"198.18.0.0/15",
	"240.0.0.0/4",
	"255.255.255.255/32",
	"2001:db8::/32",
	"64:ff9b:1::/48",
)

// nat64Net and sixToFourNet are IPv6 transition mechanisms (RFC6052,
// RFC3056) that embed an IPv4 address in the low/mid bits. IsPublicIP
// unwraps and re-validates the embedded address so an attacker can't use
// either encoding to smuggle a non-public IPv4 address past the checks
// above, which only inspect the IPv6 bytes directly.
var (
	nat64Net     = mustParseCIDRs("64:ff9b::/96")[0]
	sixToFourNet = mustParseCIDRs("2002::/16")[0]
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, len(cidrs))
	for i, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		nets[i] = n
	}
	return nets
}

// embeddedV4 returns the IPv4 address embedded in a NAT64 or 6to4 address,
// or nil if ip is neither.
func embeddedV4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}
	switch {
	case nat64Net.Contains(ip):
		return net.IP(ip16[12:16])
	case sixToFourNet.Contains(ip):
		return net.IP(ip16[2:6])
	}
	return nil
}

// IsPublicIP reports whether ip is globally routable (not loopback,
// link-local incl. 169.254.169.254, private, unspecified, multicast, one of
// the reserved ranges in nonPublicNets, or a NAT64/6to4 encoding of a
// non-public IPv4 address).
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
	for _, n := range nonPublicNets {
		if n.Contains(ip) {
			return false
		}
	}
	if v4 := embeddedV4(ip); v4 != nil {
		return IsPublicIP(v4)
	}
	return true
}

// controlBlockNonPublic runs on the resolved IP right before connect(), for
// every dial including redirects -- so it can't be bypassed by DNS rebinding.
func controlBlockNonPublic(_ string, address string, _ syscall.RawConn) error {
	// Error strings deliberately omit the address/IP: they flow into logs and
	// traces, and echoing back attacker-supplied targets would let a caller
	// enumerate internal network addressing.
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf guard: invalid address")
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ssrf guard: unable to parse resolved address")
	}

	if !IsPublicIP(ip) {
		return fmt.Errorf("ssrf guard: connection blocked to non-public address")
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
