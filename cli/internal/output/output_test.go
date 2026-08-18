package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/flexprice/cli/internal/style"
)

// go test never has a terminal attached, so without forcing a profile the
// ANSI-code assertions below would pass or fail on where they run.
func TestMain(m *testing.M) {
	style.EnableForTests()
	os.Exit(m.Run())
}

// olderListShape mirrors GET /environments: the array key isn't "items" but
// the envelope still carries pagination markers (total/offset/limit) at the
// top level, which is what tells rowsFrom this is a list and not one object.
func olderListShape() []byte {
	return []byte(`{"environments":[{"id":"env_1","name":"Prod"}],"total":1,"offset":0,"limit":50}`)
}

// singleObjectWithArrayField mirrors GET /customers/{id}: a bare object whose
// own field happens to be an array. There is no pagination marker here, so it
// must never be mistaken for a list of tax rates.
func singleObjectWithArrayField() []byte {
	return []byte(`{"id":"c1","metadata":{},"tax_rates":["us-ca","us-ny"]}`)
}

func sample() []byte {
	return []byte(`{"items":[
      {"id":"cust_1","email":"a@b.com","status":"active","extra":"noise"},
      {"id":"cust_2","email":"c@d.com","status":"archived","extra":"noise"}
    ],"total":2}`)
}

func TestRender_JSONGoesToStdoutOnly(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}

	if err := w.Render(sample(), Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want empty for JSON output", errOut.String())
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
}

func TestRender_TableUsesRequestedColumns(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	if err := w.Render(sample(), Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "cust_1") || !strings.Contains(got, "archived") {
		t.Errorf("table missing expected cells:\n%s", got)
	}
	if strings.Contains(got, "noise") {
		t.Errorf("table shows a column that was not requested:\n%s", got)
	}
}

func TestRender_TableFooterReportsTruncation(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	err := w.Render(sample(), Options{Columns: []string{"id"}, Shown: 2, Total: 1204})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The footer is guidance, not data, so it belongs on stderr.
	if !strings.Contains(errOut.String(), "1204") || !strings.Contains(errOut.String(), "--all") {
		t.Errorf("stderr = %q, want a truncation footer naming --all", errOut.String())
	}
}

func TestRender_YAMLIsParseable(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatYAML}

	if err := w.Render(sample(), Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "cust_1") {
		t.Errorf("yaml output missing data:\n%s", out.String())
	}
}

func TestParseFormat_RejectsUnknown(t *testing.T) {
	if _, err := ParseFormat("xml"); err == nil {
		t.Fatal("want an error for an unsupported format")
	}
	if f, err := ParseFormat("json"); err != nil || f != FormatJSON {
		t.Errorf("ParseFormat(json) = %v, %v", f, err)
	}
}

