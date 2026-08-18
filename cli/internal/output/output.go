// Package output renders API responses. Data is written to Out and everything
// human — footers, progress, warnings — to Err, so redirecting stdout yields
// clean machine-readable output.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
)

type Format int

const (
	FormatTable Format = iota
	FormatJSON
	FormatYAML
)

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "table":
		return FormatTable, nil
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	default:
		return FormatTable, fmt.Errorf("unsupported output format %q — use table, json or yaml", s)
	}
}

type Options struct {
	Columns []string
	// Shown and Total drive the truncation footer. Total of 0 means unknown.
	Shown int
	Total int
	Quiet bool
}

type Writer struct {
	Out    io.Writer
	Err    io.Writer
	Format Format
}

// Result reports what Render did, so a caller that knows the resource name can
// say something useful about an empty list. The renderer deliberately does not
// invent that message itself: it has no idea what "customers" are.
type Result struct {
	Empty bool
}

// RenderResult renders and reports. Render is kept as a thin wrapper so
// existing callers and tests are unaffected.
//
// Empty is only ever reported for table output. json and yaml are machine
// formats where an empty list is valid output that must be emitted verbatim —
// reporting Empty there would have the caller print prose over the top of
// valid JSON.
func (w Writer) RenderResult(raw []byte, o Options) (Result, error) {
	if w.Format != FormatTable {
		return Result{}, w.Render(raw, o)
	}
	rows, err := rowsFrom(raw)
	if err != nil {
		// Unparseable as a table — fall back to JSON so the user still sees
		// the data, and do not claim the result was empty.
		return Result{}, Writer{Out: w.Out, Err: w.Err, Format: FormatJSON}.Render(raw, o)
	}
	if len(rows) == 0 {
		return Result{Empty: true}, nil
	}
	return Result{}, w.renderTable(raw, o)
}

func (w Writer) Render(raw []byte, o Options) error {
	switch w.Format {
	case FormatJSON:
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			// Not JSON (a PDF, say) — pass it through untouched.
			_, err := w.Out.Write(raw)
			return err
		}
		enc := json.NewEncoder(w.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)

	case FormatYAML:
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			_, err := w.Out.Write(raw)
			return err
		}
		b, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Errorf("render yaml: %w", err)
		}
		_, err = w.Out.Write(b)
		return err

	default:
		return w.renderTable(raw, o)
	}
}

// Warn writes a human-facing message to stderr unless --quiet is set.
func (w Writer) Warn(o Options, format string, args ...any) {
	if o.Quiet || w.Err == nil {
		return
	}
	fmt.Fprintf(w.Err, format+"\n", args...)
}
