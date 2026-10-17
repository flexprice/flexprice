package main

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Validate checks that an expectation has exactly one operator set.
func (e *Expectation) Validate() error {
	n := 0
	for _, set := range []bool{
		e.Equals != nil, e.NotEquals != nil, e.Exists != nil, e.Contains != nil,
		e.NotEmpty != nil, e.Matches != "", e.Gt != nil, e.Gte != nil,
		e.Lt != nil, e.Lte != nil, e.LenEq != nil, e.LenGte != nil,
		e.Approx != nil, e.AnyEq != nil, e.AnyGt != nil,
	} {
		if set {
			n++
		}
	}
	if n == 0 {
		return fmt.Errorf("path %q: no operator set (equals, exists, contains, gt, len_gte, approx, any_eq, ...)", e.Path)
	}
	if n > 1 {
		return fmt.Errorf("path %q: %d operators set, expected exactly one", e.Path, n)
	}
	if e.Approx != nil && e.Approx.Epsilon < 0 {
		return fmt.Errorf("path %q: approx.epsilon must be >= 0", e.Path)
	}
	return nil
}

// Eval evaluates the expectation against a response document. Expected values
// are rendered through the template context first.
func (e *Expectation) Eval(doc any, rc *RenderCtx) error {
	val, found := GetPath(doc, e.Path)

	if e.Exists != nil {
		if *e.Exists != found {
			return fmt.Errorf("path %q: exists=%v, but found=%v", e.Path, *e.Exists, found)
		}
		return nil
	}
	if !found {
		return fmt.Errorf("path %q: not found in response (body keys: %s)", e.Path, topKeys(doc))
	}

	render := func(v any) (any, error) { return rc.Render(v) }

	switch {
	case e.Equals != nil:
		want, err := render(e.Equals)
		if err != nil {
			return err
		}
		if !looseEqual(val, want) {
			return fmt.Errorf("path %q: expected %s, got %s", e.Path, compact(want), compact(val))
		}
	case e.NotEquals != nil:
		want, err := render(e.NotEquals)
		if err != nil {
			return err
		}
		if looseEqual(val, want) {
			return fmt.Errorf("path %q: expected anything but %s", e.Path, compact(want))
		}
	case e.Contains != nil:
		want, err := render(e.Contains)
		if err != nil {
			return err
		}
		ok, err := contains(val, want)
		if err != nil {
			return fmt.Errorf("path %q: %v", e.Path, err)
		}
		if !ok {
			return fmt.Errorf("path %q: %s does not contain %s", e.Path, compact(val), compact(want))
		}
	case e.NotEmpty != nil:
		empty := isEmpty(val)
		if *e.NotEmpty == empty {
			return fmt.Errorf("path %q: expected not_empty=%v, got %s", e.Path, *e.NotEmpty, compact(val))
		}
	case e.Matches != "":
		pat, err := render(e.Matches)
		if err != nil {
			return err
		}
		re, err := regexp.Compile(fmt.Sprintf("%v", pat))
		if err != nil {
			return fmt.Errorf("path %q: bad regex: %v", e.Path, err)
		}
		if !re.MatchString(stringify(val)) {
			return fmt.Errorf("path %q: %q does not match /%v/", e.Path, stringify(val), pat)
		}
	case e.Gt != nil:
		return e.numericCmp(val, e.Gt, rc, "gt", func(a, b float64) bool { return a > b })
	case e.Gte != nil:
		return e.numericCmp(val, e.Gte, rc, "gte", func(a, b float64) bool { return a >= b })
	case e.Lt != nil:
		return e.numericCmp(val, e.Lt, rc, "lt", func(a, b float64) bool { return a < b })
	case e.Lte != nil:
		return e.numericCmp(val, e.Lte, rc, "lte", func(a, b float64) bool { return a <= b })
	case e.LenEq != nil:
		n, err := lengthOf(val)
		if err != nil {
			return fmt.Errorf("path %q: %v", e.Path, err)
		}
		if n != *e.LenEq {
			return fmt.Errorf("path %q: expected len == %d, got %d", e.Path, *e.LenEq, n)
		}
	case e.LenGte != nil:
		n, err := lengthOf(val)
		if err != nil {
			return fmt.Errorf("path %q: %v", e.Path, err)
		}
		if n < *e.LenGte {
			return fmt.Errorf("path %q: expected len >= %d, got %d", e.Path, *e.LenGte, n)
		}
	case e.Approx != nil:
		want, err := render(e.Approx.Value)
		if err != nil {
			return err
		}
		a, ok1 := toFloat(val)
		b, ok2 := toFloat(want)
		if !ok1 || !ok2 {
			return fmt.Errorf("path %q: approx requires numbers, got %s vs %s", e.Path, compact(val), compact(want))
		}
		if math.Abs(a-b) > e.Approx.Epsilon {
			return fmt.Errorf("path %q: |%v - %v| > epsilon %v", e.Path, a, b, e.Approx.Epsilon)
		}
	case e.AnyEq != nil:
		want, err := render(e.AnyEq)
		if err != nil {
			return err
		}
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("path %q: any_eq requires an array (use a wildcard path like items.*.id), got %s", e.Path, compact(val))
		}
		for _, el := range arr {
			if looseEqual(el, want) {
				return nil
			}
		}
		return fmt.Errorf("path %q: no element equals %s (in %s)", e.Path, compact(want), compact(val))
	case e.AnyGt != nil:
		want, err := render(e.AnyGt)
		if err != nil {
			return err
		}
		b, ok := toFloat(want)
		if !ok {
			return fmt.Errorf("path %q: any_gt requires a numeric bound, got %s", e.Path, compact(want))
		}
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("path %q: any_gt requires an array (use a wildcard path), got %s", e.Path, compact(val))
		}
		for _, el := range arr {
			if a, ok := toFloat(el); ok && a > b {
				return nil
			}
		}
		return fmt.Errorf("path %q: no element > %v (in %s)", e.Path, b, compact(val))
	}
	return nil
}

