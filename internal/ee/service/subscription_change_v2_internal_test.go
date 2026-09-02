package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func planChangeMove(amount int64, quantity int64) *lineItemChange {
	return &lineItemChange{
		lineItem: &subscription.SubscriptionLineItem{
			ID:       "subs_line_" + decimal.NewFromInt(amount).String(),
			Quantity: decimal.NewFromInt(quantity),
		},
		price: &price.Price{Amount: decimal.NewFromInt(amount)},
	}
}

func TestPlanChangeType_CountsQuantity(t *testing.T) {
	tests := []struct {
		name    string
		closing []*lineItemChange
		opening []*lineItemChange
		want    types.SubscriptionChangeType
	}{
		{
			name:    "ten seats at $5 out, one $20 line in, is a downgrade",
			closing: []*lineItemChange{planChangeMove(5, 10)},
			opening: []*lineItemChange{planChangeMove(20, 1)},
			want:    types.SubscriptionChangeTypeDowngrade,
		},
		{
			name:    "same unit price, more seats, is an upgrade",
			closing: []*lineItemChange{planChangeMove(5, 2)},
			opening: []*lineItemChange{planChangeMove(5, 3)},
			want:    types.SubscriptionChangeTypeUpgrade,
		},
		{
			name:    "same money either side is lateral",
			closing: []*lineItemChange{planChangeMove(10, 3)},
			opening: []*lineItemChange{planChangeMove(30, 1)},
			want:    types.SubscriptionChangeTypeLateral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planChangeType(&planChangeRequest{closingLineItems: tt.closing, openingLineItems: tt.opening})
			assert.Equal(t, tt.want, got)
		})
	}
}

func planChangeKeyRequest(sub *subscription.Subscription, clientKey string) *planChangeRequest {
	return &planChangeRequest{
		currentSub:     sub,
		toPlan:         &plan.Plan{ID: "plan_pro"},
		effectiveAt:    time.Now().UTC(),
		idempotencyKey: clientKey,
	}
}

// The key exists to stop a retried attempt from settling twice, so it must not
// move between attempts — and must not repeat once the subscription has moved on,
// or a later change to the same plan would reuse the earlier invoice.
func TestPlanChangeIdempotencyKey(t *testing.T) {
	readAt := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{ID: "subs_1", Version: 3}
	sub.UpdatedAt = readAt

	first := planChangeIdempotencyKey(planChangeKeyRequest(sub, ""), "")
	require.NotEmpty(t, first)

	t.Run("stable across retries of the same attempt", func(t *testing.T) {
		retry := planChangeKeyRequest(sub, "")
		retry.effectiveAt = retry.effectiveAt.Add(time.Minute) // a later wall clock
		assert.Equal(t, first, planChangeIdempotencyKey(retry, ""))
	})

	t.Run("moves once the subscription has been written", func(t *testing.T) {
		changed := *sub
		changed.UpdatedAt = readAt.Add(time.Hour)
		assert.NotEqual(t, first, planChangeIdempotencyKey(planChangeKeyRequest(&changed, ""), ""))
	})

	t.Run("each document a change raises gets its own key", func(t *testing.T) {
		// Two documents sharing a key would make the second fail as already-existing.
		assert.NotEqual(t, first, planChangeIdempotencyKey(planChangeKeyRequest(sub, ""), "outgoing_usage"))
	})

	t.Run("a client key replaces the derived one", func(t *testing.T) {
		withKey := planChangeIdempotencyKey(planChangeKeyRequest(sub, "caller-supplied"), "")
		assert.NotEqual(t, first, withKey)

		later := *sub
		later.UpdatedAt = readAt.Add(time.Hour)
		assert.Equal(t, withKey, planChangeIdempotencyKey(planChangeKeyRequest(&later, "caller-supplied"), ""),
			"the caller's key is the whole identity of the attempt")
	})
}

func TestCreditBasis(t *testing.T) {
	item := &subscription.SubscriptionLineItem{ID: "subs_line_1", Quantity: decimal.NewFromInt(2)}
	p := &price.Price{Amount: decimal.NewFromInt(20)}
	listPrice := decimal.NewFromInt(40)

	t.Run("a billed row caps the credit at what was charged", func(t *testing.T) {
		billed := map[string]*invoice.BilledAmounts{
			item.ID: invoice.NewBilledAmounts(decimal.NewFromInt(5), decimal.NewFromInt(1)),
		}

		paid, credits := creditBasis(item, p, billed)
		assert.True(t, paid.Equal(decimal.NewFromInt(5)))
		assert.True(t, credits.Equal(decimal.NewFromInt(1)))
	})

	t.Run("a row charging zero caps the credit at zero", func(t *testing.T) {
		billed := map[string]*invoice.BilledAmounts{
			item.ID: invoice.NewBilledAmounts(decimal.Zero, decimal.Zero),
		}

		paid, credits := creditBasis(item, p, billed)
		assert.True(t, paid.IsZero(), "an invoice that charged nothing is evidence, not absence")
		assert.True(t, credits.IsZero())
	})

	t.Run("no row for this line item falls back to list price", func(t *testing.T) {
		billed := map[string]*invoice.BilledAmounts{
			"subs_line_other": invoice.NewBilledAmounts(decimal.NewFromInt(5), decimal.Zero),
		}

		paid, credits := creditBasis(item, p, billed)
		assert.True(t, paid.Equal(listPrice))
		assert.True(t, credits.IsZero())
	})

	// A transient repo failure must not change the money: it hands creditBasis a
	// nil map, which has to land on the same basis as a lookup that found nothing.
	t.Run("a failed lookup credits the same as an empty one", func(t *testing.T) {
		failed, _ := creditBasis(item, p, nil)
		empty, _ := creditBasis(item, p, map[string]*invoice.BilledAmounts{})

		assert.True(t, failed.Equal(empty))
		assert.True(t, failed.Equal(listPrice))
	})

	t.Run("a missing price cannot invent a basis", func(t *testing.T) {
		paid, credits := creditBasis(item, nil, nil)
		assert.True(t, paid.IsZero())
		assert.True(t, credits.IsZero())
	})
}
