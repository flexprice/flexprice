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

// InvoiceResponse has a top-level "total" field that is a string dollar
// amount (e.g. "150"), not a pagination count. When that amount happens to
// be a whole number, strconv.Atoi on it succeeds, and a naive reader mistakes
// the invoice for a 150-record paginated list. Real envelopes carry total as
// a JSON number alongside a "pagination" object or an "items" array.
func TestPageInfo_InvoiceLikeResponseIsNotMisreadAsPaginated(t *testing.T) {
	raw := []byte(`{"id":"inv_1","total":"150","currency":"usd","line_items":[{"id":"li_1"},{"id":"li_2"}]}`)

	info, err := PageInfo(raw)
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if info != (Page{}) {
		t.Errorf("info = %+v, want zero value (not treated as a paginated envelope)", info)
	}
}

// Count is derived by scanning top-level array fields; when more than one
// exists, the selection must be deterministic rather than depending on Go's
// randomized map iteration order.
func TestPageInfo_CountIsDeterministicAcrossMultipleArrayFields(t *testing.T) {
	raw := []byte(`{"total":5,"limit":10,"offset":0,"alpha_items":[{"id":"a"}],"beta_items":[{"id":"b"},{"id":"c"}]}`)

	var first int
	for i := 0; i < 20; i++ {
		info, err := PageInfo(raw)
		if err != nil {
			t.Fatalf("PageInfo: %v", err)
		}
		if i == 0 {
			first = info.Count
			continue
		}
		if info.Count != first {
			t.Fatalf("Count is nondeterministic: iteration %d got %d, want %d", i, info.Count, first)
		}
	}
}

// A response cut short mid-stream (e.g. a network blip during an --all loop)
// is genuinely malformed JSON, not merely "not an object" — it must surface
// as an error so the caller can distinguish a failed page from a completed
// pagination run.
func TestPageInfo_MalformedJSONIsAnError(t *testing.T) {
	_, err := PageInfo([]byte(`{"total":1,"items":[{"id":"a"}`))
	if err == nil {
		t.Fatal("PageInfo on truncated JSON: want error, got nil")
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
