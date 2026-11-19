package inbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const alertBody = `{"event_type":"wallet.ongoing_balance.dropped","wallet":{"id":"wal_1"},"customer":{"id":"cust_1"}}`

func post(h http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_RecordsValidPost(t *testing.T) {
	in := New(Options{})
	rec := post(in.Handler(), "/webhook", alertBody, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if got := in.Find(Match{WalletID: "wal_1"}); len(got) != 1 || got[0].EventType != "wallet.ongoing_balance.dropped" {
		t.Errorf("event not recorded: %+v", got)
	}
}

func TestHandler_RejectsMissingToken(t *testing.T) {
	in := New(Options{Token: "s3cret"})
	if rec := post(in.Handler(), "/webhook", alertBody, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d", rec.Code)
	}
	if rec := post(in.Handler(), "/webhook", alertBody, map[string]string{"X-E2EProbe-Token": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d", rec.Code)
	}
	if in.Stats().Total != 0 {
		t.Error("rejected posts must not be recorded")
	}
	if rec := post(in.Handler(), "/webhook", alertBody, map[string]string{"X-E2EProbe-Token": "s3cret"}); rec.Code != http.StatusOK {
		t.Errorf("right token: status = %d", rec.Code)
	}
}

func TestHandler_RejectsInvalidJSON(t *testing.T) {
	in := New(Options{})
	if rec := post(in.Handler(), "/webhook", "nope", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if rec := post(in.Handler(), "/webhook", `{"no_type":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandler_Healthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	New(Options{}).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandler_StatsJSON(t *testing.T) {
	in := New(Options{})
	post(in.Handler(), "/webhook", alertBody, nil)
	req := httptest.NewRequest(http.MethodGet, "/inbox/stats", nil)
	rec := httptest.NewRecorder()
	in.Handler().ServeHTTP(rec, req)
	var st Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Total != 1 || st.ByType["wallet.ongoing_balance.dropped"] != 1 {
		t.Errorf("stats body = %s err = %v", rec.Body, err)
	}
}

func TestServer_StartBindsAndServes(t *testing.T) {
	in := New(Options{})
	srv := NewServer(0, in.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port, err := srv.Start(ctx)
	if err != nil || port == 0 {
		t.Fatalf("start: port=%d err=%v", port, err)
	}
	resp, err := http.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/webhook", "application/json", strings.NewReader(alertBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("post: %v %v", err, resp)
	}
	_ = resp.Body.Close()
	if in.Stats().Total != 1 {
		t.Error("served post must be recorded")
	}
}
