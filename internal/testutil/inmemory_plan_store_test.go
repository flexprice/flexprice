package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPlan builds a published plan in the default test tenant/environment.
func newTestPlan(id, name string) *plan.Plan {
	now := time.Now().UTC()
	return &plan.Plan{
		ID:            id,
		Name:          name,
		EnvironmentID: TestEnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

// TestInMemoryPlanStore_List_PlanIDs verifies that the in-memory store
// applies PlanFilter.PlanIDs the same way the real repository does
// (internal/repository/ent/plan.go applyEntityQueryOptions: plan.IDIn(f.PlanIDs...)).
func TestInMemoryPlanStore_List_PlanIDs(t *testing.T) {
	ctx := SetupContext()

	setupStore := func(t *testing.T) *InMemoryPlanStore {
		t.Helper()
		store := NewInMemoryPlanStore()
		require.NoError(t, store.Create(ctx, newTestPlan("plan-1", "Basic")))
		require.NoError(t, store.Create(ctx, newTestPlan("plan-2", "Pro")))
		require.NoError(t, store.Create(ctx, newTestPlan("plan-3", "Enterprise")))
		return store
	}

	testCases := []struct {
		name        string
		filter      *types.PlanFilter
		expectedIDs []string
	}{
		{
			name:        "no_plan_ids_returns_all_plans",
			filter:      types.NewNoLimitPlanFilter(),
			expectedIDs: []string{"plan-1", "plan-2", "plan-3"},
		},
		{
			name: "single_plan_id_returns_only_that_plan",
			filter: &types.PlanFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				PlanIDs:     []string{"plan-2"},
			},
			expectedIDs: []string{"plan-2"},
		},
		{
			name: "multiple_plan_ids_return_matching_plans",
			filter: &types.PlanFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				PlanIDs:     []string{"plan-1", "plan-3"},
			},
			expectedIDs: []string{"plan-1", "plan-3"},
		},
		{
			name: "unknown_plan_id_returns_empty",
			filter: &types.PlanFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				PlanIDs:     []string{"plan-missing"},
			},
			expectedIDs: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := setupStore(t)

			plans, err := store.List(ctx, tc.filter)
			require.NoError(t, err)

			gotIDs := lo.Map(plans, func(p *plan.Plan, _ int) string { return p.ID })
			assert.ElementsMatch(t, tc.expectedIDs, gotIDs)

			count, err := store.Count(ctx, tc.filter)
			require.NoError(t, err)
			assert.Equal(t, len(tc.expectedIDs), count)
		})
	}
}
