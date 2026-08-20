package output

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// JSON output is a contract other tools parse, so it is pinned exactly.
func TestGolden_JSONOutputIsStable(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	goldenPath := filepath.Join("testdata", "customers_list.golden.json")
	if *update {
		if err := os.WriteFile(goldenPath, out.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/output -update): %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(out.Bytes()), bytes.TrimSpace(want)) {
		t.Errorf("JSON output changed.\n got:\n%s\nwant:\n%s", out.String(), want)
	}
}

// Table rendering is presentation, not contract: assert it contains the data
// rather than pinning the exact layout, so column widths can change freely.
func TestTableOutput_ContainsTheData(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"cust_01", "ada@example.com"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("table output missing %q:\n%s", want, out.String())
		}
	}
}
