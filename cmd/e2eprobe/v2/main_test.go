package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/bootstrap"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/logger"
)

type stubStore struct {
	apiKey   string
	secretID string
	readErr  error
	writeErr error
	wrote    bool
}

func (s *stubStore) Read(_ context.Context, _, _, _ string) (string, string, error) {
	return s.apiKey, s.secretID, s.readErr
}

func (s *stubStore) Write(_ context.Context, _, _, _ string, _ *bootstrap.Credentials) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.wrote = true
	return nil
}

func TestResolveAPIKey_PrefersStoredKeyOverMinting(t *testing.T) {
	store := &stubStore{apiKey: "sk_stored"}
	cfg := &e2eprobe.Config{
		Email:               "a@b.c",
		Password:            "pw12345678",
		BootstrapSecretName: "probe-bootstrap",
		BootstrapSecretKey:  "api-key",
		PodNamespace:        "flexprice",
	}

	key, err := resolveAPIKey(context.Background(), cfg, store, true, func() (*bootstrap.Credentials, error) {
		t.Fatal("must not mint when the Secret already holds a key")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk_stored" {
		t.Errorf("key = %q, want sk_stored", key)
	}
}

func TestResolveAPIKey_MintsAndWritesWhenSecretEmpty(t *testing.T) {
	store := &stubStore{apiKey: ""}
	cfg := &e2eprobe.Config{
		Email:               "a@b.c",
		Password:            "pw12345678",
		BootstrapSecretName: "probe-bootstrap",
		BootstrapSecretKey:  "api-key",
		PodNamespace:        "flexprice",
	}

	key, err := resolveAPIKey(context.Background(), cfg, store, true, func() (*bootstrap.Credentials, error) {
		return bootstrap.NewCredentialsForTest("sk_new", "tenant_1", "env_1", "secret_1"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk_new" {
		t.Errorf("key = %q, want sk_new", key)
	}
	if !store.wrote {
		t.Error("expected the minted key to be persisted")
	}
}

func TestResolveAPIKey_WriteFailureIsFatalInCluster(t *testing.T) {
	store := &stubStore{apiKey: "", writeErr: errors.New("forbidden")}
	cfg := &e2eprobe.Config{
		Email: "a@b.c", Password: "pw12345678",
		BootstrapSecretName: "probe-bootstrap", BootstrapSecretKey: "api-key",
		PodNamespace: "flexprice",
	}

	_, err := resolveAPIKey(context.Background(), cfg, store, true, func() (*bootstrap.Credentials, error) {
		return bootstrap.NewCredentialsForTest("sk_new", "t", "e", "s"), nil
	})
	if err == nil {
		t.Fatal("a failed write in-cluster must be fatal, or the probe re-mints forever")
	}
}

func TestResolveAPIKey_OutsideClusterSkipsPersistence(t *testing.T) {
	cfg := &e2eprobe.Config{Email: "a@b.c", Password: "pw12345678"}

	key, err := resolveAPIKey(context.Background(), cfg, nil, false, func() (*bootstrap.Credentials, error) {
		return bootstrap.NewCredentialsForTest("sk_local", "t", "e", "s"), nil
	})
	if err != nil {
		t.Fatalf("running outside a cluster must still work: %v", err)
	}
	if key != "sk_local" {
		t.Errorf("key = %q, want sk_local", key)
	}
}

func TestParseFirstOTLPHeader(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantName  string
		wantValue string
		wantOK    bool
	}{
		{name: "empty", raw: "", wantOK: false},
		{name: "whitespace", raw: "   ", wantOK: false},
		{name: "no equals", raw: "signoz-access-token", wantOK: false},
		{name: "empty name", raw: "=abc", wantOK: false},
		{name: "empty value", raw: "signoz-access-token=", wantOK: false},
		{name: "single pair", raw: "signoz-access-token=xyz", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
		{name: "single pair with spaces", raw: " signoz-access-token = xyz ", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
		{name: "multiple pairs takes first", raw: "signoz-access-token=xyz,other=abc", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotValue, gotOK := parseFirstOTLPHeader(tc.raw)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotName != tc.wantName {
				t.Errorf("name = %q, want %q", gotName, tc.wantName)
			}
			if gotValue != tc.wantValue {
				t.Errorf("value = %q, want %q", gotValue, tc.wantValue)
			}
		})
	}
}

func TestValidateCommand_RejectsUnknownScenario(t *testing.T) {
	var out bytes.Buffer
	err := validateSuite("", scenario.NewCatalog(), &out)
	if err == nil || !strings.Contains(err.Error(), "provision") {
		t.Fatalf("an empty catalog must reject the default suite's scenarios, got %v", err)
	}
}

func TestDescribeCommand_PrintsMatrix(t *testing.T) {
	cat := newCatalog()
	var out bytes.Buffer
	if err := describeSuite("", cat, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "events/day: 53472") || !strings.Contains(out.String(), "taxed") {
		t.Errorf("describe output:\n%s", out.String())
	}
	if err := validateSuite("", cat, &out); err != nil {
		t.Errorf("validate with a complete catalog: %v", err)
	}
}

func TestValidateCommand_DefaultSuiteWithRealCatalog(t *testing.T) {
	var out bytes.Buffer
	if err := validateSuite("", newCatalog(), &out); err != nil {
		t.Fatalf("the embedded suite must validate against the shipped catalog: %v", err)
	}
	if !strings.Contains(out.String(), "45 bindings") || !strings.Contains(out.String(), "53472 events/day") {
		t.Errorf("summary = %q", out.String())
	}
}

func TestFilterBindings_KeepsOnlyNamedScenarios(t *testing.T) {
	bs := []suite.Binding{{Scenario: "usage", Persona: "a"}, {Scenario: "ingest", Persona: "a"}, {Scenario: "preflight"}}
	got := filterBindings(bs, splitList(" usage, preflight ,"))
	if len(got) != 2 || got[0].Scenario != "usage" || got[1].Scenario != "preflight" {
		t.Fatalf("filtered = %+v", got)
	}
	if got := splitList(""); got != nil {
		t.Errorf("an empty list must stay nil, got %v", got)
	}
}

func TestResolveBinding_NeedsAPersonaOnlyWhenAmbiguous(t *testing.T) {
	bs := []suite.Binding{{Scenario: "usage", Persona: "a"}, {Scenario: "usage", Persona: "b"}, {Scenario: "preflight"}}
	if _, err := resolveBinding(bs, "usage", ""); err == nil || !strings.Contains(err.Error(), "--persona") {
		t.Fatalf("two personas need a choice, got %v", err)
	}
	if b, err := resolveBinding(bs, "usage", "b"); err != nil || b.Persona != "b" {
		t.Fatalf("binding = %+v, err = %v", b, err)
	}
	if b, err := resolveBinding(bs, "preflight", ""); err != nil || b.Scenario != "preflight" {
		t.Fatalf("a tenant-level scenario needs no persona: %+v, %v", b, err)
	}
	if _, err := resolveBinding(bs, "nope", ""); err == nil || !strings.Contains(err.Error(), "describe") {
		t.Fatalf("an unknown scenario points at describe, got %v", err)
	}
}

func TestRunOnce_RunsOneBindingAgainstTheFake(t *testing.T) {
	cat := newCatalog()
	fc := probetest.New()
	rt, err := newRuntime(cat, &e2eprobe.Config{}, suite.Default(), fc, logger.NewNoopLogger(), "run-test", runtimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runOnce(context.Background(), rt, "ingest", "taxed", false, &out, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ingest · taxed: PASS") || len(fc.Event.Batches) != 1 {
		t.Fatalf("output = %q, batches = %d", out.String(), len(fc.Event.Batches))
	}
	var js bytes.Buffer
	_ = runOnce(context.Background(), rt, "preflight", "", true, &js, &bytes.Buffer{})
	if !strings.Contains(js.String(), `"status"`) {
		t.Fatalf("json output = %q", js.String())
	}
	if _, err := newRuntime(cat, &e2eprobe.Config{}, suite.Default(), fc, logger.NewNoopLogger(), "run-test", runtimeOptions{only: []string{"nope"}}); err == nil {
		t.Fatal("--only naming nothing in the suite is an error")
	}
	if _, err := resolveBinding(rt.bindings, "usage", ""); err == nil {
		t.Fatal("usage binds to several personas in the default suite")
	}
}

func TestValidateCommand_ExampleSuite(t *testing.T) {
	var out bytes.Buffer
	if err := validateSuite("suite.example.yaml", newCatalog(), &out); err != nil {
		t.Fatalf("the shipped example must validate against the shipped catalog: %v", err)
	}
}

func TestDescribeCommand_RendersBeforeReportingValidationErrors(t *testing.T) {
	path := t.TempDir() + "/suite.yaml"
	if err := os.WriteFile(path, []byte("version: 1\ndefaults:\n  load:\n    max_events_per_day: 1000\npersonas:\n  - name: solo\n    traits: [subscription]\n    events: standard\nscenarios:\n  - { name: ingest }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := describeSuite(path, newCatalog(), &out, &errOut)
	if err == nil || !strings.Contains(errOut.String(), "exceeds") {
		t.Fatalf("an over-budget suite must still be reported: err=%v stderr=%q", err, errOut.String())
	}
	if !strings.Contains(out.String(), "solo") || !strings.Contains(out.String(), "events/day") {
		t.Fatalf("describe must render the suite before judging it:\n%s", out.String())
	}
}

func TestOnceCommand_RequiresAnAPIKey(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example.test/v1")
	t.Setenv("E2EPROBE_API_KEY", "")
	t.Setenv("E2EPROBE_EMAIL", "probe@example.test")
	t.Setenv("E2EPROBE_PASSWORD", "password12345")
	err := once(newCatalog(), "", "preflight", "", false, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "E2EPROBE_API_KEY") {
		t.Fatalf("once must not run unauthenticated on bootstrap credentials, got %v", err)
	}
}

func TestRunOnce_ProvisioningNoteStaysOffStdout(t *testing.T) {
	fc := probetest.New()
	fc.Customer.CreateErr = probetest.ServerError(503)
	rt, err := newRuntime(newCatalog(), &e2eprobe.Config{}, suite.Default(), fc, logger.NewNoopLogger(), "run-test", runtimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	_ = runOnce(context.Background(), rt, "preflight", "", true, &out, &errOut)
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout must be one JSON document: %v\n%s", err, out.String())
	}
	if !strings.Contains(errOut.String(), "provisioning incomplete") {
		t.Fatalf("the provisioning note belongs on stderr, got %q", errOut.String())
	}
}

func TestParseCommandLine_RejectsFlagsOfOtherCommands(t *testing.T) {
	if _, _, err := parseCommandLine(io.Discard, []string{"once", "--only", "usage"}); err == nil {
		t.Fatal("once must not accept run's --only")
	}
	if _, _, err := parseCommandLine(io.Discard, []string{"run", "--scenario", "usage"}); err == nil {
		t.Fatal("run must not accept once's --scenario")
	}
	cmd, opts, err := parseCommandLine(io.Discard, []string{"--suite", "x.yaml"})
	if err != nil || cmd != "run" || opts.suitePath != "x.yaml" {
		t.Fatalf("flags without a command mean run: %q %+v %v", cmd, opts, err)
	}
	cmd, opts, err = parseCommandLine(io.Discard, []string{"once", "--scenario", "usage", "--persona", "taxed", "--json"})
	if err != nil || cmd != "once" || opts.scenario != "usage" || opts.persona != "taxed" || !opts.asJSON {
		t.Fatalf("once flags: %q %+v %v", cmd, opts, err)
	}
}
