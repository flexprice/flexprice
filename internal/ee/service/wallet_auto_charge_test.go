package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func autoTopupWallet(cooldown *types.Duration) *wallet.Wallet {
	return &wallet.Wallet{
		ID:         "wallet_1",
		CustomerID: "cust_1",
		AutoTopup: &types.AutoTopup{
			Enabled:   lo.ToPtr(true),
			Threshold: lo.ToPtr(decimal.NewFromInt(10)),
			Amount:    lo.ToPtr(decimal.NewFromInt(20)),
			Invoicing: lo.ToPtr(true),
			Cooldown:  cooldown,
		},
	}
}

func TestWithinDefaultAutoChargeCooldown(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	recent := &wallet.Transaction{BaseModel: types.BaseModel{CreatedAt: now.Add(-time.Minute)}}
	old := &wallet.Transaction{BaseModel: types.BaseModel{CreatedAt: now.Add(-2 * time.Hour)}}

	assert.True(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), recent, now))
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), old, now))
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), nil, now))

	configured := &types.Duration{Value: 5, Unit: types.DurationUnitMinute}
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(configured), recent, now),
		"a wallet with its own cooloff is governed by that, not the default")
}

func TestAutoTopupCheckoutFallsBackToInvoice(t *testing.T) {
	s := &walletService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger()}}
	ctx := context.Background()

	require.Nil(t, s.autoTopupCheckout(ctx, autoTopupWallet(nil), false),
		"direct credits are not paid for, so there is nothing to charge")
}
