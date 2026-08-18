# Flexprice CLI — DX polish design

Date: 2026-08-18
Status: approved, ready for implementation planning
Builds on: `2026-08-18-flexprice-cli-design.md`, `2026-08-18-flexprice-cli-interactive-ui-design.md`

## 1. Problem

The CLI is not uniformly plain. It is **unevenly polished**, and unevenness is what
reads as premature.

The first five seconds look finished: a block wordmark, an arrow-key region picker, and
colour-aware tables measured with `lipgloss.Width`. Every interaction after that falls
back to bare `fmt.Fprintf`.

The sharpest evidence is in the style package itself. `style.Success`, `style.Error` and
`style.Warning` are defined and **called from no production code**. Only `Header`, `Dim`,
`Accent`, `StatusColor` and `Logo` are wired up — the decorative half. The CLI has a
vocabulary for success and failure and does not use it.

### 1.1 Gap inventory

Tier 1 — every user, every command:

1. **No spinner.** `client.Do` blocks silently on all 197 spec-driven commands. clig.dev:
   *"If you're making a network request, print something before you do it so it doesn't
   hang and look broken."*
2. **Root help is a 44-item flat alphabetical wall**, 34 entries of which read
   `Operations on <x>` and carry no information. `customers`, `invoices` and
   `subscriptions` sit buried between `alert-settings` and `workflows`. The raw
   `get`/`post`/`delete` escape hatch is interleaved with real resources.
3. **Success and failure are visually identical.** `main.go` prints `Error: <text>` with
   no colour and no icon. Login success prints plain text.

Tier 2 — inconsistency:

4. **The destructive-action prompt is a raw `fmt.Fscanln` y/N** (`resource.go`), while
   `huh` drives a polished picker in the same package.
5. **`--all` progress is `\rfetched %d of %d…`** — an unstyled carriage-return hack.
6. **`whoami` and `resources` print bare aligned text**, beside tables that are fully
   styled.

Tier 3 — clig.dev conformance:

7. **No `--no-input`.** Non-interactivity is reachable only by accident of stdin not
   being a TTY.
8. **Empty state is `No results.`** with no next step.
9. **No mutation receipt.** After `customers create` the user gets a table and must
   infer that it worked.
10. **No `TERM=dumb` check.** Colour is gated on `NO_COLOR` and isatty; clig.dev lists
    three signals and two are implemented.
11. **Ctrl-C is entirely unhandled.** No `signal.Notify` anywhere in the module.

### 1.2 What already works and is not in scope

- cobra's typo suggestion fires correctly (`customer` → `customers`).
- The *content* of error messages is good (`not authenticated — run: flexprice init`),
  as is per-operation help, which carries real summaries from the spec. The content is
  fine; the presentation is absent. This design changes presentation, not wording, except
  where a gap above calls for new text.

## 2. Root cause

Eight of the eleven gaps are the same bug wearing different clothes.

Spinner-in-CI, `--no-input`, `TERM=dumb`, Ctrl-C teardown, `--quiet` handling, empty
states, receipts and the confirm prompt all reduce to one question: **does this call site
correctly know whether a human is watching?** Today there are 34 human-facing
`fmt.Fprint*` sites across 7 files in `internal/cmd`, each answering that question
independently, and most not asking it at all.

`internal/style` cannot fix this. It is a passive helper library — correct at what it
does, but nothing obliges a call site to use it, which is exactly how three of its six
functions ended up dead.

## 3. Approach

A new `internal/ui` package owns every human-facing write. Rejected alternatives are
recorded in §9.

### 3.1 Package boundary

Two packages, one sentence each:

- **`internal/style`** — decides *what colour something is*. Pure functions, string in,
  string out. Purpose unchanged.
- **`internal/ui`** — decides *what gets said, to which stream, and whether a human is
  there to see it*.

`ui` depends on `style`. Only `internal/cmd` depends on `ui`.

### 3.2 The type

```go
type UI struct {
    out, err io.Writer
    quiet    bool // --quiet
    noInput  bool // --no-input, or stdin is not a TTY
    animate  bool // stderr is a TTY, TERM != dumb, and !quiet
}
```

Constructed once in `NewRootCommand` and hung on the existing `Globals` as `g.UI`.
`Globals` is already threaded into all 7 files that write human-facing output, so this
reaches every call site with no signature changes.

### 3.3 Per-stream gating, and a bug it fixes

`style.go` gates all colour on **stdout**:

```go
var enabled = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))
```

The status footer is written to **stderr** (`table.go` → `Writer.Warn` → `Writer.Err`).
So `flexprice customers list > out.json` drops colour from a footer that is still going
to a live terminal.

That is minor on its own. The same mistake applied to the spinner would not be:
redirecting stdout is among the most common ways to run this CLI, and gating animation on
stdout would kill the spinner for a user sitting at the terminal watching stderr.

Therefore the gates are **per stream**:

- `style` keeps its stdout gate — correct for table content, which becomes its only
  consumer.
- `ui` carries its own stderr gate.
- The status footer moves out of `internal/output` and into `ui`, where it belongs.

