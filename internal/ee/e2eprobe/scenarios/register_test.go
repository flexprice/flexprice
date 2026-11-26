package scenarios

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
)

func TestRegister_DeclaresIngestPerRun(t *testing.T) {
	cat := scenario.NewCatalog()
	Register(cat)
	want := map[string]int{"lifecycle": 3, "tax-preview": 100, "commitment-preview": 100, "entitlement-enforcement": 150, "grant-inheritance": 200, "wallet-debit": 500, "low-balance-alert": 1, "usage": 0, "coupon-preview": 0}
	for name, n := range want {
		info, ok := cat.Scenario(name)
		if !ok || info.EventsPerRun != n {
			t.Errorf("%s events/run = %d (ok=%v), want %d", name, info.EventsPerRun, ok, n)
		}
	}
}

func TestRegister_EveryScenarioSaysWhatItChecksAndCovers(t *testing.T) {
	cat := scenario.NewCatalog()
	Register(cat)
	for _, name := range cat.Names() {
		info, _ := cat.Scenario(name)
		if info.Summary == "" {
			t.Errorf("%s has no summary; describe cannot say what it checks", name)
		}
		if len(info.Areas) == 0 {
			t.Errorf("%s names no product area; describe cannot show what it covers or what is left uncovered", name)
		}
	}
}
