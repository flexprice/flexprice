# Flexprice CLI Interactive UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Flexprice CLI an arrow-key-navigable onboarding experience and colored, brand-consistent output, without breaking any of the 200+ existing tests or the non-interactive/CI behavior every other task in this project depends on.

**Architecture:** A new `internal/style` package centralizes every color decision. `internal/output/table.go` and `internal/cmd/{auth,init,root}.go` are modified to call into it. Two new dependencies (`charmbracelet/huh`, `charmbracelet/lipgloss`) are added, gated behind a small spike task that verifies their real API against a throwaway program before any production code depends on them — the same pattern the original 17-task plan used for `kin-openapi`.

**Tech Stack:** Go 1.25, `charmbracelet/huh` (interactive prompts), `charmbracelet/lipgloss` (color/styling), `golang.org/x/term` (already a dependency, reused for TTY detection).

**Design doc:** `docs/design/2026-08-18-flexprice-cli-interactive-ui-design.md`

---

## File structure

```
cli/
├── go.mod, go.sum                    # +charmbracelet/huh, +charmbracelet/lipgloss
└── internal/
    ├── style/
    │   ├── style.go                  # NEW — the only package that imports lipgloss
    │   └── style_test.go             # NEW
    ├── output/
    │   ├── table.go                  # MODIFY — header + status-column coloring
    │   └── table_test.go             # MODIFY (existing file, new tests appended)
    └── cmd/
        ├── auth.go                   # MODIFY — promptRegion becomes huh.Select
        ├── auth_test.go              # MODIFY
        ├── init.go                   # MODIFY — welcome banner
        ├── init_test.go              # NEW
        ├── root.go                   # MODIFY — bare-invocation banner, --no-color wiring
        └── root_test.go              # MODIFY
```

Every file this plan touches, and the exact real signatures it depends on, were read directly
from the repository before this plan was written — not assumed.

---

## Phase 0 — Spike: confirm the real `huh` API before depending on it

`huh` has never been used in this codebase. Rather than write production code against a
guessed API shape, this phase proves the exact calls work — mirroring the `kin-openapi` spike
gate that the original 17-task CLI plan used successfully for the same reason.

### Task 1: Spike — `huh.Select`, non-interactive detection, and `lipgloss` rendering

**Files:**
- Create: `/tmp/cli-ui-spike/main.go` (throwaway, deleted at the end of this task)
- Create: `docs/design/2026-08-18-cli-interactive-ui-spike-findings.md` (committed)

- [ ] **Step 1: Set up the throwaway module**

```bash
mkdir -p /tmp/cli-ui-spike && cd /tmp/cli-ui-spike
go mod init spike
go get github.com/charmbracelet/huh@latest
go get github.com/charmbracelet/lipgloss@latest
```

- [ ] **Step 2: Write the spike**

`/tmp/cli-ui-spike/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	// --- Part A: lipgloss rendering. Confirm .Foreground(lipgloss.Color(hex)).Render(s)
	// produces a string containing ANSI escape codes, and that Bold(true) composes.
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#BB71F2")).Bold(true)
	rendered := style.Render("Welcome to Flexprice")
	fmt.Fprintf(os.Stderr, "lipgloss output (should contain \\x1b[ escape codes): %q\n", rendered)
	if rendered == "Welcome to Flexprice" {
		fmt.Fprintln(os.Stderr, "WARNING: lipgloss did not add any styling — check terminal/CI color support")
	}

	// --- Part B: huh.Select standalone usage (no huh.Form wrapper).
	var choice string
	sel := huh.NewSelect[string]().
		Title("Data region").
		Options(
			huh.NewOption("US      https://us.api.flexprice.io/v1", "us"),
			huh.NewOption("India   https://api.cloud.flexprice.io/v1", "in"),
		).
		Value(&choice)

	fmt.Fprintln(os.Stderr, "\nAbout to run huh.Select interactively. Use arrow keys, press Enter.")
	if err := sel.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sel.Run() error: %v (this is expected if stdin is not a TTY)\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "You selected: %q\n", choice)
	}
}
```

- [ ] **Step 3: Run it interactively, in a real terminal**

```bash
cd /tmp/cli-ui-spike && go run .
```

Expected: the `lipgloss` line shows a quoted string containing `\x1b[` escape sequences (not the
plain text unstyled). Then an arrow-key-navigable menu appears with two options; move with
↑/↓, press Enter, and confirm the selected value prints correctly.

