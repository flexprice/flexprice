package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
)

func newCheckout(id, entityID string, obj types.CheckoutObjective, status types.CheckoutStatus, expires time.Time) *checkout.Checkout {
	return &checkout.Checkout{
		ID:            id,
		CustomerID:    "cust_1",
		EntityType:    types.CheckoutEntityTypeSubscription,
		EntityID:      entityID,
		CheckoutType:  types.CheckoutTypeSubscriptionCreation,
		Objective:     obj,
		Status:        status,
		Provider:      "stripe",
		ExpiresAt: expires,
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

	got, err := store.GetPendingByEntity(ctx, types.CheckoutEntityTypeSubscription, "sub_1", types.CheckoutObjectivePayment)
	if err != nil {
		t.Fatalf("GetPendingByEntity: %v", err)
	}
	if got == nil || got.ID != "chk_pay" {
		t.Fatalf("expected chk_pay, got %+v", got)
	}

	none, err := store.GetPendingByEntity(ctx, types.CheckoutEntityTypeSubscription, "sub_2", types.CheckoutObjectivePayment)
	if err != nil {
		t.Fatalf("GetPendingByEntity: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for missing entity, got %+v", none)
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

	got, err := store.ListPendingExpired(ctx, now)
	if err != nil {
		t.Fatalf("ListPendingExpired: %v", err)
	}
	if len(got) != 1 || got[0].ID != "chk_old" {
		t.Fatalf("expected [chk_old], got %+v", got)
	}
}
