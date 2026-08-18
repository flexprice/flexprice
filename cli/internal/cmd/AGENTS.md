---
layer: cmd
owns:
  - "cli/internal/cmd/**"
---

# Cmd Layer

> The only package that imports cobra. Wires every other package into the
> command tree the user actually runs.

## Purpose

Defines the root command, global flags, and every subcommand — both
hand-written (`login`, `whoami`, `open`, ...) and spec-dispatched
(`customers list`, `invoices finalize`, ...).

## Key files
| File | Role |
|---|---|
| `root.go` | `Globals`, `NewRootCommand`, `bindGlobals`, `runtimeContext` |
| `resource.go` | `addResourceCommands`, `newOperationCommand` — the spec-driven tree |
| `raw.go` | `addRawCommands` — `get`/`post`/`delete` escape hatch |
| `auth.go` | `login`/`logout`/`whoami` |
| `env.go` | `env list` |
| `config.go` | `config list`/`config use` |
| `init.go` | `init` — guided first run |
| `misc.go` | `open`, `version` |

## Startup wiring order

```
NewRootCommand(version)
  → g := &Globals{}
  → bindGlobals(root.PersistentFlags(), g)
  → root.AddCommand(init, login, logout, whoami, env, config, open, version)
  → addResourceCommands(root, registry, g, version)   spec-driven, 197 commands
  → addRawCommands(root, g, version)                  get/post/delete
```

## Patterns to follow

- `*Globals` is created once per `NewRootCommand` call and threaded as a
  parameter to every constructor (`newXCommand(g *Globals, ...)`). **Never**
  make it a package-level variable — see Pitfalls.
- Every command that talks to the API calls `runtimeContext(g)` first and
  exactly once to resolve credentials — do not duplicate that resolution
  logic in a new command.
- Destructive actions (raw or spec-driven) confirm via `confirmAction`
  (`resource.go`), not a bespoke prompt — see Pitfalls below for why a
  second, disconnected confirmation path is exactly how this CLI shipped a
  real gap.
- Adding a new hand-written command: see
  [../../guides/adding-a-hand-written-command.md](../../guides/adding-a-hand-written-command.md).
- Adding or renaming a spec-driven command: see
  [../../guides/adding-a-command.md](../../guides/adding-a-command.md) — you
  are editing `commands.yaml`, not this package.

## Invariants (must hold)

- Destructive actions (`delete`, `void`, `terminate`, `cancel`, `archive`)
  always confirm before proceeding, unless `--force` or stdin is not a
  terminal — regardless of which profile is active. There is no
  environment-aware skip: no profile carries a signal that would make one
  safe to skip (see `internal/config`'s `AGENTS.md`).

## Common pitfalls

- **A package-level `Globals` variable silently clobbers state between
  instances.** `pflag`'s `*Var` functions write each flag's default into the
  bound pointer at *registration* time, not at parse time — so constructing
  a second `NewRootCommand` in the same process (exactly what a
  table-driven or parallel test does) would overwrite the first instance's
  already-parsed values before it ever executed. This was caught by writing
  a test that constructs two roots and checking for exactly this, reverting
  to a package variable to confirm the test fails, then restoring the
  parameter-based design. If you ever see `var globals Globals` reappear at
  package scope in this file, that is a regression, not a refactor.
- **`collectUnknownFlags`'s manual parsing has one real, documented
  limitation.** A space-separated flag value that itself starts with `--`
  (e.g. `--name --looks-like-a-flag`) is silently dropped rather than
  erroring, because the parser cannot distinguish it from the next flag.
  `--key=value` form has no such limitation. This is narrow enough that it
  was left as documented behavior rather than fixed — do not "fix" it
  without checking whether the fix could instead break the common
  `--key=value` case.
- **The raw `get`/`post`/`delete` escape hatch used to bypass the
  destructive-action confirmation entirely — a real, shipped gap.**
  `raw.go`'s `delete` never called `confirm()`, had no `--force` flag, and
  never checked `term.IsTerminal`, so `flexprice delete <path>` fired
  immediately while `flexprice customers delete <id>` prompted for the same
  kind of action. Fixed by extracting `confirmAction`/`promptConfirm` out of
  `resource.go` so both files share one confirmation path. If you add a new
  raw or hand-written command that can delete/void/finalize/etc., route it
  through `confirmAction`, not a bespoke prompt.
- **`"finalize"` was missing from the `destructive` map — a real, shipped
  gap, not a hypothetical.** `finalizeInvoice` is tagged `x-scope: delete` by
  the backend's own spec, but the CLI never reads `x-scope` anywhere, so
  `flexprice invoices finalize <id>` executed with zero confirmation. Added
  to `destructive`. The `destructive` map is still hand-maintained and
  disconnected from the spec's own `x-scope` classification — if a future
  action verb (e.g. `remove`, `revoke`) is genuinely destructive, it will
  have the identical silent gap unless someone remembers to add it here.
  Driving this from `x-scope` directly would close that permanently but was
  judged out of scope for this fix.
- **`editSkeleton` used to break `--edit` for any editor configured with
  arguments — a real, shipped bug.** `EDITOR="code --wait"` (VS Code) or
  `EDITOR="subl -w"` (Sublime), both common, made `exec.Command` look for a
  single binary literally named with a space in it. `splitEditorCommand`
  now splits on whitespace via `strings.Fields` before exec — it does not
  handle quoted arguments (e.g. a path containing a space); that's a known,
  accepted limitation, not an oversight.

## Related layers

- `internal/spec` — `Registry`/`BuildRequest`/`Skeleton`, consumed by
  `resource.go`
- `internal/client` — `Client`, constructed fresh per command from
  `runtimeContext`'s resolved credentials
- `internal/output` — `Writer`, the last step of every command's `RunE`
