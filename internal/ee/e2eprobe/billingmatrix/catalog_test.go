package billingmatrix

import (
	"context"
	"testing"
)

func TestCatalogNamesUniqueAndFamiliesPopulated(t *testing.T) {
	seen := map[string]bool{}
	perFamily := map[Family]int{}
	for _, s := range buildCatalog() {
		if seen[s.name] {
			t.Errorf("duplicate scenario name %q", s.name)
		}
		seen[s.name] = true
		perFamily[s.family]++
	}
	for _, f := range Families {
		if perFamily[f] == 0 {
			t.Errorf("family %s has no scenarios", f)
		}
		t.Logf("%s: %d scenarios", f, perFamily[f])
	}
}

// Every lifecycle row only puts cadences on the sub that D12 allows.
func TestLifecycleItemsRespectCadenceRule(t *testing.T) {
	for _, row := range lifecycleSpecs() {
		for _, it := range append(row.spec.items, row.spec.addons...) {
			if it.onetime() {
				continue
			}
			if !it.cad.allowedOn(row.spec.cad) {
				t.Errorf("%s: item %s (%v) not allowed on %v", row.name, it.key, it.cad, row.spec.cad)
			}
		}
	}
}

func TestRunNextRotates(t *testing.T) {
	e := NewEngine(nil, "test", false)
	names := e.Names(FamilyValidation)
	e.assertKnownIssues = false
	e.catalog[FamilyValidation] = nil
	for _, n := range names {
		e.catalog[FamilyValidation] = append(e.catalog[FamilyValidation], scenario{family: FamilyValidation, name: n, knownIssue: "skip"})
	}
	first := e.RunNext(context.Background(), FamilyValidation, 2)
	second := e.RunNext(context.Background(), FamilyValidation, 2)
	if first[0].Name == second[0].Name {
		t.Fatalf("cursor did not advance: %s", first[0].Name)
	}
	if first[0].Skipped == "" {
		t.Fatalf("known issue should be skipped")
	}
}