Record in the findings file: the exact method chain that compiled (`huh.NewSelect[string]()`,
`.Title()`, `.Options()`, `huh.NewOption(label, value)`, `.Value(&var)`, `.Run()` — adjust to
whatever actually compiled if the API differs), and whether arrow-key navigation and Enter
worked as expected.

- [ ] **Step 4: Run it with stdin redirected from `/dev/null` — confirm the non-interactive case fails cleanly, not hangs**

```bash
cd /tmp/cli-ui-spike && timeout 10 go run . < /dev/null
echo "exit code: $?"
```

Expected: the program does **not** hang for the full 10-second timeout. `sel.Run()` returns an
error (or the program otherwise exits promptly) rather than blocking waiting for terminal
input that will never come. This is the single most important thing this spike must prove:
**Task 5 of this plan depends on `huh.Select` never being invoked without a TTY check in front
of it, but if `sel.Run()` itself doesn't fail fast when stdin isn't a terminal, that guard
needs to be even stricter than planned.**

Record the exact exit code and any error message in the findings file.

- [ ] **Step 5: Write the findings file**

`docs/design/2026-08-18-cli-interactive-ui-spike-findings.md`:

```markdown
# CLI interactive UI spike findings

Date: 2026-08-18
Gate: implementation plan Task 1

## Verdict

PASS / FAIL (delete one)

## lipgloss

- Version pinned:
- `.Foreground(lipgloss.Color(hex)).Render(s)` produces ANSI codes: yes/no
- `.Bold(true)` composes with `.Foreground()`: yes/no

## huh

- Version pinned:
- Exact working method chain for a standalone Select:
- Arrow-key navigation + Enter confirmed working interactively: yes/no

## Non-TTY behavior (critical)

- `sel.Run()` with stdin from /dev/null: hung / returned an error / exited some other way
- Exact exit code:
- Exact error message, if any:
- Time taken (should be near-instant, not close to the 10s timeout):

## Consequences

If FAIL on the non-TTY behavior specifically: Task 5's `term.IsTerminal` guard must wrap
`sel.Run()` unconditionally with no path that reaches it otherwise — do not rely on huh's own
behavior as a backstop. If FAIL on lipgloss rendering: check whether the spike's terminal
itself lacks color support before concluding the library is broken — retry in a different
terminal before failing this gate.
```

- [ ] **Step 6: Clean up and commit**

```bash
rm -rf /tmp/cli-ui-spike
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/reverent-lehmann-62238a
git add docs/design/2026-08-18-cli-interactive-ui-spike-findings.md
git commit -m "docs: record spike findings for huh/lipgloss integration"
```

**Gate:** every later task in this plan that calls `huh.Select` (Task 5) must use the exact
method chain recorded as working in this findings file, not the guessed chain in Task 5's own
code block if the two differ.

---

## Phase 1 — `internal/style`

### Task 2: The style package

**Files:**
- Create: `cli/internal/style/style.go`
- Create: `cli/internal/style/style_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/style/style_test.go`:

