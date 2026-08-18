# Flexprice CLI — Interactive Onboarding UI and Styled Output

Date: 2026-08-18
Status: Approved design, ready for implementation planning
Related: `docs/design/2026-08-18-flexprice-cli-design.md`,
`docs/design/2026-08-18-flexprice-cli-implementation-plan.md`

## 1. Purpose

The CLI (all 17 implementation tasks + docs landed in the prior design) is functionally
complete but visually plain: `login`/`init` use a bare numbered list read via `fmt.Fscanln`,
and all output — tables, success messages, errors — is unstyled monochrome text. This design
adds an arrow-key-navigable onboarding experience and colored/styled output, modeled on
patterns from `gh` (GitHub CLI) and grounded in Flexprice's actual brand identity rather than
an invented palette.

## 2. Research

- **GitHub CLI (`gh`)** is the concrete reference for the interaction model: arrow-key select
  menus with the hint `[Use arrows to move, type to filter]`, used for account/protocol/auth
  method selection.
- **Stripe CLI's login is simpler than assumed** — browser-based device pairing, not an
  in-terminal wizard. `gh`'s pattern is the better reference for "interactive and polished,"
  not Stripe specifically.
- **The standard modern Go stack for this is the Charm ecosystem**: `charmbracelet/huh`
  (purpose-built for sequential prompts — select, input, confirm) built on
  `charmbracelet/bubbletea`, styled via `charmbracelet/lipgloss`. This pairing was already
  named in the *original* CLI design doc's tooling table but never installed, since the
  first 17 tasks didn't require interactivity.
- **Flexprice's real brand colors**, extracted from `assets/flexprice_logo_old.svg`'s gradient
  stops: `#9F398F` (deep magenta) → `#BB71F2` (light purple). All color choices in this design
  are grounded in this gradient, not invented.

## 3. Decisions

| # | Decision | Rationale |
|---|---|---|
| U1 | `charmbracelet/huh` + `charmbracelet/lipgloss`, two new dependencies | Purpose-built for exactly this; matches the original (unused) plan; both are small, focused, same maintainer |
| U2 | New `internal/style` package owns every color/style decision | Keeps "what color is this" separate from "what data is this" — the same single-responsibility split every other package in this project follows |
| U3 | Styling applies broadly (tables, messages) — not scoped to onboarding only | Explicit user choice; touches already-shipped `internal/output`, done carefully with existing tests preserved |
| U4 | `--output json`/`--output yaml` stay completely unstyled, always | Existing golden-file tests already lock this contract down; this design must not touch that path |
| U5 | Status-column coloring uses a header-name heuristic + generic word list, not a per-command mapping | A precise per-command status mapping is the same 197-file maintenance trap avoided in the original design (ADR 0004); a small, extensible, occasionally-incomplete list is an acceptable trade-off — an unrecognized value renders plain, never mis-colored |
| U6 | The arrow-key menu never becomes a requirement — falls back to today's exact behavior with no TTY | Protects every existing test and CI/script invocation; `huh` only activates for a human at a real terminal |
| U7 | Welcome banner shows on `init` always, and on bare `flexprice` only when no config exists yet | A fresh install gets a landing; a real command with no config gets a direct one-line error, not an interruption |

## 4. Visual style

Confirmed via the visual brainstorming companion (mockups shown, user selected and iterated
in-browser):

- **Structure**: a bordered box using the brand gradient (`#9F398F` for bold/label text,
  `#BB71F2` for borders and the active-selection highlight) for the welcome banner and
  region picker — the "Bold & branded" direction.
- **Status/message colors**: a full semantic palette layered on top — green `#4ade80` for
  success, red `#f87171` for errors, yellow `#facc15` for warnings — the "Playful &
  multi-color" direction, minus flag emoji (rejected: inconsistent rendering across terminal
  fonts/OSes — a correctness risk, not just a style call).
- **Tables**: the "fuller treatment" — a solid `#9F398F`-filled header row, a colored dot
  (`●`) before status values, a `#BB71F2` divider line under the data. Only the header row and
  status-shaped column values are colored; arbitrary cell data (emails, free-text IDs) is
  never colored, since there is no way to know in advance which columns matter across 197
  different commands' data shapes.

## 5. Architecture

### 5.1 `internal/style` (new)

The only package that imports `lipgloss` or references a raw hex color. Exposes small,
composable functions:

