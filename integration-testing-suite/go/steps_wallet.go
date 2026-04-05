package main

import (
	"context"
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runWalletSteps executes Phase 3b: Wallet Operations.
func (r *SanityRunner) runWalletSteps(ctx context.Context) {
	r.setPhase("PHASE 3b: Wallet Operations")
	r.printPhaseHeader(r.phase)

	if !r.require(r.customerID, "Create Customer", "Create Wallet") {
		r.skip("Top-Up Wallet", "depends on wallet creation")
		r.skip("Verify Wallet Balance", "depends on wallet creation")
		return
	}

	// ── Create Wallet ──────────────────────────────────────────────────
	// SDK: client.Wallets.CreateWallet(ctx, types.CreateWalletRequest{...})

	r.run("Create Wallet", "Wallets.CreateWallet", false, func() error {
		req := types.CreateWalletRequest{
			CustomerID:  strPtr(r.customerID),
			Currency:    "USD",
			Description: strPtr("Integration test prepaid wallet"),
			Metadata:    map[string]string{"source": "sanity_test"},
		}

		resp, err := r.client.Wallets.CreateWallet(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("create wallet returned nil response")
		}
		wallet := resp.Wallet
		if wallet == nil || wallet.ID == nil {
			return fmt.Errorf("create wallet returned no body")
		}
		r.walletID = *wallet.ID
		r.lastResult().EntityID = *wallet.ID
		r.lastResult().Details = fmt.Sprintf("wallet_id=%s, currency=%s, customer=%s",
			*wallet.ID, derefStr(wallet.Currency), r.customerID)
		return nil
	})

	// ── Top-Up Wallet ──────────────────────────────────────────────────
	// SDK: client.Wallets.TopUpWallet(ctx, walletID, types.TopUpWalletRequest{...})

	if !r.require(r.walletID, "Create Wallet", "Top-Up Wallet") {
		r.skip("Verify Wallet Balance", "depends on top-up")
		return
	}

	r.run("Top-Up Wallet (500 credits)", "Wallets.TopUpWallet", false, func() error {
		req := types.TopUpWalletRequest{
			Amount:            strPtr("500.00"),
			TransactionReason: types.TransactionReasonPurchasedCreditDirect,
			Description:       strPtr("Integration test top-up"),
		}

		_, err := r.client.Wallets.TopUpWallet(ctx, r.walletID, req)
		if err != nil {
			return err
		}

		r.lastResult().Details = "500 credits added"
		return nil
	})

	// ── Verify Wallet Balance ──────────────────────────────────────────
	// SDK: client.Wallets.GetWalletBalance(ctx, walletID, nil)

	r.run("Verify Wallet Balance", "Wallets.GetWalletBalance", false, func() error {
		resp, err := r.client.Wallets.GetWalletBalance(ctx, r.walletID, nil)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("get wallet balance returned nil response")
		}

		balance := resp.WalletBalanceResponse
		details := "balance retrieved"
		if balance != nil && balance.Balance != nil {
			details = fmt.Sprintf("balance=%s", *balance.Balance)
		}
		if balance != nil && balance.CreditBalance != nil {
			details += fmt.Sprintf(", credit_balance=%s", *balance.CreditBalance)
		}
		if balance != nil && balance.RealTimeCreditBalance != nil {
			details += fmt.Sprintf(", realtime_credit_balance=%s", *balance.RealTimeCreditBalance)
		}
		r.lastResult().Details = details
		return nil
	})
}
