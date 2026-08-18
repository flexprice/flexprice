package spec

import (
	"encoding/json"
	"sort"
	"strconv"
)

type Paging struct {
	Limit  int
	Offset int
}

// Page describes one response page. Total is 0 when the response is not a list.
type Page struct {
	Count  int
	Total  int
	Offset int
	Limit  int
}

// HasMore reports whether another page exists.
//
// It deliberately ignores the response's echoed offset: the API was observed
// returning offset == limit for a request that sent no offset at all, so the
// echo cannot be treated as "records already consumed". The caller tracks how
// many it has actually seen and passes that in as seen.
func (p Page) HasMore(seen int) bool {
	return p.Total > 0 && seen < p.Total
}

// PageInfo reads the pagination envelope. Two shapes exist: types.ListResponse
// nests pagination under "pagination", while older endpoints put total, limit
// and offset at the top level next to a named array (or an "items" array).
//
// A bare top-level "total"/"limit"/"offset" is not, by itself, evidence of a
// paginated envelope: InvoiceResponse has a top-level "total" field that is a
// string dollar amount (e.g. "150"), not a pagination count, and would
// otherwise be misread as one. Detection requires one of: a "pagination"
// sub-object, an unambiguous "items" array, or (for the legacy shape) total/
// limit/offset genuinely typed as JSON numbers plus a top-level array of
// objects backing them up.
func PageInfo(raw []byte) (Page, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		if json.Valid(raw) {
			return Page{}, nil // valid JSON, just not an object: nothing to page
		}
		return Page{}, err // malformed/truncated body: caller must not treat this as "done"
	}

	if nested, ok := doc["pagination"].(map[string]any); ok {
		return Page{
			Total:  intOf(nested["total"]),
			Limit:  intOf(nested["limit"]),
			Offset: intOf(nested["offset"]),
			Count:  arrayCount(doc),
		}, nil
	}

	if items, ok := doc["items"].([]any); ok {
		return Page{
			Total:  intOf(doc["total"]),
			Limit:  intOf(doc["limit"]),
			Offset: intOf(doc["offset"]),
			Count:  len(items),
		}, nil
	}

	total, ok := doc["total"].(float64)
	if !ok {
		return Page{}, nil
	}
	if v, present := doc["limit"]; present {
		if _, isNum := v.(float64); !isNum {
			return Page{}, nil
		}
	}
	if v, present := doc["offset"]; present {
		if _, isNum := v.(float64); !isNum {
			return Page{}, nil
		}
	}

	key, ok := findObjectArrayKey(doc)
	if !ok {
		return Page{}, nil
	}
	return Page{
		Total:  int(total),
		Limit:  intOf(doc["limit"]),
		Offset: intOf(doc["offset"]),
		Count:  len(doc[key].([]any)),
	}, nil
}

// arrayCount finds the item count for an envelope already confirmed to be
// paginated. It prefers a literal "items" key and only falls back to
// scanning for an array field when that key is absent.
func arrayCount(doc map[string]any) int {
	if items, ok := doc["items"].([]any); ok {
		return len(items)
	}
	if key, ok := findObjectArrayKey(doc); ok {
		return len(doc[key].([]any))
	}
	return 0
}

// findObjectArrayKey returns the top-level key holding an array of objects,
// choosing deterministically (alphabetically) when more than one exists
// rather than relying on Go's randomized map iteration order.
func findObjectArrayKey(doc map[string]any) (string, bool) {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k == "pagination" || k == "total" || k == "limit" || k == "offset" {
			continue
		}
		arr, ok := doc[k].([]any)
		if !ok {
			continue
		}
		if len(arr) > 0 {
			if _, isObj := arr[0].(map[string]any); !isObj {
				continue
			}
		}
		return k, true
	}
	return "", false
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

// ApplyPaging sets limit and offset where the operation accepts them: the query
// string for GET, the request body for the POST search operations that back list.
// Values the caller already supplied are never overwritten.
func ApplyPaging(req *Request, cmd Command, p Paging) {
	if p.Limit <= 0 {
		return
	}

	if req.Method == "GET" {
		if req.Query == nil {
			return
		}
		if req.Query.Get("limit") == "" {
			req.Query.Set("limit", strconv.Itoa(p.Limit))
		}
		if p.Offset > 0 && req.Query.Get("offset") == "" {
			req.Query.Set("offset", strconv.Itoa(p.Offset))
		}
		return
	}

	// Only set body paging when the schema actually declares the fields.
	accepts := map[string]bool{}
	for _, f := range BodyFields(cmd) {
		accepts[f.Name] = true
	}
	if !accepts["limit"] {
		return
	}

	body, ok := req.Body.(map[string]any)
	if !ok {
		body = map[string]any{}
		req.Body = body
	}
	if _, set := body["limit"]; !set {
		body["limit"] = p.Limit
	}
	if _, set := body["offset"]; !set && accepts["offset"] {
		body["offset"] = p.Offset
	}
}
