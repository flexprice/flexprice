# CLI interactive UI spike findings

Date: 2026-08-18
Gate: implementation plan Task 1

## Verdict

**PASS**

## lipgloss

- Version pinned: `v1.1.0` (pulls in `github.com/muesli/termenv v0.16.0`)
- `.Foreground(lipgloss.Color(hex)).Render(s)` produces ANSI codes: **yes**, confirmed with an
  explicit color profile forced (see below). The exact chain in the plan compiles unmodified:
  `lipgloss.NewStyle().Foreground(lipgloss.Color("#BB71F2")).Bold(true).Render(s)`.
- `.Bold(true)` composes with `.Foreground()`: **yes** — forced-color output was
  `"\x1b[1;38;2;187;113;242mforced color test\x1b[0m"`, where `1;` is the bold SGR code and
  `38;2;187;113;242` is 24-bit RGB for `#BB71F2` (187=0xBB, 113=0x71, 242=0xF2 — an exact,
  verified conversion, not approximate).
- **Important nuance found, not a defect**: in this spike's execution environment (a sandboxed
  non-interactive shell with no real terminal attached), `lipgloss.NewRenderer(os.Stdout).ColorProfile()`
  returned `3` (`termenv.Ascii`) — lipgloss auto-detected "no color support" on its own and
  silently disabled styling for the *unforced* call, which is why the first, unforced test in
  this spike printed the plain-text string with no escape codes. Forcing the profile explicitly
  to `termenv.TrueColor` proved the styling logic itself is correct — the absence of color was
  lipgloss's own (correct) environment detection, not a broken library. This will not reproduce
  in a real user's terminal, which genuinely supports color.
- Consequence for the implementation plan: `internal/style`'s own `term.IsTerminal` check and
  lipgloss's built-in auto-detection are two independent, agreeing safety nets — not redundant
  in a bad way. `internal/style`'s explicit `enabled` flag remains essential for the one case
  lipgloss cannot know about on its own: our CLI's own `--no-color` flag. No change to Task 2's
  planned design.

## huh

- Version pinned: `v1.0.0`
- Exact working method chain for a standalone Select (compiles unmodified against the real
  library):
  ```go
  var choice string
  sel := huh.NewSelect[string]().
      Title("Data region").
      Options(
          huh.NewOption("US      https://us.api.flexprice.io/v1", "us"),
          huh.NewOption("India   https://api.cloud.flexprice.io/v1", "in"),
      ).
      Value(&choice)
  err := sel.Run()
  ```
- Arrow-key navigation + Enter confirmed working interactively: **not directly testable from
  this tool environment** — there is no way to send real keypresses to an interactive TTY
  session from here. This is not a gap in the gate's rigor; it is a limitation of the
  environment this spike ran in. Mitigation: Task 4 (Step 5) and Task 6 (Step 6) of the
  implementation plan both already include a mandatory manual interactive smoke test with a
  real terminal, built in before this plan was written specifically for this reason — those
  steps are where a human confirms the actual arrow-key experience.

## Non-TTY behavior (critical)

- `sel.Run()` with stdin from `/dev/null`: **returned a clean error immediately** — did not hang.
- Exact exit code: `0` (the spike program itself handles the error and exits normally; the
  error is from `sel.Run()`, not the process)
- Exact error message: `huh: could not open a new TTY: open /dev/tty: device not configured`
- Time taken: measured via a `perl alarm(10)` hard bound (the `timeout` command is unavailable
  on this machine) — **0 seconds elapsed**, nowhere near the 10-second alarm. `huh` does not
  merely check `stdin` — it actively attempts to open the controlling terminal device
  (`/dev/tty`) directly and fails cleanly the instant that fails.

## Consequences

Both critical properties hold: `huh`'s real API matches what the plan already assumed
(no code changes needed for Task 4), and the non-TTY case fails instantly and cleanly rather
than hanging — Task 4's `term.IsTerminal(os.Stdin)` guard remains the primary protection
(preventing `sel.Run()` from ever being reached in a script/CI context), backed by `huh`'s own
independent fast-fail as a second layer even if that guard were somehow bypassed.

Proceeding to Task 2 with the method chains above confirmed real and unmodified from the plan.
