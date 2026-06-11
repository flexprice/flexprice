package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

type WalletBalanceProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	cursor int64
}

func NewWalletBalanceProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *WalletBalanceProbe {
	return &WalletBalanceProbe{client: c, reg: r, runID: runID}
}

func (p *WalletBalanceProbe) Name() string        { return "wallet-balance-probe" }
func (p *WalletBalanceProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *WalletBalanceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	extCustID := seeds.PreFundedCustomerIDs[int(idx)%len(seeds.PreFundedCustomerIDs)]

	walletIDs, err := lookupWalletIDsForExternalCustomer(ctx, p.client, extCustID)
	if err != nil {
		return fmt.Errorf("lookup wallets for %s: %w", extCustID, err)
	}
	if len(walletIDs) == 0 {
		return nil
	}
	for _, id := range walletIDs {
		if _, err := p.client.Wallets().GetBalance(ctx, id); err != nil {
			return fmt.Errorf("wallet balance %s: %w", id, err)
		}
	}
	return nil
}

// lookupWalletIDsForExternalCustomer resolves the external customer ID to an
// internal one and returns all wallet IDs associated with that customer via
// the dedicated GetWalletsByCustomerID endpoint. Returns (nil, nil) when the
// customer or their wallets aren't found yet (benign first-run state).
//
// Shared by wallet_balance_probe and wallet_debit_verification — the previous
// implementation used Wallets.Query() with an empty filter and filtered client
// side, but the upstream API returns 500 for unfiltered Query, so the
// dedicated endpoint is the only reliable path.
func lookupWalletIDsForExternalCustomer(ctx context.Context, client e2eprobe.Client, extCustID string) ([]string, error) {
	custResp, err := client.Customers().GetByExternalID(ctx, extCustID)
	if err != nil {
		// 404 → first-run state, seed-ensure hasn't created this customer yet.
		// Treat as "no wallets" so the probe quietly skips. Other errors propagate.
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get customer: %w", err)
	}
	if custResp == nil || custResp.DtoCustomerResponse == nil || custResp.DtoCustomerResponse.ID == nil {
		return nil, nil
	}
	internalCustID := *custResp.DtoCustomerResponse.ID

	walletsResp, err := client.Wallets().GetWalletsByCustomerID(ctx, internalCustID)
	if err != nil {
		return nil, fmt.Errorf("get wallets by customer: %w", err)
	}
	if walletsResp == nil || len(walletsResp.DtoWalletResponses) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(walletsResp.DtoWalletResponses))
	for _, w := range walletsResp.DtoWalletResponses {
		if w.ID != nil {
			ids = append(ids, *w.ID)
		}
	}
	return ids, nil
}
