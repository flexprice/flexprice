package spec

import (
	"encoding/json"
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
// nests pagination under "pagination", while older endpoints put total, limit and
// offset at the top level next to a named array.
func PageInfo(raw []byte) (Page, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Page{}, nil // not an object: nothing to page
	}

	var page Page
	if nested, ok := doc["pagination"].(map[string]any); ok {
		page.Total = intOf(nested["total"])
		page.Limit = intOf(nested["limit"])
		page.Offset = intOf(nested["offset"])
	} else {
		page.Total = intOf(doc["total"])
		page.Limit = intOf(doc["limit"])
		page.Offset = intOf(doc["offset"])
	}

	for key, value := range doc {
		if key == "pagination" {
			continue
		}
		if arr, ok := value.([]any); ok {
			page.Count = len(arr)
			break
		}
	}
	return page, nil
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
