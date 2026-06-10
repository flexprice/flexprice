package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/dtos"
)

func TestNewCustomerLifecycle(t *testing.T) {
	// Swap extractSubscriptionID to read the sub ID from the fake's response wrapper.
	origExtract := extractSubscriptionID
	extractSubscriptionID = func(resp interface{}) string {
		r, ok := resp.(*dtos.CreateSubscriptionResponse)
		if !ok || r == nil || r.DtoSubscriptionResponse == nil || r.DtoSubscriptionResponse.ID == nil {
			return ""
		}
		return *r.DtoSubscriptionResponse.ID
	}
	t.Cleanup(func() { extractSubscriptionID = origExtract })

	tests := []struct {
		name              string
		setup             func(fc *fakeClient, reg e2eprobe.Registry)
		opts              NewCustomerLifecycleOpts
		wantErr           bool
		wantCustomers     int
		wantSubs          int
		wantEvents        int
		wantCustEphs      int
		wantSubEphs       int
		wantSubEphIDEmpty bool
	}{
		{
			name: "happy path creates customer, sub, events",
			setup: func(_ *fakeClient, reg e2eprobe.Registry) {
				reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_seed"}})
			},
			opts: NewCustomerLifecycleOpts{
				MaxEphemerals: 20,
				AnalyticsPoll: NewCustomerLifecyclePoll{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond},
			},
			wantErr:       false,
			wantCustomers: 1,
			wantSubs:      1,
			wantEvents:    3,
			wantCustEphs:  1,
			wantSubEphs:   1,
		},
		{
			name: "no plan seeds is a no-op",
			setup: func(_ *fakeClient, _ e2eprobe.Registry) {
				// empty registry — no plan seeds
			},
			opts:          NewCustomerLifecycleOpts{MaxEphemerals: 20},
			wantErr:       false,
			wantCustomers: 0,
		},
		{
			name: "respects ephemeral cap",
			setup: func(_ *fakeClient, reg e2eprobe.Registry) {
				reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_seed"}})
				for i := 0; i < 20; i++ {
					reg.RegisterEphemeral("customer", "old", time.Now())
				}
			},
			opts:          NewCustomerLifecycleOpts{MaxEphemerals: 20},
			wantErr:       false,
			wantCustomers: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc, reg)

			s := NewNewCustomerLifecycle(fc, reg, "run-1", tc.opts)
			err := s.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}

			if tc.wantCustomers > 0 || tc.wantSubs > 0 || tc.wantEvents > 0 {
				if len(fc.customers.created) != tc.wantCustomers {
					t.Errorf("customers created = %d, want %d", len(fc.customers.created), tc.wantCustomers)
				}
				if tc.wantSubs > 0 && len(fc.subs.created) != tc.wantSubs {
					t.Errorf("subs created = %d, want %d", len(fc.subs.created), tc.wantSubs)
				}
				if tc.wantEvents > 0 && len(fc.events.ingested) != tc.wantEvents {
					t.Errorf("events ingested = %d, want %d", len(fc.events.ingested), tc.wantEvents)
				}
			} else {
				if len(fc.customers.created) != 0 {
					t.Errorf("expected no customer creates, got %d", len(fc.customers.created))
				}
			}

			if tc.wantCustEphs > 0 || tc.wantSubEphs > 0 {
				if len(reg.Ephemerals("customer")) != tc.wantCustEphs {
					t.Errorf("customer ephemerals = %d, want %d", len(reg.Ephemerals("customer")), tc.wantCustEphs)
				}
				if len(reg.Ephemerals("subscription")) != tc.wantSubEphs {
					t.Errorf("sub ephemerals = %d, want %d", len(reg.Ephemerals("subscription")), tc.wantSubEphs)
				}
				if tc.wantSubEphs > 0 {
					subEph := reg.Ephemerals("subscription")[0]
					if subEph.ID == "" {
						t.Error("ephemeral sub ID empty")
					}
				}
			}
		})
	}
}
