package scenarios

import (
	"strconv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// fastWalletDebit shortens the polls so a timed-out wait costs milliseconds.
func fastWalletDebit(d *testDeps) *walletDebit {
	s := newWalletDebit(d.Deps)
	s.pollEvery = 2 * time.Millisecond
	s.refillWait = 20 * time.Millisecond
	s.debitWait = 20 * time.Millisecond
	return s
}

// debitAfterIngest makes GetBalance report the stored balance less amount once
// any event was ingested, standing in for the usage pipeline.
func debitAfterIngest(d *testDeps, amount string) {
	d.fc.Wallet.Balance = func(id string) (*dtos.GetWalletBalanceResponse, error) {
		w := d.fc.Wallet.Items[id]
		bal := *w.RealTimeBalance
		if len(d.fc.Event.Ingested) > 0 {
			bal = decimal.RequireFromString(bal).Sub(decimal.RequireFromString(amount)).StringFixed(2)
		}
		return &dtos.GetWalletBalanceResponse{WalletBalanceResponse: &types.WalletBalanceResponse{ID: w.ID, RealTimeBalance: &bal}}, nil
	}
}

func setWalletBalance(d *testDeps, walletID, balance string) {
	w := d.fc.Wallet.Items[walletID]
	w.Balance, w.RealTimeBalance = probetest.Ptr(balance), probetest.Ptr(balance)
	d.fc.Wallet.Items[walletID] = w
}

func TestWalletDebit_RefillsBelowFloor(t *testing.T) {
	d := newTestDeps(t)
	wallet, _ := d.World.Persona("wallet-a")
	setWalletBalance(d, wallet.WalletID(), "15.00")
	debitAfterIngest(d, "5.00")
	topUps := len(d.fc.Wallet.TopUps)
	r := run(t, d, fastWalletDebit(d), "wallet-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Wallet.TopUps) != topUps+1 || d.fc.Wallet.TopUps[topUps] != (probetest.TopUp{WalletID: wallet.WalletID(), Amount: "100.00"}) {
		t.Fatalf("top-ups = %+v", d.fc.Wallet.TopUps)
	}
	if r.Attrs["balance_before"] != "115.00" || r.Attrs["balance_after"] != "110.00" || r.Attrs["refill"] != "100.00" {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestWalletDebit_PassesOnExactDebit(t *testing.T) {
	d := newTestDeps(t)
	debitAfterIngest(d, "5.00")
	topUps := len(d.fc.Wallet.TopUps)
	r := run(t, d, fastWalletDebit(d), "wallet-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Wallet.TopUps) != topUps {
		t.Errorf("a wallet at 100.00 must not be refilled: %+v", d.fc.Wallet.TopUps)
	}
	wallet, _ := d.World.Persona("wallet-a")
	if r.Attrs["wallet_id"] != wallet.WalletID() || r.Attrs["balance_before"] != "100.00" || r.Attrs["balance_after"] != "95.00" {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestWalletDebit_MismatchOnWrongDelta(t *testing.T) {
	d := newTestDeps(t)
	debitAfterIngest(d, "4.00")
	r := run(t, d, fastWalletDebit(d), "wallet-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_debit" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(r.Mismatches) != 1 || r.Mismatches[0] != "usage debit: want 5.00, got 4.00" {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestWalletDebit_UsesNoEntitlementMeter(t *testing.T) {
	d := newTestDeps(t)
	debitAfterIngest(d, "5.00")
	run(t, d, fastWalletDebit(d), "wallet-a")
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Ingested) != 500 {
		t.Fatalf("batches = %d, events = %d", len(d.fc.Event.Batches), len(d.fc.Event.Ingested))
	}
	wallet, _ := d.World.Persona("wallet-a")
	prefix := wallet.ExternalID() + "-wallet-debit-" + strconv.FormatInt(testNow.Unix(), 10) + "-"
	for i, ev := range d.fc.Event.Ingested {
		if ev.EventName != "e2eprobe_sum_commit" || ev.ExternalCustomerID != wallet.ExternalID() || ev.Properties["amount"] != "1" {
			t.Fatalf("event %d = %+v", i, ev)
		}
		if ev.EventID == nil || *ev.EventID != prefix+strconv.Itoa(i) {
			t.Fatalf("event %d id = %v, want %s%d", i, ev.EventID, prefix, i)
		}
		if ev.Timestamp == nil || *ev.Timestamp != testNow.Format(time.RFC3339Nano) {
			t.Fatalf("event %d timestamp = %v, want the scenario clock", i, ev.Timestamp)
		}
		if ev.Properties["e2eprobe"] != "true" || ev.Properties["e2eprobe_run_id"] != "run-test" {
			t.Fatalf("event %d properties = %v", i, ev.Properties)
		}
	}
}

func TestWalletDebit_InfraErrorIsFail(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Wallet.BalanceErr = probetest.ServerError(503)
	r := run(t, d, fastWalletDebit(d), "wallet-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "balance" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Event.Ingested) != 0 {
		t.Error("no usage must be driven when the balance cannot be read")
	}
}

func TestWalletDebit_SkipsPersonaWithoutWallet(t *testing.T) {
	d := newTestDeps(t)
	r := run(t, d, fastWalletDebit(d), "baseline-a")
	if r.Status != e2eprobe.StatusSkip || r.Step != "wallet" {
		t.Fatalf("result = %+v", r)
	}
}

func TestRegisterWallet_RegistersBothScenarios(t *testing.T) {
	cat := scenario.NewCatalog()
	registerWallet(cat)
	for name, every := range map[string]time.Duration{"wallet-debit": 30 * time.Minute, "low-balance-alert": 10 * time.Minute} {
		info, ok := cat.Scenario(name)
		if !ok || info.Every != every || info.Kind != e2eprobe.KindProbe {
			t.Errorf("%s = %+v (registered %v)", name, info, ok)
		}
	}
}
