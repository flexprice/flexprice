package checks

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
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

func (p *WalletBalanceProbe) Name() string         { return "wallet-balance-probe" }
func (p *WalletBalanceProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *WalletBalanceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customer := seeds.PreFundedCustomerIDs[int(idx)%len(seeds.PreFundedCustomerIDs)]
	// Query all wallets and filter client-side by customer ID.
	// WalletFilter has no customer filter field; the synthetic tenant has a
	// bounded wallet count so a full scan is acceptable.
	resp, err := p.client.Wallets().Query(ctx, types.WalletFilter{})
	if err != nil {
		return fmt.Errorf("wallet query for %s: %w", customer, err)
	}
	walletIDs := extractWalletIDsForCustomer(resp, customer)
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

// extractWalletIDs reads all wallet IDs from the SDK QueryWalletResponse
// regardless of customer. Used by wallet_debit_verification which already
// narrows by customer via WalletIds.
// Returns nil if the response has no items (probe soft no-ops).
func extractWalletIDs(resp interface{}) []string {
	return extractWalletIDsForCustomer(resp, "")
}

// extractWalletIDsForCustomer reads wallet IDs from the SDK QueryWalletResponse,
// filtering to wallets whose CustomerID matches customerID. If customerID is
// empty, all wallet IDs in the response are returned.
func extractWalletIDsForCustomer(resp interface{}, customerID string) []string {
	r, ok := resp.(*sdkdtos.QueryWalletResponse)
	if !ok || r == nil {
		return nil
	}
	inner := r.GetListResponseDtoWalletResponse()
	if inner == nil {
		return nil
	}
	items := inner.GetItems()
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, w := range items {
		if w.ID == nil {
			continue
		}
		if customerID != "" && (w.CustomerID == nil || *w.CustomerID != customerID) {
			continue
		}
		ids = append(ids, *w.ID)
	}
	return ids
}
