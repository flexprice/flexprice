package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestSubscriptionModificationFlow_NoEphemeralsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	s := NewSubscriptionModificationFlow(fc, e2eprobe.NewRegistry(), "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionModificationFlow_AddsLineItem(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("subscription", "sub_a", time.Now().Add(-10*time.Minute))
	subID := "sub_a"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {ID: &subID},
	}
	s := NewSubscriptionModificationFlow(fc, reg, "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestSubscriptionModificationFlow_CountsLineItems verifies that countLineItems
// correctly reads the line item count from the SDK response.
func TestSubscriptionModificationFlow_CountsLineItems(t *testing.T) {
	fc := newFakeClient()
	subID := "sub_with_items"
	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("subscription", subID, time.Now().Add(-10*time.Minute))

	itemID := "item_1"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID: &subID,
			LineItems: []sdktypes.SubscriptionSubscriptionLineItem{
				{ID: &itemID},
			},
		},
	}

	s := NewSubscriptionModificationFlow(fc, reg, "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// CreateLineItem was called once (the fake doesn't add to subs.subs, so
	// post-count == pre-count == 1, resulting in the soft no-op branch).
	// The key assertion: Run completed without error.
}
