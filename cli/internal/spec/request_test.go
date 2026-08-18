package spec

import (
	"strings"
	"testing"
)

func TestBuildRequest_SubstitutesPathParameters(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "retrieve")

	req, err := BuildRequest(cmd, Input{PositionalID: "cust_01K"})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.Path != "/customers/cust_01K" {
		t.Errorf("Path = %q, want /customers/cust_01K", req.Path)
	}
	if req.Method != "GET" {
		t.Errorf("Method = %q, want GET", req.Method)
	}
}

func TestBuildRequest_MissingRequiredPathParameterIsAnError(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "retrieve")

	if _, err := BuildRequest(cmd, Input{}); err == nil {
		t.Fatal("want an error when the required path parameter is absent")
	}
}

func TestBuildRequest_FlagsBecomeBodyForPostOperations(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	req, err := BuildRequest(cmd, Input{Flags: map[string]string{"external_id": "acme-1"}})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body = %T, want map", req.Body)
	}
	if body["external_id"] != "acme-1" {
		t.Errorf("body[external_id] = %v, want acme-1", body["external_id"])
	}
}

// --data supplies the base; flags override individual fields on top of it.
func TestBuildRequest_FlagsOverrideDataDocument(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	req, err := BuildRequest(cmd, Input{
		Data:  map[string]any{"external_id": "from-file", "name": "From File"},
		Flags: map[string]string{"external_id": "from-flag"},
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	body := req.Body.(map[string]any)
	if body["external_id"] != "from-flag" {
		t.Errorf("external_id = %v, want the flag to win", body["external_id"])
	}
	if body["name"] != "From File" {
		t.Errorf("name = %v, want the file value preserved", body["name"])
	}
}

func TestBuildRequest_UnknownFlagSuggestsTheNearestField(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	_, err := BuildRequest(cmd, Input{Flags: map[string]string{"externl_id": "x"}})
	if err == nil {
		t.Fatal("want an error for an unknown flag")
	}
	if got := err.Error(); !strings.Contains(got, "external_id") {
		t.Errorf("error = %q, want a suggestion naming external_id", got)
	}
}

// A flag value that cannot be parsed as its schema type is rejected here rather
// than sent through as a raw string: the server's JSON decoder would reject a
// type-mismatched body before its own field-level validation even runs, giving a
// generic "Invalid request format" with no field name attached.
func TestBuildRequest_UncoercibleFlagValueIsAnError(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	_, err := BuildRequest(cmd, Input{Flags: map[string]string{
		"external_id":              "acme-1",
		"skip_onboarding_workflow": "not-a-bool",
	}})
	if err == nil {
		t.Fatal("want an error for a value that does not parse as the field's boolean type")
	}
	if got := err.Error(); !strings.Contains(got, "skip_onboarding_workflow") || !strings.Contains(got, "boolean") {
		t.Errorf("error = %q, want it to name the field and its expected type", got)
	}
}

func TestBodyFields_ListsSchemaProperties(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	fields := BodyFields(cmd)
	if len(fields) == 0 {
		t.Fatal("BodyFields returned nothing for createCustomer")
	}

	found := false
	for _, f := range fields {
		if f.Name == "external_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("external_id missing from %d body fields", len(fields))
	}
}
