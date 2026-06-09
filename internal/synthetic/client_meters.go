package synthetic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type MeterOps interface {
	List(ctx context.Context) ([]Meter, error)
	Create(ctx context.Context, req CreateMeterRequest) (*Meter, error)
	Get(ctx context.Context, id string) (*Meter, error)
}

type Meter struct {
	ID          string            `json:"id"`
	EventName   string            `json:"event_name"`
	Name        string            `json:"name"`
	Aggregation MeterAggregation  `json:"aggregation"`
	Filters     []MeterFilter     `json:"filters,omitempty"`
	ResetUsage  string            `json:"reset_usage,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type MeterAggregation struct {
	Type       string `json:"type"`
	Field      string `json:"field,omitempty"`
	Multiplier string `json:"multiplier,omitempty"`
	BucketSize string `json:"bucket_size,omitempty"`
	GroupBy    string `json:"group_by,omitempty"`
	Expression string `json:"expression,omitempty"`
}

type MeterFilter struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type CreateMeterRequest struct {
	EventName   string            `json:"event_name"`
	Name        string            `json:"name"`
	Aggregation MeterAggregation  `json:"aggregation"`
	Filters     []MeterFilter     `json:"filters,omitempty"`
	ResetUsage  string            `json:"reset_usage,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func newMeterHTTPClient(apiHost, apiKey string) MeterOps {
	return &meterHTTPClient{
		base:   strings.TrimRight(apiHost, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type meterHTTPClient struct {
	base, apiKey string
	client       *http.Client
}

type meterListResp struct {
	Items []Meter `json:"items"`
}

func (m *meterHTTPClient) List(ctx context.Context) ([]Meter, error) {
	var r meterListResp
	if err := m.do(ctx, http.MethodGet, "/meters", nil, &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

func (m *meterHTTPClient) Create(ctx context.Context, req CreateMeterRequest) (*Meter, error) {
	var out Meter
	if err := m.do(ctx, http.MethodPost, "/meters", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *meterHTTPClient) Get(ctx context.Context, id string) (*Meter, error) {
	var out Meter
	if err := m.do(ctx, http.MethodGet, "/meters/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *meterHTTPClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", m.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("meter http %s %s: %d %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode: %w body=%s", err, string(respBody))
		}
	}
	return nil
}
