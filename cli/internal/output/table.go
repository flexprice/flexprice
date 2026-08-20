package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/flexprice/cli/internal/style"
)

// Only count as pagination markers when the value is a JSON number:
// InvoiceResponse's top-level "total" is a string amount ("150.00"), not a count.
var numericListMarkers = []string{"total", "limit", "offset"}

// Outside "items", a key is only the row list if the envelope also carries a
// pagination marker; among candidates the first non-empty array wins.
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
		var best []any
		haveCandidate := false
		for _, k := range keys {
			arr, ok := v[k].([]any)
			if !ok || !isObjectArray(arr) {
				continue
			}
			if !haveCandidate {
				best, haveCandidate = arr, true
				continue
			}
			if len(best) == 0 && len(arr) > 0 {
				best = arr
			}
		}
		if haveCandidate {
			return toRows(best), nil
		}
		return []map[string]any{v}, nil
	default:
		return nil, fmt.Errorf("unexpected response shape")
	}
}

// "pagination" is unambiguous; a bare total/limit/offset only counts as a
// number, which is what rejects InvoiceResponse's string "total".
func hasListMarker(v map[string]any) bool {
	if _, ok := v["pagination"]; ok {
		return true
	}
	for _, k := range numericListMarkers {
		if n, ok := v[k]; ok {
			if _, isNumber := n.(float64); isNumber {
				return true
			}
		}
	}
	return false
}

// An empty array counts as an object array: it carries no evidence either way,
// so callers prefer a non-empty match instead.
func isObjectArray(arr []any) bool {
	for _, it := range arr {
		if _, ok := it.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// Silently drops non-object elements (a stray null in "items"). Reporting them
// would mean threading a count back through rowsFrom and all three call sites.
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
		// Emptiness is reported through RenderResult, which lets the caller name
		// a next step; this legacy Render path just prints nothing.
		return nil
	}

	columns := o.Columns
	if len(columns) == 0 {
		columns = defaultColumns(rows[0])
	}

	// Built in full first so PadGrid can measure visible width across all rows.
	grid := make([][]string, 0, len(rows)+1)
	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = style.Header(strings.ToUpper(c))
	}
	grid = append(grid, header)
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = formatCell(c, row[c])
		}
		grid = append(grid, cells)
	}

	for _, line := range PadGrid(grid) {
		if _, err := fmt.Fprintln(w.Out, line); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
	}

	if o.Total > o.Shown && o.Shown > 0 {
		w.Warn(o, "\nshowing %d of %d — use --all to fetch every page", o.Shown, o.Total)
	}
	return nil
}

// Fallback when commands.yaml declares no columns. Both branches are
// deterministic despite Go's randomized map iteration.
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

// Status coloring keys off the column name rather than the command: 197
// commands cannot each be hand-mapped.
func formatCell(column string, value any) string {
	text := format(value)
	if strings.Contains(strings.ToLower(column), "status") {
		return style.StatusColor(text)
	}
	return text
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

// Cuts on a rune boundary: the API returns non-ASCII values, and a byte-index
// slice would split a multi-byte character into invalid UTF-8.
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-3]) + "..."
}
