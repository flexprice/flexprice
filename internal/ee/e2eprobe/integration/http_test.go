//go:build e2eprobe_integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type probeHTTP struct {
	host    string
	client  *http.Client
	headers http.Header
}

func newProbeHTTP(host string) probeHTTP {
	return probeHTTP{
		host: strings.TrimRight(host, "/"),
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		headers: make(http.Header),
	}
}

func (api probeHTTP) require(t *testing.T, ctx context.Context, method, path string, body any, want int, out any) {
	t.Helper()
	if err := api.request(ctx, method, path, body, want, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

func (api probeHTTP) request(ctx context.Context, method, path string, body any, want int, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, api.host+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header = api.headers.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := api.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("got HTTP %d, want %d (request ID %s)", resp.StatusCode, want, resp.Header.Get("X-Request-ID"))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
