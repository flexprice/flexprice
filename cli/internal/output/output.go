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
