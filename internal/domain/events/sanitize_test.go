package events

import (
	"reflect"
	"testing"
	"time"
)

func TestSanitizeProperties(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]interface{}
		want  map[string]interface{}
	}{
		{
			name:  "trims leading and trailing padding",
			input: map[string]interface{}{" output_tokens": 100, "input_tokens ": 50, "model": "gemma-4-31b"},
			want:  map[string]interface{}{"output_tokens": 100, "input_tokens": 50, "model": "gemma-4-31b"},
		},
		{
			// A padded key must never clobber a clean key that already carries the real value.
			name:  "existing clean key wins a collision",
			input: map[string]interface{}{"tokens": 100, " tokens": 999},
			want:  map[string]interface{}{"tokens": 100},
		},
		{
			// Map iteration order must not decide the billed quantity: between
			// padded keys the lexicographically smallest ("  x" < " x") wins.
			name:  "collision between padded keys resolves deterministically",
			input: map[string]interface{}{" x": 1, "  x": 2},
			want:  map[string]interface{}{"x": 2},
		},
		{
			name:  "whitespace-only key is dropped",
			input: map[string]interface{}{"   ": 1, "tokens": 5},
			want:  map[string]interface{}{"tokens": 5},
		},
		{
			name:  "clean map is untouched",
			input: map[string]interface{}{"tokens": 5},
			want:  map[string]interface{}{"tokens": 5},
		},
		{
			name:  "nil is passed through",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeProperties(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// The padded-key winner must be stable across runs, not just within one.
func TestSanitizeProperties_CollisionIsStableAcrossRuns(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := SanitizeProperties(map[string]interface{}{" x": 1, "  x": 2, "\tx": 3})
		if got["x"] != 3 {
			t.Fatalf("run %d picked %v, want the lexicographically smallest padded key", i, got["x"])
		}
	}
}

func TestNewEvent_SanitizesNameAndPropertyKeys(t *testing.T) {
	e := NewEvent(" llm_usage ", "tenant_1", "cust_1",
		map[string]interface{}{" output_tokens": 42},
		time.Time{}, "", "", "", "env_1")

	if e.EventName != "llm_usage" {
		t.Fatalf("event name not trimmed: %q", e.EventName)
	}
	if _, ok := e.Properties["output_tokens"]; !ok {
		t.Fatalf("property key not trimmed: %v", e.Properties)
	}
}
