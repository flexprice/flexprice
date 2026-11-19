package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/bootstrap"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
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
	cat := scenario.NewCatalog()
	for _, name := range []string{"provision", "preflight", "janitor", "webhook-wiring", "ingest", "usage", "wallet-debit", "invoice-freshness",
		"invoice-tax", "invoice-coupon", "invoice-commitment", "invoice-multi-cadence", "entitlement-usage", "low-balance-alert", "lifecycle", "cancel",
		"line-item-change", "tax-preview", "coupon-preview", "commitment-preview", "entitlement-enforcement", "grant-inheritance", "checkout-link",
		"payment-methods", "auto-charge", "billing-matrix-opening", "billing-matrix-change", "billing-matrix-grants", "billing-matrix-renewal", "billing-matrix-validation"} {
		cat.Register(suite.ScenarioInfo{Name: name, Kind: e2eprobe.KindProbe, Every: time.Hour}, func(scenario.Deps) scenario.Scenario { return nil })
	}
	var out bytes.Buffer
	if err := describeSuite("", cat, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "events/day: 28080") || !strings.Contains(out.String(), "taxed") {
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
	if !strings.Contains(out.String(), "48 bindings") || !strings.Contains(out.String(), "28080 events/day") {
		t.Errorf("summary = %q", out.String())
	}
}
