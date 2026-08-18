# Flexprice CLI Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `cli/` the documentation it needs to work as a standalone repository: five ADRs and an ARCHITECTURE.md explaining decisions already made (writable now), and a rewritten end-user README verified against the real binary (gated on Tasks 11/13/14).

**Architecture:** Two phases matching the design's sequencing. Phase 1 (Tasks D1–D6) writes `cli/decisions/*.md` and `cli/ARCHITECTURE.md` — pure documentation of decisions already implemented and committed, so every claim can be cited against real code. Phase 2 (Task D7) rewrites `cli/README.md` and is **not started** until Tasks 11 (`--edit`), 13 (auth commands) and 14 (resource tree) land, because its examples must run against a real binary.

**Tech Stack:** Markdown only. No new Go code, no new dependencies.

**Design doc:** `docs/design/2026-08-18-flexprice-cli-docs-design.md`

---

## File structure

```
cli/
├── README.md                                          # rewritten in Phase 2
├── ARCHITECTURE.md                                     # new, Phase 1
└── decisions/                                          # new, Phase 1
    ├── 0001-no-sdk-single-http-path.md
    ├── 0002-retry-only-idempotent-methods.md
    ├── 0003-environment-scoped-profiles-no-live-flag.md
    ├── 0004-curated-commands-yaml-over-mechanical-derivation.md
    └── 0005-region-discovery-from-openapi-servers.md
```

Every ADR follows the same shape: Context (the problem, cited against real files), Decision
(what was chosen, in one or two sentences), Consequences (what this costs or forecloses).
Roughly 30–40 lines each — short enough that a contributor reads all five in the time it takes
to read one long design doc, long enough to save them from re-deriving the reasoning from git
blame.

---

## Phase 1 — ADRs and ARCHITECTURE.md (write now)

### Task D1: ADR 0001 — No SDK; one HTTP path through `internal/client`

**Files:**
- Create: `cli/decisions/0001-no-sdk-single-http-path.md`

- [ ] **Step 1: Write the ADR**

`cli/decisions/0001-no-sdk-single-http-path.md`:

```markdown
# 0001 — No SDK; one HTTP path through internal/client

## Context

An early draft of this CLI split commands into two groups: hand-written commands
(login, events, fixtures) calling the published `go-sdk/v2`, and spec-dispatched
resource commands building raw HTTP requests directly. That is two separate HTTP
stacks — two places that set the `x-api-key` header, two retry policies, two error
shapes — reviewed and rejected before any code was written, because it breaks the
one property this CLI depends on: that every request behaves identically no matter
which command issued it.

## Decision

There is exactly one way to make a request: `internal/client.Client.Do`
(`cli/internal/client/client.go:144`). Hand-written commands and spec-dispatched
resource commands both build a `*Request` and hand it to the same `Do` method. The
CLI does not import `go-sdk/v2` or any other Flexprice SDK.

`Do` owns:
- The `x-api-key` header, and the guarantee that `x-environment-id` is never sent
  (see [0003](0003-environment-scoped-profiles-no-live-flag.md)).
- Retry policy (see [0002](0002-retry-only-idempotent-methods.md)).
- A 30-second default request timeout.
- `--debug` dumps, redacted through an allowlist rather than a denylist.
- Turning a non-2xx response into a `*client.APIError`
  (`cli/internal/client/errors.go`), which every caller renders the same way.

## Consequences

A fix or a behavior change to auth, retries, timeouts, or error rendering happens
in one file and applies to every command, including the fixture engine's requests,
without anyone needing to remember to apply it twice. The cost is that
`internal/client` cannot be bypassed for a "quick" request — if a future command
needs something `Do` does not support, that capability is added to `Do`, not
worked around locally.
```

- [ ] **Step 2: Verify the citations are accurate**

```bash
cd /path/to/repo && sed -n '144p' cli/internal/client/client.go
```

Expected: the line is `func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {`. If the line number has drifted (later tasks touching the file will shift it), update the citation in the ADR to the current line before committing.

- [ ] **Step 3: Commit**

```bash
git add cli/decisions/0001-no-sdk-single-http-path.md
git commit -m "docs(cli): add ADR 0001 — no SDK, one HTTP path"
```

### Task D2: ADR 0002 — Retry only idempotent methods

**Files:**
- Create: `cli/decisions/0002-retry-only-idempotent-methods.md`

- [ ] **Step 1: Write the ADR**

