package spec

import (
	"encoding/json"
	"testing"
)

func TestPageInfo_ReadsListResponseEnvelope(t *testing.T) {
	raw := []byte(`{"items":[{"id":"a"},{"id":"b"}],"pagination":{"total":1204,"limit":2,"offset":0}}`)

	info, err := PageInfo(raw)
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if info.Total != 1204 {
		t.Errorf("Total = %d, want 1204", info.Total)
	}
	if info.Count != 2 {
		t.Errorf("Count = %d, want 2", info.Count)
	}
}

// Older endpoints (environments) use a named array and top-level pagination.
func TestPageInfo_HandlesLegacyEnvelope(t *testing.T) {
	raw := []byte(`{"environments":[{"id":"e1"}],"total":1,"offset":0,"limit":50}`)

	info, err := PageInfo(raw)
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if info.Total != 1 || info.Count != 1 {
		t.Errorf("info = %+v, want Total 1 Count 1", info)
	}
}

func TestPageInfo_NonListResponseIsNotAnError(t *testing.T) {
	info, err := PageInfo([]byte(`{"id":"cust_1"}`))
	if err != nil {
		t.Fatalf("PageInfo on a single object: %v", err)
	}
	if info.Total != 0 || info.Count != 0 {
		t.Errorf("info = %+v, want zeroes for a single object", info)
	}
}

func TestApplyPaging_SetsQueryForGET(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := reg.Lookup("customers", "retrieve")
	if !ok {
		t.Skip("customers retrieve not registered")
	}

	req := Request{Method: "GET", Path: "/customers", Query: map[string][]string{}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 40})

	if got := req.Query.Get("limit"); got != "20" {
		t.Errorf("query limit = %q, want 20", got)
	}
	if got := req.Query.Get("offset"); got != "40" {
		t.Errorf("query offset = %q, want 40", got)
	}
}

func TestApplyPaging_SetsBodyForSearchOperations(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "list") // POST /customers/search

	req := Request{Method: "POST", Path: "/customers/search", Body: map[string]any{}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 40})

	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body = %T, want map", req.Body)
	}
	if body["limit"] != 20 || body["offset"] != 40 {
		t.Errorf("body = %v, want limit 20 offset 40", body)
	}
}

// A user-supplied limit is never overwritten by the default.
func TestApplyPaging_DoesNotOverrideAnExplicitValue(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "list")

	req := Request{Method: "POST", Path: "/customers/search", Body: map[string]any{"limit": 5}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 0})

	if got := req.Body.(map[string]any)["limit"]; got != 5 {
		t.Errorf("limit = %v, want the caller value 5 preserved", got)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
