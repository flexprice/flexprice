package checks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestWalletDebitVerification_NoPreFundedCustomers(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	// Empty registry — no pre-funded customers; Run() should be a no-op.
	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{})
	if err := v.Run(context.Background()); err != nil {
		t.Fatalf("Run() with no pre-funded customers: %v", err)
	}
}

func TestWalletDebitVerification_CustomerNotProvisioned(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})
	// c0 not in byExt → GetByExternalID returns 404 → soft-skip (nil error).
	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{EventCount: 1})
	if err := v.Run(context.Background()); err != nil {
		t.Fatalf("Run() with unprovisioned customer: %v", err)
	}
}

// TestWalletDebitVerification_Phase1TopUpHappyPath verifies that a TopUp followed
// by a balance read produces an increased balance and Run() succeeds.
func TestWalletDebitVerification_Phase1TopUpHappyPath(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_p1"
	internalCustID := "internal_p1"
	fc.customers.byExt = map[string]string{"c0": internalCustID}
	fc.wallets.walletsByCustomerID = map[string][]types.WalletResponse{
		internalCustID: {{ID: &walletID, CustomerID: &internalCustID}},
	}
	// Start balance = 10.00; topup = 5.00 → expected ≥ 15.00 after topup.
	fc.wallets.balance = "10.0000"
	fc.wallets.incrementBalanceOnTopUp = true

	// Analytics phase: immediately return a sum ≥ expected (10 × 1.00 = 10.00).
	usage := "10.0000"
	eventName := "e2eprobe_sum"
	fc.events.analyticsItems = []types.UsageAnalyticItem{
		{EventName: &eventName, TotalUsage: &usage},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})

	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		TopUpAmount:           "5.00",
		EventCount:            10,
		EventAmount:           "1.00",
		AnalyticsPollInterval: 10 * time.Millisecond,
		AnalyticsPollTimeout:  500 * time.Millisecond,
	})

	if err := v.Run(context.Background()); err != nil {
		t.Fatalf("Run() phase1+phase2 happy path: %v", err)
	}

	// Assert TopUp was called once.
	fc.wallets.mu.Lock()
	defer fc.wallets.mu.Unlock()
	if len(fc.wallets.topUpCalls) != 1 {
		t.Errorf("expected 1 TopUp call, got %d", len(fc.wallets.topUpCalls))
	}

	// Assert events were ingested.
	fc.events.mu.Lock()
	defer fc.events.mu.Unlock()
	if len(fc.events.ingested) != 10 {
		t.Errorf("expected 10 events ingested, got %d", len(fc.events.ingested))
	}
}

// TestWalletDebitVerification_Phase1TopUpFailure verifies that a TopUp API error
// propagates with the right attributes.
func TestWalletDebitVerification_Phase1TopUpFailure(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_fail"
	internalCustID := "internal_fail"
	fc.customers.byExt = map[string]string{"c0": internalCustID}
	fc.wallets.walletsByCustomerID = map[string][]types.WalletResponse{
		internalCustID: {{ID: &walletID, CustomerID: &internalCustID}},
	}
	fc.wallets.balance = "10.0000"
	fc.wallets.topUpErr = errors.New("wallet service unavailable")

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})

	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		TopUpAmount:  "5.00",
		EventCount:   5,
		EventAmount:  "1.00",
		AnalyticsPollTimeout: 50 * time.Millisecond,
	})
	err := v.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when TopUp fails")
	}
	// Verify attributes are attached.
	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["wallet_id"] != walletID {
		t.Errorf("expected wallet_id=%s, got %q", walletID, attrs["wallet_id"])
	}
	if attrs["external_customer_id"] != "c0" {
		t.Errorf("expected external_customer_id=c0, got %q", attrs["external_customer_id"])
	}
}

