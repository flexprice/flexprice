package testutil

import (
	"context"
	"testing"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// A subscription id lives under modify_subscription_params for quantity changes and under
// add_addon_params for payment-gated addon attach, so CheckoutConfigurationFilter.SubscriptionID
// must OR the two paths. Implementing it as a second sibling AND predicate — the easy mistake —
// would require one session to carry both blobs and would therefore match nothing, silently
// blinding the concurrent guard that both flows depend on.
//
// This mirrors the sql.Or in repository/ent/checkout_session.go, which needs a real database to
// exercise; keeping the two in step is manual.
func TestInMemoryCheckoutSessionStore_List_SubscriptionIDMatchesBothParamsBlobs(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	store := NewInMemoryCheckoutSessionStore()

	const subID = "subs_shared"
	base := types.GetDefaultBaseModel(ctx)

	newSession := func(id string, action types.CheckoutAction, cfg types.CheckoutConfiguration) *domainCheckout.CheckoutSession {
		return &domainCheckout.CheckoutSession{
			ID:              id,
			EnvironmentID:   types.GetEnvironmentID(ctx),
			CustomerID:      "cust_1",
			Action:          action,
			CheckoutStatus:  types.CheckoutStatusPending,
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
			Configuration:   domainCheckout.ToJSONBCheckoutConfiguration(cfg),
			ExpiresAt:       time.Now().UTC().Add(time.Hour),
			BaseModel:       base,
		}
	}

	modifySession := newSession("cs_modify", types.CheckoutActionModifySubscription, types.CheckoutConfiguration{
		ModifySubscriptionParams: &types.ModifySubscriptionParams{
			SubscriptionID: subID,
			LineItemModifications: []types.ModifySubscriptionLineItem{
				{LineItemID: "subs_li_1", Quantity: decimal.NewFromInt(2)},
			},
		},
	})
	addAddonSession := newSession("cs_add_addon", types.CheckoutActionAddAddon, types.CheckoutConfiguration{
		AddAddonParams: &types.AddAddonParams{
			SubscriptionID: subID,
			Addons: []types.AddAddonRef{{
				AssociationID: "addon_assoc_1",
				AddonID:       "addon_1",
				Cadence:       types.AddonCadenceRecurring,
				StartDate:     time.Now().UTC(),
			}},
		},
	})
	otherSubSession := newSession("cs_other_sub", types.CheckoutActionAddAddon, types.CheckoutConfiguration{
		AddAddonParams: &types.AddAddonParams{
			SubscriptionID: "subs_unrelated",
			Addons: []types.AddAddonRef{{
				AssociationID: "addon_assoc_2",
				AddonID:       "addon_2",
				Cadence:       types.AddonCadenceRecurring,
				StartDate:     time.Now().UTC(),
			}},
		},
	})
	walletSession := newSession("cs_wallet", types.CheckoutActionWalletTopup, types.CheckoutConfiguration{
		WalletTopupParams: &types.WalletTopupParams{WalletID: "wallet_1"},
	})

	for _, session := range []*domainCheckout.CheckoutSession{modifySession, addAddonSession, otherSubSession, walletSession} {
		require.NoError(t, store.Create(ctx, session))
	}

	listIDs := func(filter *types.CheckoutSessionFilter) []string {
		sessions, err := store.List(ctx, filter)
		require.NoError(t, err)
		ids := make([]string, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.ID)
		}
		return ids
	}

	t.Run("matches both params blobs", func(t *testing.T) {
		ids := listIDs(&types.CheckoutSessionFilter{
			QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
			Configuration: &types.CheckoutConfigurationFilter{SubscriptionID: subID},
		})
		require.ElementsMatch(t, []string{"cs_modify", "cs_add_addon"}, ids)
	})

	t.Run("action filter narrows within the OR", func(t *testing.T) {
		ids := listIDs(&types.CheckoutSessionFilter{
			QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
			Actions:       []types.CheckoutAction{types.CheckoutActionAddAddon},
			Configuration: &types.CheckoutConfigurationFilter{SubscriptionID: subID},
		})
		require.Equal(t, []string{"cs_add_addon"}, ids)
	})

	t.Run("unrelated subscription and wallet sessions excluded", func(t *testing.T) {
		ids := listIDs(&types.CheckoutSessionFilter{
			QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
			Configuration: &types.CheckoutConfigurationFilter{SubscriptionID: "subs_unrelated"},
		})
		require.Equal(t, []string{"cs_other_sub"}, ids)
	})

	t.Run("wallet id path is unaffected", func(t *testing.T) {
		ids := listIDs(&types.CheckoutSessionFilter{
			QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
			Configuration: &types.CheckoutConfigurationFilter{WalletID: "wallet_1"},
		})
		require.Equal(t, []string{"cs_wallet"}, ids)
	})
}
