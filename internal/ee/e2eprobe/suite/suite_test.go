package suite

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"gopkg.in/yaml.v3"
)

// fakeCatalog mirrors the names the real catalog registers, so suite tests
// stay independent of the scenario packages.
type fakeCatalog struct {
	scenarios map[string]ScenarioInfo
	profiles  map[string]int
}

func (c fakeCatalog) Scenario(name string) (ScenarioInfo, bool) {
	info, ok := c.scenarios[name]
	return info, ok
}

func (c fakeCatalog) Profile(name string) (int, bool) {
	n, ok := c.profiles[name]
	return n, ok
}

func (c fakeCatalog) Names() []string {
	names := make([]string, 0, len(c.scenarios))
	for name := range c.scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func newFakeCatalog() fakeCatalog {
	tr := func(ts ...e2eprobe.Trait) []e2eprobe.Trait { return ts }
	infos := []ScenarioInfo{
		{Name: "provision", Kind: e2eprobe.KindMaintenance, Every: 6 * time.Hour},
		{Name: "preflight", Kind: e2eprobe.KindPreflight, Every: 6 * time.Hour},
		{Name: "janitor", Kind: e2eprobe.KindMaintenance, Every: time.Hour},
		{Name: "webhook-wiring", Kind: e2eprobe.KindProbe, Every: 15 * time.Minute},
		{Name: "ingest", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitEvents), Areas: []e2eprobe.Area{e2eprobe.AreaUsage}},
		{Name: "usage", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitEvents), Every: 15 * time.Minute, Summary: "exact totals for every meter over the last full hour", Areas: []e2eprobe.Area{e2eprobe.AreaUsage}},
		{Name: "analytics-hourly", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitEvents), Every: time.Hour, Areas: []e2eprobe.Area{e2eprobe.AreaUsage}},
		{Name: "wallet-debit", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitWallet), Every: 30 * time.Minute, EventsPerRun: 500},
		{Name: "invoice-freshness", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitSubscription), Every: time.Hour},
		{Name: "invoice-tax", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitTax), Every: time.Hour},
		{Name: "invoice-coupon", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitCoupon), Every: time.Hour},
		{Name: "invoice-commitment", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitCommitment), Every: time.Hour},
		{Name: "invoice-multi-cadence", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitMultiCadence), Every: time.Hour},
		{Name: "entitlement-usage", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitSoftLimit, e2eprobe.TraitEvents), Every: 30 * time.Minute},
		{Name: "low-balance-alert", Kind: e2eprobe.KindProbe, Traits: tr(e2eprobe.TraitAlerts), Every: 30 * time.Minute, EventsPerRun: 1},
		{Name: "lifecycle", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitSubscription), Every: 30 * time.Minute, EventsPerRun: 3},
		{Name: "cancel", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitSubscription), Every: time.Hour},
		{Name: "line-item-change", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitSubscription), Every: time.Hour},
		{Name: "tax-preview", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitTax), Every: time.Hour, EventsPerRun: 100},
		{Name: "coupon-preview", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitCoupon), Every: time.Hour},
		{Name: "commitment-preview", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitCommitment), Every: time.Hour, EventsPerRun: 100, Summary: "preview bills the commitment with a true-up under it and factored overage over it"},
		{Name: "entitlement-enforcement", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitSoftLimit), Every: time.Hour, EventsPerRun: 150},
		{Name: "grant-inheritance", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitAdditiveGrant), Every: time.Hour, EventsPerRun: 200},
		{Name: "checkout-link", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitPayment), Every: 30 * time.Minute, Areas: []e2eprobe.Area{e2eprobe.AreaPayments}},
		{Name: "payment-methods", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitPayment), Every: time.Hour, Areas: []e2eprobe.Area{e2eprobe.AreaPayments}},
		{Name: "auto-charge", Kind: e2eprobe.KindFlow, Traits: tr(e2eprobe.TraitPayment), Every: 6 * time.Hour, Areas: []e2eprobe.Area{e2eprobe.AreaPayments}},
	}
	for _, f := range []string{"opening", "change", "grants", "renewal", "validation"} {
		infos = append(infos, ScenarioInfo{Name: "billing-matrix-" + f, Kind: e2eprobe.KindFlow, Every: 15 * time.Minute, Areas: []e2eprobe.Area{e2eprobe.AreaProration}})
	}
	c := fakeCatalog{scenarios: map[string]ScenarioInfo{}, profiles: map[string]int{"standard": 27, "bucketed": 6}}
	for _, info := range infos {
		c.scenarios[info.Name] = info
	}
	return c
}

