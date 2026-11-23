//go:build e2eprobe_integration

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	checks_pkg "github.com/flexprice/flexprice/internal/ee/e2eprobe/checks"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/shopspring/decimal"
)

// Run against the local backend with a key for a dedicated test tenant.
//
//	E2EPROBE_API_HOST  (e.g. http://localhost:8080/v1)
//	E2EPROBE_API_KEY   (a key in the local e2eprobe tenant)
//	go test -tags e2eprobe_integration ./cmd/e2eprobe/
func TestE2EProbe_LocalSmoke(t *testing.T) {
	if os.Getenv("E2EPROBE_API_HOST") == "" || os.Getenv("E2EPROBE_API_KEY") == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must both be set; skipping integration smoke")
	}
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	lg, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	client := e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey)
	reg := e2eprobe.NewRegistry()
	runID := fmt.Sprintf("local-smoke-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seed := checks_pkg.NewSeedEnsure(client, reg, runID, lg)
	if err := seed.Run(ctx); err != nil {
		t.Fatalf("%s: %v", seed.Name(), err)
	}
	t.Log("Probe fixtures provisioned")

	seeds := reg.Seeds()
	if len(seeds.PreFundedCustomerIDs) == 0 {
		t.Fatal("seed did not produce a funded customer")
	}
	seeds.PreFundedCustomerIDs = seeds.PreFundedCustomerIDs[:1]
	reg.LoadSeeds(seeds)
	customer, err := client.Customers().GetByExternalID(ctx, seeds.PreFundedCustomerIDs[0])
	if err != nil || customer == nil || customer.CustomerResponse == nil || customer.CustomerResponse.ID == nil {
		t.Fatalf("resolve funded customer: %v", err)
	}
	wallets, err := client.Wallets().GetWalletsByCustomerID(ctx, *customer.CustomerResponse.ID)
	if err != nil || wallets == nil || len(wallets.WalletResponses) != 1 || wallets.WalletResponses[0].ID == nil {
		t.Fatalf("expected one funded wallet: %v", err)
	}
	walletPath := "/wallets/" + *wallets.WalletResponses[0].ID
	readBalance := func() decimal.Decimal {
		t.Helper()
		var wallet struct {
			CreditBalance *decimal.Decimal `json:"credit_balance"`
		}
		if err := client.Raw().Do(ctx, http.MethodGet, walletPath, nil, &wallet); err != nil || wallet.CreditBalance == nil {
			t.Fatalf("read wallet credit balance: %v", err)
		}
		return *wallet.CreditBalance
	}

	before := readBalance()
	walletProbe := checks_pkg.NewWalletDebitVerification(client, reg, runID, checks_pkg.WalletDebitOpts{})
	if err := walletProbe.Run(ctx); err != nil {
		t.Fatalf("%s: %v", walletProbe.Name(), err)
	}
	after := readBalance()
	if !after.Sub(before).Equal(decimal.NewFromInt(5)) {
		t.Fatalf("wallet top-up: expected +5 credits, got %s", after.Sub(before))
	}
	t.Log("Wallet top-up, raw event ingestion, and usage analytics passed")

	debit := map[string]string{"credits": "1", "transaction_reason": "MANUAL_BALANCE_DEBIT", "idempotency_key": runID}
	if err := client.Raw().Do(ctx, http.MethodPost, walletPath+"/debit", debit, nil); err != nil {
		t.Fatalf("manual debit: %v", err)
	}
	want := after.Sub(decimal.NewFromInt(1))
	if got := readBalance(); !got.Equal(want) {
		t.Fatalf("manual debit: expected %s credits, got %s", want, got)
	}
	var conflict *sdkerrors.ErrorResponse
	err = client.Raw().Do(ctx, http.MethodPost, walletPath+"/debit", debit, nil)
	if !errors.As(err, &conflict) || conflict.HTTPStatusCode == nil || *conflict.HTTPStatusCode != http.StatusConflict {
		t.Fatalf("duplicate debit: expected HTTP 409, got %v", err)
	}
	if got := readBalance(); !got.Equal(want) {
		t.Fatalf("duplicate debit changed the balance: expected %s credits, got %s", want, got)
	}
	t.Log("Manual debit passed and duplicate debit was rejected without changing the balance")
}
