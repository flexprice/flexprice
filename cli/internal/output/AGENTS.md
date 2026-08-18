---
layer: output
owns:
  - "cli/internal/output/**"
---

# Output Layer

> Renders API responses. The only package that writes to stdout.

## Purpose

Turns raw response bytes into what the terminal or a script sees:
`table`/`json`/`yaml`, with data always on stdout and everything else —
warnings, footers, progress — on stderr.

## Key files
| File | Role |
|---|---|
| `output.go` | `Writer`, `Render`, `ParseFormat`, `Options` |
| `table.go` | `rowsFrom` (envelope detection), `renderTable`, `defaultColumns` |

## Render path

```
Render(raw, options)
  → format == json/yaml  → decode, re-encode in the target format
  → format == table      → rowsFrom(raw) to find the row list, then render
                            columns: explicit --columns, else commands.yaml's
                            columns:, else defaultColumns' heuristic
```

## Patterns to follow

- Data goes to stdout. Warnings, the truncation footer, and anything else
  a human reads goes to stderr, via `Writer.Warn`. Never write human-facing
  text to `Out`.
- `rowsFrom` must not treat every array-valued field as the row list — a
  single object response that happens to contain an array field (e.g. a
  customer's `tax_rates`) is not a list of rows. Only a field literally
  named `items`, or an array field alongside a pagination marker
  (`pagination`/`total`/`limit`/`offset`) at the top level, qualifies.

## Invariants (must hold)

- Column ordering must be deterministic across runs — Go's `encoding/json`
  sorts map keys, and `defaultColumns` sorts its fallback list; do not
  introduce map iteration into a path that produces visible output.
- String truncation in `format()` cuts on a rune boundary, not a byte index
  — a value containing multi-byte characters (the API has real environment
  names like `بيئة تجريبية`) must never be sliced mid-character.

## Common pitfalls

- **A non-JSON response is not guarded against an interactive terminal.**
  `client.Client.Do` returns only `([]byte, error)` — no `Content-Type` is
  ever surfaced to a caller — so `Render` in table mode falls through
  `json.Unmarshal` failing, then to the JSON-format fallback, which writes
  the raw bytes straight to `os.Stdout` regardless of whether stdout is a
  terminal. `invoices pdf` is a real, registered command that returns binary
  PDF bytes, so `flexprice invoices pdf <id>` with the default `--output
  table` currently pipes binary data to an interactive terminal. This is
  known, confirmed twice during implementation, and deliberately not fixed
  here — fixing it properly needs a `Content-Type`-aware signature change to
  `Client.Do`, which is out of this package's scope. If you touch a
  binary-returning operation, do not assume this is handled.
- **The array-detection heuristic used to pick the wrong field.** An
  earlier version picked the alphabetically-first array-valued key, which
  worked for `{"items":..., "pagination":...}` but would have rendered a
  single customer's `tax_rates` array as the row list instead of the
  customer itself. Fixed by requiring either the literal key `items`, or a
  pagination marker alongside the array. Do not revert to a bare
  "first array found" rule.

## Related layers

- `internal/client` — the source of the raw bytes this package renders, and
  the reason `Content-Type` is unavailable
- `internal/cmd` — the only consumer of `Writer`
