package scenarios

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestPreflight_PassesWhenConnectionsAndSettingsMatch(t *testing.T) {
	d := newTestDeps(t)
	d.fc.TenantOps.Connections = []e2eprobe.Connection{{ID: "c1", ProviderType: "stripe"}}
	d.Suite.Tenant.Settings = map[string]map[string]any{"wallet_balance_alert_config": {"enabled": true}}
	d.fc.TenantOps.Settings["wallet_balance_alert_config"] = map[string]any{"enabled": true, "extra": "ignored"}
	r := run(t, d, newPreflight(d.Deps), "")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
}

func TestPreflight_MismatchNamesMissingConnectionAndSetting(t *testing.T) {
	d := newTestDeps(t)
	d.Suite.Tenant.Settings = map[string]map[string]any{"invoice_config": {"auto_finalize": true}}
	d.fc.TenantOps.Settings["invoice_config"] = map[string]any{"auto_finalize": false}
	r := run(t, d, newPreflight(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "preflight" || len(r.Mismatches) != 2 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(r.Mismatches[0], "connection stripe") || !strings.Contains(r.Mismatches[1], "invoice_config.auto_finalize") {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestPreflight_ReportsPersonaGapsAsAttrs(t *testing.T) {
	d := newTestDeps(t)
	d.fc.TenantOps.Connections = []e2eprobe.Connection{{ID: "c1", ProviderType: "stripe"}}
	committed, _ := d.World.Persona("committed")
	sub := d.fc.Subscription.Items[committed.SubscriptionID()]
	sub.CommitmentAmount = nil
	d.fc.Subscription.Items[committed.SubscriptionID()] = sub
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := run(t, d, newPreflight(d.Deps), "")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("gaps are reported, not failed: %+v", r)
	}
	if !strings.Contains(r.Attrs["gap.committed.commitment"], "predates") {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestPreflight_InfraErrorIsAFailure(t *testing.T) {
	d := newTestDeps(t)
	d.fc.TenantOps.ConnectionsErr = probetest.ServerError(503)
	r := run(t, d, newPreflight(d.Deps), "")
	if r.Status != e2eprobe.StatusFail || r.Step != "connections" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	_ = types.StatusPublished
}
