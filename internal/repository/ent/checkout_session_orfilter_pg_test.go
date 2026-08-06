//go:build pgintegration

package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// The in-memory store mirrors this predicate, but only real Postgres executes the sqljson
// Path/Or the guard actually depends on. Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestCheckoutConfigFilter_ORsBothParamsBlobs
func TestCheckoutConfigFilter_ORsBothParamsBlobs(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_orfilter_test")
	repo := NewCheckoutSessionRepository(client, log)

	// Unique per run: cleanup soft-deletes (status=archived), so fixed ids would collide on
	// the primary key the second time this test is run against the same database.
	runID := types.GenerateUUIDWithPrefix("orfilter")
	subID := "subs_shared_" + runID
	customerID := "cust_" + runID
	base := types.GetDefaultBaseModel(ctx)

	mk := func(id string, action types.CheckoutAction, cfgJSON types.CheckoutConfiguration) *domainCheckout.CheckoutSession {
		return &domainCheckout.CheckoutSession{
			ID:              id,
			EnvironmentID:   types.GetEnvironmentID(ctx),
			CustomerID:      customerID,
			Action:          action,
			CheckoutStatus:  types.CheckoutStatusPending,
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
			Configuration:   domainCheckout.ToJSONBCheckoutConfiguration(cfgJSON),
			ExpiresAt:       time.Now().UTC().Add(time.Hour),
			BaseModel:       base,
		}
	}

	sessions := []*domainCheckout.CheckoutSession{
		mk("cs_modify_"+runID, types.CheckoutActionModifySubscription, types.CheckoutConfiguration{
			ModifySubscriptionParams: &types.ModifySubscriptionParams{SubscriptionID: subID},
		}),
		mk("cs_addaddon_"+runID, types.CheckoutActionAddAddon, types.CheckoutConfiguration{
			AddAddonParams: &types.AddAddonParams{SubscriptionID: subID},
		}),
		mk("cs_other_"+runID, types.CheckoutActionAddAddon, types.CheckoutConfiguration{
			AddAddonParams: &types.AddAddonParams{SubscriptionID: "subs_unrelated_" + runID},
		}),
	}
	for _, s := range sessions {
		require.NoError(t, repo.Create(ctx, s))
	}
	t.Cleanup(func() {
		for _, s := range sessions {
			_ = repo.Delete(ctx, s.ID)
		}
	})

	got, err := repo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		CustomerIDs:   []string{customerID},
		Configuration: &types.CheckoutConfigurationFilter{SubscriptionID: subID},
	})
	require.NoError(t, err)

	ids := lo.Map(got, func(s *domainCheckout.CheckoutSession, _ int) string { return s.ID })
	require.ElementsMatch(t, []string{"cs_modify_" + runID, "cs_addaddon_" + runID}, ids,
		"the JSON-path predicate must OR modify_subscription_params and add_addon_params")
}
