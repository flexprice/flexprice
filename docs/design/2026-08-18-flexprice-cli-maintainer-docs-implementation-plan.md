# Flexprice CLI Maintainer Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a future maintainer — human or agent — everything needed to work in `cli/` without having read the design docs that produced it: a constitution, six per-package guides, and two task walkthroughs.

**Architecture:** Pure documentation, no code changes. Nine new Markdown files plus one link added to an existing file. Each per-package `AGENTS.md` and each guide is fully independent of the others — no shared state, safe to write in parallel.

**Tech Stack:** Markdown only.

**Design doc:** `docs/design/2026-08-18-flexprice-cli-maintainer-docs-design.md`

---

## A note on Pitfalls sections

The design (§8, decision M4) deferred every package's "Common pitfalls" section
until a separately-running code review produced findings to ground them in,
rather than writing speculative ones. In practice, this session's own
implementation and review work already produced real, citable incidents for
every package — not guesses, actual bugs found and fixed with commit SHAs:

- **keyring** — a live macOS "Keychain Not Found" system dialog, triggered by
  running the real binary without `FLEXPRICE_KEY_BACKEND=file` set.
- **cmd** — `Globals` as a package-level variable caused a second
  `NewRootCommand` call to silently clobber the first's parsed state
  (`TestNewRootCommand_InstancesDoNotShareState`).
- **output** — a binary response (e.g. `invoices pdf`) passes through
  unguarded to an interactive terminal; known, confirmed twice, deliberately
  deferred rather than fixed.
- **spec** — the cycle guard bounds breadth, not just depth; removing it grew
  a real walk from 1,693 to 17,789 nodes.
- **client** — the default retry policy retried POST identically to GET,
  which could have duplicated a subscription on a post-commit 5xx.
- **config** — `Save`'s `Chmod` calls were proven to be no-ops under any
  realistic umask, and `Save` was made atomic after finding it was not.

Each Pitfalls section below is written now, grounded in these. The separately
running code review may surface additional issues — Task 11 is a lightweight
follow-up to fold in anything new it finds, not a hard gate on writing this
plan's content.

---

## File structure

```
cli/
├── AGENTS.md                              new
├── ARCHITECTURE.md                        one line added
├── guides/
│   ├── adding-a-command.md                new
│   └── adding-a-hand-written-command.md   new
└── internal/
    ├── client/AGENTS.md                   new
    ├── cmd/AGENTS.md                      new
    ├── config/AGENTS.md                   new
    ├── keyring/AGENTS.md                  new
    ├── output/AGENTS.md                   new
    └── spec/AGENTS.md                     new
```

---

## Task 1: `cli/AGENTS.md` (constitution)

**Files:**
- Create: `cli/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/AGENTS.md`:

```markdown
---
layer: constitution
owns:
  - "cli/**"
---

# Flexprice CLI — Constitution

> Invariants that must hold everywhere in this repo.
> Package-specific rules → the AGENTS.md in that package.
> Why → decisions/, narrative → ARCHITECTURE.md.

## Stack
Go 1.25 · cobra · kin-openapi · go-keyring · BurntSushi/toml · go-retryablehttp

## Directory map
| Package | Path | One-line rule |
|---|---|---|
| Client | `internal/client/` | The only package that makes a network call — [ADR 0001](decisions/0001-no-sdk-single-http-path.md) |
| Spec | `internal/spec/` | Loads the embedded OpenAPI doc, resolves commands, builds requests — never calls the network |
| Config | `internal/config/` | Profiles and credential precedence, no secrets |
| Keyring | `internal/keyring/` | API key storage — OS keychain, encrypted-file fallback |
| Output | `internal/output/` | Renders responses — the only package that writes to stdout |
| Cmd | `internal/cmd/` | The only package that imports cobra; wires the above together |

## Hard invariants

- No second way to make an HTTP request — everything goes through
  `client.Client.Do` ([ADR 0001](decisions/0001-no-sdk-single-http-path.md)).
- Never retry a non-idempotent method (POST/PATCH) on 5xx or a transport
  error ([ADR 0002](decisions/0002-retry-only-idempotent-methods.md)).
- Never send `x-environment-id` — an API key already determines its
  environment.
- No live/test flag on a profile, no `--environment` flag anywhere — nothing
  reachable by a key reveals its environment
  ([ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md)).
- `commands.yaml` is hand-curated. An unmapped operation gets a derived name
  and a CI warning, never a build failure
  ([ADR 0004](decisions/0004-curated-commands-yaml-over-mechanical-derivation.md)).
- Region list comes from the embedded spec's `servers[]`, never hardcoded
  ([ADR 0005](decisions/0005-region-discovery-from-openapi-servers.md)).
- Comments only where the logic or a constraint is genuinely non-obvious —
  no narration of straightforward code.
- No `Co-Authored-By`, no mention of Claude/Anthropic/AI assistance, anywhere
  in a commit or a comment.

## Where to look next

- [ARCHITECTURE.md](ARCHITECTURE.md) — narrative overview, request lifecycle
- [decisions/](decisions/) — why, for the five decisions above
- [guides/](guides/) — how to do the two most common maintenance tasks
- The package you're working in has its own `AGENTS.md` — read it first
```

