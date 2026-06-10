package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestWalletDebitVerification(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(fc *fakeClient, reg e2eprobe.Registry)
		opts            WalletDebitOpts
		wantErr         bool
		wantEventsCount int
	}{
		{
			name: "no pre-funded customers is a no-op",
			setup: func(_ *fakeClient, _ e2eprobe.Registry) {
				// empty registry
			},
			opts:            WalletDebitOpts{},
			wantErr:         false,
			wantEventsCount: 0,
		},
		{
			name: "empty wallet list exits before ingest",
			setup: func(_ *fakeClient, reg e2eprobe.Registry) {
				reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})
				// fc.wallets.walletItems is nil → extractWalletIDsForCustomer returns nil
			},
			opts: WalletDebitOpts{
				EventCount:   10,
				PollInterval: 10 * time.Millisecond,
				PollTimeout:  50 * time.Millisecond,
			},
			wantErr:         false,
			wantEventsCount: 0,
		},
		{
			name: "with wallet ingests expected event count",
			setup: func(fc *fakeClient, reg e2eprobe.Registry) {
				walletID := "wallet_002"
				customerID := "c0"
				fc.wallets.walletItems = []types.DtoWalletResponse{{ID: &walletID, CustomerID: &customerID}}
				// Large balance so top-up is skipped.
				fc.wallets.balance = "9999.00"
				reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})
			},
			opts: WalletDebitOpts{
				EventCount:   10,
				EventAmount:  "0.01",
				PollInterval: 10 * time.Millisecond,
				PollTimeout:  50 * time.Millisecond,
			},
			// Run will time out waiting for balance drop (fake balance is static).
			wantErr:         true,
			wantEventsCount: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc, reg)

			v := NewWalletDebitVerification(fc, reg, "run-1", tc.opts)
			err := v.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}

			fc.events.mu.Lock()
			defer fc.events.mu.Unlock()
			if len(fc.events.ingested) != tc.wantEventsCount {
				t.Errorf("events ingested = %d, want %d", len(fc.events.ingested), tc.wantEventsCount)
			}
		})
	}
}
