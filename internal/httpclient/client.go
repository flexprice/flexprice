package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Request represents an HTTP request
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// Client interface for making HTTP requests
type Client interface {
	Send(ctx context.Context, req *Request) (*Response, error)
}

// ClientConfig holds configuration for the HTTP client
type ClientConfig struct {
	Timeout time.Duration
}

// DefaultClient implements the Client interface
type DefaultClient struct {
	client *http.Client
}

// NewDefaultClient creates a new DefaultClient
func NewDefaultClient() Client {
	return &DefaultClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Send makes an HTTP request and returns the response
func (c *DefaultClient) Send(ctx context.Context, req *Request) (*Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set Content-Length if body is present
	if req.Body != nil {
		httpReq.ContentLength = int64(len(req.Body))
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Set headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Make request
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Copy response headers
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// Return HTTP error for non-2xx responses
	if resp.StatusCode >= 400 {
		return nil, NewError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    headers,
	}, nil
}
