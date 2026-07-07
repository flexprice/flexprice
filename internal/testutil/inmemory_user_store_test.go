package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestUser builds a published user in the default test tenant.
func newTestUser(id, email string, userType types.UserType) *user.User {
	now := time.Now().UTC()
	return &user.User{
		ID:    id,
		Email: email,
		Type:  userType,
		Roles: []string{},
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

// TestInMemoryUserStore_ListByFilter_UserIDs verifies that the in-memory
// store applies UserFilter.UserIDs the same way the real repository does
// (internal/repository/ent/user.go ListByFilter: entUser.IDIn(filter.UserIDs...)).
func TestInMemoryUserStore_ListByFilter_UserIDs(t *testing.T) {
	ctx := SetupContext()

	setupStore := func(t *testing.T) *InMemoryUserStore {
		t.Helper()
		store := NewInMemoryUserStore()
		require.NoError(t, store.Create(ctx, newTestUser("user-1", "u1@test.com", types.UserTypeUser)))
		require.NoError(t, store.Create(ctx, newTestUser("user-2", "u2@test.com", types.UserTypeUser)))
		require.NoError(t, store.Create(ctx, newTestUser("user-3", "u3@test.com", types.UserTypeServiceAccount)))
		return store
	}

	testCases := []struct {
		name        string
		filter      *types.UserFilter
		expectedIDs []string
	}{
		{
			name:        "no_user_ids_returns_all_tenant_users",
			filter:      &types.UserFilter{},
			expectedIDs: []string{"user-1", "user-2", "user-3"},
		},
		{
			name:        "single_user_id_returns_only_that_user",
			filter:      &types.UserFilter{UserIDs: []string{"user-2"}},
			expectedIDs: []string{"user-2"},
		},
		{
			name:        "multiple_user_ids_return_matching_users",
			filter:      &types.UserFilter{UserIDs: []string{"user-1", "user-3"}},
			expectedIDs: []string{"user-1", "user-3"},
		},
		{
			name:        "unknown_user_id_returns_empty",
			filter:      &types.UserFilter{UserIDs: []string{"user-missing"}},
			expectedIDs: []string{},
		},
		{
			name: "user_ids_combined_with_type_filter",
			filter: &types.UserFilter{
				UserIDs: []string{"user-1", "user-3"},
				Type:    lo.ToPtr(types.UserTypeServiceAccount),
			},
			expectedIDs: []string{"user-3"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := setupStore(t)

			users, total, err := store.ListByFilter(ctx, tc.filter)
			require.NoError(t, err)

			gotIDs := lo.Map(users, func(u *user.User, _ int) string { return u.ID })
			assert.ElementsMatch(t, tc.expectedIDs, gotIDs)
			assert.Equal(t, int64(len(tc.expectedIDs)), total)
		})
	}
}
