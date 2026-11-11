package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/types"
)

func TestInMemoryEntitlementStore_ListSkipsSubscriptionEntitlementGrantsByDefault(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryEntitlementStore()
	feature := &entitlement.Entitlement{ID: "ent_feature", EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: "subs_1", FeatureID: "feat_1"}
	subGrant := &entitlement.Entitlement{ID: "ent_sub", EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: "subs_1", GrantScope: types.EntitlementGrantScopeSubscription}
	for _, e := range []*entitlement.Entitlement{feature, subGrant} {
		if _, err := store.Create(ctx, e); err != nil {
			t.Fatalf("create %s: %v", e.ID, err)
		}
	}

	ids := func(filter *types.EntitlementFilter) []string {
		got, err := store.List(ctx, filter)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := make([]string, 0, len(got))
		for _, e := range got {
			out = append(out, e.ID)
		}
		return out
	}

	if got := ids(types.NewNoLimitEntitlementFilter()); len(got) != 1 || got[0] != "ent_feature" {
		t.Fatalf("default list = %v; want only the feature entitlement", got)
	}
	filter := types.NewNoLimitEntitlementFilter()
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{types.EntitlementGrantScopeSubscription}
	if got := ids(filter); len(got) != 1 || got[0] != "ent_sub" {
		t.Fatalf("subscription-scope list = %v; want only the subscription entitlement grant", got)
	}
}