```go
package style

import (
	"strings"
	"testing"
)

func TestSuccess_IncludesCheckmarkAndText(t *testing.T) {
	got := Success("Verified")
	if !strings.Contains(got, "✓") || !strings.Contains(got, "Verified") {
		t.Errorf("Success(%q) = %q, want it to contain a checkmark and the text", "Verified", got)
	}
}

func TestError_IncludesCrossAndText(t *testing.T) {
	got := Error("failed")
	if !strings.Contains(got, "✗") || !strings.Contains(got, "failed") {
		t.Errorf("Error(%q) = %q, want it to contain a cross and the text", "failed", got)
	}
}

func TestWarning_IncludesWarningSymbolAndText(t *testing.T) {
	got := Warning("check your input")
	if !strings.Contains(got, "⚠") || !strings.Contains(got, "check your input") {
		t.Errorf("Warning(...) = %q, want it to contain a warning symbol and the text", got)
	}
}

// Icons must survive Disable(): a monochrome terminal still benefits from ✓/✗/⚠
// as information, even with no color applied. Color is the only thing gated.
func TestDisable_KeepsIconsRemovesColorCodes(t *testing.T) {
	Disable()
	defer Enable()

	got := Success("Verified")
	if !strings.Contains(got, "✓") || !strings.Contains(got, "Verified") {
		t.Errorf("Success after Disable() = %q, want icon and text preserved", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("Success after Disable() = %q, want no ANSI escape codes", got)
	}
}

func TestEnable_RestoresColorCodes(t *testing.T) {
	Disable()
	Enable()
	got := Header("test")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("Header after Enable() = %q, want ANSI escape codes present", got)
	}
}

func TestStatusColor_KnownGoodValue(t *testing.T) {
	got := StatusColor("active")
	if !strings.Contains(got, "active") {
		t.Errorf("StatusColor(active) = %q, want it to contain the original text", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(active) = %q, want ANSI color codes for a known-good status", got)
	}
}

func TestStatusColor_KnownBadValue(t *testing.T) {
	got := StatusColor("archived")
	if !strings.Contains(got, "archived") || !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(archived) = %q, want colored text containing \"archived\"", got)
	}
}

// The unrecognized case is the load-bearing one: an unmatched value must never
// be colored, since a wrong guess is worse than no color at all. Design doc §5.2.
func TestStatusColor_UnrecognizedValueIsUnchanged(t *testing.T) {
	got := StatusColor("some-domain-specific-state")
	if got != "some-domain-specific-state" {
		t.Errorf("StatusColor(unrecognized) = %q, want the value returned completely unchanged", got)
	}
}

// Exact-word match, not substring: "proactive" contains "active" as a substring
// and must not mis-trigger the good-status color.
func TestStatusColor_DoesNotSubstringMatch(t *testing.T) {
	got := StatusColor("proactive")
	if got != "proactive" {
		t.Errorf("StatusColor(proactive) = %q, want it unchanged (not a substring match on \"active\")", got)
	}
}

func TestStatusColor_CaseInsensitive(t *testing.T) {
	got := StatusColor("ACTIVE")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(ACTIVE) = %q, want the match to be case-insensitive", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/style/ -v
```

Expected: FAIL — `undefined: Success`, `undefined: Error`, etc. (the package does not exist yet).

- [ ] **Step 3: Add the dependencies confirmed by the Task 1 spike**

```bash
cd cli
go get github.com/charmbracelet/lipgloss@<version recorded in the spike findings file>
```

- [ ] **Step 4: Implement**

`cli/internal/style/style.go`:

```go
// Package style is the only place in the CLI that decides what color
// something is. Every other package calls into here rather than constructing
// ANSI codes or referencing a hex color directly.
package style

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Flexprice's brand gradient, extracted from assets/flexprice_logo_old.svg's
// gradient stops — not an invented palette.
const (
	colorMagenta = lipgloss.Color("#9F398F")
	colorPurple  = lipgloss.Color("#BB71F2")
	colorGreen   = lipgloss.Color("#4ade80")
	colorRed     = lipgloss.Color("#f87171")
	colorYellow  = lipgloss.Color("#facc15")
)

// enabled gates every ANSI color code this package emits. It does not gate
// icons (✓/✗/⚠) — those persist even in a monochrome terminal, since they
// carry information a plain-text reader still benefits from. Defaults to on
// only when NO_COLOR is unset and stdout is a real terminal; --no-color calls
// Disable() explicitly once flags are parsed (see cmd/root.go's
// PersistentPreRunE — flags are not populated yet at command construction
// time, so this default is necessarily a best-guess until that hook runs).
var enabled = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))

// Disable turns off color styling for the rest of the process.
func Disable() { enabled = false }

// Enable turns color styling back on. Exists primarily for tests that need to
// restore state after calling Disable(), and for a future --color flag if one
// is ever added to force color in a piped context.
func Enable() { enabled = true }

func styled(s string, c lipgloss.Color, bold bool) string {
	if !enabled {
		return s
	}
	st := lipgloss.NewStyle().Foreground(c)
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

func Success(s string) string { return "✓ " + styled(s, colorGreen, false) }
func Error(s string) string   { return "✗ " + styled(s, colorRed, false) }
func Warning(s string) string { return "⚠ " + styled(s, colorYellow, false) }
func Header(s string) string  { return styled(s, colorMagenta, true) }
func Accent(s string) string  { return styled(s, colorPurple, false) }

var (
	goodStatus = map[string]bool{
		"active": true, "succeeded": true, "finalized": true,
		"paid": true, "completed": true, "published": true,
	}
	badStatus = map[string]bool{
		"failed": true, "archived": true, "voided": true,
		"cancelled": true, "expired": true, "deleted": true,
	}
	warnStatus = map[string]bool{
		"pending": true, "draft": true, "processing": true,
	}
)

// StatusColor returns value styled according to a small, deliberately
// generic, and deliberately incomplete word list — or value completely
// unchanged if it matches nothing. An unrecognized status is never guessed
// at: coloring something the wrong color is worse than leaving it plain.
// Matching is case-insensitive but exact-word, not substring, so
// "proactive" cannot mis-trigger on "active". Design doc §5.2 / §3.
func StatusColor(value string) string {
	lower := strings.ToLower(value)
	switch {
	case goodStatus[lower]:
		return styled(value, colorGreen, false)
	case badStatus[lower]:
		return styled(value, colorRed, false)
	case warnStatus[lower]:
		return styled(value, colorYellow, false)
	default:
		return value
	}
}
```

