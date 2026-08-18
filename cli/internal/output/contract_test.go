package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The status footer moved to internal/ui; this pins that it never leaks back
// into the machine-readable stream.
func TestRenderTable_WritesNothingHumanToStdout(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("profile:")) {
		t.Errorf("the status footer leaked into stdout:\n%s", out.String())
	}
}

// The renderer has no business knowing about stderr commentary at all.
func TestOptions_HasNoStatusField(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
	if err := w.Render([]byte(`{"items":[{"id":"a"}],"pagination":{"total":1}}`), Options{
		Columns: []string{"id"},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("renderer wrote to stderr without being asked: %q", errOut.String())
	}
}
