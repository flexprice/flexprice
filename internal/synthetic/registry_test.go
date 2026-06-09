package synthetic

import (
	"sync"
	"testing"
	"time"
)

func TestRegistry_Seeds(t *testing.T) {
	r := NewRegistry()
	s := Seeds{
		PersistentCustomerIDs: []string{"c0", "c1", "c2"},
		PreFundedCustomerIDs:  []string{"c0"},
		PlanIDs:               []string{"plan_1"},
		MeterIDs:              map[string]string{"synthetic_count": "meter_count"},
		FeatureIDs:            []string{"feat_1"},
		PersistentSubIDs:      []string{"sub_1", "sub_2"},
	}
	r.LoadSeeds(s)
	got := r.Seeds()
	if len(got.PersistentCustomerIDs) != 3 || len(got.PreFundedCustomerIDs) != 1 {
		t.Errorf("seeds=%+v", got)
	}
	if got.MeterIDs["synthetic_count"] != "meter_count" {
		t.Errorf("meter map=%+v", got.MeterIDs)
	}
	// Mutate returned slice; expect no leak into registry state.
	got.PersistentCustomerIDs[0] = "tampered"
	if r.Seeds().PersistentCustomerIDs[0] == "tampered" {
		t.Error("registry exposed mutable seed slice")
	}
}

func TestRegistry_Ephemerals(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.RegisterEphemeral("subscription", "sub_a", now)
	r.RegisterEphemeral("subscription", "sub_b", now.Add(-time.Hour))
	r.RegisterEphemeral("customer", "cust_a", now)
	if len(r.Ephemerals("subscription")) != 2 || len(r.Ephemerals("customer")) != 1 {
		t.Fatal("ephemerals miscount")
	}
	r.ArchiveEphemeral("subscription", "sub_a")
	if got := r.Ephemerals("subscription"); len(got) != 1 || got[0].ID != "sub_b" {
		t.Errorf("after archive: %+v", got)
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.RegisterEphemeral("c", "x", time.Now()) }()
		go func() { defer wg.Done(); _ = r.Ephemerals("c") }()
	}
	wg.Wait()
}
