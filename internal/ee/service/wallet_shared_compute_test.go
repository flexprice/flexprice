package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

// A caller joining a compute already running for the wallet waits only as long as its own context allows,
// and a caller with time left gets the shared result.
func TestComputeRealtimeBalanceSharedWaitsOnEachCallersContext(t *testing.T) {
	group := &singleflight.Group{}
	s := &walletService{ServiceParams: ServiceParams{WalletBalanceComputes: group}, computeBalanceTimeout: time.Second}
	w := &wallet.Wallet{ID: "wallet_shared"}
	release := make(chan struct{})
	balance := decimal.NewFromInt(42)
	running := group.DoChan(w.ID, func() (any, error) {
		<-release
		return &dto.WalletBalanceResponse{RealTimeBalance: &balance}, nil
	})

	impatient, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.computeRealtimeBalanceShared(impatient, w)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the impatient caller returns on its own deadline")

	patientResp := make(chan *dto.WalletBalanceResponse, 1)
	patientErr := make(chan error, 1)
	go func() {
		resp, err := s.computeRealtimeBalanceShared(context.Background(), w)
		patientResp <- resp
		patientErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	require.NoError(t, <-patientErr)
	require.Equal(t, "42", (<-patientResp).RealTimeBalance.String(), "the impatient caller leaving didn't fail the shared compute")
	<-running
}