- [ ] **Step 2: Verify every internal link resolves**

```bash
cd cli
for f in ARCHITECTURE.md \
         decisions/0001-no-sdk-single-http-path.md \
         decisions/0002-retry-only-idempotent-methods.md \
         decisions/0003-environment-scoped-profiles-no-live-flag.md \
         decisions/0004-curated-commands-yaml-over-mechanical-derivation.md \
         decisions/0005-region-discovery-from-openapi-servers.md; do
  test -f "$f" && echo "OK: $f" || echo "MISSING: $f"
done
# guides/ does not exist yet — created by Tasks 8-9. Expect MISSING for
# guides/*.md links until this plan's guide tasks land; do not treat as a
# failure of this task specifically.
```

Expected: all six `OK`.

- [ ] **Step 3: Verify the directory map matches reality**

```bash
find cli/internal -maxdepth 1 -type d | sort
```

Expected: `client`, `cmd`, `config`, `exitcode`, `keyring`, `output`, `spec`.
Note `exitcode` is intentionally not a row in the table — it's a small leaf
dependency of `client`, not a stage of the request pipeline; this is
consistent with `ARCHITECTURE.md`'s existing treatment of it.

- [ ] **Step 4: Commit**

```bash
git add cli/AGENTS.md
git commit -m "docs(cli): add the CLI constitution"
```

---

## Task 2: `cli/internal/client/AGENTS.md`

**Files:**
- Create: `cli/internal/client/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/client/AGENTS.md`:

```markdown
---
layer: client
owns:
  - "cli/internal/client/**"
---

# Client Layer

> The only package that makes a network call. Every request — hand-written
> commands, spec-dispatched resource commands, and any future fixture
> engine — goes through here.
> Why → ../../decisions/0001-no-sdk-single-http-path.md,
> ../../decisions/0002-retry-only-idempotent-methods.md.

## Purpose

Issues HTTP requests, applies retry policy, redacts `--debug` output, and
normalizes every error response this API returns into one type. Nothing
outside this package touches `net/http` for talking to Flexprice.

## Key files
| File | Role |
|---|---|
| `client.go` | `Client`, `New`, `Do`, `retryPolicy`, `--debug` redaction |
| `errors.go` | `APIError`, `NewAPIError` — normalizes three response envelope shapes |

## Request path

```
Do(ctx, method, path, query, body)
  → build *http.Request, set x-api-key + User-Agent
  → retryablehttp with retryPolicy as CheckRetry
  → non-2xx → NewAPIError(status, body, method, path)
  → 2xx → raw bytes returned as-is
```

## Patterns to follow

- Every request goes through `Do`. Do not add a second function that issues
  requests, even for a "simple" one-off call.
- Always thread `ctx` through — no request without a context.
- `Options.Timeout` defaults to 30s (`DefaultTimeout`) if unset; do not
  construct a `Client` with an unbounded timeout.

## Invariants (must hold)

- `retryPolicy` retries only `GET`/`HEAD`/`PUT`/`DELETE`/`OPTIONS` on 5xx or
  a transport error, plus `429` for every method. Never add POST/PATCH to
  the retried set without solving the duplicate-write problem first (see
  Pitfalls below).
- `--debug` redaction (`safeKeys` in `client.go`) is an allowlist, not a
  denylist. A field not on the list is redacted even if it looks harmless —
  do not "fix" a redacted field you want to see by widening the list without
  checking whether it can carry free text.
- `New`'s `BaseURL` handling returns an error via `c.baseErr` rather than
  panicking or silently defaulting — checked lazily on the first `Do` call so
  `New` itself has no error return.

## Common pitfalls

- **Retrying a non-idempotent write can duplicate it.** The first
  implementation used `go-retryablehttp`'s default policy, which inspects
  only the status code, never the method — it retried `POST` exactly like
  `GET`. `CreateSubscriptionRequest` has no idempotency key at all, and where
  a body-level `idempotency_key` exists elsewhere, the server generates one
  containing a timestamp if the caller omits it, so it differs per attempt
  even though the retried body is byte-identical — server-side dedup does not
  save you. Fixed in the commit that added `retryPolicy`; full reasoning in
  [ADR 0002](../../decisions/0002-retry-only-idempotent-methods.md). If you
  are ever tempted to add POST to the retried methods, you are re-opening
  this bug.
- **A malformed `--base-url` used to fail deep in the HTTP stack** with a
  confusing `unsupported protocol scheme ""` rather than naming the actual
  cause. `New` now validates `Scheme`/`Host` are present and surfaces a clear
  error instead.

## Related layers

- `internal/spec` — builds the `*Request` this package's `Do` executes
- `internal/cmd` — the only caller of `client.New`/`Do`
- `internal/exitcode` — `APIError.ExitCode()` maps into these constants
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'func (c \*Client) Do' internal/client/client.go
grep -n 'func retryPolicy' internal/client/client.go
grep -n 'var safeKeys' internal/client/client.go
grep -n 'idempotentMethods' internal/client/client.go
grep -n 'DefaultTimeout' internal/client/client.go
grep -n 'baseErr' internal/client/client.go
```