func TestDefault_ParsesAndAppliesDefaults(t *testing.T) {
	s := Default()
	if s.Version != 1 {
		t.Fatalf("version = %d", s.Version)
	}
	if s.Defaults.Tick.Duration != 10*time.Minute {
		t.Errorf("tick = %s, want 10m", s.Defaults.Tick)
	}
	if s.Defaults.Concurrency != 4 || s.Defaults.Alert.AfterFailures != 3 || s.Defaults.Alert.AfterDuration.Duration != 20*time.Minute ||
		s.Defaults.Alert.RenotifyEvery.Duration != 6*time.Hour || s.Defaults.Alert.SkipAfter.Duration != 24*time.Hour ||
		s.Defaults.Load.MaxEventsPerDay != 100000 || s.Defaults.Janitor.MaxAge.Duration != time.Hour || s.Defaults.Digest.Every.Duration != 24*time.Hour ||
		s.Tenant.Webhooks.SilenceAfter.Duration != 6*time.Hour {
		t.Errorf("defaults not applied: %+v %+v", s.Defaults, s.Tenant.Webhooks)
	}
	if len(s.Personas) != 10 {
		t.Errorf("personas = %d, want 10", len(s.Personas))
	}
	canary, ok := s.Persona("alert-canary")
	if !ok || !canary.HasTrait(e2eprobe.TraitWallet) || !canary.HasTrait(e2eprobe.TraitAlerts) || canary.HasTrait(e2eprobe.TraitEvents) {
		t.Errorf("alert-canary traits wrong: %+v", canary)
	}
	if canary.Wallet == nil || canary.Wallet.Thresholds == nil || canary.Wallet.Thresholds.Critical != "0" {
		t.Errorf("alert-canary thresholds missing: %+v", canary.Wallet)
	}
	taxed, _ := s.Persona("taxed")
	if taxed.ExternalIDOrDefault() != "e2eprobe-taxed" || !taxed.HasTrait(e2eprobe.TraitMultiCadence) || !taxed.HasTrait(e2eprobe.TraitEvents) {
		t.Errorf("taxed persona wrong: %+v", taxed)
	}
	committed, _ := s.Persona("committed")
	if committed.ExternalIDOrDefault() != "e2eprobe-committed" {
		t.Errorf("default external id = %q", committed.ExternalIDOrDefault())
	}
	if err := Validate(s, newFakeCatalog()); err != nil {
		t.Fatalf("default suite must validate: %v", err)
	}
}

func TestParse_PartialFileGetsDefaults(t *testing.T) {
	s, err := Parse([]byte("version: 1\npersonas:\n  - name: a\n    traits: [subscription]\nscenarios:\n  - name: lifecycle\n    personas: [a]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Defaults.Tick.Duration != 10*time.Minute || s.Defaults.Alert.AfterFailures != 3 || s.Defaults.Load.MaxEventsPerDay != 100000 {
		t.Errorf("defaults not applied on partial file: %+v", s.Defaults)
	}
	sc, ok := s.ScenarioConfig("lifecycle")
	if !ok || !sc.IsEnabled() {
		t.Errorf("scenario missing or disabled by default: %+v", sc)
	}
}

func TestParse_RejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte("version: 1\ndefaults:\n  tick: 10m\n  concurency: 2\n"))
	if err == nil || !strings.Contains(err.Error(), "concurency") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	var d Duration
	if err := yaml.Unmarshal([]byte("90s"), &d); err != nil || d.Duration != 90*time.Second {
		t.Fatalf("unmarshal: %v %s", err, d)
	}
	out, err := yaml.Marshal(d)
	if err != nil || strings.TrimSpace(string(out)) != "1m30s" {
		t.Fatalf("marshal = %q, %v", out, err)
	}
	if err := yaml.Unmarshal([]byte("soon"), &d); err == nil {
		t.Error("invalid duration must fail")
	}
}

func TestAlertPolicy_OverlaysScenarioOverride(t *testing.T) {
	s, err := Parse([]byte("version: 1\nscenarios:\n  - name: usage\n    alert: { after_failures: 5 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	p := s.AlertPolicy("usage")
	if p.AfterFailures != 5 || p.AfterDuration.Duration != 20*time.Minute || p.SkipAfter.Duration != 24*time.Hour {
		t.Errorf("policy = %+v", p)
	}
	if s.AlertPolicy("other").AfterFailures != 3 {
		t.Error("scenario without override must get defaults")
	}
}

// withPayer adds the Stripe payer persona the example suite carries; the
// embedded default has no gateway so a fresh region never pages on one.
func withPayer(s *Suite) *Suite {
	s.Personas = append(s.Personas, Persona{
		Name:    "payer-stripe",
		Traits:  []e2eprobe.Trait{e2eprobe.TraitPayment},
		Payment: &Payment{Provider: "stripe", Currency: "USD", FixedCustomer: "e2eprobe-cust-pay-stripe-cards", DeclineLast4: "0341"},
	})
	s.Tenant.Connections = []string{"stripe"}
	s.Scenarios = append(s.Scenarios,
		Scenario{Name: "checkout-link", Every: Duration{Duration: 30 * time.Minute}},
		Scenario{Name: "payment-methods", Every: Duration{Duration: time.Hour}},
		Scenario{Name: "auto-charge", Every: Duration{Duration: 6 * time.Hour}},
	)
	return s
}

func TestBytes_EmbeddedOrFile(t *testing.T) {
	embedded, err := Bytes("")
	if err != nil || len(embedded) == 0 {
		t.Fatalf("Bytes(\"\") = %d bytes, %v; want the embedded suite", len(embedded), err)
	}
	if _, err := Parse(embedded); err != nil {
		t.Fatalf("the embedded bytes must parse: %v", err)
	}
	path := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Bytes(path)
	if err != nil || string(got) != "version: 1\n" {
		t.Fatalf("Bytes(file) = %q, %v", got, err)
	}
}
