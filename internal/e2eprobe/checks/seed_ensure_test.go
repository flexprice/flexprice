package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestSeedEnsure(t *testing.T) {
	tests := []struct {
		name                    string
		setup                   func(fc *fakeClient)
		wantErr                 bool
		wantFeaturesCreated     int
		wantCustomersCreated    int
		wantPlansCreated        int
		wantPricesCreated       int
		wantSubsCreated         int
		wantWalletsCreated      int
		wantPersistentCustomers int
		wantPreFundedCustomers  int
		wantMeterIDs            int
		wantFeatureIDs          int
		wantPlanIDs             int
		wantSubIDs              int
	}{
		{
			name: "AllPresent: features pre-exist, customers present, plan pre-exists",
			setup: func(fc *fakeClient) {
				// Pre-populate 9 features with lookup keys and meter IDs.
				for _, spec := range seedFeatureSpecs {
					lk := spec.lookupKey
					mID := "meter_" + spec.eventName
					fc.features.features = append(fc.features.features, types.DtoFeatureResponse{
						ID:        strPtr("feat_" + spec.lookupKey),
						LookupKey: &lk,
						MeterID:   &mID,
					})
				}
				// Pre-populate 10 customers.
				for i := 0; i < 10; i++ {
					ext := persistentExternalCustomerID(i)
					fc.customers.byExt[ext] = "cust_" + ext
				}
				// Pre-populate 1 plan.
				lk := e2eprobePlanLookupKey
				planID := "plan_existing"
				fc.plans.plans = append(fc.plans.plans, types.DtoPlanResponse{
					ID:        &planID,
					LookupKey: &lk,
				})
				// subs and wallets are empty — they'll be created
			},
			wantErr:                 false,
			wantFeaturesCreated:     0,  // all 9 found via Query
			wantCustomersCreated:    0,  // all 10 already present
			wantPlansCreated:        0,  // plan found via Query
			wantPricesCreated:       10, // base + 9 usage prices
			wantSubsCreated:         10, // one per persistent customer
			wantWalletsCreated:      3,  // one per pre-funded customer
			wantPersistentCustomers: 10,
			wantPreFundedCustomers:  3,
			wantMeterIDs:            9,
			wantFeatureIDs:          9,
			wantPlanIDs:             1,
			wantSubIDs:              10,
		},
		{
			name: "CreatesMissing: all empty, all entities created",
			setup: func(fc *fakeClient) {
				// Customers not found on initial lookup.
				fc.customers.getErr = errNotFound
			},
			wantErr:                 false,
			wantFeaturesCreated:     9,
			wantCustomersCreated:    10,
			wantPlansCreated:        1,
			wantPricesCreated:       10, // base + 9 usage
			wantSubsCreated:         10,
			wantWalletsCreated:      3,
			wantPersistentCustomers: 10,
			wantPreFundedCustomers:  3,
			wantMeterIDs:            9,
			wantFeatureIDs:          9,
			wantPlanIDs:             1,
			wantSubIDs:              10,
		},
		{
			name: "PartialExisting: features exist but plan/subs/wallets don't",
			setup: func(fc *fakeClient) {
				// Pre-populate 9 features.
				for _, spec := range seedFeatureSpecs {
					lk := spec.lookupKey
					mID := "meter_" + spec.eventName
					fc.features.features = append(fc.features.features, types.DtoFeatureResponse{
						ID:        strPtr("feat_" + spec.lookupKey),
						LookupKey: &lk,
						MeterID:   &mID,
					})
				}
				// Customers all exist.
				for i := 0; i < 10; i++ {
					ext := persistentExternalCustomerID(i)
					fc.customers.byExt[ext] = "cust_" + ext
				}
				// Plan and everything below are missing.
			},
			wantErr:                 false,
			wantFeaturesCreated:     0,
			wantCustomersCreated:    0,
			wantPlansCreated:        1,
			wantPricesCreated:       10,
			wantSubsCreated:         10,
			wantWalletsCreated:      3,
			wantPersistentCustomers: 10,
			wantPreFundedCustomers:  3,
			wantMeterIDs:            9,
			wantFeatureIDs:          9,
			wantPlanIDs:             1,
			wantSubIDs:              10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc)
			s := NewSeedEnsure(fc, reg, "run-1", nil)
			err := s.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}

			if len(fc.features.created) != tc.wantFeaturesCreated {
				t.Errorf("features.created = %d, want %d", len(fc.features.created), tc.wantFeaturesCreated)
			}
			if len(fc.customers.created) != tc.wantCustomersCreated {
				t.Errorf("customers.created = %d, want %d", len(fc.customers.created), tc.wantCustomersCreated)
			}
			if len(fc.plans.created) != tc.wantPlansCreated {
				t.Errorf("plans.created = %d, want %d", len(fc.plans.created), tc.wantPlansCreated)
			}
			if len(fc.prices.created) != tc.wantPricesCreated {
				t.Errorf("prices.created = %d, want %d", len(fc.prices.created), tc.wantPricesCreated)
			}
			if len(fc.subs.created) != tc.wantSubsCreated {
				t.Errorf("subs.created = %d, want %d", len(fc.subs.created), tc.wantSubsCreated)
			}
			if len(fc.wallets.created) != tc.wantWalletsCreated {
				t.Errorf("wallets.created = %d, want %d", len(fc.wallets.created), tc.wantWalletsCreated)
			}

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
			if len(got.FeatureIDs) != tc.wantFeatureIDs {
				t.Errorf("FeatureIDs = %d, want %d", len(got.FeatureIDs), tc.wantFeatureIDs)
			}
			if len(got.PlanIDs) != tc.wantPlanIDs {
				t.Errorf("PlanIDs = %d, want %d", len(got.PlanIDs), tc.wantPlanIDs)
			}
			if len(got.PersistentSubIDs) != tc.wantSubIDs {
				t.Errorf("PersistentSubIDs = %d, want %d", len(got.PersistentSubIDs), tc.wantSubIDs)
			}
		})
	}
}
