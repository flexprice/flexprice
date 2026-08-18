package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/flexprice/cli/internal/style"
)

// numericListMarkers are top-level keys that indicate a paginated list
// envelope only when their JSON value is a number — genuine pagination
// envelopes always encode total/limit/offset as numbers. A same-named field
// on a single-object response can hold any other type, e.g. InvoiceResponse's
// top-level "total" is a string (the invoice amount, "150.00"), not a count.
var numericListMarkers = []string{"total", "limit", "offset"}

// rowsFrom finds the list of rows in a response.
//
// Two envelope shapes are in use across the API:
//
//	{"items":[...], "pagination":{"total":..,"limit":..,"offset":..}}   // types.ListResponse[T]
//	{"environments":[...], "total":.., "offset":.., "limit":..}          // older shape
//
// "items" is checked first since it is the common-case key and unambiguous.
// For any other shape, a key is only treated as the row list if the envelope
// also carries a pagination marker (pagination/total/limit/offset, with
// total/limit/offset only counting when they are JSON numbers — see
// hasListMarker) — that is what separates a genuine list response from a
// single object that happens to have an array-valued field (e.g. tax_rates
// on a customer, or InvoiceResponse's string "total" alongside its real
// "line_items" array). Without that guard, an alphabetically-first-array
// heuristic picks the wrong field on single-object responses; see
// output_test.go for the cases this guards against. Among several
// array-valued keys, the first non-empty array-of-objects wins (alphabetical
// among ties) rather than strictly alphabetical-first, since an empty array
// can never be the intended row source when a populated one exists alongside
// it (e.g. InvoiceResponse's empty "coupon_applications" sorting before its
// populated "line_items"). A single object with no array field renders as
// one row.
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

// hasListMarker reports whether v carries a genuine pagination envelope
// marker. "pagination" is the primary, unambiguous signal (types.ListResponse
// nests total/limit/offset under it, and no single-object response has ever
// been observed to use that key name). A bare total/limit/offset at the top
// level (the older envelope shape) only counts when its value is a JSON
// number — json.Unmarshal decodes JSON numbers as float64, so a same-named
// string field (e.g. InvoiceResponse's dollar-amount "total") is correctly
// rejected.
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

// isObjectArray reports whether every element is a JSON object. An empty
// array counts as an object array (vacuously true) since it carries no
// evidence either way — callers that must choose among several array-valued
// keys should prefer a non-empty match over an empty one, since an empty
// array can never be the intended row source when a populated one exists.
func isObjectArray(arr []any) bool {
	for _, it := range arr {
		if _, ok := it.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// toRows converts a JSON array into rows, silently dropping any element that
// isn't a JSON object (e.g. a stray null in "items"). Only the fallback
// key-scan path in rowsFrom vets its array with isObjectArray first; the
// "items" and top-level-array paths call toRows directly, so a non-object
// element there is dropped with no indication to the caller. Surfacing that
// (e.g. a dropped-element count) would mean adding a return value here and
// threading it back through rowsFrom's signature and all three call sites —
// more restructuring than this fix warrants, so it is left as-is with this
// note rather than done partially.
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
		// Emptiness is reported through RenderResult so the caller, which knows
		// the resource name, can name a next step. Reaching here directly via
		// the legacy Render path prints nothing rather than a bare
		// "No results." that suggests nothing.
		return nil
	}

	columns := o.Columns
	if len(columns) == 0 {
		columns = defaultColumns(rows[0])
	}

	// Build the full grid first so column widths can be measured on VISIBLE
	// width. text/tabwriter counts raw bytes, so ANSI escape sequences (~20
	// invisible bytes per styled cell) inflate its width calculation and
	// misalign every column once styling is on — verified directly: a styled
	// header rendered ~20 columns narrower than its own data rows. lipgloss.Width
	// is ANSI-aware and also handles wide/East-Asian runes correctly.
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

	widths := make([]int, len(columns))
	for _, cells := range grid {
		for i, cell := range cells {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	const gutter = 2
	for _, cells := range grid {
		var line strings.Builder
		for i, cell := range cells {
			line.WriteString(cell)
			// No trailing whitespace on the final column — it would be
			// invisible but would show up in golden-file comparisons and when
			// piping output into other tools.
			if i < len(cells)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gutter))
			}
		}
		if _, err := fmt.Fprintln(w.Out, line.String()); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
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

// formatCell renders one cell, applying status coloring only when the column
// name looks like a status column. Design doc §5.2: a column is "status-shaped"
// when its name contains "status", case-insensitive — not tied to any specific
// command, since 197 commands cannot each be hand-mapped without repeating the
// same maintenance trap this project has avoided everywhere else.
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
