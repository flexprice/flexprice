package e2eprobe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMeterHTTPClient_List(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"meter_1","event_name":"e2eprobe_count","name":"X","aggregation":{"type":"COUNT"}}]}`))
	}))
	defer srv.Close()
	c := newMeterHTTPClient(srv.URL, "k123")
	meters, err := c.List(context.Background())
	if err != nil || len(meters) != 1 || meters[0].ID != "meter_1" || gotAuth != "k123" {
		t.Fatalf("got=%+v err=%v auth=%q", meters, err, gotAuth)
	}
}

func TestMeterHTTPClient_Create_SendsBody(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"meter_new","event_name":"e2eprobe_count","name":"X","aggregation":{"type":"COUNT"}}`))
	}))
	defer srv.Close()
	c := newMeterHTTPClient(srv.URL, "k")
	m, err := c.Create(context.Background(), CreateMeterRequest{
		EventName:   "e2eprobe_count",
		Name:        "X",
		Aggregation: MeterAggregation{Type: "COUNT"},
	})
	if err != nil || m.ID != "meter_new" {
		t.Fatalf("create: %+v %v", m, err)
	}
	var sent map[string]any
	_ = json.Unmarshal(gotBody, &sent)
	if sent["event_name"] != "e2eprobe_count" {
		t.Errorf("body=%v", sent)
	}
}

func TestMeterHTTPClient_Non2xx_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()
	if _, err := newMeterHTTPClient(srv.URL, "k").List(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
