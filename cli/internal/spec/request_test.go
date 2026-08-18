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

// Rejected client-side: the server's decoder would reject a type-mismatched
// body with a generic "Invalid request format" and no field name.
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

// The --all loop rebuilds the request per page from the same Input, so
// `payments list --status succeeded --all` would silently drop the filter after
// page one if consumed flags were deleted from the caller's map.
func TestBuildRequest_DoesNotMutateCallerFlags(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := reg.Lookup("payments", "list")
	if !ok {
		t.Skip("payments list not registered")
	}

	in := Input{Flags: map[string]string{"status": "succeeded"}}

	if _, err := BuildRequest(cmd, in); err != nil {
		t.Fatalf("first BuildRequest: %v", err)
	}
	if _, ok := in.Flags["status"]; !ok {
		t.Fatal("caller's Flags map lost \"status\" after the first BuildRequest call")
	}

	req2, err := BuildRequest(cmd, in)
	if err != nil {
		t.Fatalf("second BuildRequest: %v", err)
	}
	if got := req2.Query.Get("status"); got != "succeeded" {
		t.Errorf("second BuildRequest's query status = %q, want \"succeeded\" — the filter was lost on rebuild", got)
	}
}
