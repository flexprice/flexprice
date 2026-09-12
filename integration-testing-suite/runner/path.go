package main

import (
	"strconv"
	"strings"
)

// GetPath extracts a value from a JSON-like document using a dotted path:
//
//	"id"                  → doc["id"]
//	"subscription.id"     → nested object field
//	"items.0.id"          → array index
//	"items.*.feature_id"  → wildcard: collects the field from every element,
//	                        returning a []any (misses are dropped)
//	"" or "."             → the document itself
//
// The second return is false when the path does not resolve.
func GetPath(doc any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return doc, true
	}
	return getTokens(doc, strings.Split(path, "."))
}

func getTokens(doc any, tokens []string) (any, bool) {
	cur := doc
	for i, tok := range tokens {
		if tok == "*" {
			arr, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			rest := tokens[i+1:]
			out := make([]any, 0, len(arr))
			for _, el := range arr {
				if len(rest) == 0 {
					out = append(out, el)
					continue
				}
				if v, ok := getTokens(el, rest); ok {
					out = append(out, v)
				}
			}
			return out, true
		}
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[tok]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 0 || idx >= len(c) {
				return nil, false
			}
			cur = c[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}
