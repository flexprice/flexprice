package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopRevenueFacts satisfies interfaces.RevenueService without touching a DB.
type noopRevenueFacts struct{}

func (noopRevenueFacts) RollupSubscription(context.Context, string) error { return nil }
func (noopRevenueFacts) RollupDirty(context.Context, types.RollupDirtyRequest) (types.RollupDirtyResult, error) {
	return types.RollupDirtyResult{}, nil
}
func (noopRevenueFacts) FinalizeSubscriptionPeriod(context.Context, string) error { return nil }
func (noopRevenueFacts) RevertInvoiceFacts(context.Context, string) error         { return nil }
func (noopRevenueFacts) ReconcileBookedInvoices(context.Context, time.Time) (int, int, int, error) {
	return 0, 0, 0, nil
}
func (noopRevenueFacts) GetRevenueAnalytics(context.Context, *dto.RevenueAnalyticsRequest) (*dto.RevenueAnalyticsResponse, error) {
	return nil, nil
}

// TestAsyncRevenueFactsUpdate_DropsCallerTransaction: three of the four hook
// call sites fire from inside an open transaction. Carrying that transaction
// into a goroutine that outlives the request puts two goroutines on one
// Postgres connection and keeps using it after the commit — the wire protocol
// desynchronizes and the poisoned connection goes back to the pool, so the
// next unrelated request fails instead. The detached work must run on its own
// connection, while keeping the tenant scope it needs.
func TestAsyncRevenueFactsUpdate_DropsCallerTransaction(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = types.SetEnvironmentID(ctx, "env_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	require.NotNil(t, ctx.Value(types.CtxDBTransaction), "the caller is inside a transaction")

	seen := make(chan context.Context, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}

	asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
		func(asyncCtx context.Context, _ interfaces.RevenueService) error {
			seen <- asyncCtx
			return nil
		})

	select {
	case got := <-seen:
		assert.Nil(t, got.Value(types.CtxDBTransaction),
			"detached work must not reuse the request's transaction")
		assert.Equal(t, "tenant_hook", types.GetTenantID(got), "tenant scope survives")
		assert.Equal(t, "env_hook", types.GetEnvironmentID(got), "environment scope survives")
	case <-time.After(5 * time.Second):
		t.Fatal("the detached update never ran")
	}
}

// TestAsyncRevenueFactsUpdate_WaitsForCommit: the update reads the invoice the
// caller's transaction is still writing. Fired immediately it would read from
// another connection, see nothing, and leave the flip to the drift sweep — so
// inside a transaction it queues and runs only once the commit publishes the
// write. A rollback never runs it at all.
func TestAsyncRevenueFactsUpdate_WaitsForCommit(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = types.SetEnvironmentID(ctx, "env_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	ctx = types.WithPostCommitHooks(ctx)

	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}
	fire := func() {
		asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
			func(context.Context, interfaces.RevenueService) error {
				ran <- struct{}{}
				return nil
			})
	}

	fire()
	select {
	case <-ran:
		t.Fatal("the update ran while the transaction was still open")
	case <-time.After(150 * time.Millisecond):
	}

	types.RunPostCommitHooks(ctx)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the update never ran after the commit")
	}

	// Once the transaction has finished, registration is closed: a late
	// caller must run inline rather than queue onto a list nobody drains.
	fire()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("a registration after the commit was silently dropped")
	}
}

// TestAsyncRevenueFactsUpdate_DroppedOnRollback: queued work belongs to the
// transaction that produced it. A rollback means the invoice it would read was
// never written, so the work is discarded rather than run.
func TestAsyncRevenueFactsUpdate_DroppedOnRollback(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	ctx = types.WithPostCommitHooks(ctx)

	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}
	fire := func() {
		asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
			func(context.Context, interfaces.RevenueService) error {
				ran <- struct{}{}
				return nil
			})
	}

	fire()
	types.DiscardPostCommitHooks(ctx)
	select {
	case <-ran:
		t.Fatal("work queued by a rolled-back transaction must not run")
	case <-time.After(150 * time.Millisecond):
	}

	// Registration is closed now, so a later caller runs inline instead of
	// queueing into a list that will never be drained.
	fire()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("a registration after the rollback was silently dropped")
	}
}

