package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
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
