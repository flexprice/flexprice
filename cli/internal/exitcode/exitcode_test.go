package exitcode

import "testing"

// These values are a public contract that scripts depend on. This test exists
// to make a change to any of them a deliberate act rather than a side effect.
func TestValuesAreStable(t *testing.T) {
	want := map[string]int{
		"OK": 0, "Generic": 1, "Usage": 2,
		"Auth": 3, "NotFound": 4, "RateLimited": 5,
		"Interrupted": 130,
	}
	actual := map[string]int{
		"OK": OK, "Generic": Generic, "Usage": Usage,
		"Auth": Auth, "NotFound": NotFound, "RateLimited": RateLimited,
		"Interrupted": Interrupted,
	}
	for name, w := range want {
		if actual[name] != w {
			t.Errorf("%s = %d, want %d — this is a public contract", name, actual[name], w)
		}
	}
}
