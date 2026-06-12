package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestJanitor(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(fc *fakeClient, reg e2eprobe.Registry)
		wantErr       bool
		wantRemaining int    // expected count of "customer" ephemerals after Run
		wantRemainingID string // if set, the remaining ephemeral must have this ID
	}{
		{
			name: "archives old customer, keeps fresh",
			setup: func(_ *fakeClient, reg e2eprobe.Registry) {
				reg.RegisterEphemeral("customer", "old", time.Now().Add(-5*time.Hour))
				reg.RegisterEphemeral("customer", "fresh", time.Now().Add(-30*time.Minute))
			},
			wantErr:          false,
			wantRemaining:    1,
			wantRemainingID:  "fresh",
		},
		{
			name: "no-op on empty registry",
			setup: func(_ *fakeClient, _ e2eprobe.Registry) {
				// nothing to register
			},
			wantErr:       false,
			wantRemaining: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc, reg)
			j := NewJanitor(fc, reg, 4*time.Hour, "run-1")
			err := j.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}
			got := reg.Ephemerals("customer")
			if len(got) != tc.wantRemaining {
				t.Errorf("remaining ephemerals = %d, want %d; got %+v", len(got), tc.wantRemaining, got)
			}
			if tc.wantRemainingID != "" && len(got) > 0 && got[0].ID != tc.wantRemainingID {
				t.Errorf("remaining ephemeral ID = %q, want %q", got[0].ID, tc.wantRemainingID)
			}
		})
	}
}

func TestJanitor_SweepOrphans(t *testing.T) {
	// Populate Flexprice with two old ephemeral customers and one fresh one.
	// The janitor orphan sweep should delete the old ones but leave the fresh one.
	oldTime := time.Now().Add(-5 * time.Hour).UTC().Format(time.RFC3339)
	freshTime := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)

	oldID1 := "cust-internal-old-1"
	oldExtID1 := "e2eprobe-cust-eph-old-1"
	oldID2 := "cust-internal-old-2"
	oldExtID2 := "e2eprobe-cust-eph-old-2"
	freshID := "cust-internal-fresh"

	fc := newFakeClient()
	fc.customers.queryResult = []types.DtoCustomerResponse{
		{
			ID:         strPtr(oldID1),
			ExternalID: strPtr(oldExtID1),
			CreatedAt:  strPtr(oldTime),
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			ID:         strPtr(oldID2),
			ExternalID: strPtr(oldExtID2),
			CreatedAt:  strPtr(oldTime),
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			ID:         strPtr(freshID),
			ExternalID: strPtr("e2eprobe-cust-eph-fresh"),
			CreatedAt:  strPtr(freshTime),
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			// persistent customer — should never be touched
			ID:         strPtr("cust-internal-persistent"),
			ExternalID: strPtr("e2eprobe-cust-persistent-0"),
			CreatedAt:  strPtr(oldTime),
			Metadata:   map[string]string{"e2eprobe_cohort": "persistent"},
		},
	}

	reg := e2eprobe.NewRegistry()
	j := NewJanitor(fc, reg, 1*time.Hour, "run-sweep")
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// Only the two old ephemeral customers should have been deleted.
	deleted := fc.customers.deleted
	if len(deleted) != 2 {
		t.Fatalf("deleted %d customers, want 2; got %v", len(deleted), deleted)
	}
	deletedSet := map[string]bool{deleted[0]: true, deleted[1]: true}
	if !deletedSet[oldID1] || !deletedSet[oldID2] {
		t.Errorf("deleted set = %v; want both %s and %s", deletedSet, oldID1, oldID2)
	}
	if deletedSet[freshID] {
		t.Errorf("fresh customer %s was incorrectly deleted", freshID)
	}
}
