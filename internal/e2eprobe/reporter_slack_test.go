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
	r := NewSlackReporter(srv.URL, "#syn", nil)
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

func TestSlackReporter_SwallowsHTTPErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	r := NewSlackReporter(srv.URL, "", nil)
	r.Report(context.Background(), FailureReport{CheckName: "x"})
}
