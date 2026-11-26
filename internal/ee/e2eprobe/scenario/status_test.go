package scenario

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

func TestHandler_ServesJSON(t *testing.T) {
	deps, _ := newDeps(t, true)
	cat := NewCatalog()
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Hour, func(context.Context, *Context) e2eprobe.Result { return e2eprobe.Pass() })
	tracker, _ := newTracker(t, deps)
	b := suite.Binding{Scenario: "usage", Persona: "taxed", Every: time.Hour}
	r, _ := NewRunner(cat, deps, []suite.Binding{b}, tracker, nil, Options{})
	r.RunOnce(context.Background(), b)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var st Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body)
	}
	if st.RunID != "run-test" || len(st.Bindings) != 1 || st.Bindings[0].Scenario != "usage" || st.Bindings[0].Passes != 1 {
		t.Errorf("status = %+v", st)
	}
	req = httptest.NewRequest(http.MethodGet, "/history", nil)
	rec = httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d", rec.Code)
	}
	var hist History
	if err := json.Unmarshal(rec.Body.Bytes(), &hist); err != nil {
		t.Fatalf("decode history: %v body=%s", err, rec.Body)
	}
	runs := hist.Bindings["usage · taxed"]
	if len(runs) != 1 || runs[0].Status != e2eprobe.StatusPass || runs[0].At.IsZero() {
		t.Errorf("history = %+v", hist)
	}
}