### 3.4 Vocabulary

```go
func (u *UI) Spinner(msg string) *Spinner            // inert handle when !animate
func (u *UI) Success(format string, a ...any)
func (u *UI) Failure(err error)
func (u *UI) Info(format string, a ...any)           // human-facing → stderr
func (u *UI) Data(format string, a ...any)           // command output → stdout
func (u *UI) Receipt(verb, resource, id string)      // "✓ Created customer cus_01J8X…"
func (u *UI) EmptyState(resource string)             // "No customers yet." + next step
func (u *UI) Confirm(action, subject string) error
func (u *UI) Select(title string, opts []Option) (string, error)
```

Each method already knows the quiet / TTY / no-input rules, so no call site has to.
`Confirm` and `Select` return an error naming the flag to pass when `noInput` is set,
which is how gap 7 is fixed once rather than per prompt.

## 4. Spinner semantics

The highest-risk item in this design: a spinner writes escape sequences to a stream that
also carries error output and CI logs.

### 4.1 When it runs

| Condition | Spinner |
|---|---|
| stderr is not a TTY (CI, redirected, piped) | off |
| `TERM=dumb` | off |
| `--quiet` | off |
| otherwise | on |

`NO_COLOR` is deliberately **absent** from that list. It governs colour, not motion;
clig.dev treats the two separately. Under `NO_COLOR` the spinner runs uncoloured.

### 4.2 Implementation: hand-rolled, not `bubbles/spinner`

Roughly 40 lines — a goroutine, a ticker, `\r` + frame + message, and an erase on stop.

`bubbles/spinner` is rejected despite bubbletea already being in the module graph,
because it requires a `tea.Program`, which takes over the terminal and runs an input
loop. This CLI prompts via `huh` immediately after some spinners stop, and two components
contending for terminal control is a class of bug that is miserable to debug and hard to
test. An inline spinner has no such lifecycle, and this keeps bubbletea an indirect
dependency rather than a direct one.

### 4.3 Cursor restoration ties gap 1 to gap 11

A spinner hides the cursor (`\x1b[?25l`). A process that dies without restoring it
(`\x1b[?25h`) leaves the user with an invisible cursor in their shell — a sticky failure
that outlives the CLI and that people typically resolve by killing the terminal.

So Ctrl-C handling is not a separate nicety; it exists primarily to guarantee cursor
restoration. `main.go` gains a `signal.NotifyContext`; the context threads into
`client.Do`, which already accepts one. Teardown stops the spinner, restores the cursor,
prints `✗ Cancelled`, and exits **130** — the conventional SIGINT code. 130 is additive
and changes no existing value in the `exitcode` contract.

### 4.4 Placement

- Wrapping `client.Do` in `resource.go` and `raw.go`.
- Wrapping `VerifyKey` during `login`.
- Replacing the `--all` loop's `\rfetched %d of %d…`, with the message updated **per
  completed page** rather than on a timer. A stalled page then shows as a frozen count
  instead of a spinner that animates while making no progress.

## 5. Grouped help

cobra 1.10.2 has native `AddGroup()` / `GroupID` — the mechanism `gh` uses — so no custom
help function is required. `internal/cmd/groups.go` holds the mapping; `addResourceCommands`
sets `GroupID` while building the tree.

| Group | Members |
|---|---|
| Setup | `init` `login` `logout` `whoami` `env` `config` `open` `version` |
| Core billing | `customers` `subscriptions` `subscription-schedules` `subscription-line-items` `invoices` `payments` `checkout` |
| Usage & metering | `events` `features` `entitlements` `costs` |
| Credits & discounts | `wallets` `credit-grants` `credit-notes` `coupons` `coupon-associations` |
| Catalog & pricing | `plans` `prices` `price-units` `addons` `tax-rates` `tax-associations` |
| Platform | `environments` `secrets` `users` `tenants` `rbac` `groups` `integrations` |
| Automation | `workflows` `tasks` `scheduled-tasks` `alerts` `alert-settings` |
| Advanced | `get` `post` `delete` `resources` `completion` `help` |

All 34 spec-derived resources are mapped (7 + 4 + 5 + 6 + 7 + 5 = 34).

### 5.1 Two guards against rot

A hand-maintained taxonomy over a spec-derived command tree will drift the moment the API
adds a resource. Both guards are required:

1. **Runtime fallback.** An unmapped resource gets `GroupID: "additional"` and renders
   under "Additional commands". It is never silently dropped from help.
2. **`TestEveryResourceHasAGroup`.** Walks the registry and fails, naming any unmapped
   resource. Adding a resource then costs one line in `groups.go`, and CI says which.

### 5.2 Resource shorts

The 34 identical `Operations on <x>` strings are replaced with hand-written one-line
descriptions in the same `groups.go` table. Deriving a parent short from operation
summaries was considered and rejected: the summaries describe individual operations
(`Get customer by external ID`), not the resource, so any derivation rule produces
misleading parents. These are one line each and rarely change.

## 6. Remaining gaps