Expected: every symbol named above exists in the current file. If any line
has moved or a name has changed, update the citation in the file you wrote
before committing.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/client/AGENTS.md
git commit -m "docs(cli): add internal/client AGENTS.md"
```

---

## Task 3: `cli/internal/spec/AGENTS.md`

**Files:**
- Create: `cli/internal/spec/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/spec/AGENTS.md`:

```markdown
---
layer: spec
owns:
  - "cli/internal/spec/**"
---

# Spec Layer

> Turns the embedded OpenAPI document into a resolvable command tree and
> turns one resolved command plus user input into an HTTP request. Never
> touches the network.
> Why → ../../decisions/0004-curated-commands-yaml-over-mechanical-derivation.md,
> ../../decisions/0005-region-discovery-from-openapi-servers.md.

## Purpose

The biggest package in the CLI, and the one with the most non-obvious
constraints — start here if you are extending what commands the CLI
supports.

## Key files
| File | Role |
|---|---|
| `loader.go` | `Load` (parses the embedded spec), `Regions`, `Operations`, `EventTypes` |
| `registry.go` | `NewRegistry`, `Command`, curates `commands.yaml` against the spec |
| `request.go` | `BuildRequest` — turns a `Command` + `Input` into a `Request` |
| `skeleton.go` | `Skeleton` — generates an editable body for `--edit` |
| `paginate.go` | `PageInfo`, `ApplyPaging` — reads/writes pagination envelopes |

## Command resolution path

```
Load()                         parse the embedded OpenAPI document
  → Operations(doc)            every callable operation, Webhook Events stubs excluded
  → NewRegistry(doc)            curate against commands.yaml, derive names for the rest
  → Registry.Lookup(r, a)       resolve one resource+action to a Command
  → BuildRequest(cmd, in)       path/query/body from flags, --data, or --edit
  → (optional) Skeleton(cmd)    generate an --edit body for a deep schema
```

## Patterns to follow

- `doc.Paths` is `*openapi3.Paths`, **not a map** — always go through
  `.Map()`, never index it directly.
- The `Webhook Events` tag (56 entries) is documentation stubs with no
  `operationId` and synthetic paths that 404 if called — excluded from
  `Operations()`, but kept as the source for `EventTypes()`.
- `BuildRequest` must never mutate the caller's `Input.Flags` map — it is
  called more than once against the same `Input` by the `--all` pagination
  loop in `internal/cmd/resource.go` (see Pitfalls below).

## Invariants (must hold)

- `Skeleton`'s depth cap is 16, not smaller — natural nesting in this spec
  reaches depth 14, and a cap of 12 was measured to truncate real nodes.
- `Skeleton`'s cycle guard (`onPath`) bounds breadth, not just depth —
  removing it grows a real walk (`SubscriptionResponse`) from 1,693 to
  17,789 nodes. The depth cap alone guarantees termination; the cycle guard
  is what keeps it fast.
- `Skeleton` only ever emits **required** fields as live JSON; every optional
  field is listed in a comment block, never emitted with a placeholder value.
  This is deliberate: an untouched optional numeric field sent as `""` fails
  the server's request binding outright with no field-level detail — proven
  by a live round-trip against the real API during the implementation spike.
