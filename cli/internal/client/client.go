package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

const redacted = "[redacted]"

// safeKeys are the only response fields printed in full under --debug. Redaction
// is allowlist-based so that fields nobody anticipated fail closed.
var safeKeys = map[string]bool{
	"id": true, "object": true, "status": true, "type": true, "name": true,
	"created_at": true, "updated_at": true, "currency": true, "amount": true,
	"external_id": true, "lookup_key": true, "environment_id": true,
	"tenant_id": true, "error": true, "message": true, "hint": true, "code": true,
	"has_more": true, "iter_first_key": true, "iter_last_key": true, "total": true,
}

type Options struct {
	BaseURL string
	APIKey  string
	Version string
	Debug   bool
	// DebugOut receives --debug dumps. Never stdout: data goes to stdout so that
	// `--output json > file` stays clean.
	DebugOut io.Writer
}

type Client struct {
	base    *url.URL
	apiKey  string
	version string
	debug   bool
	debugW  io.Writer
	http    *retryablehttp.Client
}

func New(o Options) *Client {
	base, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if err != nil {
		base = &url.URL{}
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 200 * time.Millisecond
	rc.RetryWaitMax = 3 * time.Second
	rc.Logger = nil // retryablehttp logs to stderr by default; the CLI owns its output

	return &Client{
		base:    base,
		apiKey:  o.APIKey,
		version: o.Version,
		debug:   o.Debug,
		debugW:  o.DebugOut,
		http:    rc,
	}
}

// Do issues a request against path (relative to BaseURL) and returns the raw body.
// A non-2xx status returns *APIError.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
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
