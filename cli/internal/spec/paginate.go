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

// Ignores the echoed offset: the API returns offset == limit even when no
// offset was sent, so the caller tracks how many it has actually seen.
func (p Page) HasMore(seen int) bool {
	return p.Total > 0 && seen < p.Total
}

// A bare top-level "total" is not evidence on its own — InvoiceResponse's is a
// string amount — so detection needs a numeric total plus a real array.
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

// Prefers a literal "items" key, falling back to scanning only when absent.
func arrayCount(doc map[string]any) int {
	if items, ok := doc["items"].([]any); ok {
		return len(items)
	}
	if key, ok := findObjectArrayKey(doc); ok {
		return len(doc[key].([]any))
	}
	return 0
}

// Chooses alphabetically when more than one array exists, rather than relying
// on Go's randomized map order.
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

// Sets limit/offset in the query for GET or the body for POST search
// operations, never overwriting values the caller already supplied.
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