- `commands.yaml` validation is default-allow (`registry.go`): an unmapped
  operation gets a derived name and a warning, never a hard failure. Do not
  make this stricter — see [ADR 0004](../../decisions/0004-curated-commands-yaml-over-mechanical-derivation.md)
  for why.

## Common pitfalls

- **Mechanical name derivation silently misassigns commands, it does not just
  produce ugly ones.** The bootstrap tool used to generate a starting
  `commands.yaml` resolved `entitlements retrieve` to `getAddonEntitlements`
  instead of `getEntitlement`, and `subscriptions list` to
  `listAllSubscriptionSchedules` instead of `querySubscription` — both
  because the wrong operation ID happened to sort first alphabetically in a
  first-come-first-served collision resolver. Never trust a derived name
  without checking it against the spec; see
  [guides/adding-a-command.md](../../guides/adding-a-command.md).
- **`BuildRequest` used to delete consumed keys from the caller's own
  `Flags` map.** `Input` is passed by value, but `Flags` is a map, so the
  delete mutated the caller's original regardless. The `--all` pagination
  loop calls `BuildRequest` more than once against the same `Input` to
  rebuild each page's request — a GET-based list with a query filter (e.g.
  `payments list --status succeeded --all`) would apply the filter to page
  one only and silently lose it on every page after. Fixed by cloning
  `Flags` internally before consuming it. If you add a new field to `Input`
  that gets consumed/deleted during `BuildRequest`, check whether it needs
  the same treatment.
- **The spec's `required` list under-describes what the API actually
  needs.** `CreateSubscriptionRequest` does not mark `customer_id` as
  required, even though a subscription is meaningless without one. Do not
  assume "not in `required`" means "safe to omit" when writing anything that
  reasons about which fields matter.

## Related layers

- `internal/cmd` — the only consumer of `Registry`/`BuildRequest`/`Skeleton`
- `cli/spec/commands.yaml` — the curated map this package validates
- `cli/spec/openapi.json` — the embedded spec, synced via `make sync-cli-spec`
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'func Load()\|func Regions(\|func Operations(\|func EventTypes(' internal/spec/loader.go
grep -n 'func NewRegistry(' internal/spec/registry.go
grep -n 'func BuildRequest(' internal/spec/request.go
grep -n 'func Skeleton(' internal/spec/skeleton.go
grep -n 'func PageInfo(\|func ApplyPaging(' internal/spec/paginate.go
grep -n 'maxSkeletonDepth' internal/spec/skeleton.go
grep -n 'WebhookEventsTag' internal/spec/loader.go
```

Expected: every symbol exists. Update citations if any have moved.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/spec/AGENTS.md
git commit -m "docs(cli): add internal/spec AGENTS.md"
```

---

## Task 4: `cli/internal/config/AGENTS.md`

**Files:**
- Create: `cli/internal/config/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/config/AGENTS.md`:

```markdown
---
layer: config
owns:
  - "cli/internal/config/**"
---

# Config Layer

> Profiles and credential precedence. Holds no secrets — API keys live in
> `internal/keyring` and are referenced by `KeyRef`.
> Why → ../../decisions/0003-environment-scoped-profiles-no-live-flag.md.

## Purpose

Loads and saves `~/.flexprice/config.toml`, and resolves which credentials a
given invocation should use.

## Key files
| File | Role |
|---|---|
| `config.go` | `Config`, `Profile`, `Load`, `Save`, `Resolve`, `ProfileName` |
| `resolve.go` | `ResolveContext`, `Overrides`, `RuntimeContext` — the precedence chain |

## Credential precedence

```
ResolveContext(cfg, store, overrides)
  1. --api-key flag / FLEXPRICE_API_KEY env  → wins outright
  2. named or default profile's KeyRef        → looked up in the keyring
  3. --region / --base-url                    → resolves the base URL
  4. neither key nor region resolvable         → clear error naming the fix
