package testutil

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"sync"

	"github.com/flexprice/flexprice/internal/httpclient"
)

// MockHTTPClient implements a mock HTTP client for testing
type MockHTTPClient struct {
	mu     sync.RWMutex
	routes map[string]MockResponse
}

// MockResponse represents a mock HTTP response
type MockResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// NewMockHTTPClient creates a new mock HTTP client
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		routes: make(map[string]MockResponse),
	}
}

// RegisterResponse registers a mock response for a given URL
func (m *MockHTTPClient) RegisterResponse(url string, resp MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes[url] = resp
}

// RegisterCSVResponse is a helper to register a CSV response
func (m *MockHTTPClient) RegisterCSVResponse(url string, headers []string, records [][]string) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.Write(headers)
	for _, record := range records {
		writer.Write(record)
	}
	writer.Flush()

	m.RegisterResponse(url, MockResponse{
		StatusCode: http.StatusOK,
		Body:       buf.Bytes(),
		Headers: map[string]string{
			"Content-Type": "text/csv",
		},
	})
}

// Send implements the httpclient.Client interface
func (m *MockHTTPClient) Send(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find the matching route
	var matchedResponse MockResponse
	var found bool
	for route, resp := range m.routes {
		if strings.HasSuffix(req.URL, route) {
			matchedResponse = resp
			found = true
			break
		}
	}

	if !found {
		return &httpclient.Response{
			StatusCode: http.StatusNotFound,
			Body:       []byte("Not Found"),
			Headers:    map[string]string{},
		}, nil
	}

	return &httpclient.Response{
		StatusCode: matchedResponse.StatusCode,
		Body:       matchedResponse.Body,
		Headers:    matchedResponse.Headers,
	}, nil
}

// Clear removes all registered responses
func (m *MockHTTPClient) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = make(map[string]MockResponse)
}
