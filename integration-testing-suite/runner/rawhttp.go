package main

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
)

// RawClient executes http: steps — the escape hatch for endpoints the
// generated SDK does not expose yet. Every http: step is also reported as an
// SDK coverage gap.
type RawClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewRawClient creates a raw client. baseURL includes scheme and the /v1
// prefix, e.g. "https://api.cloud.flexprice.io/v1".
func NewRawClient(baseURL, apiKey string) *RawClient {
	return &RawClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Do executes a rendered HTTP request and returns (body, status, error).
// Non-2xx/3xx responses are errors unless expectStatus matches exactly.
func (c *RawClient) Do(ctx context.Context, method, path string, query, headers map[string]string, body any, expectStatus int) (any, int, error) {
	u := c.baseURL + "/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		q := url.Values{}
		for k, v := range query {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), u, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	ok := resp.StatusCode < 400
	if expectStatus != 0 {
		ok = resp.StatusCode == expectStatus
	}
	if !ok {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d from %s %s: %s", resp.StatusCode, method, path, truncateStr(string(respBody), 300))
	}

	if len(respBody) == 0 {
		return map[string]any{}, resp.StatusCode, nil
	}
	var doc any
	if err := json.Unmarshal(respBody, &doc); err != nil {
		// Non-JSON success bodies (PDFs, plain text) are summarized.
		return map[string]any{"raw": truncateStr(string(respBody), 500)}, resp.StatusCode, nil
	}
	return doc, resp.StatusCode, nil
}
