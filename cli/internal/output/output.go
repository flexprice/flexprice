// Package output renders API response data to Out. Human-facing commentary is
// internal/ui's job, not this package's.
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

// Lets a caller that knows the resource name say something useful about an
// empty list; the renderer has no idea what "customers" are.
type Result struct {
	Empty bool
}

// Empty is only reported for table output: json and yaml are machine formats
// where an empty list is valid output the caller must not print prose over.
func (w Writer) RenderResult(raw []byte, o Options) (Result, error) {
	if w.Format != FormatTable {
		return Result{}, w.Render(raw, o)
	}
	rows, err := rowsFrom(raw)
	if err != nil {
		// Unparseable as a table: fall back to JSON rather than claiming empty.
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

func (w Writer) Warn(o Options, format string, args ...any) {
	if o.Quiet || w.Err == nil {
		return
	}
	fmt.Fprintf(w.Err, format+"\n", args...)
}
