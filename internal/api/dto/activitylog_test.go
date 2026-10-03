package dto

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestActivityDisplayPartsCarryOnlyTheSentencePieces(t *testing.T) {
	raw, err := json.Marshal(ActivityDisplayParts{Actor: "Alice", Verb: "updated", EntityType: "customer", Entity: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"actor", "entity", "entity_type", "verb"}
	if len(keys) != len(want) {
		t.Fatalf("parts keys = %v, want %v (change values and counts live in changes)", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("parts keys = %v, want %v", keys, want)
		}
	}
}
