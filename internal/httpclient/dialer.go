package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/validator"
)

// ErrBlockedAddress is returned when a connection is refused because the
// resolved address is not publicly routable.
var ErrBlockedAddress = fmt.Errorf("refusing to connect to a non-public address")

// dialTimeout and keepAlive mirror http.DefaultTransport's dialer settings, so
// swapping in the guarded dialer does not change connection behaviour.
const (
	dialTimeout = 30 * time.Second
	keepAlive   = 30 * time.Second
)

// publicOnlyDialContext refuses to open a connection to an address that is not
// publicly routable.
//
// Validating a user-supplied URL when it is stored is not sufficient on its own:
// the name is resolved again when the request is finally made, so a host that
// resolved to a public address at validation time can resolve to an internal one
// by the time we dial (DNS rebinding). This check runs against the address
// actually being connected to, which is the only point where that gap closes.
// It also covers hosts that were never validated at all.
func publicOnlyDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}

	// A literal address is checked directly and dialed as given.
	if ip := net.ParseIP(host); ip != nil {
		if !validator.IsPublicIP(ip) {
			return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, host)
		}
		return dialer.DialContext(ctx, network, addr)
	}

	// For a name, resolve once and dial one of the addresses we checked, rather
	// than handing the name back to the resolver. Re-resolving would reintroduce
	// the very gap this guard exists to close.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for host: %s", host)
	}

	var lastErr error
	for _, ipAddr := range ips {
		if !validator.IsPublicIP(ipAddr.IP) {
			// Any non-public answer fails the whole dial. A host that mixes
			// public and internal addresses is not one we should be reaching.
			return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ipAddr.IP)
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

// newGuardedTransport returns an http.Transport identical to Go's default
// except that it refuses to connect to non-public addresses.
func newGuardedTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = publicOnlyDialContext

	// The clone inherits ProxyFromEnvironment, which would defeat the guard: with
	// HTTP_PROXY set, the dial goes to the proxy address, publicOnlyDialContext
	// classifies the proxy rather than the destination, and the proxy then reaches
	// the original target unchecked. It also makes behaviour depend on the
	// environment the process happens to run in.
	transport.Proxy = nil

	return transport
}

// NewPublicOnlyClient returns a client for fetching URLs that came from a user:
// task imports, custom export endpoints, and provider metadata supplied through
// the API. It refuses redirects and refuses to connect to a non-public address.
//
// Use this rather than NewDefaultClient wherever the destination is
// caller-controlled. It is deliberately not the default: internal service calls
// and self-hosted provider endpoints legitimately target private addresses, and
// applying this guard to them would break those callers.
func NewPublicOnlyClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     OtelTransport(newGuardedTransport()),
		CheckRedirect: RejectRedirects,
	}
}

// NewPublicOnlySendClient is NewPublicOnlyClient behind the Client interface,
// for callers that go through Send rather than holding an *http.Client.
func NewPublicOnlySendClient(timeout time.Duration) Client {
	return &DefaultClient{client: NewPublicOnlyClient(timeout)}
}
