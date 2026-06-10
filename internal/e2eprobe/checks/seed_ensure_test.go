package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func TestSeedEnsure(t *testing.T) {
	tests := []struct {
		name                     string
		setup                    func(fc *fakeClient)
		wantErr                  bool
		wantMetersCreated        int
		wantCustomersCreated     int
		wantPersistentCustomers  int
		wantPreFundedCustomers   int
		wantMeterIDs             int
	}{
		{
			name: "all meters and customers already present",
			setup: func(fc *fakeClient) {
				for _, ev := range []string{
					"e2eprobe_count", "e2eprobe_sum", "e2eprobe_avg",
					"e2eprobe_count_unique", "e2eprobe_latest", "e2eprobe_max",
					"e2eprobe_sum_multiplier", "e2eprobe_weighted_sum", "e2eprobe_sum_filtered",
				} {
					_, _ = fc.meters.Create(context.Background(), e2eprobe.CreateMeterRequest{EventName: ev, Name: ev})
				}
				for i := 0; i < 10; i++ {
					fc.customers.byExt[persistentExternalCustomerID(i)] = "cust_id"
				}
			},
			wantErr:                 false,
			wantMetersCreated:       9,  // pre-created only; no new creates
			wantCustomersCreated:    0,
			wantPersistentCustomers: 10,
			wantPreFundedCustomers:  3,
			wantMeterIDs:            9,
		},
		{
			name: "creates all missing meters and customers",
			setup: func(fc *fakeClient) {
				fc.customers.getErr = errors.New("404")
			},
			wantErr:              false,
			wantMetersCreated:    9,
			wantCustomersCreated: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc)
			s := NewSeedEnsure(fc, reg, "run-1")
			err := s.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}
			if len(fc.meters.created) != tc.wantMetersCreated {
				t.Errorf("meters.created = %d, want %d", len(fc.meters.created), tc.wantMetersCreated)
			}
			if len(fc.customers.created) != tc.wantCustomersCreated {
				t.Errorf("customers.created = %d, want %d", len(fc.customers.created), tc.wantCustomersCreated)
			}
			if tc.wantPersistentCustomers > 0 {
				got := reg.Seeds()
				if len(got.PersistentCustomerIDs) != tc.wantPersistentCustomers {
					t.Errorf("PersistentCustomerIDs = %d, want %d", len(got.PersistentCustomerIDs), tc.wantPersistentCustomers)
				}
				if len(got.PreFundedCustomerIDs) != tc.wantPreFundedCustomers {
					t.Errorf("PreFundedCustomerIDs = %d, want %d", len(got.PreFundedCustomerIDs), tc.wantPreFundedCustomers)
				}
				if len(got.MeterIDs) != tc.wantMeterIDs {
					t.Errorf("MeterIDs = %d, want %d", len(got.MeterIDs), tc.wantMeterIDs)
				}
			}
		})
	}
}
