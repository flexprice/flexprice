package suite

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

func TestValidate(t *testing.T) {
	cat := newFakeCatalog()
	cases := []struct {
		name string
		edit func(s *Suite)
		want string
	}{
		{"version", func(s *Suite) { s.Version = 2 }, "version"},
		{"empty persona name", func(s *Suite) { s.Personas[0].Name = "" }, "name"},
		{"bad persona name", func(s *Suite) { s.Personas[0].Name = "Bad Name" }, "Bad Name"},
		{"duplicate persona", func(s *Suite) { s.Personas[1].Name = s.Personas[0].Name }, "duplicate persona"},
		{"duplicate external id", func(s *Suite) { s.Personas[1].ExternalID = s.Personas[0].ExternalIDOrDefault() }, "duplicate external_id"},
		{"unknown trait", func(s *Suite) { s.Personas[0].Traits = append(s.Personas[0].Traits, "teleport") }, "teleport"},
		{"explicit events trait", func(s *Suite) { s.Personas[0].Traits = append(s.Personas[0].Traits, e2eprobe.TraitEvents) }, "derived"},
		{"unknown profile", func(s *Suite) { s.Personas[0].Events = "firehose" }, "firehose"},
		{"wallet without trait", func(s *Suite) { s.Personas[0].Wallet = &Wallet{Balance: "1"} }, "wallet"},
		{"wallet trait without block", func(s *Suite) { p := mustPersona(s, "wallet-a"); p.Wallet = nil }, "wallet"},
		{"alerts without wallet", func(s *Suite) {
			p := mustPersona(s, "alert-canary")
			p.Traits = []e2eprobe.Trait{e2eprobe.TraitSubscription, e2eprobe.TraitAlerts}
			p.Wallet = nil
		}, "alerts"},
		{"alerts without thresholds", func(s *Suite) { mustPersona(s, "alert-canary").Wallet.Thresholds = nil }, "thresholds"},
		{"commitment without trait", func(s *Suite) { s.Personas[0].Commitment = &Commitment{Amount: "1"} }, "commitment"},
		{"commitment trait without block", func(s *Suite) { mustPersona(s, "committed").Commitment = nil }, "commitment"},
		{"payment without trait", func(s *Suite) { s.Personas[0].Payment = &Payment{Provider: "stripe"} }, "payment"},
		{"bad provider", func(s *Suite) { mustPersona(s, "payer-stripe").Payment.Provider = "paypal" }, "paypal"},
		{"external id prefix", func(s *Suite) { s.Personas[0].ExternalID = "cust-real" }, "e2eprobe-"},
		{"unknown scenario", func(s *Suite) { s.Scenarios = append(s.Scenarios, Scenario{Name: "teleport"}) }, "teleport"},
		{"duplicate scenario", func(s *Suite) { s.Scenarios = append(s.Scenarios, Scenario{Name: "usage"}) }, "duplicate scenario"},
		{"unknown family", func(s *Suite) {
			mustScenario(s, "invoice").Families = append(mustScenario(s, "invoice").Families, "void")
		}, "invoice-void"},
		{"explicit persona unknown", func(s *Suite) { mustScenario(s, "lifecycle").Personas = []string{"ghost"} }, "ghost"},
		{"explicit persona missing trait", func(s *Suite) { mustScenario(s, "tax-preview").Personas = []string{"baseline-a"} }, "tax"},
		{"no persona for trait", func(s *Suite) {
			mustPersona(s, "alert-canary").Traits = []e2eprobe.Trait{e2eprobe.TraitSubscription, e2eprobe.TraitWallet}
		}, "low-balance-alert"},
		{"after_failures", func(s *Suite) { s.Defaults.Alert.AfterFailures = 0 }, "after_failures"},
		{"after_duration", func(s *Suite) { s.Defaults.Alert.AfterDuration = Duration{} }, "after_duration"},
		{"skip_after", func(s *Suite) { s.Defaults.Alert.SkipAfter = Duration{} }, "skip_after"},
		{"concurrency", func(s *Suite) { s.Defaults.Concurrency = 0 }, "concurrency"},
		{"tick", func(s *Suite) { s.Defaults.Tick = Duration{30 * time.Second} }, "tick"},
		{"scenario every", func(s *Suite) { mustScenario(s, "usage").Every = Duration{10 * time.Second} }, "every"},
		{"budget", func(s *Suite) { s.Defaults.Load.MaxEventsPerDay = 1000 }, "53568 events/day exceeds 1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := withPayer(Default())
			tc.edit(s)
			err := Validate(s, cat)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidate_JoinsEveryViolation(t *testing.T) {
	s := withPayer(Default())
	s.Version = 3
	s.Defaults.Concurrency = 0
	err := Validate(s, newFakeCatalog())
	if err == nil || !strings.Contains(err.Error(), "version") || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("expected both violations, got %v", err)
	}
}

func mustPersona(s *Suite, name string) *Persona {
	for i := range s.Personas {
		if s.Personas[i].Name == name {
			return &s.Personas[i]
		}
	}
	panic("no persona " + name)
}

func mustScenario(s *Suite, name string) *Scenario {
	for i := range s.Scenarios {
		if s.Scenarios[i].Name == name {
			return &s.Scenarios[i]
		}
	}
	panic("no scenario " + name)
}
