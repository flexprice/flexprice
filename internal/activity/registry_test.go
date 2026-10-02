package activity

import "testing"

// notCustomerScoped lists entities that legitimately resolve no customer_id:
// plans and prices are catalog/tenant-level, not customer-level, so they
// carry neither CustomerID nor CustomerLookup.
var notCustomerScoped = map[string]bool{
	"Plan":  true,
	"Price": true,
}

func TestDefinitionsRegisterCleanly(t *testing.T) {
	defs := Definitions()

	seenEntType := map[string]bool{}
	seenEntityType := map[string]bool{}
	for _, d := range defs {
		if seenEntType[d.EntType] {
			t.Errorf("duplicate EntType %q", d.EntType)
		}
		seenEntType[d.EntType] = true

		if seenEntityType[string(d.EntityType)] {
			t.Errorf("duplicate EntityType %q", d.EntityType)
		}
		seenEntityType[string(d.EntityType)] = true

		if d.Table == "" {
			t.Errorf("%s: empty Table", d.EntType)
		}

		hasCustomerID := d.CustomerID != nil
		hasCustomerLookup := d.CustomerLookup != nil
		if !hasCustomerID && !hasCustomerLookup && !notCustomerScoped[d.EntType] {
			t.Errorf("%s: neither CustomerID nor CustomerLookup set", d.EntType)
		}
	}

	reg := NewRegistry(defs...)
	if len(seenEntType) != len(defs) {
		t.Fatalf("NewRegistry would have overwritten a duplicate EntType")
	}
	for _, d := range defs {
		if _, ok := reg.ByEntType(d.EntType); !ok {
			t.Errorf("%s: not retrievable by EntType after registration", d.EntType)
		}
		if _, ok := reg.ByEntityType(d.EntityType); !ok {
			t.Errorf("%s: not retrievable by EntityType after registration", d.EntType)
		}
	}
}

// An update only carries the columns it changed, so an entity that reads its
// customer from its own customer_id column must fetch it with the old values,
// or its updates drop out of the customer roll-up.
func TestOwnCustomerIDIsFetchedOnUpdate(t *testing.T) {
	for _, d := range Definitions() {
		if d.CustomerID == nil || d.CustomerID(map[string]any{"customer_id": "c"}) != "c" {
			continue
		}
		fetched := append(append([]string{}, d.LabelFields...), d.ParentFields...)
		if !containsStr(fetched, "customer_id") {
			t.Errorf("%s: customer_id is not in LabelFields or ParentFields", d.EntType)
		}
	}
}
