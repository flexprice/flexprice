package scenarios

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/billingmatrix"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

// rejectRaw fails every raw call, so each picked scenario errors at its first request.
func rejectRaw(d *testDeps) {
	d.fc.RawOps.DoFn = func(string, string, any, any) error { return errors.New("api unavailable") }
}

func TestBillingMatrix_Registers(t *testing.T) {
	cat := scenario.NewCatalog()
	registerBillingMatrix(cat)
	for _, f := range billingmatrix.Families {
		name := "billing-matrix-" + string(f)
		info, ok := cat.Scenario(name)
		if !ok || info.Kind != e2eprobe.KindFlow || len(info.Traits) != 0 || info.Every != f.Interval() {
			t.Errorf("%s: registered=%v info=%+v", name, ok, info)
		}
		s, err := cat.Build(name, scenario.Deps{})
		if err != nil || s.Name() != name || s.Kind() != e2eprobe.KindFlow || len(s.Requires()) != 0 {
			t.Errorf("%s: built %+v (%v)", name, s, err)
		}
	}

	// The suite entry expands to one tenant-level binding per family.
	s := suite.Default()
	cfg, ok := s.ScenarioConfig("billing-matrix")
	if !ok {
		t.Fatal("default suite has no billing-matrix entry")
	}
	cfg.Enabled = nil
	s.Scenarios = []suite.Scenario{cfg}
	bindings, err := s.Bindings(cat)
	if err != nil || len(bindings) != len(billingmatrix.Families) {
		t.Fatalf("bindings = %+v (%v)", bindings, err)
	}
	for _, b := range bindings {
		if b.Persona != "" || b.Every != 15*time.Minute || b.Kind != e2eprobe.KindFlow {
			t.Errorf("binding = %+v", b)
		}
	}
}

func TestBillingMatrix_ReportsFailedScenariosAsMismatches(t *testing.T) {
	d := newTestDeps(t)
	rejectRaw(d)
	r := run(t, d, newBillingMatrix(billingmatrix.FamilyOpening), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "billing_matrix_opening" || len(r.Mismatches) != 3 {
		t.Fatalf("result = %+v", r)
	}
	failed := strings.Split(r.Attrs["failed_scenarios"], ",")
	if r.Attrs["failed_scenarios"] == "" || len(failed) != 3 {
		t.Fatalf("failed_scenarios = %q", r.Attrs["failed_scenarios"])
	}
	for i, m := range r.Mismatches {
		if !strings.HasPrefix(m, failed[i]+": ") || !strings.Contains(m, "api unavailable") {
			t.Errorf("mismatch %d = %q, want one line for %s", i, m, failed[i])
		}
	}
	if r.Attrs["detail"] == "" || len(r.Attrs["detail"]) > 2000 {
		t.Errorf("detail = %q", r.Attrs["detail"])
	}
	if len(d.fc.RawOps.Calls) == 0 {
		t.Error("scenarios never reached the API")
	}
}

func TestBillingMatrix_RegistersSweeperOnce(t *testing.T) {
	d := newTestDeps(t)
	rejectRaw(d)
	sweepLists := func() int {
		d.fc.RawOps.Calls = nil
		d.World.Sweep(context.Background(), d.now)
		n := 0
		for _, call := range d.fc.RawOps.Calls {
			if call == "POST /customers/search" {
				n++
			}
		}
		return n
	}
	if n := sweepLists(); n != 0 {
		t.Fatalf("a sweep before the first run listed billing-matrix customers %d times", n)
	}
	s := newBillingMatrix(billingmatrix.FamilyValidation)
	run(t, d, s, "")
	run(t, d, s, "")
	if n := sweepLists(); n != 1 {
		t.Fatalf("the sweep listed billing-matrix customers %d times, want once for one sweeper: %v", n, d.fc.RawOps.Calls)
	}
}

func TestBillingMatrix_ReadsEntryConfig(t *testing.T) {
	tests := []struct {
		name   string
		perRun int
		want   int
	}{
		{"per_run from the entry", 1, 1},
		{"default when per_run is omitted", 0, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			rejectRaw(d)
			for i := range d.Suite.Scenarios {
				if d.Suite.Scenarios[i].Name == "billing-matrix" {
					d.Suite.Scenarios[i].PerRun = tc.perRun
				}
			}
			r := run(t, d, newBillingMatrix(billingmatrix.FamilyOpening), "")
			if r.Status != e2eprobe.StatusFail || len(r.Mismatches) != tc.want {
				t.Fatalf("result = %+v", r)
			}
			if got := len(strings.Split(r.Attrs["failed_scenarios"], ",")); got != tc.want {
				t.Errorf("failed_scenarios = %q", r.Attrs["failed_scenarios"])
			}
		})
	}
}
