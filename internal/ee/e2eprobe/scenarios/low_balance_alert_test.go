package scenarios

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// fastLowBalanceAlert shortens the webhook wait so a miss costs milliseconds.
func fastLowBalanceAlert(d *testDeps) *lowBalanceAlert {
	s := newLowBalanceAlert(d.Deps)
	s.webhookWait = 20 * time.Millisecond
	return s
}

func TestLowBalanceAlert_PassesWhenWebhookArrives(t *testing.T) {
	d := newTestDeps(t)
	canary, _ := d.World.Persona("alert-canary")
	d.Inbox.Record(inbox.Event{ReceivedAt: d.now.Add(time.Second), EventType: "wallet.ongoing_balance.dropped", WalletID: canary.WalletID()})
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Ingested) != 1 {
		t.Fatalf("events = %+v", d.fc.Event.Ingested)
	}
	ev := d.fc.Event.Ingested[0]
	// A $30 canary needs 3200 units to cross critical, below the 3500 minimum.
	if ev.EventName != "e2eprobe_sum_commit" || ev.ExternalCustomerID != canary.ExternalID() || ev.Properties["amount"] != "3500" || ev.Properties["e2eprobe_role"] != "alert-canary" {
		t.Errorf("event = %+v", ev)
	}
	if ev.EventID == nil || *ev.EventID != canary.ExternalID()+"-alert-"+strconv.FormatInt(testNow.Unix(), 10) {
		t.Errorf("event id = %v", ev.EventID)
	}
	if ev.Timestamp == nil || *ev.Timestamp != testNow.Format(time.RFC3339Nano) {
		t.Errorf("event timestamp = %v, want the scenario clock", ev.Timestamp)
	}
	if r.Attrs["leg"] != "drop" || r.Attrs["webhook_at"] == "" || r.Attrs["wallet_id"] != canary.WalletID() || r.Attrs["drive_units"] != "3500" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	if len(d.fc.Alert.Queries) != 0 {
		t.Errorf("a delivered webhook needs no alert log lookup, got %d queries", len(d.fc.Alert.Queries))
	}
}

func TestLowBalanceAlert_AttributesMissingWebhookToDeliveryWhenAlertLogged(t *testing.T) {
	d := newTestDeps(t)
	canary, _ := d.World.Persona("alert-canary")
	entity := types.AlertEntityTypeWallet
	d.fc.Alert.Logs = []types.AlertLogResponse{{ID: probetest.Ptr("al_1"), EntityType: &entity, EntityID: probetest.Ptr(canary.WalletID()), CreatedAt: probetest.Ptr(d.now.Add(time.Second))}}
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusFail || r.Step != "webhook_not_delivered" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Alert.Queries) != 1 {
		t.Fatalf("queries = %+v", d.fc.Alert.Queries)
	}
	q := d.fc.Alert.Queries[0]
	if q.EntityType == nil || *q.EntityType != types.AlertEntityTypeWallet || q.EntityID == nil || *q.EntityID != canary.WalletID() || q.StartTime == nil || !q.StartTime.Equal(testNow) {
		t.Errorf("query = %+v", q)
	}
}

func TestLowBalanceAlert_AttributesToAlertPipelineWhenNoLog(t *testing.T) {
	d := newTestDeps(t)
	canary, _ := d.World.Persona("alert-canary")
	// A delivery from an earlier cycle must not satisfy this run's wait.
	d.Inbox.Record(inbox.Event{ReceivedAt: d.now.Add(-time.Minute), EventType: "wallet.ongoing_balance.dropped", WalletID: canary.WalletID()})
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusFail || r.Step != "alert_not_fired" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(r.Reason, "no low-balance alert was raised within 20ms") {
		t.Errorf("reason = %q", r.Reason)
	}
}

func TestLowBalanceAlert_RecoversWhenInAlarm(t *testing.T) {
	d := newTestDeps(t)
	canary, _ := d.World.Persona("alert-canary")
	d.fc.Wallet.AlertStates[canary.WalletID()] = types.AlertStateInAlarm
	topUps := len(d.fc.Wallet.TopUps)
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusPass || r.Attrs["leg"] != "recover" || r.Attrs["alert_state"] != "in_alarm" {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Wallet.TopUps) != topUps+1 || d.fc.Wallet.TopUps[topUps] != (probetest.TopUp{WalletID: canary.WalletID(), Amount: "30.00"}) {
		t.Errorf("top-ups = %+v", d.fc.Wallet.TopUps)
	}
	if len(d.fc.Event.Ingested) != 0 {
		t.Error("the recovery leg must not drive usage")
	}
}

func TestLowBalanceAlert_SkipsOnThresholdGap(t *testing.T) {
	d := newTestDeps(t)
	canary, _ := d.World.Persona("alert-canary")
	w := d.fc.Wallet.Items[canary.WalletID()]
	w.AlertSettings = nil
	d.fc.Wallet.Items[canary.WalletID()] = w
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusSkip || r.Step != "alerts" || !strings.Contains(r.Reason, "thresholds") {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Ingested) != 0 {
		t.Error("a persona with an alerts gap must not be driven")
	}
}

func TestLowBalanceAlert_InfraErrorIsFail(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Alert.Err = probetest.ServerError(503)
	r := run(t, d, fastLowBalanceAlert(d), "alert-canary")
	if r.Status != e2eprobe.StatusFail || r.Step != "alert_logs" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestComputeDriveUnits(t *testing.T) {
	tests := []struct {
		balance string
		want    int
	}{
		{"", 3500},
		{"abc", 3500},
		{"0", 3500},
		{"-5.00", 3500},
		{"0.001", 3500},
		{"30.00", 3500}, // 3000 + 200 is under the minimum
		{"33.00", 3500}, // exactly the minimum
		{"33.01", 3501}, // ceil(3301) + 200
		{" 657.70 ", 65970},
		{"99998.00", 10_000_000}, // exactly the cap
		{"1000000", 10_000_000},  // capped
	}
	for _, tc := range tests {
		if got := computeDriveUnits(tc.balance); got != tc.want {
			t.Errorf("computeDriveUnits(%q) = %d, want %d", tc.balance, got, tc.want)
		}
	}
}