```

## Patterns to follow

- `Profile` has no environment name and no live/test flag — this is
  deliberate, not an oversight. See
  [ADR 0003](../../decisions/0003-environment-scoped-profiles-no-live-flag.md)
  before you consider adding one back.
- Every error path in `resolve.go` must be checked for API-key leakage
  before merging a change — no `fmt.Errorf` may interpolate the key value.

## Invariants (must hold)

- `Save` writes atomically: temp file in the same directory, then
  `os.Rename`. Never revert this to `O_TRUNC` — a crash mid-write must not
  destroy the user's existing profiles.
- File and directory permissions are `0600`/`0700`, set explicitly even
  though they are typically no-ops under a normal umask (see Pitfalls).

## Common pitfalls

- **The `Chmod` calls that "harden" permissions after `MkdirAll`/`OpenFile`
  are provably no-ops under any realistic umask.** `MkdirAll(0o700)` and
  `OpenFile(..., 0o600)` already request modes with no group/other bits, and
  umask only clears bits — it cannot add them back. This was verified by
  removing the `Chmod` calls and confirming the permissions test still
  passed under umask `022` and umask `000`. They are kept anyway as defense
  against the requested mode being loosened later, not because they
  currently do anything. Do not read their presence as evidence that
  something would otherwise go wrong today.
- **`toml.Unmarshal` merges into an existing map rather than replacing it.**
  `Load` is safe today because it always starts from a fresh empty
  `Profiles` map, but if a future change reuses a non-empty `*Config` across
  calls, stale profiles would silently survive a `Load` that no longer
  mentions them.

## Related layers

- `internal/keyring` — where the credential a `Profile.KeyRef` points to
  actually lives
- `internal/cmd` — `runtimeContext` in `root.go` is the only caller of
  `ResolveContext`
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'func Load(\|func Save(\|func (c \*Config) Resolve(\|func ProfileName(' internal/config/config.go
grep -n 'func ResolveContext(' internal/config/resolve.go
grep -n 'type Profile struct' -A6 internal/config/config.go
```

Expected: every symbol exists, and `Profile`'s fields are exactly `Region`,
`BaseURL`, `Label`, `KeyRef` — no `Environment` or `Live` field. If that has
changed, the pattern note above is now wrong and must be corrected before
committing, not left stale.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/config/AGENTS.md
git commit -m "docs(cli): add internal/config AGENTS.md"
```

---

## Task 5: `cli/internal/keyring/AGENTS.md`

**Files:**
- Create: `cli/internal/keyring/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/keyring/AGENTS.md`:

```markdown
---
layer: keyring
owns:
  - "cli/internal/keyring/**"
---

# Keyring Layer

> API key storage. Prefers the OS keychain, falls back to an encrypted file
> when none is available.

## Purpose

`config.Profile.KeyRef` points here. This package is the only one that
touches an OS credential store or writes key material to disk.

## Key files
| File | Role |
|---|---|
| `keyring.go` | `Store` interface, `OSKeyring`, `Open` (probes and picks a backend) |
| `file.go` | `FileStore` — AES-GCM encrypted file fallback |

## Backend selection

```
Open()
  → FLEXPRICE_KEY_BACKEND=file set?  → FileStore, no probe
  → else: probe the OS keychain (bounded, see Pitfalls)
  → probe succeeds → OSKeyring
  → probe fails or times out → FileStore, with a warning string for the caller to print
```

## Patterns to follow

- Never call the OS keychain without going through `Open`'s bounded probe
  first. A direct, unprobed call can hang indefinitely in a headless or
  no-keychain environment (see Pitfalls) — this is not hypothetical, it has
  happened in this repo's own development.
- Testing this package: `FileStore` tests must construct `&FileStore{Dir:
  t.TempDir()}` directly and never call `Open()` — calling `Open()` in a
  test risks the same real-keychain interaction described below.

## Invariants (must hold)

- The keychain probe in `Open` is bounded (`probeTimeout`, 2s) — this exists
  specifically because the Linux secret-service backend calls
  `svc.Unlock(...)` over D-Bus, which can block indefinitely when D-Bus is
  reachable but no prompt agent exists. Never remove this timeout.
- `FileStore.Set` writes atomically (temp file + rename) for the same reason
  `config.Save` does — two processes racing on the same profile must not
  produce a corrupt file.

## Common pitfalls

- **Running the real CLI binary against the real OS keychain in an
  automated or agent context is genuinely dangerous, not just noisy.**
  During this CLI's own development, an agent running the built binary
  without `FLEXPRICE_KEY_BACKEND=file` set triggered a live macOS "Keychain
  Not Found" system dialog on the developer's actual screen, offering a
  "Reset To Defaults" button that would have reset the user's real default
  keychain — a destructive, unrelated system action. **Any test, script, or
  agent invocation of the built `flexprice` binary that touches credential
  storage must set `FLEXPRICE_KEY_BACKEND=file` and ideally a scratch
  `HOME`.** This is not a style preference; treat it as a hard rule for
  anyone automating this CLI.
- **The encrypted file fallback is not protection against a local
  attacker.** Its AES-GCM key is derived from the hostname and directory
  path — stable, not secret. It stops casual disclosure (an accidental
  `cat`, a backup, shoulder-surfing); the real control is the `0600` file
  mode, not the encryption. Do not describe this to a user as "your key is
  encrypted" in a way that implies more than that.
