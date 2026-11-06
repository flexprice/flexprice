package service

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

type lineItemMemoKey struct{}

type activeLineItemsKey struct {
	subscriptionID string
	from           int64
}

// lineItemMemo caches active line item lists for one invoice compute.
type lineItemMemo struct {
	mu    sync.Mutex
	lists map[activeLineItemsKey][]*subscription.SubscriptionLineItem
}

// withLineItemMemo lets repeated loads within ctx share one query.
func withLineItemMemo(ctx context.Context) context.Context {
	if _, ok := ctx.Value(lineItemMemoKey{}).(*lineItemMemo); ok {
		return ctx
	}
	return context.WithValue(ctx, lineItemMemoKey{}, &lineItemMemo{
		lists: make(map[activeLineItemsKey][]*subscription.SubscriptionLineItem),
	})
}

// listActiveLineItems returns published line items of subscriptionID not ended before from.
func listActiveLineItems(
	ctx context.Context,
	repo subscription.LineItemRepository,
	subscriptionID string,
	from time.Time,
) ([]*subscription.SubscriptionLineItem, error) {
	load := func() ([]*subscription.SubscriptionLineItem, error) {
		filter := types.NewNoLimitSubscriptionLineItemFilter()
		filter.SubscriptionIDs = []string{subscriptionID}
		filter.ActiveFilter = true
		filter.CurrentPeriodStart = &from
		return repo.List(ctx, filter)
	}

	memo, ok := ctx.Value(lineItemMemoKey{}).(*lineItemMemo)
	if !ok {
		return load()
	}

	key := activeLineItemsKey{subscriptionID: subscriptionID, from: from.UnixNano()}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	if items, ok := memo.lists[key]; ok {
		return append([]*subscription.SubscriptionLineItem(nil), items...), nil
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	memo.lists[key] = items
	return append([]*subscription.SubscriptionLineItem(nil), items...), nil
}