func (e *Expectation) numericCmp(val, bound any, rc *RenderCtx, op string, cmp func(a, b float64) bool) error {
	want, err := rc.Render(bound)
	if err != nil {
		return err
	}
	a, ok1 := toFloat(val)
	b, ok2 := toFloat(want)
	if !ok1 || !ok2 {
		return fmt.Errorf("path %q: %s requires numbers, got %s vs %s", e.Path, op, compact(val), compact(want))
	}
	if !cmp(a, b) {
		return fmt.Errorf("path %q: expected %v %s %v", e.Path, a, op, b)
	}
	return nil
}

// MatchError checks an ErrorExpectation against the actual error.
func (ee *ErrorExpectation) MatchError(err error, status int, rc *RenderCtx) error {
	if ee.Contains != "" {
		want, rerr := rc.RenderString(ee.Contains)
		if rerr != nil {
			return rerr
		}
		needle := fmt.Sprintf("%v", want)
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(needle)) {
			return fmt.Errorf("error %q does not contain %q", truncateStr(err.Error(), 200), needle)
		}
	}
	if ee.Status != 0 {
		if status == 0 {
			return fmt.Errorf("expected error status %d, but no status could be extracted from the error (error: %s)", ee.Status, truncateStr(err.Error(), 200))
		}
		if status != ee.Status {
			return fmt.Errorf("expected error status %d, got %d (error: %s)", ee.Status, status, truncateStr(err.Error(), 200))
		}
	}
	return nil
}

// ---------- comparison helpers ----------

// looseEqual compares JSON-ish values forgivingly: numbers compare
// numerically across types, and numeric strings compare equal to numbers
// (the API returns decimal amounts as strings).
func looseEqual(a, b any) bool {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			return as == bs
		}
	}
	if ab, aok := a.(bool); aok {
		if bb, bok := b.(bool); bok {
			return ab == bb
		}
	}
	return reflect.DeepEqual(normalizeJSON(a), normalizeJSON(b))
}

// toFloat converts numbers and numeric strings to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func contains(haystack, needle any) (bool, error) {
	switch h := haystack.(type) {
	case string:
		return strings.Contains(h, fmt.Sprintf("%v", needle)), nil
	case []any:
		for _, el := range h {
			if looseEqual(el, needle) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		key := fmt.Sprintf("%v", needle)
		_, ok := h[key]
		return ok, nil
	}
	return false, fmt.Errorf("contains requires a string, array, or object, got %T", haystack)
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func lengthOf(v any) (int, error) {
	switch x := v.(type) {
	case string:
		return len(x), nil
	case []any:
		return len(x), nil
	case map[string]any:
		return len(x), nil
	}
	return 0, fmt.Errorf("len_* requires a string, array, or object, got %T", v)
}

// normalizeJSON round-trips a value through JSON so types are canonical.
func normalizeJSON(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return out
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return compact(v)
}

// compact renders a value as short JSON for error messages.
func compact(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncateStr(string(data), 200)
}

func topKeys(doc any) string {
	m, ok := doc.(map[string]any)
	if !ok {
		return fmt.Sprintf("(%T)", doc)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) > 12 {
		keys = keys[:12]
	}
	return strings.Join(keys, ", ")
}