- **A hostname change makes a stored file-backed key undecryptable.**
  `Get`'s error message names the likely cause and the fix
  (`flexprice login` again) rather than surfacing a bare crypto error —
  keep that framing if you touch this path.

## Related layers

- `internal/config` — `Profile.KeyRef` names which backend/entry a profile
  uses
- `internal/cmd` — `runtimeContext` in `root.go` is the only caller of `Open`
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'func Open(' internal/keyring/keyring.go
grep -n 'probeTimeout' internal/keyring/keyring.go
grep -n 'func (f \*FileStore) Set(' internal/keyring/file.go
grep -n 'FLEXPRICE_KEY_BACKEND' internal/keyring/keyring.go
```

Expected: every symbol exists.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/keyring/AGENTS.md
git commit -m "docs(cli): add internal/keyring AGENTS.md"
```

---

## Task 6: `cli/internal/output/AGENTS.md`

**Files:**
- Create: `cli/internal/output/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/output/AGENTS.md`:

```markdown
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
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'func (w Writer) Render(' internal/output/output.go
grep -n 'func rowsFrom(' internal/output/table.go
grep -n 'func defaultColumns(' internal/output/table.go
grep -n 'func format(' internal/output/table.go
```

Expected: every symbol exists.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/output/AGENTS.md
git commit -m "docs(cli): add internal/output AGENTS.md"
```

---

## Task 7: `cli/internal/cmd/AGENTS.md`

**Files:**
- Create: `cli/internal/cmd/AGENTS.md`

- [ ] **Step 1: Write the file**

`cli/internal/cmd/AGENTS.md`:

```markdown
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

## Related layers

- `internal/spec` — `Registry`/`BuildRequest`/`Skeleton`, consumed by
  `resource.go`
- `internal/client` — `Client`, constructed fresh per command from
  `runtimeContext`'s resolved credentials
- `internal/output` — `Writer`, the last step of every command's `RunE`
```

- [ ] **Step 2: Verify citations**

```bash
cd cli
grep -n 'type Globals struct' internal/cmd/root.go
grep -n 'func NewRootCommand(' internal/cmd/root.go
grep -n 'func runtimeContext(' internal/cmd/root.go
grep -n 'func addResourceCommands(' internal/cmd/resource.go
grep -n 'func addRawCommands(' internal/cmd/raw.go
grep -n 'func collectUnknownFlags(' internal/cmd/resource.go
grep -n 'var destructive' internal/cmd/resource.go
grep -n 'TestNewRootCommand_InstancesDoNotShareState' internal/cmd/root_test.go
```

Expected: every symbol exists.

- [ ] **Step 3: Commit**

```bash
git add cli/internal/cmd/AGENTS.md
git commit -m "docs(cli): add internal/cmd AGENTS.md"
```

---

## Task 8: `cli/guides/adding-a-command.md`

**Files:**
- Create: `cli/guides/adding-a-command.md`

- [ ] **Step 1: Write the file**

`cli/guides/adding-a-command.md`:

```markdown
# Adding or renaming a curated command

The CLI does not generate command names from the API spec — it resolves them
through `cli/spec/commands.yaml`, a hand-maintained map from `resource
action` to an OpenAPI `operationId`. This is the single most common
maintenance task in this repository: the backend adds an endpoint, and
someone needs to give it a real name.

## Why this can't be automatic

There is no `GET /customers` in the API — listing is `POST
/customers/search` (`queryCustomer`). A name derived mechanically from the
path and HTTP method has nothing to derive `customers list` from. Worse, a
purely mechanical bootstrap tool that resolved collisions
alphabetically once produced two silent misassignments: `entitlements
retrieve` resolved to `getAddonEntitlements` instead of `getEntitlement`, and
`subscriptions list` resolved to `listAllSubscriptionSchedules` instead of
`querySubscription` — both because the wrong operation ID happened to sort
first. Every command name in this CLI reflects a human decision for exactly
this reason.

## What happens when you don't do anything

Nothing breaks. `internal/spec/registry.go`'s validation is default-allow: an
operation missing from `commands.yaml` gets an automatically derived name
(`kebab-case` of its tag and operationId) and a warning, printed under
`--debug`. CI (`.github/workflows/cli-validate.yml`) surfaces this as a
notice, not a failure — a backend PR that adds an endpoint is never blocked
on this file.

## Steps to give it a real name

