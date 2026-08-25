package checks

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
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
	oldTime := time.Now().Add(-5 * time.Hour).UTC()
	freshTime := time.Now().Add(-10 * time.Minute).UTC()

	oldID1 := "cust-internal-old-1"
	oldExtID1 := "e2eprobe-cust-eph-old-1"
	oldID2 := "cust-internal-old-2"
	oldExtID2 := "e2eprobe-cust-eph-old-2"
	freshID := "cust-internal-fresh"

	fc := newFakeClient()
	fc.customers.queryResult = []types.CustomerResponse{
		{
			ID:         strPtr(oldID1),
			ExternalID: strPtr(oldExtID1),
			CreatedAt:  &oldTime,
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			ID:         strPtr(oldID2),
			ExternalID: strPtr(oldExtID2),
			CreatedAt:  &oldTime,
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			ID:         strPtr(freshID),
			ExternalID: strPtr("e2eprobe-cust-eph-fresh"),
			CreatedAt:  &freshTime,
			Metadata:   map[string]string{"e2eprobe_role": "ephemeral"},
		},
		{
			// persistent customer — should never be touched
			ID:         strPtr("cust-internal-persistent"),
			ExternalID: strPtr("e2eprobe-cust-persistent-0"),
			CreatedAt:  &oldTime,
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

// TestJanitor_ArchiveSwallowsErrorResponseNotFound verifies that a 404 surfaced
// as *sdkerrors.ErrorResponse (the shape returned by GetCustomerByExternalID
// and other endpoints with an explicit 404 branch) is treated as "already
// gone" rather than a check failure. Prior to the fix, only *sdkerrors.APIError
// was recognized, so any concurrent archive of an ephemeral customer (e.g., by
// cancel-customer-flow) would race the janitor's lookup and emit a false
// e2eprobe.check.failed alert.
func TestJanitor_ArchiveSwallowsErrorResponseNotFound(t *testing.T) {
	fc := newFakeClient()
	// Inject an *ErrorResponse{404} on the GetByExternalID call — this mirrors
	// what the real SDK returns when the underlying customer was archived
	// between the janitor's decision to sweep and its lookup.
	notFoundCode := types.ErrorCodeNotFound
	notFoundStatus := int64(http.StatusNotFound)
	notFoundMsg := "Customer with lookup key foo was not found"
	fc.customers.getErr = &sdkerrors.ErrorResponse{
		Code:           &notFoundCode,
		HTTPStatusCode: &notFoundStatus,
		Message:        &notFoundMsg,
	}

	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("customer", "e2eprobe-cust-eph-vanished", time.Now().Add(-5*time.Hour))
	j := NewJanitor(fc, reg, 4*time.Hour, "run-race")

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run() returned error for concurrently-archived customer: %v", err)
	}
	if got := reg.Ephemerals("customer"); len(got) != 0 {
		t.Errorf("ephemeral not archived from registry after 404: %+v", got)
	}
}

// TestJanitor_SweepOrphans_DetectsByNameOrPrefix verifies the ephemeral
// identification is loose: external_id prefix OR name match OR metadata,
// any of which is sufficient. Customers in the wild may lack the metadata
// tag if they were created by an older e2eprobe version.
func TestJanitor_SweepOrphans_DetectsByNameOrPrefix(t *testing.T) {
	oldTime := time.Now().Add(-5 * time.Hour).UTC()

	fc := newFakeClient()
	fc.customers.queryResult = []types.CustomerResponse{
		// Detected by external_id prefix only (no metadata, no helpful name).
		{
			ID:         strPtr("by-prefix"),
			ExternalID: strPtr("e2eprobe-cust-eph-prefix-only"),
			CreatedAt:  &oldTime,
		},
		// Detected by name only (no metadata, no prefix).
		{
			ID:         strPtr("by-name"),
			ExternalID: strPtr("some-other-id"),
			Name:       strPtr("E2EProbe Ephemeral random"),
			CreatedAt:  &oldTime,
		},
		// Persistent — must NOT match (no Ephemeral in name, persistent prefix).
		{
			ID:         strPtr("persistent"),
			ExternalID: strPtr("e2eprobe-cust-persistent-0"),
			Name:       strPtr("E2EProbe Persistent 0"),
			CreatedAt:  &oldTime,
		},
		// Unrelated tenant customer — must NOT match.
		{
			ID:         strPtr("unrelated"),
			ExternalID: strPtr("some-real-customer"),
			Name:       strPtr("Acme Corp"),
			CreatedAt:  &oldTime,
		},
	}

	reg := e2eprobe.NewRegistry()
	j := NewJanitor(fc, reg, 1*time.Hour, "run-detect")
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	deleted := map[string]bool{}
	for _, id := range fc.customers.deleted {
		deleted[id] = true
	}
	if !deleted["by-prefix"] {
		t.Errorf("customer detected by external_id prefix was not deleted")
	}
	if !deleted["by-name"] {
		t.Errorf("customer detected by name match was not deleted")
	}
	if deleted["persistent"] {
		t.Errorf("persistent customer was incorrectly deleted")
	}
	if deleted["unrelated"] {
		t.Errorf("unrelated customer was incorrectly deleted")
	}
}

// TestJanitor_SweepOrphanTaxAssociations verifies that tax associations
// whose EntityID (subscription) has been deleted are best-effort-cleaned
// up on the next janitor tick, while associations pointing at live subs
// (e.g. the seed association on persistent cust #0) are preserved.
func TestJanitor_SweepOrphanTaxAssociations(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{SharedTaxRateID: "taxrate_1", SharedTaxRateCode: "E2EPROBE_TAX_10PCT"})

	subAlive := "sub_alive"
	subGone := "sub_gone"
	taID1 := "ta_1"
	taID2 := "ta_2"
	fc.taxAssociations.listResp = &sdkdtos.ListTaxAssociationsResponse{
		ListTaxAssociationsResponse: &types.ListTaxAssociationsResponse{
			Items: []types.TaxAssociationResponse{
				{ID: &taID1, EntityID: &subAlive},
				{ID: &taID2, EntityID: &subGone},
			},
		},
	}
	// Only sub_alive is present in the fake — sub_gone will 404 on Get.
	fc.subs.subs = map[string]types.SubscriptionResponse{subAlive: {ID: &subAlive}}

	j := NewJanitor(fc, reg, 1*time.Hour, "test-run")
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !containsStr(fc.taxAssociations.deleted, taID2) {
		t.Errorf("orphan tax association %s not deleted; deleted=%v", taID2, fc.taxAssociations.deleted)
	}
	if containsStr(fc.taxAssociations.deleted, taID1) {
		t.Errorf("live tax association %s was incorrectly deleted", taID1)
	}
}

// TestJanitor_SweepOrphanTaxAssociations_NoSeedSoftSkip verifies that when
// the seed tax rate hasn't been provisioned yet, the sweep skips cleanly
// without emitting any errors.
func TestJanitor_SweepOrphanTaxAssociations_NoSeedSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	// Seeds contain no SharedTaxRateID.
	reg.LoadSeeds(e2eprobe.Seeds{})

	j := NewJanitor(fc, reg, 1*time.Hour, "test-run")
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fc.taxAssociations.deleted) != 0 {
		t.Errorf("no tax associations should be deleted without a seed rate; got %v", fc.taxAssociations.deleted)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
