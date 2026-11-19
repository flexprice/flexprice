package scenarios

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
)

func TestProvision_PassesAndFailsWithStep(t *testing.T) {
	d := newTestDeps(t)
	if r := run(t, d, newProvision(d.Deps), ""); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	d.fc.Wallet.CreateErr = probetest.ServerError(500)
	for id := range d.fc.Wallet.Items {
		delete(d.fc.Wallet.Items, id)
	}
	r := run(t, d, newProvision(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "provision" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
