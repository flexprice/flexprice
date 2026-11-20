package suite

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

func bindingsOf(t *testing.T, s *Suite, scenario string) []Binding {
	t.Helper()
	all, err := s.Bindings(newFakeCatalog())
	if err != nil {
		t.Fatal(err)
	}
	var out []Binding
	for _, b := range all {
		if b.Scenario == scenario {
			out = append(out, b)
		}
	}
	return out
}

func TestBindings_ByTraitAndExplicit(t *testing.T) {
	s := Default()
	usage := bindingsOf(t, s, "usage")
	if len(usage) != 8 {
		t.Fatalf("usage bindings = %d, want 8 (every events persona, bucketed included)", len(usage))
	}
	for _, b := range usage {
		if b.Every != 15*time.Minute || b.Kind != e2eprobe.KindProbe {
			t.Errorf("usage binding carries wrong every/kind: %+v", b)
		}
	}
	lifecycle := bindingsOf(t, s, "lifecycle")
	if len(lifecycle) != 1 || lifecycle[0].Persona != "baseline-a" {
		t.Errorf("lifecycle bindings = %+v, want baseline-a only", lifecycle)
	}
	preflight := bindingsOf(t, s, "preflight")
	if len(preflight) != 1 || preflight[0].Persona != "" || preflight[0].Every != 6*time.Hour {
		t.Errorf("preflight binding = %+v", preflight)
	}
	wallet := bindingsOf(t, s, "wallet-debit")
	if len(wallet) != 1 || wallet[0].Persona != "wallet-a" {
		t.Errorf("wallet-debit must bind to wallet-a only, got %+v", wallet)
	}
}

func TestBindings_ExpandsFamilies(t *testing.T) {
	s := Default()
	if got := len(bindingsOf(t, s, "billing-matrix-opening")); got != 0 {
		t.Errorf("billing matrix is disabled by default, got %d bindings", got)
	}
	enabled := true
	mustScenario(s, "billing-matrix").Enabled = &enabled
	for _, f := range []string{"opening", "change", "grants", "renewal", "validation"} {
		b := bindingsOf(t, s, "billing-matrix-"+f)
		if len(b) != 1 || b[0].Persona != "" {
			t.Errorf("billing-matrix-%s bindings = %+v", f, b)
		}
	}
	tax := bindingsOf(t, s, "invoice-tax")
	if len(tax) != 1 || tax[0].Persona != "taxed" || tax[0].Every != time.Hour {
		t.Errorf("invoice-tax bindings = %+v", tax)
	}
	if mc := bindingsOf(t, s, "invoice-multi-cadence"); len(mc) != 1 || mc[0].Persona != "taxed" {
		t.Errorf("invoice-multi-cadence bindings = %+v", mc)
	}
}

func TestBindings_DefaultEveryFromCatalog(t *testing.T) {
	s := Default()
	ingest := bindingsOf(t, s, "ingest")
	if len(ingest) != 8 {
		t.Fatalf("ingest bindings = %d", len(ingest))
	}
	if ingest[0].Every != 0 {
		t.Errorf("ingest keeps the catalog's zero interval so the runner uses the tick, got %s", ingest[0].Every)
	}
	s2, _ := Parse([]byte("version: 1\npersonas:\n  - name: a\n    traits: [subscription]\nscenarios:\n  - name: cancel\n"))
	if b := bindingsOf(t, s2, "cancel"); len(b) != 1 || b[0].Every != time.Hour {
		t.Errorf("cancel must take the catalog default of 1h, got %+v", b)
	}
}

func TestBindings_SortedAndSkipDisabled(t *testing.T) {
	s := Default()
	off := false
	mustScenario(s, "usage").Enabled = &off
	all, err := s.Bindings(newFakeCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Scenario > all[i].Scenario || (all[i-1].Scenario == all[i].Scenario && all[i-1].Persona > all[i].Persona) {
			t.Fatalf("bindings not sorted at %d: %+v %+v", i, all[i-1], all[i])
		}
	}
	if len(bindingsOf(t, s, "usage")) != 0 {
		t.Error("disabled scenario must produce no bindings")
	}
}

func TestEventsPerDay(t *testing.T) {
	if got := Default().EventsPerDay(newFakeCatalog()); got != 53472 {
		t.Errorf("events/day = %d, want 53472 (28080 from profiles + 25392 from flows)", got)
	}
	profiles, flows := Default().Ingest(newFakeCatalog())
	if profiles != 28080 || flows != 25392 {
		t.Errorf("ingest split = %d profiles, %d flows; want 28080 and 25392", profiles, flows)
	}
}
