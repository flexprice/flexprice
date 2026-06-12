package e2eprobe

import (
	"testing"
)

func TestEventDeck_DeterministicWithSeed(t *testing.T) {
	a := NewEventDeck(EventDeckOpts{
		Customers:   []string{"c0", "c1"},
		EventNames:  []string{"e1", "e2"},
		Seed:        42,
	})
	b := NewEventDeck(EventDeckOpts{
		Customers:   []string{"c0", "c1"},
		EventNames:  []string{"e1", "e2"},
		Seed:        42,
	})
	for i := 0; i < 20; i++ {
		ea := a.Next()
		eb := b.Next()
		if ea.EventName != eb.EventName || ea.Source != eb.Source || ea.ExternalCustomerID != eb.ExternalCustomerID {
			t.Fatalf("non-deterministic at i=%d: %+v vs %+v", i, ea, eb)
		}
	}
}

func TestEventDeck_CoversAllSources(t *testing.T) {
	d := NewEventDeck(EventDeckOpts{
		Customers:  []string{"c0"},
		EventNames: []string{"e1"},
		Seed:       7,
	})
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[d.Next().Source] = true
	}
	for _, want := range []string{"api", "web", "mobile", "batch"} {
		if !seen[want] {
			t.Errorf("source %q never emitted in 200 draws", want)
		}
	}
}

func TestEventDeck_AlwaysHasAmount(t *testing.T) {
	d := NewEventDeck(EventDeckOpts{Customers: []string{"c"}, EventNames: []string{"e"}, Seed: 1})
	for i := 0; i < 50; i++ {
		ev := d.Next()
		if _, ok := ev.Properties["amount"]; !ok {
			t.Fatalf("event missing amount: %+v", ev)
		}
	}
}

func TestEventDeck_SometimesAddsRandomExtras(t *testing.T) {
	d := NewEventDeck(EventDeckOpts{Customers: []string{"c"}, EventNames: []string{"e"}, Seed: 3})
	extras := map[string]bool{
		"session_id": true,
		"endpoint":   true,
		"status":     true,
		"method":     true,
		"tier":       true,
	}
	sawExtra := false
	for i := 0; i < 100; i++ {
		ev := d.Next()
		for k := range ev.Properties {
			if extras[k] {
				sawExtra = true
			}
		}
	}
	if !sawExtra {
		t.Error("never saw a random extra property in 100 draws")
	}
}

// TestEventDeck_AllCustomerSourceCombinationsReachable guards against the
// modular-correlation bug where customer and source were both indexed by
// `n % len(...)`. With 10 customers and 4 sources, gcd(10,4)=2 meant odd-
// indexed customers could never receive even-indexed sources (and vice
// versa). That permanently zeroed out source-filtered meters like
// e2eprobe_sum_filtered for half the customer pool.
func TestEventDeck_AllCustomerSourceCombinationsReachable(t *testing.T) {
	customers := []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9"}
	d := NewEventDeck(EventDeckOpts{
		Customers:  customers,
		EventNames: []string{"e1"},
		Seed:       12345,
	})
	seen := map[string]map[string]bool{}
	for _, c := range customers {
		seen[c] = map[string]bool{}
	}
	for i := 0; i < 5000; i++ {
		ev := d.Next()
		seen[ev.ExternalCustomerID][ev.Source] = true
	}
	for _, c := range customers {
		for _, s := range []string{"api", "web", "mobile", "batch"} {
			if !seen[c][s] {
				t.Errorf("(customer=%s, source=%s) never reached in 5000 draws — modular-correlation regression", c, s)
			}
		}
	}
}

func TestEventDeck_EmitsOrphanWhenConfigured(t *testing.T) {
	d := NewEventDeck(EventDeckOpts{
		Customers:        []string{"c0"},
		EventNames:       []string{"e2eprobe_count"},
		OrphanEventName:  "e2eprobe_orphan",
		OrphanFrequency:  4,
		Seed:             1,
	})
	hits := 0
	for i := 0; i < 16; i++ {
		if d.Next().EventName == "e2eprobe_orphan" {
			hits++
		}
	}
	if hits != 4 {
		t.Errorf("orphan hits=%d, want 4", hits)
	}
}