// TestWalletDebitVerification_Phase2AnalyticsTimeout verifies that a polling
// timeout returns a CheckError with the customer ID attribute.
func TestWalletDebitVerification_Phase2AnalyticsTimeout(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_ana"
	internalCustID := "internal_ana"
	fc.customers.byExt = map[string]string{"c0": internalCustID}
	fc.wallets.walletsByCustomerID = map[string][]types.WalletResponse{
		internalCustID: {{ID: &walletID, CustomerID: &internalCustID}},
	}
	fc.wallets.balance = "10.0000"
	fc.wallets.incrementBalanceOnTopUp = true
	// analyticsItems is empty → sum is always 0, so polling will time out.

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})

	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		TopUpAmount:           "5.00",
		EventCount:            3,
		EventAmount:           "1.00",
		AnalyticsPollInterval: 10 * time.Millisecond,
		AnalyticsPollTimeout:  30 * time.Millisecond,
	})
	err := v.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when analytics polling times out")
	}
	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["external_customer_id"] != "c0" {
		t.Errorf("expected external_customer_id=c0, got %q", attrs["external_customer_id"])
	}
}

// TestWalletDebitVerification_Phase2IngestDropDetected verifies that when
// fewer events landed in the raw events table than were ingested, the probe
// alerts with an "ingest dropped events" error carrying the landed count.
// This is the exact failure mode caught in production where 4 of 10 events
// vanished between Events.Ingest 2xx and the feature_usage aggregation table.
func TestWalletDebitVerification_Phase2IngestDropDetected(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_drop"
	internalCustID := "internal_drop"
	fc.customers.byExt = map[string]string{"c0": internalCustID}
	fc.wallets.walletsByCustomerID = map[string][]types.WalletResponse{
		internalCustID: {{ID: &walletID, CustomerID: &internalCustID}},
	}
	fc.wallets.balance = "10.0000"
	fc.wallets.incrementBalanceOnTopUp = true

	// Force ListRaw to return fewer events than were ingested (6 of 10).
	// The fake doesn't echo back ingested events when listRawItems is set, so
	// we pre-seed it to simulate a partial ingest landing.
	eventName := "e2eprobe_sum"
	fc.events.listRawItems = []types.Event{
		{ID: strPtr("e1"), EventName: &eventName},
		{ID: strPtr("e2"), EventName: &eventName},
		{ID: strPtr("e3"), EventName: &eventName},
		{ID: strPtr("e4"), EventName: &eventName},
		{ID: strPtr("e5"), EventName: &eventName},
		{ID: strPtr("e6"), EventName: &eventName},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})

	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		TopUpAmount:           "5.00",
		EventCount:            10,
		EventAmount:           "1.00",
		AnalyticsPollInterval: 10 * time.Millisecond,
		AnalyticsPollTimeout:  50 * time.Millisecond,
		LandedPollInterval:    5 * time.Millisecond,
		LandedPollTimeout:     30 * time.Millisecond,
	})
	err := v.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when only 6 of 10 events landed in raw events table")
	}
	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["landed_count"] != "6" {
		t.Errorf("expected landed_count=6, got %q", attrs["landed_count"])
	}
	if attrs["expected_count"] != "10" {
		t.Errorf("expected expected_count=10, got %q", attrs["expected_count"])
	}
	if attrs["debit_batch"] == "" {
		t.Error("expected debit_batch attribute to be set")
	}
}

// TestWalletDebitVerification_Phase2AnalyticsSuccess verifies that when the
// analytics response contains items with sufficient TotalUsage, Run() succeeds.
func TestWalletDebitVerification_Phase2AnalyticsSuccess(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_ana2"
	internalCustID := "internal_ana2"
	fc.customers.byExt = map[string]string{"c0": internalCustID}
	fc.wallets.walletsByCustomerID = map[string][]types.WalletResponse{
		internalCustID: {{ID: &walletID, CustomerID: &internalCustID}},
	}
	fc.wallets.balance = "10.0000"
	fc.wallets.incrementBalanceOnTopUp = true

	// 5 events × 1.00 = 5.00 expected; return 5.0 in analytics immediately.
	usage := "5.0000"
	eventName := "e2eprobe_sum"
	fc.events.analyticsItems = []types.UsageAnalyticItem{
		{EventName: &eventName, TotalUsage: &usage},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})

	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		TopUpAmount:           "5.00",
		EventCount:            5,
		EventAmount:           "1.00",
		AnalyticsPollInterval: 10 * time.Millisecond,
		AnalyticsPollTimeout:  500 * time.Millisecond,
	})
	if err := v.Run(context.Background()); err != nil {
		t.Fatalf("Run() with immediate analytics success: %v", err)
	}
}
