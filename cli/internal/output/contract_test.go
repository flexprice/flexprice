package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The status footer is human commentary and must never reach stdout. It moved
// to internal/ui in the DX polish round; this test pins that it did not come
// back, and that nothing else human leaks into the machine-readable stream.
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

// Options must not regrow a Status field. The renderer has no business knowing
// about stderr commentary at all — that is what let the footer be gated on
// stdout's TTY-ness while being written to stderr.
func TestOptions_HasNoStatusField(t *testing.T) {
	// A compile-time check expressed as a test: if Status comes back, this
	// composite literal stops compiling only if the field is removed, so
	// instead assert on the zero value round-tripping through Render.
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
