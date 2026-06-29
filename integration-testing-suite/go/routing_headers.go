package main

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// RoutingHeaders holds the DB routing decision counts extracted from
// X-DB-Routing-* response headers emitted by the server when
// X-Debug-DB-Routing: true is sent on the request.
type RoutingHeaders struct {
	Reader       int
	WriterPinned int
	WriterTx     int
	WriterForced int
	WriterCalls  int
}

func parseRoutingHeaders(h http.Header) RoutingHeaders {
	return RoutingHeaders{
		Reader:       headerInt(h, "X-Db-Routing-Reader"),
		WriterPinned: headerInt(h, "X-Db-Routing-Writer-Pinned"),
		WriterTx:     headerInt(h, "X-Db-Routing-Writer-Tx"),
		WriterForced: headerInt(h, "X-Db-Routing-Writer-Forced"),
		WriterCalls:  headerInt(h, "X-Db-Routing-Writer-Calls"),
	}
}

func headerInt(h http.Header, key string) int {
	v := h.Get(key)
	if v == "" {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// RoutingExpectation describes what routing decisions are expected for a step.
// Zero values mean "no assertion on this field".
type RoutingExpectation struct {
	// Minimum values (inclusive). Zero = no lower bound asserted.
	WriterPinnedMin int
	WriterCallsMin  int
	ReaderMin       int

	// Maximum values (inclusive). -1 = not set. Zero = assert zero.
	WriterPinnedMax int // use -1 to skip upper-bound check
	ReaderMax       int // use -1 to skip upper-bound check
}

func (e RoutingExpectation) check(rh RoutingHeaders) error {
	if e.WriterPinnedMin > 0 && rh.WriterPinned < e.WriterPinnedMin {
		return fmt.Errorf("writer_pinned=%d, want >= %d (under-pinning: reads after write went to replica)", rh.WriterPinned, e.WriterPinnedMin)
	}
	if e.WriterPinnedMax >= 0 && rh.WriterPinned > e.WriterPinnedMax {
		return fmt.Errorf("writer_pinned=%d, want <= %d (over-pinning: reads leaked onto write path)", rh.WriterPinned, e.WriterPinnedMax)
	}
	if e.WriterCallsMin > 0 && rh.WriterCalls < e.WriterCallsMin {
		return fmt.Errorf("writer_calls=%d, want >= %d", rh.WriterCalls, e.WriterCallsMin)
	}
	if e.ReaderMin > 0 && rh.Reader < e.ReaderMin {
		return fmt.Errorf("reader=%d, want >= %d", rh.Reader, e.ReaderMin)
	}
	if e.ReaderMax >= 0 && rh.Reader > e.ReaderMax {
		return fmt.Errorf("reader=%d, want <= %d", rh.Reader, e.ReaderMax)
	}
	return nil
}

// RoutingCapture is an http.RoundTripper that:
//  1. Injects X-Debug-DB-Routing: true on every request so the server emits
//     X-DB-Routing-* response headers.
//  2. When the server returns X-Writer-Pinned-Until: <epoch-ms> (indicating a
//     write just happened), stores that expiry and injects X-Pin-To-Writer: true
//     on all subsequent requests until the window expires — enabling cross-request
//     read-after-write consistency validation.
//  3. Captures the last set of X-DB-Routing-* headers for test assertions.
type RoutingCapture struct {
	inner          http.RoundTripper
	mu             sync.Mutex
	last           RoutingHeaders
	pinnedUntilMs  atomic.Int64 // epoch-ms; 0 = not pinned
}

func NewRoutingCapture(inner http.RoundTripper) *RoutingCapture {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &RoutingCapture{inner: inner}
}

func (rc *RoutingCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Debug-DB-Routing", "true")

	// Cross-request writer pin: if a previous response told us to pin, and the
	// window hasn't expired, tell the server to pre-pin this request's context.
	if until := rc.pinnedUntilMs.Load(); until > 0 {
		if time.Now().UnixMilli() < until {
			req.Header.Set("X-Pin-To-Writer", "true")
		} else {
			rc.pinnedUntilMs.Store(0) // window expired, clear it
		}
	}

	resp, err := rc.inner.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	rc.mu.Lock()
	rc.last = parseRoutingHeaders(resp.Header)
	rc.mu.Unlock()

	// If the server signals that a write just happened, store the pin expiry.
	if v := resp.Header.Get("X-Writer-Pinned-Until"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			rc.pinnedUntilMs.Store(ms)
		}
	}

	return resp, nil
}

// Last returns the routing headers captured from the most recent response.
func (rc *RoutingCapture) Last() RoutingHeaders {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.last
}