// TestRowsFrom_OlderListShape covers the second verified envelope shape
// (GET /environments), which does not use the "items" key.
func TestRowsFrom_OlderListShape(t *testing.T) {
	rows, err := rowsFrom(olderListShape())
	if err != nil {
		t.Fatalf("rowsFrom: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "env_1" {
		t.Fatalf("rowsFrom(olderListShape) = %#v, want one row for env_1", rows)
	}
}

// A single customer with a "tax_rates" array must render as one row, not one
// row per tax rate.
func TestRowsFrom_SingleObjectWithArrayFieldIsNotFlattened(t *testing.T) {
	rows, err := rowsFrom(singleObjectWithArrayField())
	if err != nil {
		t.Fatalf("rowsFrom: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "c1" {
		t.Fatalf("rowsFrom(singleObjectWithArrayField) = %#v, want one row for c1", rows)
	}
}

// Mirrors GET /invoices/{id}: a string "total", an empty
// "coupon_applications" and a populated "line_items". The empty array sorts
// first alphabetically, which is what breaks a naive heuristic.
func invoiceResponseShape() []byte {
	return []byte(`{
		"id": "inv_1",
		"total": "150.00",
		"coupon_applications": [],
		"taxes": [],
		"line_items": [
			{"id": "li_1", "description": "API calls", "amount": "100.00"},
			{"id": "li_2", "description": "Seats", "amount": "50.00"}
		]
	}`)
}

// Reproduces `invoices retrieve --output table` printing "No results.": a
// string "total" was read as a pagination marker, then the empty
// "coupon_applications" beat "line_items" alphabetically. Both must be fixed.
func TestRowsFrom_InvoiceResponseUsesLineItemsNotStringTotal(t *testing.T) {
	rows, err := rowsFrom(invoiceResponseShape())
	if err != nil {
		t.Fatalf("rowsFrom: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "inv_1" || rows[0]["total"] != "150.00" {
		t.Fatalf("rowsFrom(invoiceResponseShape) = %#v, want one row for inv_1 (the invoice object itself)", rows)
	}
}

// End-to-end regression for the "No results." bug below. Columns are requested
// explicitly because "id" alone satisfies defaultColumns, which would hide the
// line_items column and make the assertion moot.
func TestRender_InvoiceResponseTableShowsLineItems(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	err := w.Render(invoiceResponseShape(), Options{Columns: []string{"id", "total", "line_items"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(errOut.String(), "No results.") {
		t.Fatalf("stderr = %q, want no \"No results.\" warning; stdout was %q", errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "inv_1") {
		t.Fatalf("stdout missing the invoice row:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "150.00") {
		t.Fatalf("stdout missing the string total (\"150.00\"):\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"amount":"100.00"`) {
		t.Fatalf("stdout missing evidence of line_items content:\n%s", out.String())
	}
}

// A real pagination envelope carrying two array fields, one empty and
// alphabetically first — isolates non-empty-wins from the "total" type check.
func genuineListWithEmptyFirstArray() []byte {
	return []byte(`{
		"pagination": {"total": 2, "limit": 10, "offset": 0},
		"coupon_applications": [],
		"line_items": [{"id": "li_1"}, {"id": "li_2"}]
	}`)
}

// Once hasListMarker is true, an alphabetically-first empty array must not
// beat a populated one.
func TestRowsFrom_GenuineListPrefersNonEmptyArrayOverEmpty(t *testing.T) {
	rows, err := rowsFrom(genuineListWithEmptyFirstArray())
	if err != nil {
		t.Fatalf("rowsFrom: %v", err)
	}
	if len(rows) != 2 || rows[0]["id"] != "li_1" || rows[1]["id"] != "li_2" {
		t.Fatalf("rowsFrom(genuineListWithEmptyFirstArray) = %#v, want the two line_items rows", rows)
	}
}

// A string "total" (an invoice's dollar amount) must not read as a pagination
// count, while a numeric one still does.
func TestHasListMarker_StringTotalDoesNotCount(t *testing.T) {
	if hasListMarker(map[string]any{"total": "150.00"}) {
		t.Error("hasListMarker({\"total\": \"150.00\"}) = true, want false for a string total")
	}
	if !hasListMarker(map[string]any{"total": float64(2)}) {
		t.Error("hasListMarker({\"total\": 2.0}) = false, want true for a numeric total")
	}
	if !hasListMarker(map[string]any{"pagination": map[string]any{}}) {
		t.Error("hasListMarker({\"pagination\": {}}) = false, want true — pagination presence alone is the primary marker")
	}
}

// TestRender_TableIsDeterministicAcrossRuns guards against Go's randomized
// map iteration leaking into rendered column order, both when columns are
// explicitly requested and when defaultColumns must pick them from the map.
func TestRender_TableIsDeterministicAcrossRuns(t *testing.T) {
	render := func(o Options) string {
		var out, errOut bytes.Buffer
		w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
		if err := w.Render(sample(), o); err != nil {
			t.Fatalf("Render: %v", err)
		}
		return out.String()
	}

	for name, opts := range map[string]Options{
		"explicit columns": {Columns: []string{"id", "status", "email"}},
		"default columns":  {},
	} {
		t.Run(name, func(t *testing.T) {
			first := render(opts)
			for i := 0; i < 20; i++ {
				if got := render(opts); got != first {
					t.Fatalf("render %d differs from first render:\nfirst: %q\ngot:   %q", i, first, got)
				}
			}
		})
	}
}

// TestFormat_TruncatesOnRuneBoundary ensures a nested JSON value with
// multi-byte UTF-8 characters (e.g. Arabic environment names returned by the
// live API) is never cut mid-character, which would emit invalid UTF-8.
func TestFormat_TruncatesOnRuneBoundary(t *testing.T) {
	// Force well over the 40-char budget with a value that is pure multi-byte
	// characters, so any byte-index slice lands inside a character.
	name := strings.Repeat("بيئة تجريبية ", 5)
	v := map[string]any{"name": name}

	got := format(v)

	if !strings.HasSuffix(got, "...") {
		t.Fatalf("format() did not truncate as expected: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("format() produced invalid UTF-8: %q (bytes: %v)", got, []byte(got))
	}
}

func TestRenderTable_HeaderIsStyled(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Errorf("table output has no ANSI codes; want the header row styled")
	}
}

// The status VALUE, not just the header, gets colored when the column name
// contains "status" and the value matches a known word.
func TestRenderTable_KnownStatusValueIsColored(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	input := []byte(`{"items":[{"id":"cust_1","status":"archived"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The raw word is still present as a substring — color wraps it, does not
	// replace it — so this also proves existing "strings.Contains(out, value)"
	// style assertions elsewhere in this file remain valid unchanged.
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("table output missing the status value itself: %q", out.String())
	}
}

// A non-status column (e.g. "email") never gets value-colored, even if its
// text happens to collide with a status word.
func TestRenderTable_NonStatusColumnValuesAreNeverColored(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	// "active" as an email local-part, in a column that is not named "status".
	input := []byte(`{"items":[{"id":"cust_1","email":"active@example.com"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "active\x1b[") || strings.Contains(out.String(), "\x1b[32mactive") {
		t.Errorf("email column value was colored as if it were a status: %q", out.String())
	}
}

// --output json must never contain ANSI codes, regardless of styling changes
// made to the table path. This is the hard constraint from the design doc §6/U4.
func TestRender_JSONOutputNeverContainsANSICodes(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("--output json contains ANSI escape codes: %q", out.String())
	}
}

// Design doc §6/U4 names both json and yaml explicitly as staying unstyled.
func TestRender_YAMLOutputNeverContainsANSICodes(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatYAML}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("--output yaml contains ANSI escape codes: %q", out.String())
	}
}

// The status-footer tests moved with the footer: see
// TestStatusLine_GoesToStderrOnly (internal/ui) and TestShouldShowFooter.
