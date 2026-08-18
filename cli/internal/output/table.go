package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// listMarkers are top-level keys that only appear on paginated list envelopes,
// never on a single-object response. Their presence is what lets rowsFrom tell
// "{"items":[...], "pagination":{...}}" (a list) apart from a bare object that
// happens to contain an array field, e.g. a customer with "tax_rates":[...].
var listMarkers = []string{"pagination", "total", "limit", "offset"}

// rowsFrom finds the list of rows in a response.
//
// Two envelope shapes are in use across the API:
//
//	{"items":[...], "pagination":{"total":..,"limit":..,"offset":..}}   // types.ListResponse[T]
//	{"environments":[...], "total":.., "offset":.., "limit":..}          // older shape
//
// "items" is checked first since it is the common-case key and unambiguous.
// For any other shape, a key is only treated as the row list if the envelope
// also carries a pagination marker (pagination/total/limit/offset) — that is
// what separates a genuine list response from a single object that happens to
// have an array-valued field (e.g. tax_rates on a customer). Without that
// guard, an alphabetically-first-array heuristic picks the wrong field on
// single-object responses; see output_test.go for the case this guards
// against. A single object with no array field renders as one row.
func rowsFrom(raw []byte) ([]map[string]any, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	switch v := doc.(type) {
	case []any:
		return toRows(v), nil
	case map[string]any:
		if arr, ok := v["items"].([]any); ok {
			return toRows(arr), nil
		}

		if !hasListMarker(v) {
			return []map[string]any{v}, nil
		}

		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			arr, ok := v[k].([]any)
			if !ok || !isObjectArray(arr) {
				continue
			}
			return toRows(arr), nil
		}
		return []map[string]any{v}, nil
	default:
		return nil, fmt.Errorf("unexpected response shape")
	}
}

func hasListMarker(v map[string]any) bool {
	for _, k := range listMarkers {
		if _, ok := v[k]; ok {
			return true
		}
	}
	return false
}

// isObjectArray reports whether every element is a JSON object. An empty
// array counts as an object array (vacuously true) since it carries no
// evidence either way.
func isObjectArray(arr []any) bool {
	for _, it := range arr {
		if _, ok := it.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func toRows(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (w Writer) renderTable(raw []byte, o Options) error {
	rows, err := rowsFrom(raw)
	if err != nil {
		// Unparseable as a table — fall back to JSON so the user still sees the data.
		return Writer{Out: w.Out, Err: w.Err, Format: FormatJSON}.Render(raw, o)
	}
	if len(rows) == 0 {
		w.Warn(o, "No results.")
		return nil
	}

	columns := o.Columns
	if len(columns) == 0 {
		columns = defaultColumns(rows[0])
	}

	tw := tabwriter.NewWriter(w.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(columns, "\t")))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = format(row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	if o.Total > o.Shown && o.Shown > 0 {
		w.Warn(o, "\nshowing %d of %d — use --all to fetch every page", o.Shown, o.Total)
	}
	return nil
}

// defaultColumns is the fallback when commands.yaml declares none: id, a name-ish
// field, status, and a timestamp. Design doc §3, Round 3.
//
// Both branches are deterministic — the preferred list is a fixed order, and
// the fallback sorts map keys — so repeated renders of the same payload
// produce byte-identical column sets despite Go's randomized map iteration.
func defaultColumns(row map[string]any) []string {
	preferred := []string{"id", "name", "external_id", "email", "status", "created_at"}
	var out []string
	for _, p := range preferred {
		if _, ok := row[p]; ok {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out
	}

	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	return keys
}

const maxCellRunes = 40

func format(v any) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		return fmt.Sprintf("%t", t)
	case map[string]any, []any:
		b, _ := json.Marshal(t)
		return truncateRunes(string(b), maxCellRunes)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// truncateRunes cuts s to at most max runes, appending "...". Slicing by byte
// index (s[:37]) can split a multi-byte UTF-8 character in half — the API
// returns non-ASCII values (e.g. environment names like "بيئة تجريبية"), and a
// mid-character cut produces invalid UTF-8 in terminal output. Cutting on the
// rune boundary avoids that regardless of script.
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-3]) + "..."
}