1. Find the new operation. If you know its `operationId`, search for it in
   `docs/swagger/swagger-3-0.json`. If you only know roughly what it does,
   run the registry's bootstrap tool to see what name it would get by
   default:

   ```bash
   cd cli && go run ./tools/bootstrap-commands | grep -A2 -B2 <partial-name>
   ```

2. Open `cli/spec/commands.yaml` and find (or create) the resource block for
   the right resource name — match the existing pattern of domain nouns
   (`customers`, `invoices`, `subscriptions`), not the raw API tag.

3. Add the mapping using a domain verb, not an HTTP verb — `retrieve` not
   `get`, `list` not `search`, matching what similar existing entries in the
   file already use:

   ```yaml
   invoices:
     retrieve: getInvoice
     finalize: finalizeInvoice     # <- e.g. adding this line
   ```

4. If the new operation is a list operation, add a `columns:` entry so table
   output shows something useful instead of falling back to a generic
   heuristic:

   ```yaml
   invoices:
     columns: [id, invoice_number, invoice_status, total, created_at]
   ```

5. Run the registry's validation test:

   ```bash
   cd cli && go test ./internal/spec/ -run TestRegistry -v
   ```

   `TestRegistry_EveryOperationIsAccountedFor` will fail if you introduced a
   collision (two mappings resolving to the same resource+action) or pointed
   at an `operationId` that doesn't exist in the spec — both are real
   mistakes to fix, not test flakiness.

6. Verify the actual command works end to end:

   ```bash
   cd cli && go build -o bin/flexprice . && ./bin/flexprice <resource> --help
   ```

## When to exclude an operation instead

Some operations are superseded by a newer version (`recalculateInvoice` by
`recalculateInvoiceV2`) or otherwise shouldn't be a command at all. List
these under `commands.yaml`'s `exclude:` key with a one-line comment
explaining why — an operation must be either mapped or excluded, never
silently absent, which is exactly what
`TestRegistry_EveryOperationIsAccountedFor` enforces.
```

- [ ] **Step 2: Verify the referenced commands actually work**

```bash
cd cli
test -f tools/bootstrap-commands/main.go && echo "OK: bootstrap tool exists"
grep -n 'func TestRegistry_EveryOperationIsAccountedFor' internal/spec/registry_test.go
test -f ../.github/workflows/cli-validate.yml && echo "OK: CI workflow exists"
```

Expected: all three checks pass.

- [ ] **Step 3: Commit**

```bash
mkdir -p cli/guides
git add cli/guides/adding-a-command.md
git commit -m "docs(cli): add the adding-a-command guide"
```

---

## Task 9: `cli/guides/adding-a-hand-written-command.md`

**Files:**
- Create: `cli/guides/adding-a-hand-written-command.md`

- [ ] **Step 1: Write the file**

`cli/guides/adding-a-hand-written-command.md`:

```markdown
# Adding a new hand-written command

Most commands are resolved automatically from the API spec (see
[adding-a-command.md](adding-a-command.md)). Some — `login`, `whoami`,
`init`, and future commands like `events`/`fixtures`/`listen` — are
hand-written because they don't map to a single API operation, or need
interactive behavior the spec-driven path doesn't support. This walks
through the actual pattern this codebase uses, taken directly from
`internal/cmd/auth.go` and `internal/cmd/misc.go`.

## The pattern

1. **New file in `internal/cmd/`.** One file per logical group of commands
   (`auth.go` holds `login`/`logout`/`whoami`; `misc.go` holds `open`/
   `version`) — don't add a new top-level command to an existing file that
   isn't already about the same thing.

2. **A constructor taking `*Globals`.** Every command constructor follows
   this shape:

   ```go
   func newExampleCommand(g *Globals, version string) *cobra.Command {
       return &cobra.Command{
           Use:   "example",
           Short: "One-line description",
           RunE: func(cc *cobra.Command, args []string) error {
               // ...
           },
       }
   }
   ```

   `g *Globals` is a parameter, never a package-level variable — see
   `internal/cmd/AGENTS.md`'s Pitfalls section for exactly why this matters
   and what breaks if you get it wrong.

3. **Resolve credentials via `runtimeContext`, if you need the API.**

   ```go
   rc, _, err := runtimeContext(g)
   if err != nil {
       return err
   }
   cl := client.New(client.Options{
       BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
       Debug: g.Debug, DebugOut: os.Stderr,
   })
   ```

   Do not resolve credentials any other way — this is the one place
   precedence (flag → env var → keyring → config file) is applied, and
   duplicating it elsewhere risks the two paths disagreeing.