`cli/decisions/0002-retry-only-idempotent-methods.md`:

```markdown
# 0002 — Retry only idempotent methods; POST never retries on 5xx

## Context

The HTTP client's first implementation (commit `091fcc201`) used
`go-retryablehttp`'s default retry policy, which retries a request up to three
times on any 5xx status or transport error. That policy inspects only the
response status — never the request method — so it retried `POST` identically to
`GET`.

On a billing API that is unsafe. A 502 raised after the server has already
committed a write is indistinguishable, from the client's point of view, from one
raised before the write happened. Retrying `POST /subscriptions` after a
post-commit failure can create a second subscription that bills a real customer.
Checked directly against the API this CLI talks to (fix commit `573e85140`):
`CreateSubscriptionRequest` has no idempotency field at all, and on the endpoints
that do accept a body-level `idempotency_key`, the server generates one
containing the current timestamp when the caller omits it — which differs on
every retry attempt even though `go-retryablehttp` resends a byte-identical body,
so server-side deduplication does not help either.

## Decision

`internal/client.retryPolicy` (`cli/internal/client/client.go:58`) retries a
request only when its method is in `idempotentMethods` — `GET`, `HEAD`, `PUT`,
`DELETE`, `OPTIONS` — or when the response is `429 Too Many Requests`, which is
retried for every method because it explicitly means the server did not process
the request. `POST` and `PATCH` never retry on a 5xx or a transport error.

## Consequences

A `POST` that fails with a 5xx surfaces to the user as a single failed attempt
rather than silently succeeding after duplicating a write. The cost is that a
transient 502 on `POST /events` — the highest-volume write this CLI makes — is
not automatically retried; a future high-volume ingestion path (`events bulk`,
`events simulate`) needs its own application-level retry-with-dedup logic if that
matters, rather than getting it for free from the transport.
```

- [ ] **Step 2: Verify the citation**

```bash
cd /path/to/repo && sed -n '58p' cli/internal/client/client.go
```

Expected: `func retryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {`

- [ ] **Step 3: Commit**

```bash
git add cli/decisions/0002-retry-only-idempotent-methods.md
git commit -m "docs(cli): add ADR 0002 — retry only idempotent methods"
```

### Task D3: ADR 0003 — Environment-scoped profiles, no derived live flag

**Files:**
- Create: `cli/decisions/0003-environment-scoped-profiles-no-live-flag.md`

- [ ] **Step 1: Write the ADR**

`cli/decisions/0003-environment-scoped-profiles-no-live-flag.md`:

```markdown
# 0003 — Environment-scoped profiles; no derived live/test flag

## Context

An earlier design had `flexprice login` call `GET /environments`, read the first
entry's `EnvironmentType`, and store a derived `live: bool` on the profile so
destructive commands could warn before acting against production. That design was
tested directly against the API and does not work: `GET /environments` returns
every environment in the tenant — not the key's own — `GET /environments/{id}`
returns `200` for all of them with no discrimination, and `GET
/secrets/api/keys`, which is correctly scoped to the active key, does not include
an `environment_id` field. No endpoint reachable by an environment-scoped key
reveals which environment that key belongs to.

## Decision

`config.Profile` (`cli/internal/config/config.go`) carries `Region`, `BaseURL`,
`Label`, and `KeyRef` — no environment name, no `live` flag. `Label` is free text
the user sets at `flexprice login --label`; it is display-only and never read for
any safety decision. There is no `--environment` flag anywhere in the CLI: an API
key already determines its environment server-side, so the CLI never sends
`x-environment-id` (see [0001](0001-no-sdk-single-http-path.md)).

## Consequences

A destructive command (delete, void, terminate) cannot warn "this profile is
production" because the CLI genuinely does not know. It instead prompts for
confirmation on every destructive action regardless of profile — safe by
default, at the cost of the confirmation firing in development too, where it is
mostly friction. This is a real gap: the CLI's UX would improve materially if the
API exposed the active key's own environment, e.g. as `environment_id` on `GET
/secrets/api/keys` or via a `/v1/me` endpoint. That backend change is out of
scope for this CLI and tracked as an open item in the design doc, not as
something this repository can fix.
```

- [ ] **Step 2: Verify the citation**

```bash
cd /path/to/repo && sed -n '/type Profile struct/,/^}/p' cli/internal/config/config.go
```

