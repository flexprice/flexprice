package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

const redacted = "[redacted]"

// safeKeys are the only response fields printed in full under --debug. Redaction
// is allowlist-based so that fields nobody anticipated fail closed.
//
// Every key here is structurally constrained — an identifier, an enum, a number
// or a timestamp. Free-text fields are deliberately excluded even when they look
// harmless: "message", "name" and "hint" are exactly the fields a server
// interpolates dynamic content into, so allowlisting them by key would leak
// whatever happened to be interpolated.
var safeKeys = map[string]bool{
	"id": true, "object": true, "status": true, "type": true,
	"created_at": true, "updated_at": true, "currency": true, "amount": true,
	"external_id": true, "lookup_key": true, "environment_id": true,
	"tenant_id": true, "code": true,
	"has_more": true, "iter_first_key": true, "iter_last_key": true, "total": true,
}

// methodKey carries the request method into CheckRetry, which otherwise cannot
// see it when the transport fails and there is no response to inspect.
type methodKey struct{}

// idempotentMethods can be repeated without creating a second resource.
var idempotentMethods = map[string]bool{
	"GET": true, "HEAD": true, "PUT": true, "DELETE": true, "OPTIONS": true,
}

// retryPolicy refuses to retry non-idempotent requests on 5xx or transport errors.
//
// retryablehttp's default policy inspects only the status code and never the
// method, so it would retry POST identically to GET. On a billing API that is
// unsafe: a 502 raised after the server has already committed is indistinguishable
// from one raised before it, so retrying POST /subscriptions can create duplicate
// subscriptions that bill real customers. The API offers no Idempotency-Key
// header, CreateSubscriptionRequest has no idempotency field at all, and where a
// body-level idempotency_key is omitted the server generates one containing a
// timestamp — which differs on every attempt even though the body is byte
// identical, so server-side dedup does not help either.
//
// 429 is retried for every method: the server is explicitly stating it did not
// process the request.
func retryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	if method, _ := ctx.Value(methodKey{}).(string); !idempotentMethods[strings.ToUpper(method)] {
		return false, err
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

type Options struct {
	BaseURL string
	APIKey  string
	Version string
	Debug   bool
	// Timeout bounds a whole request including retries. Zero means DefaultTimeout;
	// cleanhttp sets only a Transport, so without this the client can hang forever
	// on a connection that stalls after headers arrive.
	Timeout time.Duration
	// DebugOut receives --debug dumps. Never stdout: data goes to stdout so that
	// `--output json > file` stays clean.
	DebugOut io.Writer
}

// DefaultTimeout bounds a whole request including retries.
const DefaultTimeout = 30 * time.Second

type Client struct {
	base    *url.URL
	apiKey  string
	version string
	debug   bool
	debugW  io.Writer
	http    *retryablehttp.Client
	// baseErr defers a malformed BaseURL to the first Do call. New has no error
	// return so that call sites stay a single expression; reporting it here still
	// names the real cause rather than surfacing "unsupported protocol scheme"
	// from deep in the HTTP stack.
	baseErr error
}

func New(o Options) *Client {
	c := &Client{
		apiKey:  o.APIKey,
		version: o.Version,
		debug:   o.Debug,
		debugW:  o.DebugOut,
	}

	base, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	switch {
	case err != nil:
		c.baseErr = fmt.Errorf("invalid base URL %q: %w", o.BaseURL, err)
	case base.Scheme == "" || base.Host == "":
		c.baseErr = fmt.Errorf("invalid base URL %q: expected a scheme and host, for example https://us.api.flexprice.io/v1", o.BaseURL)
	}
	if base == nil {
		base = &url.URL{}
	}
	c.base = base

	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 200 * time.Millisecond
	rc.RetryWaitMax = 3 * time.Second
	rc.Logger = nil // retryablehttp logs to stderr by default; the CLI owns its output
	rc.CheckRetry = retryPolicy
	rc.HTTPClient.Timeout = timeout
	c.http = rc

	return c
}

// Do issues a request against path (relative to BaseURL) and returns the raw body.
//
// A non-2xx status returns *APIError; the raw body is returned alongside it so a
// caller rendering JSON can still emit the error envelope. Callers must check the
// error before using the body. APIError.Raw carries the same bytes.
//
// Pass a literal nil for body when there is none: a typed nil pointer is a
// non-nil interface and would be encoded as the JSON literal null.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	if c.baseErr != nil {
		return nil, c.baseErr
	}
	ctx = context.WithValue(ctx, methodKey{}, method)

	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(b)
		c.dump("request body", b)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// The API key is environment-scoped, so x-environment-id is never sent. Design doc §6.
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("User-Agent", "flexprice-cli/"+c.version)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.debugf("%s %s", method, u.String())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	c.dump("response body", raw)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, NewAPIError(resp.StatusCode, raw, method, path)
	}
	return raw, nil
}

func (c *Client) debugf(format string, args ...any) {
	if !c.debug || c.debugW == nil {
		return
	}
	fmt.Fprintf(c.debugW, "> "+format+"\n", args...)
}

// dump prints a redacted view of a JSON payload under --debug.
func (c *Client) dump(label string, raw []byte) {
	if !c.debug || c.debugW == nil {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintf(c.debugW, "> %s: <%d bytes, not JSON>\n", label, len(raw))
		return
	}
	b, _ := json.MarshalIndent(redactAny(v), "> ", "  ")
	fmt.Fprintf(c.debugW, "> %s:\n> %s\n", label, b)
}

// Redact returns a copy of m with every value not on the allowlist replaced.
func Redact(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if !safeKeys[k] {
			out[k] = redacted
			continue
		}
		out[k] = redactAny(v)
	}
	return out
}

func redactAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return Redact(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactAny(e)
		}
		return out
	default:
		return v
	}
}