// TestAsyncRevenueFactsUpdate_RunsImmediatelyOutsideTransaction: the
// payment-processor path calls this with no transaction open, and must not
// wait for a commit that will never come.
func TestAsyncRevenueFactsUpdate_RunsImmediatelyOutsideTransaction(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}

	asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
		func(context.Context, interfaces.RevenueService) error {
			ran <- struct{}{}
			return nil
		})

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the update never ran")
	}
}

func TestNotifySubscriptionChanged_WritesOnlyWhenRevRecEnabled(t *testing.T) {
	tests := []struct {
		name          string
		setting       map[string]interface{}
		wantContracts int
	}{
		{name: "no setting", wantContracts: 0},
		{name: "disabled", setting: map[string]interface{}{"enabled": false}, wantContracts: 0},
		{name: "enabled", setting: map[string]interface{}{"enabled": true}, wantContracts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			subRepo := testutil.NewInMemorySubscriptionStore()
			sub := &subscription.Subscription{
				ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
				CustomerID:         "cust_1",
				Currency:           "usd",
				SubscriptionStatus: types.SubscriptionStatusActive,
				BaseModel:          types.GetDefaultBaseModel(ctx),
			}
			require.NoError(t, subRepo.Create(ctx, sub))
			lineItemRepo := testutil.NewInMemorySubscriptionLineItemStore()
			subRepo.SetLineItemStore(lineItemRepo)
			require.NoError(t, lineItemRepo.Create(ctx, &subscription.SubscriptionLineItem{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID: sub.ID,
				PriceID:        "price_1",
				// Usage has no period segment, so the gate test needs no billing engine.
				PriceType:     types.PRICE_TYPE_USAGE,
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
				BaseModel:     types.GetDefaultBaseModel(ctx),
			}))

			settingsRepo := testutil.NewInMemorySettingsStore()
			if tt.setting != nil {
				require.NoError(t, settingsRepo.Create(ctx, &settings.Setting{
					ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
					Key:           types.SettingKeyRevenueRecognitionConfig,
					Value:         tt.setting,
					EnvironmentID: types.GetEnvironmentID(ctx),
					BaseModel:     types.GetDefaultBaseModel(ctx),
				}))
			}

			contractRepo := testutil.NewInMemoryContractStore()
			pobRepo := testutil.NewInMemoryPOBStore(contractRepo)
			params := ServiceParams{
				Logger:          logger.NewNoopLogger(),
				SubRepo:         subRepo,
				SettingsRepo:    settingsRepo,
				ContractService: contractsvc.NewContractService(contractRepo),
				POBService: pobsvc.NewPerformanceObligationService(pobRepo, contractRepo, subRepo, testutil.NewInMemoryInvoiceStore(),
					testutil.NewInMemoryTaxAppliedStore(), testutil.NewInMemoryRevenueFactStore()),
			}
			NotifySubscriptionChanged(ctx, params, sub.ID)

			countContracts := func() int {
				count, err := contractRepo.Count(ctx, nil)
				require.NoError(t, err)
				return count
			}
			countPOBs := func() int {
				count, err := pobRepo.Count(ctx, nil)
				require.NoError(t, err)
				return count
			}
			// One line item, so one POB alongside the contract.
			if tt.wantContracts > 0 {
				require.Eventually(t, func() bool { return countContracts() == tt.wantContracts && countPOBs() == tt.wantContracts }, 5*time.Second, 10*time.Millisecond)
				return
			}
			time.Sleep(300 * time.Millisecond)
			require.Equal(t, 0, countContracts())
			require.Equal(t, 0, countPOBs())
		})
	}
}