| Gap | Fix | Stream |
|---|---|---|
| 3 · success/failure identical | `main.go` → `ui.Failure(err)`; login/logout → `ui.Success` | stderr |
| 4 · raw `y/N` prompt | `ui.Confirm` wrapping `huh.NewConfirm`, matching the region picker | stderr |
| 6 · bare `whoami` / `resources` | `ui.Data` plus `style` for keys; `✓`/`✗` on whether the key resolved | stdout |
| 7 · no `--no-input` | one global flag; `ui.Confirm` / `ui.Select` error naming the flag to pass | — |
| 8 · `No results.` | `ui.EmptyState` — `No customers yet.` plus a concrete next command | stderr |
| 9 · no mutation receipt | `ui.Receipt` — `✓ Created customer cus_01J8X…` | stderr |
| 10 · no `TERM=dumb` | added to `style`'s colour gate and `ui`'s animate gate | — |

`ui.Receipt` fires only when the action is a mutation **and** the response carries an
`id`. When it cannot identify what happened it says nothing rather than guessing.

`whoami` and `resources` gain styling but **keep writing to stdout**, via `ui.Data`.
Their output is a command's result, not commentary on it, and people parse both — moving
either to stderr to make it "human-facing" would break those callers. Styling changes how
they look on a terminal; it must not change which stream they use.

### 6.1 The stdout contract

Receipts, empty states and the status footer go to **stderr**, never stdout.

`flexprice customers create … --output json | jq` must produce byte-identical stdout
before and after this change. The existing golden tests in `internal/output` must pass
**unmodified**. If any of them requires editing, that is the signal the contract broke —
not a reason to update the golden file.

## 7. Tone

Plain and factual wherever data or money is involved; light warmth confined to `init` and
`login`, where the user is a newcomer rather than an operator.

```
  Welcome to Flexprice — let's get you set up.
  ⠋ Verifying your key…
  ✓ You're all set. Here's what to try first:
        flexprice customers list
```

```
  ⠋ Fetching customers…
  ✓ 42 customers
  ✗ POST /v1/subscriptions failed (HTTP 422)
      plan_id: required
```

No varied or characterful copy in operational commands. Whimsical wording beside
irreversible billing actions costs trust, and personality carried by colour, iconography
and layout does not date the way copy does.

## 8. Testing

Two defects in the previous round — misaligned table columns and a visibly crooked
welcome box — **passed their tests the entire time**, because those tests asserted only
that output was produced. This plan is written against that failure mode.

1. **A 4-way matrix test on `ui`** — TTY × `--quiet` × `TERM=dumb` × `--no-input` —
   asserting exactly what is and is not written in each cell. This is the test that
   catches the spinner-in-CI class of bug.
2. **Assert absence.** A non-TTY `UI` must write **zero** ANSI escape bytes. This is the
   inverse of the assertion style that let the earlier bugs through, and it is what stops
   Christmas-tree CI logs.
3. **A golden file for the grouped root help** — the first thing a new user sees, and
   currently untested in any form.
4. **`TestEveryResourceHasAGroup`** — the taxonomy freshness gate from §5.1.
5. **A cursor-restoration test** — teardown emits `\x1b[?25h`. The failure mode is bad
   enough that this must not rest on manual checking.
6. **Existing golden tests run unmodified**, as the stdout-contract check in §6.1.

### 8.1 What testing cannot cover

No test can assert that a spinner *looks* right, that its frame rate is comfortable, or
that its erase leaves no artifact. That needs a human at a real terminal. This is the
same class of gap the handoff already records for the arrow-key region picker, and both
should be listed together as manual verification steps rather than implied to be covered.

Verification command, per the operational hazard recorded in the handoff — the binary
touches the real OS keychain regardless of which HTTP backend it points at:

```
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file ./bin/flexprice init
```

## 9. Alternatives considered

**Point fixes** — fix each of the 11 gaps where it lives. Fastest, each change
independently reviewable. Rejected because it leaves the structural condition that
produced the problem: output stays 34 scattered `fmt.Fprintf` calls and `style` stays
optional, so the 198th command reintroduces the drift. It answers *what* drifted, not
*why*.

**Hybrid** — `ui` for new interaction surface only; migrate the existing 34 call sites
opportunistically. Rejected because it leaves two packages with an ambiguous boundary and
a codebase where some output is governed and some is not — which is precisely the
condition that produced the current unevenness, rebuilt in a new shape.

**`bubbles/spinner`** — rejected in §4.2 (terminal-control contention with `huh`).

**Deriving resource shorts from spec summaries** — rejected in §5.2 (summaries describe
operations, not resources).

## 10. Cost and risk

Touches all 7 files in `internal/cmd` and rewrites ~34 call sites. That is real churn in
a package with existing test coverage. The mitigations are that the suite is currently
green across 8 packages and 168 test functions, and that `Globals` threading means no
signature changes beyond those already present.

The principal risk is the spinner leaking escape sequences into non-interactive output.
It is mitigated by centralising the suppression rule in one place with the matrix test in
§8, rather than relying on 34 call sites to each remember it.