Expected: the struct has exactly the four fields `Region`, `BaseURL`, `Label`, `KeyRef` — no `Environment` or `Live` field.

- [ ] **Step 3: Commit**

```bash
git add cli/decisions/0003-environment-scoped-profiles-no-live-flag.md
git commit -m "docs(cli): add ADR 0003 — environment-scoped profiles"
```

### Task D4: ADR 0004 — Curated `commands.yaml` over mechanical derivation

**Files:**
- Create: `cli/decisions/0004-curated-commands-yaml-over-mechanical-derivation.md`

- [ ] **Step 1: Write the ADR**

`cli/decisions/0004-curated-commands-yaml-over-mechanical-derivation.md`:

```markdown
# 0004 — Curated commands.yaml over mechanical derivation

## Context

The obvious way to turn 198 OpenAPI operations into CLI commands is to derive
each command's name from its path and HTTP method — `GET /customers` becomes
`customers list`, and so on. This API does not support that: there is no `GET
/customers` at all, since listing is `POST /customers/search`
(`queryCustomer`), so `customers list` cannot be derived from a verb-and-path
rule. A throwaway bootstrap tool that derived names by an alphabetical
first-match heuristic was run against the real spec and produced two silent
misassignments — `entitlements retrieve` resolved to `getAddonEntitlements`
instead of `getEntitlement`, and `subscriptions list` resolved to
`listAllSubscriptionSchedules` instead of `querySubscription` — both because the
wrong operation ID happened to sort first alphabetically. Neither would have
been visible without manually checking each command's actual output.

## Decision

`cli/spec/commands.yaml` hand-maps all but one of the 198 callable operations to
a resource and action name (`recalculateInvoice` is excluded, superseded by
`recalculateInvoiceV2` — see the comment above `exclude:` in the file). The
registry (`cli/internal/spec/registry.go`) validates this map but does not
generate it. Validation is deliberately **default-allow**
(`cli/internal/spec/registry.go:90`): an operation missing from the map gets an
auto-derived name and a warning, not a build failure, so a backend engineer
adding an endpoint is never blocked on updating a CLI file they may not know
exists. CI fails only on a genuine defect — a name collision, or a mapping
pointing at an operation ID that no longer exists.

## Consequences

Every command name in this CLI reflects a human decision, verified against
`TestRegistry_EveryOperationIsAccountedFor`
(`cli/internal/spec/registry_test.go:43`), which confirms every operation is
either mapped or explicitly excluded — never silently missing. The cost is
maintenance: a new backend endpoint is usable immediately under a derived name,
but someone has to notice the CI warning and give it a real name, or the CLI
accumulates commands like `flexprice customers get-customer-entitlements-by-
external-id`.
```

- [ ] **Step 2: Verify the citations**

```bash
cd /path/to/repo && sed -n '90p' cli/internal/spec/registry.go
grep -n 'func TestRegistry_EveryOperationIsAccountedFor' cli/internal/spec/registry_test.go
grep -n 'exclude:' cli/spec/commands.yaml
```

Expected: line 90 begins `// Default-allow: anything unmapped gets a derived name and a warning.`; the test function exists at the cited line (adjust the citation if it has moved); `exclude: [recalculateInvoice]` appears in `commands.yaml`.

- [ ] **Step 3: Commit**

```bash
git add cli/decisions/0004-curated-commands-yaml-over-mechanical-derivation.md
git commit -m "docs(cli): add ADR 0004 — curated commands.yaml"
```

### Task D5: ADR 0005 — Region discovery from OpenAPI `servers[]`

**Files:**
- Create: `cli/decisions/0005-region-discovery-from-openapi-servers.md`

- [ ] **Step 1: Write the ADR**

`cli/decisions/0005-region-discovery-from-openapi-servers.md`:

```markdown
# 0005 — Regions read from the OpenAPI spec's servers[], never hardcoded

## Context

Flexprice runs two API regions today — `https://us.api.flexprice.io/v1` and
`https://api.cloud.flexprice.io/v1` — and a key issued in one region is rejected
by the other with the same bare `401` as an invalid key, so `flexprice login`
has to know the full region list to offer a useful choice and a useful error
message. The tempting shortcut is a hardcoded `map[string]string{"us": "...",
"in": "..."}` in Go.

## Decision

