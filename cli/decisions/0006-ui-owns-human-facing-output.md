# 0006 — internal/ui owns every human-facing write

## Context

Human-facing output was 34 `fmt.Fprint*` calls spread across seven files in
`internal/cmd`. Each independently decided whether to respect `--quiet`, whether
a terminal was attached, and whether to use colour — and most decided nothing at
all. The clearest symptom: three of `internal/style`'s six exported functions
(`Success`, `Error`, `Warning`) were defined and called from no production code,
so success and failure rendered identically, as undifferentiated plain text.

An audit against clig.dev found eleven gaps. Eight of them reduce to one question
no call site was asking: **is a human watching this stream right now.** Spinner
suppression in CI, `--no-input`, `TERM=dumb`, Ctrl-C teardown, `--quiet`
handling, empty states, receipts and the confirmation prompt are all the same
question with different consequences.

`internal/style` could not fix this. It is a passive helper library — correct at
what it does, but nothing obliges a call site to use it, which is exactly how
half of it ended up dead.

There was also a live stream bug. `style`'s gate reads stdout:

```go
var enabled = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))
```

but the status footer is written to **stderr**, so `flexprice customers list >
out.json` stripped colour from a stream still going to a live terminal. Minor
alone; applied to a spinner it would have suppressed animation during one of the
most common ways this CLI is run.

## Decision

`internal/ui` owns every human-facing write. One value carries the streams and
the quiet / TTY / no-input / animate decisions, and hangs off the existing
`Globals`, which already reaches all seven files — so no signatures changed.

`internal/style` keeps a single responsibility — deciding what colour something
is — but becomes a `Palette` value so colour can be gated **per stream**. The
package-level functions remain, backed by a stdout-gated default, so
`internal/output` was untouched by the split.

The UI is constructed in `PersistentPreRunE`, not in `NewRootCommand`: pflag does
not populate bound values until `Execute` parses them, so a UI built at
construction time captures `--quiet` and `--no-color` as their defaults. This is
the same trap that made `Globals` a per-root value rather than a package
variable.

## Consequences

- `--quiet`, `TERM=dumb`, CI detection and `--no-input` are implemented once.
- Colour on stderr is gated on stderr. The redirect bug above is fixed, and the
  spinner inherits the correct behaviour rather than the bug.
- `internal/output` no longer knows about stderr commentary at all; the status
  footer moved out with its tests.
- New commands get correct behaviour by default. `fmt.Fprintf(os.Stderr, …)` in
  `internal/cmd` is now the anomaly rather than the norm — four remain, each
  deliberate: `openURL` has no `*Globals`, `readSecret` must write on the raw
  stream `term.ReadPassword` reads from, and `RestoreTerminal` is teardown that
  must not depend on UI state.
- One behaviour change ships with this: `ui.Confirm` **refuses** when stdin is
  not a terminal, where the old raw prompt returned nil and proceeded. A script
  piping into `flexprice … delete` previously destroyed data with nothing asked;
  it now fails until `--force` is passed.

## Note on testing

Two assertions in this work passed while being vacuous, and both were only found
by deliberately breaking the code under test:

- "a non-TTY UI writes zero escape bytes" passed with the stream gates deleted,
  because `Color` defaulted false *and* lipgloss suppresses colour under `go
  test` on its own. Fixed with a `TestMain` forcing a true-colour profile and
  cases that request colour explicitly.
- The raw `get`/`post`/`delete` commands fell outside the help taxonomy because
  `addRawCommands` ran after the group-assignment loop. Every group test passed;
  it was visible only by reading the rendered help.

When adding to this package, prefer assertions that fail when the specific
mechanism is removed, and check them by removing it.
