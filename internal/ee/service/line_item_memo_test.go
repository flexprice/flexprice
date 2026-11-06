package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type countingLineItemRepo struct {
	subscription.LineItemRepository
	calls int
}

func (r *countingLineItemRepo) List(_ context.Context, f *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	r.calls++
	return []*subscription.SubscriptionLineItem{{ID: f.SubscriptionIDs[0]}}, nil
}

func TestListActiveLineItems(t *testing.T) {
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := jan.AddDate(0, 1, 0)

	tests := []struct {
		name      string
		memo      bool
		wantCalls int
	}{
		{name: "without memo every call queries", memo: false, wantCalls: 4},
		{name: "memo shares identical queries", memo: true, wantCalls: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &countingLineItemRepo{}
			ctx := context.Background()
			if tt.memo {
				ctx = withLineItemMemo(ctx)
			}

			first, err := listActiveLineItems(ctx, repo, "sub_a", jan)
			require.NoError(t, err)
			again, err := listActiveLineItems(ctx, repo, "sub_a", jan)
			require.NoError(t, err)
			_, err = listActiveLineItems(ctx, repo, "sub_a", feb)
			require.NoError(t, err)
			other, err := listActiveLineItems(ctx, repo, "sub_b", jan)
			require.NoError(t, err)

			require.Equal(t, tt.wantCalls, repo.calls)
			require.Equal(t, first, again)
			require.Equal(t, "sub_b", other[0].ID)

			again[0] = nil
			reread, err := listActiveLineItems(ctx, repo, "sub_a", jan)
			require.NoError(t, err)
			require.NotNil(t, reread[0], "callers get their own slice")
		})
	}
}
