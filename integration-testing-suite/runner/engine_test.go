package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
)

// fakeAPI is a minimal Flexprice API double, exercised through the REAL SDK
// (the dispatcher points the SDK at this server), so the whole engine path —
// reflection dispatch, typed marshalling, response unwrap — is covered.
type fakeAPI struct {
	mux          *http.ServeMux
	pollCount    atomic.Int32
	deleted      atomic.Bool
	createdNames []string
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{mux: http.NewServeMux()}

	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	f.mux.HandleFunc("POST /customers", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		ext, _ := req["external_id"].(string)
		if strings.HasPrefix(ext, "dup-") {
			writeJSON(w, 409, map[string]any{"error": map[string]any{"message": "customer already exists"}})
			return
		}
		name, _ := req["name"].(string)
		f.createdNames = append(f.createdNames, name)
		writeJSON(w, 201, map[string]any{"id": "cust_1", "external_id": ext, "name": name})
	})

	f.mux.HandleFunc("GET /customers/cust_1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "cust_1", "external_id": "known", "name": "First"})
	})

	// Polling target: "pending" for the first two calls, then "ready".
	f.mux.HandleFunc("GET /customers/cust_poll", func(w http.ResponseWriter, r *http.Request) {
		n := f.pollCount.Add(1)
		name := "pending"
		if n >= 3 {
			name = "ready"
		}
		writeJSON(w, 200, map[string]any{"id": "cust_poll", "name": name})
	})

	f.mux.HandleFunc("GET /customers/missing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"error": map[string]any{"message": "customer not found"}})
	})

	f.mux.HandleFunc("DELETE /customers/cust_1", func(w http.ResponseWriter, r *http.Request) {
		f.deleted.Store(true)
		w.WriteHeader(204)
	})

	// Raw-HTTP step target (an endpoint "missing from the SDK").
	f.mux.HandleFunc("GET /internal/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "healthy"})
	})

	ts := httptest.NewServer(f.mux)
	t.Cleanup(ts.Close)
	return f, ts
}

func newTestExecutor(serverURL string) *Executor {
	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity("sk_test"),
	)
	return &Executor{
		Dispatcher:  NewDispatcher(client),
		Raw:         NewRawClient(serverURL, "sk_test"),
		TargetName:  "fake",
		StepTimeout: 10 * time.Second,
	}
}

func loadJourneyFromString(t *testing.T, yaml string) *Journey {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "j.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := loadJourneyFile(path)
	if err != nil {
		t.Fatalf("load journey: %v", err)
	}
	return j
}

const happyJourney = `
journey: happy-path
description: end-to-end engine features against the fake API
tags: [test]
steps:
  - id: customer
    call: Customers.CreateCustomer
    with: { external_id: "e-{{ .run.id }}", name: "First" }
    capture: { customer_id: id, external_id: external_id }
    expect:
      - { path: external_id, equals: "e-{{ .run.id }}" }
  - id: fetch
    call: Customers.GetCustomer
    with: "{{ .steps.customer.customer_id }}"
    expect:
      - { path: id, equals: "{{ .steps.customer.customer_id }}" }
  - id: dup
    call: Customers.CreateCustomer
    with: { external_id: "dup-x", name: "Dup" }
    expect_error: { status: 409, contains: already exists }
  - id: poll
    call: Customers.GetCustomer
    with: cust_poll
    until:
      - { path: name, equals: ready }
    timeout: 5s
    interval: 10ms
  - id: health
    http: { method: GET, path: /internal/health }
    expect:
      - { path: status, equals: healthy }
teardown:
  - name: Delete Customer
    call: Customers.DeleteCustomer
    with: "{{ .steps.customer.customer_id }}"
`

func TestHappyPathJourney(t *testing.T) {
	fake, ts := newFakeAPI(t)
	exec := newTestExecutor(ts.URL)
	j := loadJourneyFromString(t, happyJourney)

	if errs := ValidateJourney(j, exec.Dispatcher); len(errs) > 0 {
		t.Fatalf("validation errors: %v", errs)
	}

	res := exec.RunJourney(context.Background(), j)
	for _, s := range res.Steps {
		if s.Status != StatusPass {
			t.Errorf("step %q: status=%s err=%v skip=%s", s.Name, s.Status, s.Err, s.SkipReason)
		}
	}
	if res.Failed() {
		t.Fatal("journey should pass")
	}
	if !fake.deleted.Load() {
		t.Error("teardown did not delete the customer")
	}
	// Polling must have retried (server returns ready on 3rd call).
	var pollStep *StepResult
	for _, s := range res.Steps {
		if s.ID == "poll" {
			pollStep = s
		}
	}
	if pollStep == nil || pollStep.Attempts < 3 {
		t.Errorf("poll step should have >= 3 attempts, got %+v", pollStep)
	}
}

