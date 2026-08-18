package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkeleton_ProducesValidJSONForDeepSchema(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := reg.Lookup("subscriptions", "create")
	if !ok {
		t.Fatal("subscriptions create not registered")
	}

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}

	// The skeleton is commented for humans; the JSON below the comments must parse.
	body := stripComments(out)
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("skeleton is not valid JSON: %v\n%s", err, body)
	}
	if len(v) == 0 {
		t.Fatal("skeleton has no fields")
	}
}

func TestSkeleton_IncludesRequiredFields(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("subscriptions", "create")

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	// CreateSubscriptionRequest's actual required list, verified against
	// docs/swagger/swagger-3-0.json: billing_period, currency, plan_id.
	for _, want := range []string{"billing_period", "currency", "plan_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("skeleton missing required field %q", want)
		}
	}
}

// customer_id is not in CreateSubscriptionRequest's required list, but a
// subscription is meaningless without one. The optional-fields comment block
// is where a field like this must surface, or a user filling in the skeleton
// has no signal it exists at all.
func TestSkeleton_ListsFunctionallyNecessaryOptionalFields(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("subscriptions", "create")

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	if !strings.Contains(out, "customer_id") {
		t.Error("skeleton does not mention customer_id anywhere, required or optional")
	}
}

// customer_id specifically must appear in the optional-fields comment block,
// not as a live JSON field — it is not in CreateSubscriptionRequest's required
// list, and the fill policy never emits optional fields as live JSON (see
// TestSkeleton_OptionalFieldsAreNeverEmittedLive).
func TestSkeleton_CustomerIDAppearsInOptionalFieldsSection(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("subscriptions", "create")

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}

	inOptionalSection := false
	found := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Optional fields you may add") {
			inOptionalSection = true
			continue
		}
		if inOptionalSection && !strings.HasPrefix(trimmed, "//") {
			break // optional-fields comment block ended
		}
		if inOptionalSection && strings.Contains(trimmed, "customer_id") {
			found = true
		}
	}
	if !found {
		t.Error("customer_id does not appear in the optional-fields comment block")
	}

	body := stripComments(out)
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("skeleton is not valid JSON: %v", err)
	}
	if _, ok := v["customer_id"]; ok {
		t.Error("customer_id must not be emitted as live JSON — it is optional")
	}
}

// Cyclic $refs must not hang or overflow. This is the property the spike proved.
func TestSkeleton_TerminatesOnCyclicSchemas(t *testing.T) {
	reg := testRegistry(t)
	for _, action := range []string{"create", "update"} {
		cmd, ok := reg.Lookup("subscriptions", action)
		if !ok {
			continue
		}
		if _, err := Skeleton(cmd); err != nil {
			t.Errorf("subscriptions %s: %v", action, err)
		}
	}
}

// Two runs on the same command must be byte-identical: Go's encoding/json sorts
// map keys alphabetically in MarshalIndent, and the optional-fields comment
// list is explicitly sorted too, so nothing here should depend on map
// iteration order.
func TestSkeleton_IsDeterministicAcrossRuns(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("subscriptions", "create")

	first, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	for i := 0; i < 20; i++ {
		next, err := Skeleton(cmd)
		if err != nil {
			t.Fatalf("Skeleton: %v", err)
		}
		if next != first {
			t.Fatalf("Skeleton output not deterministic across runs (iteration %d)\nfirst:\n%s\nnext:\n%s", i, first, next)
		}
	}
}

// A body with only optional fields (no required fields at all) must still
// produce a useful skeleton: valid JSON (even if "{}"), plus a populated
// optional-fields comment block so the user has something to go on. Several
// "update" operations have this shape because every field is optional-to-change.
func TestSkeleton_HandlesBodyWithNoRequiredFields(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := findCommandWithNoRequiredBodyFields(t, reg)
	if !ok {
		t.Skip("no registered command has a body with zero required fields")
	}

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton(%s %s): %v", cmd.Resource, cmd.Action, err)
	}

	body := stripComments(out)
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("skeleton is not valid JSON: %v\n%s", err, body)
	}

	fields := BodyFields(cmd)
	if len(fields) == 0 {
		t.Fatalf("test setup: %s %s has no body fields at all", cmd.Resource, cmd.Action)
	}
	if !strings.Contains(out, "Optional fields you may add") {
		t.Errorf("%s %s: skeleton with no required fields must still list optional fields for guidance", cmd.Resource, cmd.Action)
	}
	for _, f := range fields {
		if !strings.Contains(out, f.Name) {
			t.Errorf("%s %s: optional field %q from the schema is missing from the skeleton entirely", cmd.Resource, cmd.Action, f.Name)
		}
	}
}

func findCommandWithNoRequiredBodyFields(t *testing.T, reg *Registry) (Command, bool) {
	t.Helper()
	for _, cmd := range reg.Commands() {
		fields := BodyFields(cmd)
		if len(fields) == 0 {
			continue
		}
		hasRequired := false
		for _, f := range fields {
			if f.Required {
				hasRequired = true
				break
			}
		}
		if !hasRequired {
			return cmd, true
		}
	}
	return Command{}, false
}

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