- [ ] **Step 5: Run the tests**

```bash
cd cli && go test ./internal/style/ -race -v
```

Expected: PASS, all ten tests.

- [ ] **Step 6: Commit**

```bash
git add cli/internal/style cli/go.mod cli/go.sum
git commit -m "feat(cli): style package for color-consistent output"
```

---

## Phase 2 — Table styling

### Task 3: Wire `style.Header` and `style.StatusColor` into table rendering

**Files:**
- Modify: `cli/internal/output/table.go`
- Modify: `cli/internal/output/table_test.go` (existing file — find it via `ls cli/internal/output/*_test.go` if the exact test file name differs; add tests to whichever file already covers `renderTable`)

- [ ] **Step 1: Write the failing tests**

Append to the existing table test file:

```go
func TestRenderTable_HeaderIsStyled(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Errorf("table output has no ANSI codes; want the header row styled")
	}
}

// The status VALUE, not just the header, gets colored when the column name
// contains "status" and the value matches a known word.
func TestRenderTable_KnownStatusValueIsColored(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	input := []byte(`{"items":[{"id":"cust_1","status":"archived"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The raw word is still present as a substring — color wraps it, does not
	// replace it — so this also proves existing "strings.Contains(out, value)"
	// style assertions elsewhere in this file remain valid unchanged.
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("table output missing the status value itself: %q", out.String())
	}
}

// A non-status column (e.g. "email") never gets value-colored, even if its
// text happens to collide with a status word — status.
func TestRenderTable_NonStatusColumnValuesAreNeverColored(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	// "active" as an email local-part, in a column that is not named "status".
	input := []byte(`{"items":[{"id":"cust_1","email":"active@example.com"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "active\x1b[") || strings.Contains(out.String(), "\x1b[32mactive") {
		t.Errorf("email column value was colored as if it were a status: %q", out.String())
	}
}

// --output json must never contain ANSI codes, regardless of styling changes
// made to the table path. This is the hard constraint from the design doc §6/U4.
func TestRender_JSONOutputNeverContainsANSICodes(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("--output json contains ANSI escape codes: %q", out.String())
	}
}

// Design doc §6/U4 names both json and yaml explicitly as staying unstyled.
func TestRender_YAMLOutputNeverContainsANSICodes(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatYAML}

	input := []byte(`{"items":[{"id":"cust_1","status":"active"}],"pagination":{"total":1,"limit":20,"offset":0}}`)
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("--output yaml contains ANSI escape codes: %q", out.String())
	}
}
```

Add `"github.com/flexprice/cli/internal/style"` is NOT imported in the test file — tests only
check for the presence of `\x1b[`, they do not call `style` directly.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/output/ -run 'TestRenderTable_HeaderIsStyled|TestRenderTable_KnownStatusValueIsColored|TestRenderTable_NonStatusColumnValuesAreNeverColored|TestRender_JSONOutputNeverContainsANSICodes|TestRender_YAMLOutputNeverContainsANSICodes' -v
```

Expected: `TestRenderTable_HeaderIsStyled` and `TestRenderTable_KnownStatusValueIsColored` FAIL
(no styling exists yet). `TestRenderTable_NonStatusColumnValuesAreNeverColored`,
`TestRender_JSONOutputNeverContainsANSICodes`, and `TestRender_YAMLOutputNeverContainsANSICodes`
PASS already — they are regression guards for behavior that must stay true, not new behavior
being added.

- [ ] **Step 3: Implement**

In `cli/internal/output/table.go`, add the import and modify `renderTable`:

```go
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/flexprice/cli/internal/style"
)
```

Replace the header-and-rows section of `renderTable`:

```go
	tw := tabwriter.NewWriter(w.Out, 0, 0, 2, ' ', 0)
	headerCells := make([]string, len(columns))
	for i, c := range columns {
		headerCells[i] = style.Header(strings.ToUpper(c))
	}
	fmt.Fprintln(tw, strings.Join(headerCells, "\t"))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = formatCell(c, row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
```

Add a new function `formatCell` that wraps the existing `format` with status-column detection,
and keep `format` itself unchanged (it is still used for the value before any status coloring
is applied):

```go
// formatCell renders one cell, applying status coloring only when the column
// name looks like a status column. Design doc §5.2: a column is "status-shaped"
// when its name contains "status", case-insensitive — not tied to any specific
// command, since 197 commands cannot each be hand-mapped without repeating the
// same maintenance trap this project has avoided everywhere else.
func formatCell(column string, value any) string {
	text := format(value)
	if strings.Contains(strings.ToLower(column), "status") {
		return style.StatusColor(text)
	}
	return text
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/output/ -race -v
```

Expected: PASS — every test in the package, including the four new ones and every
pre-existing test (`TestRender_JSONGoesToStdoutOnly`, `TestRender_TableUsesRequestedColumns`,
`TestGolden_JSONOutputIsStable`, etc.).

- [ ] **Step 5: Run the golden-file test specifically and confirm it is untouched**

```bash
cd cli && go test ./internal/output/ -run TestGolden_JSONOutputIsStable -v
```

Expected: PASS with no `-update` flag needed — this test's fixture must not need
regeneration, proving the JSON path truly was not touched by this task.

- [ ] **Step 6: Commit**

```bash
git add cli/internal/output
git commit -m "feat(cli): color-style table headers and status column values"
```

---

## Phase 3 — Interactive region picker

### Task 4: Replace `promptRegion` with `huh.Select`

**Files:**
- Modify: `cli/internal/cmd/auth.go`
- Modify: `cli/internal/cmd/auth_test.go`

Depends on: Task 1 (use the exact `huh` method chain recorded in the spike findings file —
substitute it for the chain below if they differ).

- [ ] **Step 1: Write the failing test**

Add to `cli/internal/cmd/auth_test.go`:

```go
// promptRegion's non-TTY fallback must be preserved exactly: huh.Select must
// never be invoked when stdin is not a real terminal, or every existing test
// and CI/script invocation of this CLI breaks. This is the single most
// important test in this task.
func TestPromptRegion_NoTTYFallsBackToExactPriorBehavior(t *testing.T) {
	regions := []spec.Region{
		{Key: "us", BaseURL: "https://us.api.flexprice.io/v1"},
		{Key: "in", BaseURL: "https://api.cloud.flexprice.io/v1"},
	}
	// os.Stdin in `go test` is not a TTY by default, so this exercises the
	// real non-interactive path without needing to fake terminal state.
	_, err := promptRegion(regions)
	if err == nil {
		t.Fatal("want an error when stdin is not a terminal")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error = %q, want it to name --region as the alternative", err.Error())
	}
}
```

- [ ] **Step 2: Run it to verify it currently passes** (this behavior already exists — this
  step confirms the test is correctly written against current behavior before Step 3 changes
  the implementation underneath it)

```bash
cd cli && go test ./internal/cmd/ -run TestPromptRegion_NoTTYFallsBackToExactPriorBehavior -v
```

Expected: PASS (the current `Fscanln`-based `promptRegion` already has this guard).

- [ ] **Step 3: Add the huh dependency and implement**

```bash
cd cli
go get github.com/charmbracelet/huh@<version recorded in the spike findings file>
```

Replace `promptRegion` in `cli/internal/cmd/auth.go`:

```go
func promptRegion(regions []spec.Region) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal available — pass --region (for example --region us)")
	}

	options := make([]huh.Option[string], len(regions))
	for i, r := range regions {
		label := fmt.Sprintf("%-6s  %s", r.Key, r.BaseURL)
		options[i] = huh.NewOption(label, r.Key)
	}

	var choice string
	sel := huh.NewSelect[string]().
		Title("Data region").
		Options(options...).
		Value(&choice)

	if err := sel.Run(); err != nil {
		return "", fmt.Errorf("region selection cancelled: %w", err)
	}
	return choice, nil
}
```

Add `"github.com/charmbracelet/huh"` to the import block.

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/cmd/ -race -v
```

Expected: PASS, including `TestPromptRegion_NoTTYFallsBackToExactPriorBehavior` and every other
existing test in the package (`TestVerifyKey_*`, `TestMaskKey_*`, `TestRootCommand_*`,
`TestNewRootCommand_InstancesDoNotShareState`, all resource/raw tests).

- [ ] **Step 5: Manual interactive smoke test**

```bash
cd cli && go build -o /tmp/flexprice-smoke .
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file /tmp/flexprice-smoke login --api-key sk_test_fake
```

Expected: an arrow-key-navigable region menu appears (not a numbered list), showing
`us    https://us.api.flexprice.io/v1` and `in    https://api.cloud.flexprice.io/v1`. Navigate
with ↑/↓, press Enter — confirm the flow proceeds to key verification afterward (it will fail
verification against `sk_test_fake` with no real server, which is expected and fine — the
region picker itself is what's being verified here). Clean up:

```bash
rm -f /tmp/flexprice-smoke
```

- [ ] **Step 6: Commit**

```bash
git add cli/internal/cmd/auth.go cli/internal/cmd/auth_test.go cli/go.mod cli/go.sum
git commit -m "feat(cli): arrow-key region picker via huh.Select"
```

---

## Phase 4 — Welcome banner

### Task 5: `init`'s welcome banner

**Files:**
- Modify: `cli/internal/cmd/init.go`
- Create: `cli/internal/cmd/init_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/cmd/init_test.go`:

```go
package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// --quiet suppresses the decorative banner but the command must still attempt
// to run login underneath it (this test only checks the banner is absent from
// --quiet output; it does not exercise the full login flow, which needs a
// real terminal or --api-key and is covered by auth_test.go).
func TestInitCommand_QuietSuppressesBanner(t *testing.T) {
	g := &Globals{Quiet: true}
	cmd := newInitCommand(g, "test")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	printInitBanner(&out, g)

	if strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("banner printed despite --quiet: %q", out.String())
	}
}

func TestInitCommand_BannerShowsWithoutQuiet(t *testing.T) {
	g := &Globals{Quiet: false}

	var out bytes.Buffer
	printInitBanner(&out, g)

	if !strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("banner missing without --quiet: %q", out.String())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/cmd/ -run TestInitCommand -v
```

Expected: FAIL — `undefined: printInitBanner`.

- [ ] **Step 3: Implement**

Replace `cli/internal/cmd/init.go`:

```go
package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/style"
)

// printInitBanner writes the bordered welcome box. Split out from
// newInitCommand's RunE so it can be tested without exercising the full
// login flow (which needs a real terminal or --api-key).
func printInitBanner(w io.Writer, g *Globals) {
	if g.Quiet {
		return
	}
	fmt.Fprintln(w, style.Accent("┌────────────────────────────────┐"))
	fmt.Fprintf(w, "%s  %s %s %s\n",
		style.Accent("│"), style.Header("Welcome to"), style.Accent("Flexprice"), style.Accent("│"))
	fmt.Fprintln(w, style.Accent("└────────────────────────────────┘"))
	fmt.Fprintln(w, "Usage-based billing from your terminal")
	fmt.Fprintln(w)
}

// newInitCommand is the guided first run: login, then tell the user what to do next.
func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up the CLI (guided)",
		RunE: func(c *cobra.Command, args []string) error {
			printInitBanner(os.Stderr, g)
			fmt.Fprintln(os.Stderr, "Your API key is scoped to one environment — you can add more later with `flexprice login`.")
			fmt.Fprintln(os.Stderr)

			login := newLoginCommand(g, version)
			login.SetContext(c.Context())
			if err := login.RunE(login, nil); err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr, "\nNext steps:")
			fmt.Fprintln(os.Stderr, "  flexprice whoami            confirm what you are pointed at")
			fmt.Fprintln(os.Stderr, "  flexprice resources         see everything you can act on")
			fmt.Fprintln(os.Stderr, "  flexprice customers list    try a read")
			fmt.Fprintln(os.Stderr, "  flexprice env list          see your other environments")
			return nil
		},
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/cmd/ -race -v
```

Expected: PASS, all tests including the two new ones and the full pre-existing suite.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/cmd/init.go cli/internal/cmd/init_test.go
git commit -m "feat(cli): branded welcome banner on init, suppressed by --quiet"
```

---

## Phase 5 — Bare-invocation banner and `--no-color` wiring

### Task 6: Root command changes

**Files:**
- Modify: `cli/internal/cmd/root.go`
- Modify: `cli/internal/cmd/root_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cli/internal/cmd/root_test.go`:

```go
// --no-color must disable style package output for the whole process, wired
// through PersistentPreRunE since flags are not populated until Execute()
// parses them — NewRootCommand itself runs before that.
func TestNoColorFlag_DisablesStyling(t *testing.T) {
	style.Enable() // ensure a clean starting state regardless of test order
	defer style.Enable()

	root := NewRootCommand("test")
	root.SetArgs([]string{"--no-color", "version"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := style.Header("test"); strings.Contains(got, "\x1b[") {
		t.Errorf("style.Header still produces color after --no-color: %q", got)
	}
}

// Bare `flexprice` with no config file shows the welcome banner instead of
// plain help. This test uses a scratch HOME so it never touches a real
// config file on the machine running the test.
func TestBareInvocation_NoConfigShowsWelcomeBanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := NewRootCommand("test")
	root.SetArgs([]string{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("bare invocation with no config did not show the welcome banner: %q", out.String())
	}
}

// Once a config file exists, bare invocation reverts to plain help — the
// banner is only for a genuinely fresh install, not every bare invocation.
func TestBareInvocation_WithConfigShowsPlainHelp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(dir+"/.flexprice", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.flexprice/config.toml", []byte("default_profile = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand("test")
	root.SetArgs([]string{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("bare invocation with an existing config showed the welcome banner: %q", out.String())
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("bare invocation with an existing config did not show normal help: %q", out.String())
	}
}
```

Add `"github.com/flexprice/cli/internal/style"` and `"os"` to the test file's imports if not
already present.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/cmd/ -run 'TestNoColorFlag_DisablesStyling|TestBareInvocation' -v
```

Expected: `TestNoColorFlag_DisablesStyling` and `TestBareInvocation_NoConfigShowsWelcomeBanner`
FAIL (no wiring exists yet). `TestBareInvocation_WithConfigShowsPlainHelp` currently PASSES
(today's actual behavior already matches it) — that is the regression guard for this task.

- [ ] **Step 3: Implement**

In `cli/internal/cmd/root.go`, add imports:

```go
import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/style"
)
```

Modify `NewRootCommand` — add a `PersistentPreRunE` and a `RunE` for the bare-invocation
banner, right after `bindGlobals`:

```go
	bindGlobals(root.PersistentFlags(), g)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if g.NoColor {
			style.Disable()
		}
		return nil
	}

	root.RunE = func(cmd *cobra.Command, args []string) error {
		if !hasExistingConfig() {
			printInitBanner(os.Stderr, g)
			fmt.Fprintln(os.Stderr, "Run `flexprice init` to get started, or read the docs:")
			fmt.Fprintln(os.Stderr, "  https://docs.flexprice.io/cli")
			return nil
		}
		return cmd.Help()
	}
```

Add the helper function, near `runtimeContext`:

```go
// hasExistingConfig reports whether a config file already exists, without
// resolving credentials — used only to decide whether bare `flexprice`
// shows the welcome banner or normal help. A missing home directory or any
// other lookup failure is treated the same as "no config": show the banner
// rather than erroring on a decision this lightweight.
func hasExistingConfig() bool {
	path, err := config.DefaultPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/cmd/ -race -v
```

Expected: PASS — all three new tests, plus the complete pre-existing suite (every command's
tests, `TestNewRootCommand_InstancesDoNotShareState`, everything in `resource_test.go`,
`raw_test.go`, `auth_test.go`).

- [ ] **Step 5: Full module verification**

```bash
cd cli && go build ./... && go vet ./... && gofmt -l . && go test ./... -race
```

Expected: clean build, clean vet, clean gofmt, every package's tests passing.

- [ ] **Step 6: Full manual smoke test**

```bash
cd cli && go build -o /tmp/flexprice-smoke .
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file /tmp/flexprice-smoke
```

Expected: the welcome banner appears (fresh `HOME`, no config exists). Then:

```bash
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file /tmp/flexprice-smoke --no-color version
```

Expected: `version` output has no ANSI escape codes even though the CLI's styling is wired in
elsewhere. Clean up:

```bash
rm -f /tmp/flexprice-smoke
```

- [ ] **Step 7: Commit**

```bash
git add cli/internal/cmd/root.go cli/internal/cmd/root_test.go
git commit -m "feat(cli): welcome banner on bare invocation with no config, wire --no-color"
```
