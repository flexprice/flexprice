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
	host      string
	client    *http.Client
	authName  string
	authValue string
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
	}
}

func (p probeHTTP) withAuth(name, value string) probeHTTP {
	p.authName = name
	p.authValue = value
	return p
}

func (p probeHTTP) require(t *testing.T, ctx context.Context, method, path string, body any, want int, out any) {
	t.Helper()
	if err := p.request(ctx, method, path, body, want, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

func (p probeHTTP) request(ctx context.Context, method, path string, body any, want int, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, p.host+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if p.authName != "" {
		req.Header.Set(p.authName, p.authValue)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
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
