package events

import (
	"testing"
	"time"
)

func TestNormalizeProperties_TrimsKeys(t *testing.T) {
	props := NormalizeProperties(map[string]interface{}{
		" output_tokens": 100,
		"input_tokens ":  50,
		"model":          "gemma-4-31b",
	})

	if _, ok := props["output_tokens"]; !ok {
		t.Fatalf("leading-space key not trimmed: %v", props)
	}
	if _, ok := props["input_tokens"]; !ok {
		t.Fatalf("trailing-space key not trimmed: %v", props)
	}
	if _, ok := props[" output_tokens"]; ok {
		t.Fatal("padded key still present")
	}
	if props["model"] != "gemma-4-31b" {
		t.Fatalf("clean key altered: %v", props["model"])
	}
}

// A padded key must never clobber a clean key that already carries the real value.
func TestNormalizeProperties_ClashKeepsCleanKey(t *testing.T) {
	props := NormalizeProperties(map[string]interface{}{
		"tokens":  100,
		" tokens": 999,
	})

	if props["tokens"] != 100 {
		t.Fatalf("clean key was overwritten: %v", props["tokens"])
	}
	if len(props) != 1 {
		t.Fatalf("expected padded duplicate to be dropped: %v", props)
	}
}

func TestNormalizeProperties_NilSafe(t *testing.T) {
	if NormalizeProperties(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestNewEvent_NormalizesNameAndPropertyKeys(t *testing.T) {
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