`spec.Regions` (`cli/internal/spec/loader.go:38`) derives the region list from
the embedded OpenAPI document's `servers[]` array at every CLI invocation, and
`spec.regionKey` (`cli/internal/spec/loader.go:53`) derives each region's short
flag key (`us`, `in`) from its `servers[].description`. No region string is
written directly into CLI Go code anywhere.

## Consequences

Adding a third region is a `docs/swagger/swagger-3-0.json` change plus a
`make sync-cli-spec` run — the next CLI build offers it automatically, with no
Go code to update and no risk of the region list drifting out of sync with the
list the API itself advertises. The cost is indirection: `flexprice login
--help` cannot show the region list statically in generated documentation,
since it depends on the spec embedded in the specific binary the user is
running rather than being fixed at compile time.
```

- [ ] **Step 2: Verify the citations**

```bash
cd /path/to/repo && sed -n '38p;53p' cli/internal/spec/loader.go
python3 -c "import json; s=json.load(open('docs/swagger/swagger-3-0.json')); print([x['url'] for x in s['servers']])"
```

Expected: line 38 is `func Regions(doc *openapi3.T) []Region {`, line 53 is `func regionKey(url, description string) string {`, and the servers list prints both region URLs.

- [ ] **Step 3: Commit**

```bash
git add cli/decisions/0005-region-discovery-from-openapi-servers.md
git commit -m "docs(cli): add ADR 0005 — regions from OpenAPI servers[]"
```

### Task D6: `cli/ARCHITECTURE.md`

**Files:**
- Create: `cli/ARCHITECTURE.md`

Depends on: Tasks D1–D5 (this file links to all five ADRs).

- [ ] **Step 1: Write ARCHITECTURE.md**

`cli/ARCHITECTURE.md`:

````markdown
# Architecture

This document explains how the Flexprice CLI is put together, for anyone
contributing to `flexprice/cli` who has not read the design documents that
produced it. The decisions with real trade-offs behind them are recorded
separately as short ADRs in [`decisions/`](decisions/); this document is the
narrative that ties them together.

## Request lifecycle

Every command — whether hand-written (`login`, `events`) or generated from the
API spec (`customers list`, `invoices finalize`) — follows the same path:

```
 user input                registry              request builder
(flags, positional  ──▶  commands.yaml   ──▶   internal/spec.BuildRequest
 ID, --data, --edit)     internal/spec         (path/query/body assembly,
                         .Registry.Lookup        flag-vs-schema validation)
                                                         │
                                                         ▼
                                                 internal/client.Client.Do
                                                 (auth header, retry policy,
                                                  timeout, --debug redaction)
                                                         │
                                                         ▼
                                                internal/output.Writer.Render
                                                (table / json / yaml,
                                                 stdout for data, stderr
                                                 for everything else)
```

Every box in that diagram is one package with one job:

- **`internal/spec`** (`loader.go`, `registry.go`, `request.go`) turns the
  embedded OpenAPI document plus `commands.yaml` into a resolvable command
  tree, and turns one resolved command plus user input into an HTTP request.
  Nothing in this package makes a network call.
- **`internal/client`** (`client.go`, `errors.go`) is the only package that
  makes a network call. See [ADR 0001](decisions/0001-no-sdk-single-http-path.md).
- **`internal/output`** (`output.go`, `table.go`) turns a raw JSON response
  into what the terminal or a script sees, and nowhere else in the codebase
  writes to `os.Stdout`.
- **`internal/config`** and **`internal/keyring`** resolve *who* is making the
  request — profile, region, API key — before `internal/client` is ever
  called. See [ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md).
- **`internal/cmd`** is the only package that imports `cobra`. It wires the
  above together into the command tree cobra dispatches.

## Why runtime dispatch, not code generation

The API has 198 callable operations. Generating 198 Go files — one per
command — was considered and rejected: it means a build step nobody remembers
to run, generated code nobody reviews line-by-line, and a repository that grows
by hundreds of files for every new endpoint. Instead, the OpenAPI document is
embedded in the binary via `go:embed` (`cli/spec/embed.go`) and parsed once per
invocation (`internal/spec.Load`, roughly 50ms for the current ~880KB spec).
The command tree cobra sees is built by walking the parsed document against
`commands.yaml` at startup, not by reading generated source.

The corresponding cost is that command names cannot be derived automatically
from the spec — see [ADR 0004](decisions/0004-curated-commands-yaml-over-mechanical-derivation.md)
for why, and why that cost is worth paying by hand rather than working around.

## Auth and profiles

A Flexprice API key is scoped to exactly one environment. The CLI's entire auth
model follows from that one fact — see
[ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md) for the
full reasoning, including why there is no `--environment` flag and no
automatic production/development detection.

## Error and exit-code contract

`internal/client.NewAPIError` (`errors.go`) normalizes three response shapes
this API actually returns — a structured `{code, message, http_status_code,
details}` envelope, a bare `{"error": "Unauthorized"}` string from the auth
middleware, and non-JSON bodies from anything sitting in front of the API — into
one `*APIError` type with a stable `ExitCode()` (`internal/exitcode`). Every
command that fails exits with one of those codes; scripts can depend on them
never changing meaning.

## Where to look next

- [`decisions/`](decisions/) — why, for the five decisions with real
  trade-offs behind them.
- [`README.md`](README.md) — how to install and use the CLI as a consumer.
````

- [ ] **Step 2: Verify every internal link resolves**

```bash
cd /path/to/repo/cli
for f in decisions/0001-no-sdk-single-http-path.md \
         decisions/0002-retry-only-idempotent-methods.md \
         decisions/0003-environment-scoped-profiles-no-live-flag.md \
         decisions/0004-curated-commands-yaml-over-mechanical-derivation.md \
         README.md; do
  test -f "$f" && echo "OK: $f" || echo "MISSING: $f"
done
```

Expected: every file except `README.md` prints `OK` (README.md is not created until Phase 2 — that one line is expected to print `MISSING` until Task D7 lands; do not treat that as a failure of this task).

- [ ] **Step 3: Verify the package list matches reality**

```bash
cd /path/to/repo && find cli/internal -maxdepth 1 -type d | sort
```

Expected: `cli/internal`, `cli/internal/client`, `cli/internal/cmd`, `cli/internal/config`, `cli/internal/exitcode`, `cli/internal/keyring`, `cli/internal/output`, `cli/internal/spec` — matching every package named in the "Every box in that diagram" list above. If a package has been added or renamed since this plan was written, update the list before committing.

- [ ] **Step 4: Commit**

```bash
git add cli/ARCHITECTURE.md
git commit -m "docs(cli): add ARCHITECTURE.md"
```

---

## Phase 2 — README (gated)

**Do not start this task until Tasks 11 (`--edit` skeleton generation), 13 (auth
commands: `init`/`login`/`logout`/`whoami`/`env`/`config`) and 14 (resource tree and
raw HTTP) have landed and their own code review is complete.** Every command shown in
the README below is a placeholder for "the real output of running this against the
built binary" — do not write this task from the plan's prose alone.

### Task D7: Rewrite `cli/README.md`, verified against the built binary

**Files:**
- Modify: `cli/README.md`

Depends on: Tasks 11, 13, 14 (implementation), Task D6 (this links to `ARCHITECTURE.md`).

- [ ] **Step 1: Build the CLI fresh**

```bash
cd /path/to/repo && make cli-build
./cli/bin/flexprice --help
```

Expected: the help output lists `init`, `login`, `logout`, `whoami`, `env`, `config`,
every resource from `cli/spec/commands.yaml` (`customers`, `invoices`,
`subscriptions`, ...), `get`, `post`, `delete`, `resources`, `version`, `completion`.
If any of these is missing, Phase 2 cannot proceed — stop and report which task's
output is absent rather than writing a README for commands that do not exist yet.

- [ ] **Step 2: Capture real output for every example the README will use**

Run each of the following against a `development`-environment key and record the
actual output. Do not invent output — copy what the binary actually prints.

```bash
./cli/bin/flexprice init
./cli/bin/flexprice whoami
./cli/bin/flexprice resources
./cli/bin/flexprice customers list --limit 3
./cli/bin/flexprice customers list --limit 3 --output json
./cli/bin/flexprice customers create --help
```

For each command, note: the exact flags used, the exact output shape (truncate long
JSON/table output for the README, but do not alter field names or command syntax),
and confirm the command's name matches an entry in `cli/spec/commands.yaml` — grep for
it:

```bash
grep -n 'list: queryCustomer\|create: createCustomer' cli/spec/commands.yaml
```

- [ ] **Step 3: Write README.md**

`cli/README.md` — structure below; fill every `<...>` placeholder with the real
output captured in Step 2 before committing. The prose sections (Install,
Authentication, Output & scripting, Contributing, License) are final as written;
only the command examples need real captured output substituted in.

```markdown
# Flexprice CLI

Usage-based billing from your terminal — send events, inspect how they metered,
and drive the Flexprice API without leaving the command line.

    brew install flexprice/tap/flexprice
    flexprice init

## Install

**Homebrew (macOS, Linux)**

    brew install flexprice/tap/flexprice

**Install script (macOS, Linux)**

    curl -fsSL https://flexprice.io/install.sh | sh

**Go**

    go install github.com/flexprice/cli@latest

## Quickstart

`flexprice init` walks you through picking a data region and pasting an API
key, verifies it against the API, and stores it in your OS keychain.

    $ flexprice init
    <PASTE REAL init OUTPUT FROM STEP 2 HERE>

Confirm what you're pointed at:

    $ flexprice whoami
    <PASTE REAL whoami OUTPUT FROM STEP 2 HERE>

See everything you can act on:

    $ flexprice resources
    <PASTE REAL resources OUTPUT FROM STEP 2 HERE, TRUNCATED WITH "... and N more">

## What you can do

Every resource in the API is a top-level command, grouped by action:

    flexprice customers list
    flexprice customers retrieve cust_01K...
    flexprice customers create --external_id=acme --email=billing@acme.com
    flexprice invoices finalize inv_01K...
    flexprice subscriptions cancel sub_01K...

For a request body too deep to express as flags — creating a subscription, for
example — open it in your editor with the required fields pre-filled:

    flexprice subscriptions create --edit

or supply it directly:

    flexprice subscriptions create --data @subscription.json

Anything not covered by a named command is reachable through the raw escape
hatch:

    flexprice get /v1/customers/cust_01K...
    flexprice post /v1/events --data @event.json

## Authentication

An API key belongs to exactly one environment — there is no `--environment`
flag, because the key itself already determines it. Switching environments
means switching profiles:

    flexprice login --label "production"    # stores a second profile
    flexprice config list                    # see every stored profile
    flexprice -p production customers list   # use one for a single command

`flexprice env list` shows every environment in your tenant, but the CLI
cannot tell you which one your active key belongs to — the API itself does not
expose that. See [ADR 0003](ARCHITECTURE.md) if you're curious why.

## Output & scripting

    flexprice customers list --output json > customers.json

Data always goes to stdout; progress messages, warnings, and footers always go
to stderr — redirecting stdout never mixes the two. Exit codes are stable and
safe to script against:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure |
| 2 | Usage error |
| 3 | Authentication failure |
| 4 | Not found |
| 5 | Rate limited |

## Configuration

Non-secret settings live in `~/.flexprice/config.toml`; API keys live in your
OS keychain (or an encrypted file fallback where no keychain is available).
`FLEXPRICE_API_KEY` and `--api-key` override the stored key for a single
invocation or for CI.

## Full command reference

    flexprice <resource> --help

lists every action for a resource; a generated reference for every command is
published at https://docs.flexprice.io/cli.

## Contributing

**Source of truth is `flexprice/flexprice` at `cli/`.** This repository is a
release mirror — please open pull requests against the monorepo. Issues here
are welcome. See [ARCHITECTURE.md](ARCHITECTURE.md) for how the code is put
together.

## License

Apache-2.0. See [LICENSE](LICENSE).
```

- [ ] **Step 4: Verify every command in the README actually runs**

```bash
cd /path/to/repo/cli
grep -oE 'flexprice [a-z-]+ [a-z-]+' README.md | sort -u | while read -r cmd; do
  resource=$(echo "$cmd" | awk '{print $2}')
  action=$(echo "$cmd" | awk '{print $3}')
  ./bin/flexprice "$resource" --help 2>&1 | grep -q "$action" && echo "OK: $cmd" || echo "CHECK: $cmd"
done
```

Expected: every extracted command prints `OK`. A `CHECK` line means the README shows a
command that does not exist as written — fix the README, not the script, since the
binary is the source of truth.

- [ ] **Step 5: Verify every internal link resolves**

```bash
cd /path/to/repo/cli
test -f ARCHITECTURE.md && echo "OK: ARCHITECTURE.md" || echo "MISSING: ARCHITECTURE.md"
test -f LICENSE && echo "OK: LICENSE" || echo "MISSING: LICENSE"
```

Expected: both print `OK`.

- [ ] **Step 6: Commit**

```bash
git add cli/README.md
git commit -m "docs(cli): rewrite README for end-user consumption, verified against the built binary"
```
