package checks

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/flexprice/flexprice/internal/synthetic"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type WalletBalanceProbe struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
	cursor int64
}

func NewWalletBalanceProbe(c synthetic.Client, r synthetic.Registry, runID string) *WalletBalanceProbe {
	return &WalletBalanceProbe{client: c, reg: r, runID: runID}
}

func (p *WalletBalanceProbe) Name() string         { return "wallet-balance-probe" }
func (p *WalletBalanceProbe) Kind() synthetic.Kind { return synthetic.KindProbe }

func (p *WalletBalanceProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PreFundedCustomerIDs) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	customer := seeds.PreFundedCustomerIDs[int(idx)%len(seeds.PreFundedCustomerIDs)]
	resp, err := p.client.Wallets().Query(ctx, types.WalletFilter{WalletIds: []string{customer}})
	if err != nil {
		return fmt.Errorf("wallet query for %s: %w", customer, err)
	}
	walletIDs := extractWalletIDs(resp)
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

// extractWalletIDs reads wallet IDs from the SDK QueryWalletResponse.
// Returns nil if the response has no items (probe soft no-ops).
func extractWalletIDs(resp interface{}) []string {
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
		if w.ID != nil {
			ids = append(ids, *w.ID)
		}
	}
	return ids
}
