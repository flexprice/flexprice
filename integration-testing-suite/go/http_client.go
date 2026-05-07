package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RawClient provides thin HTTP helpers for API resources that are not
// exposed by the generated Speakeasy Go SDK.
type RawClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewRawClient creates a RawClient. baseURL should include the scheme and
// path prefix, e.g. "https://us.api.flexprice.io/v1".
func NewRawClient(baseURL, apiKey string) *RawClient {
	return &RawClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Post sends a POST request and returns the decoded JSON body.
func (c *RawClient) Post(ctx context.Context, path string, body interface{}) (map[string]interface{}, int, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

// Get sends a GET request and returns the decoded JSON body.
func (c *RawClient) Get(ctx context.Context, path string) (map[string]interface{}, int, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// Put sends a PUT request and returns the decoded JSON body.
func (c *RawClient) Put(ctx context.Context, path string, body interface{}) (map[string]interface{}, int, error) {
	return c.do(ctx, http.MethodPut, path, body)
}

// Delete sends a DELETE request.
func (c *RawClient) Delete(ctx context.Context, path string) (int, error) {
	_, status, err := c.do(ctx, http.MethodDelete, path, nil)
	return status, err
}

// DeleteWithBody sends a DELETE request with a JSON body.
func (c *RawClient) DeleteWithBody(ctx context.Context, path string, body interface{}) (map[string]interface{}, int, error) {
	return c.do(ctx, http.MethodDelete, path, body)
}

// GetArray sends a GET request and returns the decoded JSON array.
// Use this for endpoints that return a top-level JSON array [...] instead of an object {...}.
func (c *RawClient) GetArray(ctx context.Context, path string) ([]interface{}, int, error) {
	return c.doArray(ctx, http.MethodGet, path, nil)
}

func (c *RawClient) do(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, int, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Some endpoints return 204 No Content.
	if len(respBody) == 0 {
		return nil, resp.StatusCode, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response JSON: %w (body: %s)", err, string(respBody))
	}

	return result, resp.StatusCode, nil
}

func (c *RawClient) doArray(ctx context.Context, method, path string, body interface{}) ([]interface{}, int, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if len(respBody) == 0 {
		return nil, resp.StatusCode, nil
	}

	var result []interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response JSON array: %w (body: %s)", err, string(respBody))
	}

	return result, resp.StatusCode, nil
}
