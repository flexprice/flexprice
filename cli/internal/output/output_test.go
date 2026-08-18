package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

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

// TestRowsFrom_SingleObjectWithArrayFieldIsNotFlattened guards against a
// bare alphabetical-first-array heuristic: a single customer object with a
// "tax_rates" array must render as one row (the customer), not as one row
// per tax rate string.
func TestRowsFrom_SingleObjectWithArrayFieldIsNotFlattened(t *testing.T) {
	rows, err := rowsFrom(singleObjectWithArrayField())
	if err != nil {
		t.Fatalf("rowsFrom: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "c1" {
		t.Fatalf("rowsFrom(singleObjectWithArrayField) = %#v, want one row for c1", rows)
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
