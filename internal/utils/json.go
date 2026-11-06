package utils

import (
	"bytes"
	"encoding/json"
)

// MergeJSON overlays the non-empty JSON fields of patch onto base, decodes the result into out
// and reports whether anything changed.
func MergeJSON(base, patch, out any) (bool, error) {
	before, err := json.Marshal(base)
	if err != nil {
		return false, err
	}
	baseFields, err := ToJSONMap(base)
	if err != nil {
		return false, err
	}
	patchFields, err := ToJSONMap(patch)
	if err != nil {
		return false, err
	}
	overlayFields(baseFields, patchFields)
	after, err := json.Marshal(baseFields)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(after, out); err != nil {
		return false, err
	}
	normalized, err := json.Marshal(out)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, normalized), nil
}

// ToJSONMap returns v's JSON object fields; null yields an empty map.
func ToJSONMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]any{}
	}
	return fields, nil
}

// overlayFields copies src into dst, skipping null and empty-string values and merging nested objects.
func overlayFields(dst, src map[string]any) {
	for k, v := range src {
		switch v := v.(type) {
		case nil:
			continue
		case string:
			if v == "" {
				continue
			}
		case map[string]any:
			nested, _ := dst[k].(map[string]any)
			if nested == nil {
				nested = map[string]any{}
			}
			overlayFields(nested, v)
			dst[k] = nested
			continue
		}
		dst[k] = v
	}
}
