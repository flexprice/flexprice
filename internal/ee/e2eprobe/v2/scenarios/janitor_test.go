package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
)

func TestJanitor_ReportsLegacyAndFailures(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Customer.Add("e2eprobe-cust-persistent-9", "cust_p9", map[string]string{"e2eprobe_cohort": world.PersistentCohort}, testNow.Add(-24*time.Hour))
	d.fc.Customer.Add("e2eprobe-eph-stuck-1", "cust_stuck", nil, testNow.Add(-3*time.Hour))
	d.fc.Customer.DeleteErr = probetest.Conflict()
	d.now = testNow.Add(2 * time.Hour)
	r := run(t, d, newJanitor(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "sweep" || r.Infra || len(r.Mismatches) != 1 || !strings.Contains(r.Mismatches[0], "e2eprobe-eph-stuck-1") {
		t.Fatalf("a refused delete is a mismatch: %+v", r)
	}
	if r.Attrs["legacy_fixtures"] != "e2eprobe-cust-persistent-9" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	d.fc.Customer.DeleteErr = nil
	r = run(t, d, newJanitor(d.Deps), "")
	if r.Status != e2eprobe.StatusPass || r.Attrs["orphans"] != "1" {
		t.Fatalf("second sweep = %+v", r)
	}
}

func TestJanitor_SweepDuringAnOutageIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Customer.QueryErr = probetest.ServerError(503)
	r := run(t, d, newJanitor(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "sweep" || !r.Infra {
		t.Fatalf("a 503 while sweeping belongs to the infra rollup: %+v", r)
	}
}
