package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
)

func newCheckout(id, entityID string, mode types.CheckoutObjective, status types.CheckoutStatus, expires time.Time) *checkout.Checkout {
	return &checkout.Checkout{
		ID:             id,
		CustomerID:     "cust_1",
		EntityType:     types.CheckoutEntityTypeSubscription,
		EntityID:       entityID,
		CheckoutAction: types.CheckoutActionSubscriptionCreation,
		Mode:           mode,
		Status:         status,
		Provider:       types.CheckoutProviderStripe,
		ExpiresAt:      expires,
	}
}

func TestInMemoryCheckoutStore_GetPendingByEntity(t *testing.T) {
	ctx := SetupContext()
	store := NewInMemoryCheckoutStore()
	now := time.Now().UTC()

	if err := store.Create(ctx, newCheckout("chk_pay", "sub_1", types.CheckoutObjectivePayment, types.CheckoutStatusPending, now.Add(time.Hour))); err != nil {
		t.Fatalf("Create chk_pay: %v", err)
	}
	if err := store.Create(ctx, newCheckout("chk_setup", "sub_1", types.CheckoutObjectiveSetup, types.CheckoutStatusPending, now.Add(time.Hour))); err != nil {
		t.Fatalf("Create chk_setup: %v", err)
	}
	if err := store.Create(ctx, newCheckout("chk_done", "sub_1", types.CheckoutObjectivePayment, types.CheckoutStatusCompleted, now.Add(time.Hour))); err != nil {
		t.Fatalf("Create chk_done: %v", err)
	}

	tests := []struct {
		name     string
		params   checkout.GetPendingByEntityParams
		wantID   string
		wantNil  bool
	}{
		{
			name:   "payment pending found",
			params: checkout.GetPendingByEntityParams{EntityType: types.CheckoutEntityTypeSubscription, EntityID: "sub_1", Mode: types.CheckoutObjectivePayment},
			wantID: "chk_pay",
		},
		{
			name:    "missing entity returns nil",
			params:  checkout.GetPendingByEntityParams{EntityType: types.CheckoutEntityTypeSubscription, EntityID: "sub_2", Mode: types.CheckoutObjectivePayment},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetPendingByEntity(ctx, tt.params)
			if err != nil {
				t.Fatalf("GetPendingByEntity: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil || got.ID != tt.wantID {
				t.Fatalf("expected %s, got %+v", tt.wantID, got)
			}
		})
	}
}

func TestInMemoryCheckoutStore_ListPendingExpired(t *testing.T) {
	ctx := SetupContext()
	store := NewInMemoryCheckoutStore()
	now := time.Now().UTC()

	if err := store.Create(ctx, newCheckout("chk_old", "sub_1", types.CheckoutObjectivePayment, types.CheckoutStatusPending, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Create chk_old: %v", err)
	}
	if err := store.Create(ctx, newCheckout("chk_future", "sub_2", types.CheckoutObjectivePayment, types.CheckoutStatusPending, now.Add(time.Hour))); err != nil {
		t.Fatalf("Create chk_future: %v", err)
	}
	if err := store.Create(ctx, newCheckout("chk_old_done", "sub_3", types.CheckoutObjectivePayment, types.CheckoutStatusCompleted, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Create chk_old_done: %v", err)
	}

	got, err := store.ListPendingExpired(ctx, now, nil)
	if err != nil {
		t.Fatalf("ListPendingExpired: %v", err)
	}
	if len(got) != 1 || got[0].ID != "chk_old" {
		t.Fatalf("expected [chk_old], got %+v", got)
	}
}
