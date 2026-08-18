package output

import (
	"bytes"
	"testing"
)

// The renderer no longer invents an empty-state message: it reports that the
// result set was empty and lets the caller, which knows the resource name, say
// something useful.
func TestRenderResult_ReportsEmptyWithoutPrinting(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	res, err := w.RenderResult([]byte(`{"items":[],"pagination":{"total":0}}`), Options{})
	if err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	if !res.Empty {
		t.Error("Empty should be true for a zero-row response")
	}
	if errOut.Len() != 0 {
		t.Errorf("renderer should not print its own empty-state text, got %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("an empty result should print no table, got %q", out.String())
	}
}

func TestRenderResult_NotEmptyWhenRowsExist(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	res, err := w.RenderResult([]byte(`{"items":[{"id":"cust_01"}],"pagination":{"total":1}}`), Options{})
	if err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	if res.Empty {
		t.Error("Empty should be false when rows exist")
	}
	if !bytes.Contains(out.Bytes(), []byte("cust_01")) {
		t.Errorf("row missing from output: %q", out.String())
	}
}

// json and yaml are machine formats: an empty list is valid output and must be
// emitted verbatim, never replaced with prose. Reporting Empty for them would
// make the caller print "No customers yet." over the top of valid JSON.
func TestRenderResult_MachineFormatsNeverReportEmpty(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatYAML} {
		var out, errOut bytes.Buffer
		w := Writer{Out: &out, Err: &errOut, Format: format}

		res, err := w.RenderResult([]byte(`{"items":[],"pagination":{"total":0}}`), Options{})
		if err != nil {
			t.Fatalf("RenderResult: %v", err)
		}
		if res.Empty {
			t.Errorf("format %v reported Empty; machine formats must emit their payload", format)
		}
		if out.Len() == 0 {
			t.Errorf("format %v produced no output for an empty list", format)
		}
	}
}