const cascadeJourney = `
journey: cascade
steps:
  - id: bad
    call: Customers.GetCustomer
    with: missing
  - id: after
    call: Customers.GetCustomer
    with: cust_1
teardown:
  - name: Delete Created Customer
    call: Customers.DeleteCustomer
    with: "{{ .steps.created.customer_id }}"
`

func TestFailureCascadeAndTeardownSkip(t *testing.T) {
	fake, ts := newFakeAPI(t)
	exec := newTestExecutor(ts.URL)
	j := loadJourneyFromString(t, cascadeJourney)

	res := exec.RunJourney(context.Background(), j)
	if !res.Failed() {
		t.Fatal("journey should fail")
	}
	byID := map[string]*StepResult{}
	for _, s := range res.Steps {
		key := s.ID
		if key == "" {
			key = s.Name
		}
		byID[key] = s
	}
	if byID["bad"].Status != StatusFail {
		t.Errorf("bad step: %+v", byID["bad"])
	}
	if byID["after"].Status != StatusSkip {
		t.Errorf("after step should be skipped: %+v", byID["after"])
	}
	td := byID["Delete Created Customer"]
	if td.Status != StatusSkip {
		t.Errorf("teardown referencing never-created entity should skip: %+v", td)
	}
	if fake.deleted.Load() {
		t.Error("teardown should not have deleted anything")
	}
}

const optionalJourney = `
journey: optional
steps:
  - id: flaky
    call: Customers.GetCustomer
    with: missing
    optional: true
  - id: after
    call: Customers.GetCustomer
    with: cust_1
`

func TestOptionalStepDowngradesToWarning(t *testing.T) {
	_, ts := newFakeAPI(t)
	exec := newTestExecutor(ts.URL)
	j := loadJourneyFromString(t, optionalJourney)

	res := exec.RunJourney(context.Background(), j)
	if res.Failed() {
		t.Fatal("optional failure must not fail the journey")
	}
	if !res.Steps[0].Warned {
		t.Errorf("flaky step should be warned: %+v", res.Steps[0])
	}
	if res.Steps[1].Status != StatusPass {
		t.Errorf("after step should run and pass: %+v", res.Steps[1])
	}
}

const repeatJourney = `
journey: repeat
steps:
  - id: many
    call: Customers.CreateCustomer
    repeat: 3
    with: { external_id: "e-{{ .run.id }}-{{ .iter }}", name: "n-{{ .iter }}" }
`

func TestRepeatRendersIterPerIteration(t *testing.T) {
	fake, ts := newFakeAPI(t)
	exec := newTestExecutor(ts.URL)
	j := loadJourneyFromString(t, repeatJourney)

	res := exec.RunJourney(context.Background(), j)
	if res.Failed() {
		t.Fatalf("repeat journey failed: %+v", res.Steps[0].Err)
	}
	if len(fake.createdNames) != 3 {
		t.Fatalf("expected 3 creates, got %d", len(fake.createdNames))
	}
	want := []string{"n-0", "n-1", "n-2"}
	for i, n := range fake.createdNames {
		if n != want[i] {
			t.Errorf("create %d: name=%q want %q", i, n, want[i])
		}
	}
}

func TestValidationCatchesAuthoringMistakes(t *testing.T) {
	_, ts := newFakeAPI(t)
	exec := newTestExecutor(ts.URL)

	bad := `
journey: bad
steps:
  - id: a
    call: Customers.CreateCustomerz
    with: { external_id: x }
  - id: b
    call: Customers.CreateCustomer
    with: { externalid_typo: x }
  - id: c
    call: Customers.GetCustomer
    with: "{{ .steps.zzz.id }}"
  - id: d
    call: Customers.UpdateCustomer
    with: { name: x }
`
	j := loadJourneyFromString(t, bad)
	errs := ValidateJourney(j, exec.Dispatcher)
	if len(errs) < 4 {
		t.Fatalf("expected >= 4 validation errors, got %d: %v", len(errs), errs)
	}
	all := ""
	for _, e := range errs {
		all += e.Error() + "\n"
	}
	for _, want := range []string{"no method", "unknown field", ".steps.zzz", "takes 3 argument"} {
		if !strings.Contains(all, want) {
			t.Errorf("validation errors should mention %q:\n%s", want, all)
		}
	}
}