4. **Render output via `internal/output.Writer`, if you return API data.**

   ```go
   format, err := output.ParseFormat(g.Output)
   if err != nil {
       return err
   }
   w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
   return w.Render(raw, output.Options{Quiet: g.Quiet})
   ```

   Never write response data directly with `fmt.Println` — it bypasses the
   stdout/stderr contract every other command follows, and breaks
   `--output json` for this command specifically.

5. **Register it in `root.go`.** Add the new command to the
   `root.AddCommand(...)` call in `NewRootCommand`. If you are adding this
   alongside other new commands landing at the same time, be careful: this
   is a single shared call, and two changes touching it independently (e.g.
   two people, or two agents, working in parallel) can produce a real merge
   conflict or a silent overwrite if not integrated carefully — this
   happened during the CLI's initial build.

## What you get for free by following this pattern

- `--profile`, `--output`, `--debug`, `--quiet`, and every other global flag
  work automatically, since they're all on the shared `*Globals`.
- Credential precedence, retry safety, and error normalization all come from
  `runtimeContext` + `client.Client`, not anything you write yourself.
- `--output json`/`yaml`/`table` all work automatically once you render
  through `output.Writer`.

## What you should NOT do

- Do not call `net/http` directly — see `internal/client/AGENTS.md`.
- Do not add a second credential-resolution path — see
  `internal/config/AGENTS.md`.
- Do not print progress or data with a bare `fmt.Println`/`fmt.Printf` to an
  unspecified stream — always be explicit about stdout vs. stderr.
```

- [ ] **Step 2: Verify the referenced files and patterns are real**

```bash
cd cli
grep -n 'func newLoginCommand(g \*Globals' internal/cmd/auth.go
grep -n 'func newOpenCommand(g \*Globals' internal/cmd/misc.go
grep -n 'func runtimeContext(g \*Globals)' internal/cmd/root.go
grep -n 'root.AddCommand(' internal/cmd/root.go
```

Expected: every symbol exists, confirming the guide describes the real
pattern rather than an idealized one.

- [ ] **Step 3: Commit**

```bash
git add cli/guides/adding-a-hand-written-command.md
git commit -m "docs(cli): add the adding-a-hand-written-command guide"
```

---

## Task 10: Link the guides from `ARCHITECTURE.md`

**Files:**
- Modify: `cli/ARCHITECTURE.md`

Depends on: Tasks 8–9 (the files this links to must exist).

- [ ] **Step 1: Add the link**

In `cli/ARCHITECTURE.md`'s existing "Where to look next" section, add one
line so it reads:

```markdown
## Where to look next

- [`decisions/`](decisions/) — why, for the five decisions with real
  trade-offs behind them.
- [`guides/`](guides/) — walkthroughs for the two most common maintenance
  tasks.
- [`README.md`](README.md) — how to install and use the CLI as a consumer.
```

- [ ] **Step 2: Verify the link resolves and the section reads correctly**

```bash
cd cli
test -f guides/adding-a-command.md && test -f guides/adding-a-hand-written-command.md && echo OK
grep -n 'Where to look next' -A6 ARCHITECTURE.md
```

Expected: `OK`, and the section shows all three bullet points in order.

- [ ] **Step 3: Commit**

```bash
git add cli/ARCHITECTURE.md
git commit -m "docs(cli): link guides/ from ARCHITECTURE.md"
```

---

## Task 11: Fold in new findings from the separate code review (follow-up)

This task is intentionally open-ended and runs after the independently
commissioned code review (divided by package, then whole-system) produces
its findings — it is a lightweight update, not a blocker on Tasks 1–10.

**Files:**
- Modify: whichever of the six per-package `AGENTS.md` files correspond to a
  review finding worth recording.

- [ ] **Step 1: Read the review's findings**

For each finding that describes a real bug, a non-obvious constraint, or a
"if you touch X, watch out for Y" fact — not a style nit or something already
fixed with no lasting trap — add one bullet to the relevant package's
existing "Common pitfalls" section, in the same voice and level of detail as
the existing entries (concrete, cites the actual mechanism, says what to do
instead — not a vague warning).

- [ ] **Step 2: Do not add a bullet for findings that are:**

- Already fixed with nothing left for a future reader to trip over.
- Pure style preferences with no functional consequence.
- Already covered by an existing ADR or an existing pitfall entry.

- [ ] **Step 3: Commit, one commit per package touched**

```bash
git add cli/internal/<package>/AGENTS.md
git commit -m "docs(cli): fold in review finding on <one-line description>"
```
