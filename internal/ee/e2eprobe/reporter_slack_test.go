package e2eprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackReporter_Posts(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	r := NewSlackReporter(srv.URL, "#syn", nil, nil)
	r.Report(context.Background(), FailureReport{
		CheckName:  "cycle-invoice-probe",
		CheckKind:  KindProbe,
		Step:       "freshness",
		Err:        errors.New("stale"),
		RunID:      "abc",
		Attributes: map[string]string{"sub_id": "sub_x"},
		OccurredAt: time.Now(),
	})
	var p map[string]any
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, gotBody)
	}
	text, _ := p["text"].(string)
	for _, want := range []string{"cycle-invoice-probe", "probe", "sub_x", "stale"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q: %s", want, text)
		}
	}
	if p["channel"] != "#syn" {
		t.Errorf("channel=%v", p["channel"])
	}
}

func TestSlackBotReporter_PostsWithBearer(t *testing.T) {
	var gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	rep := &slackReporter{endpoint: srv.URL, botToken: "xoxb-test", channel: "#syn", client: srv.Client()}
	rep.Report(context.Background(), FailureReport{CheckName: "cycle-invoice-probe", CheckKind: KindProbe})
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("auth=%q", gotAuth)
	}
	var p map[string]any
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["channel"] != "#syn" {
		t.Errorf("channel=%v", p["channel"])
	}
}

func TestSlackBotReporter_SwallowsAPIErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()
	rep := &slackReporter{endpoint: srv.URL, botToken: "xoxb-test", channel: "#x", client: srv.Client()}
	rep.Report(context.Background(), FailureReport{CheckName: "x"})
}

func TestSlackReporter_SwallowsHTTPErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	r := NewSlackReporter(srv.URL, "", nil, nil)
	r.Report(context.Background(), FailureReport{CheckName: "x"})
}