```go
package style

func Success(s string) string  // green, ✓ prefix
func Error(s string) string    // red, ✗ prefix
func Warning(s string) string  // yellow, ⚠ prefix
func Header(s string) string   // brand purple, bold
func Accent(s string) string   // brand light purple

// StatusColor returns the styled form of a status-column value, or the
// value unchanged if it doesn't match a known word. Never guesses.
func StatusColor(value string) string
```

Nothing outside this package writes an ANSI escape code or references `#9F398F`/`#BB71F2`
directly.

### 5.2 `internal/output/table.go` (modified)

The header row renders through `style.Header`. A column is treated as a status column when
its header name contains `"status"` (case-insensitive — catches `status`, `payment_status`,
`subscription_status`). Within a status column, each value is checked against:

```
good (green):     active, succeeded, finalized, paid, completed, published
bad (red):        failed, archived, voided, cancelled, expired, deleted
in-between (yellow): pending, draft, processing
```

Exact-word match, not substring — avoids a value like `"proactive"` mis-triggering on
`"active"`. Anything unmatched renders as plain text. `--output json`/`--output yaml` do not
route through this file's styling logic at all, matching current behavior exactly — this
design changes nothing about that path.

### 5.3 `internal/cmd/auth.go` (modified)

`promptRegion` is reimplemented with `huh.NewSelect`, showing the same region key + base URL
information as today, navigable with arrow keys and Enter, with type-to-filter (matching
`gh`'s pattern). The existing `term.IsTerminal(os.Stdin)` guard is preserved unchanged:
without a real terminal, the function skips straight to today's exact behavior — require
`--region`, same error message as now. `huh` is never invoked when stdin is not a TTY.

### 5.4 `internal/cmd/init.go` (modified)

Gains the bordered welcome banner (brand-gradient box, product one-liner) before delegating
to `login`'s existing flow. `--quiet` suppresses the banner's decorative box and copy but not
the functional region/key prompts underneath it.

### 5.5 `internal/cmd/root.go` (modified)

Bare `flexprice` (no subcommand) shows the welcome banner with an arrow-key choice
(`Get started →` / `Read the docs first`) **only when no config file exists yet** — checked via
`config.DefaultPath()` + a file-existence check, not a new state flag. Once a config file
exists, bare `flexprice` reverts to today's plain help output. A real command (e.g.
`customers list`) run with no config never shows the banner — it gets today's direct
`not authenticated — run: flexprice init` error, unchanged.

## 6. Fallback behavior

- **No real terminal (scripts, CI, piped input)**: every interactive element (region picker,
  welcome banner's choice) is skipped entirely, falling back to today's exact flag-based
  behavior. This is not a new code path to test — it is a preservation of the existing one.
- **`--no-color` / `NO_COLOR` env var**: `lipgloss` auto-detects both and disables ANSI color
  output accordingly — no custom detection code needed. Icons (`✓`, `✗`, `⚠`) are not color and
  persist even with color disabled, since they carry information a monochrome terminal user
  still benefits from.
- **Non-interactive terminal that still supports color** (e.g., a color-capable CI log
  viewer): color renders normally; only true TTY-ness gates the *interactive* elements, not
  the *color* ones — these are independent checks.

## 7. Testing

- Existing `internal/output` golden-file tests (pinned to `--output json`) are untouched —
  styling never touches that code path.
- Existing table-content tests (`strings.Contains(out, "cust_1")` etc.) remain valid unchanged:
  status coloring wraps a value in escape codes, it does not replace or relocate the value, so
  substring assertions on data values still pass. A test asserting on exact header text would
  need updating to strip ANSI codes first — flagged as a concrete implementation-plan task,
  not deferred vaguely.
- New tests: `internal/style` gets direct unit tests (`Success("ok")` produces the expected
  wrapped string; `StatusColor` on a known-good/known-bad/unrecognized value each produce the
  right result). `promptRegion`'s non-TTY fallback gets a regression test proving `huh` is
  never invoked without a terminal — this is the test that protects every other test's ability
  to run non-interactively.

## 8. Out of scope

Styling for the auto-generated `gendocs` Markdown output (that's a separate, plain-text
consumer). Any change to the `--edit` skeleton-generation flow (Task 11) — it opens the user's
own `$EDITOR`, which is not something this CLI's styling can or should reach into. Animated
transitions or spinners — `huh`/`lipgloss` support them, but nothing in this design calls for
one; can be considered separately if a specific slow operation (e.g., a future `--all`
pagination fetch) wants one.
