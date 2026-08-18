# Flexprice CLI — Design

Date: 2026-08-18
Status: Approved design, ready for implementation planning
Source: `flexprice/flexprice` at `cli/` · Distribution: `github.com/flexprice/cli`

## 1. Purpose

A command-line tool for developers integrating Flexprice into their application — the same
audience and the same job as the Stripe CLI, applied to usage-based billing.

Flexprice's inner loop is not Stripe's. Stripe's is *checkout → webhook fires → my handler runs*.
Flexprice's is *define a meter → send events → did they aggregate correctly → what does this
customer's usage and invoice look like now*. Today "did my event actually count?" has no answer
outside the dashboard. The CLI owns that loop.

## 2. Competitive landscape

| Vendor | CLI | Tech / auth | Notable |
|---|---|---|---|
| Stripe | Mature | Go + Cobra; browser pairing → key in OS keyring; profiles in `~/.config/stripe/config.toml` | `listen`, `trigger`, `logs tail`, `fixtures`, `samples`, raw `get/post/delete`, plugins |
| Polar.sh | Official, early | OAuth device flow; table/JSON/YAML output | Webhook tunneling, `polar init`, migration from LemonSqueezy/Paddle/Stripe |
| Autumn | `npx atmn` | npm/TS; key from env | Pricing-as-code: `autumn.config.ts` → `atmn push` |
| OpenMeter | Collector only | Go, Benthos/Redpanda Connect; bearer token | Streaming ingestion from K8s/S3/logs |
| Lago | None (`lago` is a docker-compose alias) | — | — |
| Orb | None | — | — |
| Metronome | None | — | — |

Orb, Metronome and Lago ship nothing. Stripe sets the expectation; Polar is copying it. Autumn's
config-as-code is the only genuinely different idea in the space. A good CLI is a differentiator here.

### Stripe's command grammar (adopted)

```
stripe <resource> <operation> [id] [--param=value] [-d "nested[key]=value"]
```

Five decisions taken from it: resources at the top level (no `api` namespace); domain verbs
(`create`/`retrieve`/`update`/`list`/`delete`) rather than HTTP verbs; ID as a positional argument;
loose parameter pass-through rather than a declared flag per body field; namespaces where the API
nests. Their raw escape hatch (`stripe get /v1/...`) stays separate from resource commands.

## 3. Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Go + Cobra, hand-written shell with runtime spec dispatch | Matches the backend team's language; one binary |
| D2 | **Not** Speakeasy's CLI target, **not** `go-sdk/v2` | See §4.1 — a hybrid needs exactly one HTTP path |
| D3 | Resources at top level; no `api` namespace | Stripe's grammar; users' muscle memory |
| D4 | Paste-key login now, browser pairing later | Zero backend dependency; pairing slots in without changing the command |
| D5 | Region chosen at login, read from the spec's `servers[]` | Adding a region becomes a spec change, not a code change |
| D6 | Profile is the atomic auth unit | API keys are environment-scoped; see §6 |
| D7 | Build fresh; leave the Rust `flexprice/flexprice-cli` untouched and archive it | 4 commits, ~4 months cold, no external adoption; Rust blocks spec reuse and adds a second language |
| D8 | Source in this monorepo at `cli/`; release pushes to `flexprice/cli` | Spec drift is the CLI's primary failure mode, and both defences against it (§4.2, §5) only work in-repo |
| D9 | `listen` deferred to v1.1 | It is the only capability requiring new backend endpoints |
| D10 | Usage simulation included, as a fixture step type | The only capability no competitor has; marginal cost given the engine exists |

## 4. Architecture

```
cli/
├── main.go
├── spec/
│   ├── openapi.json          # go:embed, synced from docs/swagger by make
│   └── commands.yaml         # curated resource/action → operationId map
└── internal/
    ├── cmd/        root + all commands
    ├── client/     THE single HTTP path: base URL, auth, retries, error mapping
    ├── config/     ~/.flexprice/config.toml, profiles, precedence
    ├── keyring/    OS keychain; encrypted file fallback
    ├── spec/       resolver: resource+action → operation → params → request
    ├── output/     table | json | yaml
    ├── fixtures/   scenario engine, simulation step, embedded built-ins
    └── listen/     v1.1 — relay, registration lifecycle, heartbeat
```

### 4.1 Why no SDK, and where Speakeasy sits

The toolchain, traced through the Makefile:

```
Go annotations ─ swag v2 ─▶ docs/swagger/swagger.json (OpenAPI 2.0)
    └─ converter.swagger.io ─▶ docs/swagger/swagger-3-0.json (OpenAPI 3.0)
         ├─ speakeasy ─▶ api/go · api/python · api/typescript
         ├─ filter-by-tags ─▶ swagger-3-0-mcp.json ─▶ api/mcp
         └─ NEW: cli/spec/openapi.json
```

**Speakeasy is downstream of the spec, not upstream of the CLI.** It is a sibling consumer, exactly
as the CLI is. An earlier draft had curated commands calling `go-sdk/v2` while `api` dispatch built
raw requests — that is two HTTP stacks, two auth implementations, two retry policies and two error
models, which is precisely the seam a hybrid design must not have. `go-sdk/v2` is therefore not used
by the CLI. Every command, curated or dispatched, builds a request from the spec and hands it to
`internal/client`. The single-chokepoint property is structural rather than aspirational.

Speakeasy's CLI generation target was evaluated and rejected: it wants to own configuration, auth and
the keychain, which conflicts with a hand-written shell owning them.

Relationship to the MCP server: MCP serves agents inside an IDE over 6 allow-listed tags; the CLI
serves humans and CI over all 33. `--output json` is the shared seam.

Note: `make swagger` converts 2.0→3.0 by POSTing to the public `converter.swagger.io` service. The
output is committed, so builds do not depend on it, but spec regeneration does. Out of scope here;
recorded because it affects spec-sync reliability.

### 4.2 Spec sync

`make sync-cli-spec` copies `docs/swagger/swagger-3-0.json` into `cli/spec/`. It runs in the same CI
job as `make swagger`, so an API change not reflected in the CLI appears as a diff in the pull
request. The binary never fetches a spec at runtime.

## 5. Command mapping

The spec has 255 operations across 34 tags. **56 of those, all under the `Webhook Events` tag, are
documentation stubs** — no `operationId`, synthetic paths such as `POST /webhook-events/invoice.created`
— that exist to document webhook payload schemas. They 404 if called. The resolver excludes that tag
from command generation but keeps it as the authoritative list of event types for `trigger` and
`listen --events`. Real surface: **199 operations across 33 tags.**

Mechanical derivation from paths and verbs produces a bad CLI. Observed in the spec:

- There is no `GET /customers`. Listing is `POST /customers/search` (`queryCustomer`).
- `PUT /customers` (`updateCustomer`) updates at collection level, with no `{id}`.
- Sub-resources exist: `subscriptions/lineitems/*`, `wallets/transactions/*`, `subscriptions/schedules/*`.
- Action operations exist: `finalize`, `void`, `cancel`, `activate`, `top-up`, `terminate`, `recalculate`.
- Versioned duplicates exist: `getSubscriptionV2`, `recalculateInvoiceV2`.
- Tag and path disagree in places: `GET /customers/{id}/invoices/summary` is tagged `Invoices`.
- Path prefixes are inconsistent: most omit `/v1`, some include it (`GET /v1/subscription-schedules`).

`cli/spec/commands.yaml` therefore holds a curated map, bootstrapped by a heuristic script and then
hand-corrected:

```yaml
customers:
  list:     queryCustomer          # POST /customers/search
  retrieve: getCustomer
  create:   createCustomer
  columns:  [id, external_id, email, created_at]
invoices:
  finalize: finalizeInvoice        # flexprice invoices finalize inv_01K
  void:     voidInvoice
subscriptions:
  lineitems: { list: querySubscriptionLineItems, update: updateSubscriptionLineItem }
exclude:
  - recalculateInvoice             # superseded by v2
```

**CI validation is default-allow.** An unmapped operation auto-derives a name from its `operationId`
and CI warns. CI fails only on a name collision, or a mapping pointing at an operation that does not
exist. Backend pull requests are never blocked by CLI mapping work — a strict gate would be an
unwanted tax on engineers who do not own the CLI, and would be deleted within two months. A scheduled
job opens naming pull requests for newly derived commands, assigned via CODEOWNERS on this file.

Derived names are a pure function of `operationId`, and operationId stability is already a contract
held for the SDKs, so name stability is bounded by an existing guarantee.

These findings also surface genuine API inconsistencies worth fixing upstream. That is not CLI scope;
the CLI must work against the API as it stands.

## 6. Auth, regions, profiles

**An API key is scoped to exactly one environment.** Everything below follows from that.

- The CLI never sends `x-environment-id`. That header exists for JWT/dashboard auth, where one user
  reaches many environments. A key already *is* the environment; sending the header can only create
  disagreement. There is no `--environment` flag at any level.
- Switching environments means switching keys, which means switching profiles.
- JWT/Bearer authentication is out of scope for v1.

Credential precedence: `--api-key` flag → `FLEXPRICE_API_KEY` → OS keychain → config file.

`~/.flexprice/config.toml` (mode 0600, directory 0700) holds no secrets:

```toml
default_profile = "sandbox"

[profiles.sandbox]
  region   = "in"
  base_url = "https://api.cloud.flexprice.io/v1"
  label    = "Sandbox"                       # free text, set by the user
  key_ref  = "keychain:flexprice/sandbox"
```

**Profiles are named by the user** (`--profile-name`, `--label`, defaulting to `default`), and carry
no environment name and no live/test flag.

This is forced by the API, and was verified against it rather than assumed. **Nothing reachable by an
environment-scoped key reveals which environment that key belongs to:**

| Probe | Result |
|---|---|
| `GET /environments` | returns **every** environment in the tenant (~50), not the key's |
| `GET /environments/{id}` | **200 for all of them** — no discrimination |
| `GET /secrets/api/keys` | *is* environment-scoped (returns only the active key) but omits `environment_id` |
| Response headers | carry no environment or tenant identifier |

An earlier draft derived the profile name and a `live` flag from `EnvironmentType` by reading the
first entry of `GET /environments`. That entry is arbitrary. A silently wrong `live` flag is worse
than no flag at all — it would make the production guard confidently incorrect — so both the
derivation and the guard are removed. §10 uses a plain confirmation prompt instead.

Exposing `environment_id` on `/secrets/api/keys`, or adding a `/v1/me`, would make all of this
derivable; recorded in §20.

### Regions

Read from the embedded spec's `servers[]`, never hardcoded:

```json
[{ "url": "https://us.api.flexprice.io/v1",    "description": "US Region" },
 { "url": "https://api.cloud.flexprice.io/v1", "description": "India Region" }]
```

Adding a region to the spec makes the next build offer it. `--base-url` covers self-hosted.

A key from the wrong region returns 401, indistinguishable from an invalid key. Login must
disambiguate: *"The US region rejected this key. Keys are region-specific — if your account is in
India, run `flexprice login --region in`."* `--api-key` without a profile is an error unless paired
with `--region` or `--base-url`.

### Login flow

Prompt region, then read the key from the terminal without echo (never argv). Verify with a real
authenticated call, then `GET /v1/environments` to resolve the environment and its `EnvironmentType`.
`EnvironmentType` is `development` or `production`; `production` marks the profile `live`. Live/test
is derived, never asked and never guessed.

`GET /v1/environments` is tenant-scoped (subject to the key's RBAC), so one key can enumerate the
tenant's environments. `flexprice env list` shows which have a local profile and prints the exact
command for those that do not — turning "why can't I switch?" into a next step.

**That endpoint is not in the OpenAPI spec.** The route exists and is authenticated like any other,
but `EnvironmentHandler.GetEnvironments` carries no swaggo annotations, so it is absent from
`swagger-3-0.json` and cannot be resolved through the command registry. The CLI calls it by literal
path. Annotating the handler upstream is recorded in §20; until then the endpoint is reachable by
`flexprice get /v1/environments` but not as a registry command. Its response shape is
`{"environments":[…],"total","offset","limit"}`, which predates the `types.ListResponse[T]`
(`{"items":[…],"pagination":{…}}`) envelope most endpoints use — the CLI handles both.

### Key storage

OS keychain via macOS Keychain, Windows Credential Manager, or libsecret. libsecret is frequently
absent in containers and WSL; the fallback is an encrypted file with a **first-use warning** naming
the path and mode. `whoami` prints the active backend. `FLEXPRICE_KEY_BACKEND=file` opts in and
silences the warning.

Rotation: `flexprice login --profile <p>` overwrites, displaying old and new key prefixes for
confirmation. `flexprice logout --profile <p>` removes.

## 7. Request bodies

`CreateSubscriptionRequest` is **depth 9 with 37 top-level properties, 21 of them nested objects or
arrays** (`phases`, `line_items`, `credit_grants`, `override_entitlements`, `line_item_commitments`,
`coupons`, …). `CreatePriceRequest` is depth 4 with `tiers` and `transform_quantity`. Stripe's
`-d "metadata[key]=value"` bracket syntax handles their shallow bodies; it cannot express ours. A
flag-only `create` is not buildable for the most important object in the product.

Three input modes:

```bash
flexprice customers create --email=a@b.com                       # flags, shallow operations
flexprice subscriptions create --data @sub.json                  # file
cat sub.json | flexprice subscriptions create --data -           # stdin
flexprice subscriptions create --data '{"customer_id":"..."}'    # literal
flexprice subscriptions create --edit                            # $EDITOR, spec-generated skeleton
flexprice subscriptions create --data @base.json --customer_id=x # merged; flags win
```

`--edit` opens a skeleton generated by walking the resolved schema: required fields first, types as
comments, cycles broken. Editor resolution is `$VISUAL` → `$EDITOR` → `vi` (`notepad` on Windows);
with no TTY it errors and points at `--data`.

The spec determines per operation whether flags alone can express the body. Where they cannot,
`--help` says so and points at `--edit`. Flag names are validated against the spec, with
did-you-mean suggestions on typos.

## 8. Command surface

### v1.0 — no backend dependency

```
flexprice init                       guided: region → key → verify → next steps
flexprice login    [--region] [--api-key] [--profile]
flexprice logout   [--profile]
flexprice whoami                     environment, live/test, region, key prefix, key backend
flexprice env list                   environments and which have local profiles
flexprice config   list | use <p> | set <k> <v>

flexprice <resource> <action> [id] [flags]
flexprice resources                  every resource
flexprice <resource> --help          its actions
flexprice get|post|delete <path>     raw escape hatch

flexprice events send   --name api_call --customer cust_1 --props tokens=1500
flexprice events bulk   --file events.ndjson
flexprice events tail   [--customer] [--meter] [--interval]
flexprice events query
flexprice events usage
flexprice events simulate --meter tokens --rate 50/s --duration 10m --backdate 30d

flexprice trigger <scenario>
flexprice fixtures run <file> | list

flexprice open dashboard | webhooks
flexprice version                    binary version + embedded spec build
flexprice completion bash|zsh|fish|powershell
```

Globals: `-p/--profile`, `--output table|json|yaml`, `--columns`, `--limit`, `--all`, `--quiet`,
`--debug`, `--api-key`, `--base-url`, `--region`, `--no-color`.

### v1.1 — requires backend work

```
flexprice listen --forward-to localhost:3000/hooks [--public-url] [--events] [--print-json]
```

## 9. Events

`GetEventsRequest` supports cursor pagination (`iter_first_key`, `iter_last_key`, `has_more`), so
`tail` advances a forward cursor rather than rescanning. It is polling, not a push stream, and help
text says so; latency is the poll interval plus ingestion pipeline lag.

Default interval 2s, backing off to 10s when idle, `--interval` to override. Requests carry
`User-Agent: flexprice-cli/<version>` so backend can rate-limit CLI traffic independently of
application traffic.

Load: 50 concurrent developers at a 2s poll is roughly 25 req/s against `/events/query`; idle backoff
puts realistic steady state well below that. Mitigation ladder if it runs hot: cap `page_size` →
server-side rate limit keyed on User-Agent plus key → a dedicated lightweight tail endpoint.

`events bulk` batches against `/events/bulk`, reports progress, and retries partial failures.

## 10. Fixtures and simulation

A scenario is an ordered list of steps; each step is an operationId plus parameters, with
`${step.field}` interpolation:

```yaml
name: invoice.finalized
steps:
  - { id: cust, op: createCustomer,     params: { external_id: "cli-demo-${random}" } }
  - { id: plan, op: createPlan,         params: { name: "CLI Demo" } }
  - { id: sub,  op: createSubscription, params: { customer_id: "${cust.id}", plan_id: "${plan.id}" } }
  - { id: use,  simulate: { meter: tokens, customer: "${cust.id}", rate: "50/s", duration: 10m } }
  - { id: inv,  op: finalizeInvoice,    params: { id: "${sub.invoice_id}" } }
```

The engine uses the same resolver and `internal/client` as every other command, so a fixture step and
a typed command are one code path and cannot drift. `trigger` runs embedded built-ins (`go:embed`);
`fixtures run` runs user files. `events simulate` is a thin wrapper generating a one-step scenario.

Destructive operations (`delete`, `void`, `terminate`, `cancel`, `archive`) prompt for confirmation
**regardless of environment**, unless `--force` or a non-TTY stdin. There is no environment-selective
guard, because §6 establishes there is no environment signal to be selective with. `trigger` and
`fixtures run` carry the same prompt. Created IDs print on completion; automatic teardown is deliberately excluded — a CLI that
deletes by inference eventually deletes the wrong thing.

## 11. `listen` (v1.1)

```
CLI                            Flexprice API                     Svix
 │ POST /v1/webhooks/listeners ──▶ create endpoint on tenant's app
 │   { url, event_types, ttl }  ◀── listener_id
 │ heartbeat every 30s ─────────▶ extends TTL (2 min)
 │ Ctrl-C: DELETE /listeners/{id} ▶ deregister
 └ server sweeps expired listeners
```

TTL plus heartbeat means a SIGKILL, a closed laptop or a crashed terminal cannot leak a dead endpoint
into a customer's Svix application. Ctrl-C is the fast path; the sweeper is the guarantee. A
per-environment listener cap prevents a runaway CLI from flooding a tenant's Svix app. The sweeper
emits metrics, alerting on failure or on orphan count above threshold.

Server-side URL validation rejects loopback, link-local and private targets. Egress is Svix's, not
ours — stated in the docs rather than assumed.

The public-URL source is an interface with two implementations, shipped in order:

1. `--public-url` — the user brings their own tunnel (ngrok, cloudflared). No undocumented
   dependencies; works self-hosted. **v1.1 must ship this.**
2. Svix relay as the zero-config default, once the protocol is verified. **Svix's relay protocol is
   not a documented public API**; if it breaks, `listen` degrades to (1) with a clear message.

v1.0 ships `flexprice open webhooks` plus documented manual registration, so there is partial value
before v1.1.

## 12. Errors and exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure |
| 2 | Usage error |
| 3 | Authentication / authorization |
| 4 | Not found |
| 5 | Rate limited |

Every error renders **what failed, why, and what to do next**, mapped from the API error envelope.
Three envelope shapes exist, all verified against the live API, and the parser handles each:

```
{"code":"not_found","message":"Customer with ID x was not found",
 "http_status_code":404,"details":{"customer_id":"x"}}   # the standard shape
{"error":"Unauthorized"}                                  # auth middleware: a bare string
<non-JSON>                                                # gateways and proxies
```

`details` is field-keyed and is the most actionable part of a validation failure, so it is rendered
rather than discarded.
A 404 on a *mapped* operation renders as a version-skew hint naming the embedded spec build and
suggesting an upgrade, not a bare 404.

Human-readable output goes to stderr; data goes to stdout, so `--output json > file.json` is always
clean. `--quiet` suppresses progress and spinners. Colour respects `NO_COLOR` and TTY detection.
`--debug` dumps requests and responses with **allowlist-based** redaction — everything is redacted
except known-safe keys, so it fails closed on fields nobody anticipated.

## 13. Tooling

| Concern | Choice |
|---|---|
| Commands | `spf13/cobra` |
| Spec parsing | `getkin/kin-openapi` |
| Prompts | `charmbracelet/huh` (TTY-guarded) |
| Rendering | `charmbracelet/lipgloss` |
| Keychain | `zalando/go-keyring` or `99designs/keyring` — decide at spike |
| Config | `BurntSushi/toml` |
| HTTP | `net/http` + `hashicorp/go-retryablehttp` |
| YAML | `goccy/go-yaml` |
| Release | `goreleaser` |
| Not used | Speakeasy, `go-sdk/v2`, ngrok SDK |

Everything except `kin-openapi` is boring and low-risk. `kin-openapi` is the only serious OpenAPI 3
library in Go, carries the most weight in this design, has thin documentation and machine-oriented
validation errors that must be translated for humans, and needs deliberate cycle handling.

`99designs/keyring` ships an encrypted file backend natively, which would remove hand-rolled fallback
code at the cost of weight. Verify current maintenance status of both before committing.

## 14. Spike gate (blocks implementation)

**Question:** can we generate a usable `--edit` skeleton for `CreateSubscriptionRequest` — depth 9,
21 nested properties, cyclic `$ref`s — and validate flags against it?

**Pass:** skeleton generates, a developer completes it, and it round-trips successfully through the
real API.

**Fail:** `--edit` is cut, `--data` remains the path for complex bodies, architecture unchanged.

## 15. Distribution

**Source lives in this monorepo at `cli/`, and releases push to `github.com/flexprice/cli`.**

The reason is the coupling in §4.2 and §5, not repo hygiene. The CLI's primary failure mode is spec
drift, and both defences against it — `make sync-cli-spec` and the `commands.yaml` validator —
produce their signal as a diff and a CI warning *in the pull request that changed the API*. Split
across repositories, that signal fires elsewhere, days later, on a bot pull request nobody reads.
Adding an endpoint, mapping it, and releasing the CLI stays one atomic change.

The counter-argument is external contribution. The evidence says it is theoretical for now: the Rust
CLI has 1 star, 0 forks, and no external pull requests in six months. Optimising layout for
contributors who do not exist yet, at the cost of drift protection needed on day one, is the wrong
trade. Revisit when the CLI has sustained external contributors or a dedicated owner — moving source
out later is a few hours of git work, and the switching cost is low in both directions.

`flexprice/cli` is therefore the **distribution and user-facing front door**:

- Releases, binaries, install instructions, and discovery
- **Issues enabled, pull requests disabled**, with a README line directing contributors to the
  monorepo. Standard practice for mirrored repositories, and it gives users somewhere to report bugs
  without implying the code is editable there
- `go install github.com/flexprice/cli@latest` works against the mirror; it needs only a `go.mod` at
  the repository root
- Currently **private and unlicensed** (created 2026-08-17). Must be made public before any
  distribution channel works

**Release cadence is independent of the backend.** Tags are prefixed — `cli/v1.0.0` — and goreleaser
fires only on that pattern, so a CLI patch never waits on a backend release despite sharing a
repository.

**Licensing:** the monorepo root is AGPL-3.0. The CLI is published permissively, which is legally
straightforward since Flexprice holds the copyright, but it must be stated explicitly in `cli/LICENSE`
rather than inherited by implication from the root.

goreleaser builds darwin/linux/windows across amd64/arm64. Homebrew tap, `install.sh`, `go install`,
and an optional `@flexprice/cli` npm wrapper.

Docs: `cobra doc` generates the command reference into flexprice-docs, plus a hand-written quickstart.

## 16. Testing

- Unit tests over the resolver, `commands.yaml` mapping, and config precedence.
- Golden-file tests pin `--output json` only — that is the contract. Table rendering gets advisory
  snapshots so cosmetic changes do not churn the suite.
- A conformance test asserting every mapping resolves and its required parameters are satisfiable
  from the spec.
- Integration tests run against a server already running via `make run-local` and **skip cleanly when
  nothing is listening**. The suite starts no containers.

## 17. Success metrics

Both measurable from existing logs via `User-Agent`, requiring no new instrumentation and no
phone-home telemetry:

1. Share of active tenants with at least one CLI-originated API call at 90 days.
2. Time from signup to first ingested event, CLI cohort versus non-CLI cohort.

## 18. Out of scope for v1

Browser-pairing login · TUI dashboard · plugin system · pricing-as-code push/pull · sandbox seeding
presets · MCP changes · fixture auto-teardown · JWT/Bearer authentication.

## 19. Known risks

| Risk | Mitigation |
|---|---|
| `kin-openapi` ergonomics and cycle handling | §14 spike gate; documented fallback |
| Svix relay protocol undocumented | `--public-url` ships first and works standalone |
| `commands.yaml` rots | Default-allow CI, scheduled naming PRs, CODEOWNERS |
| `listen` in v1.1 weakens launch | v1.0 ships `open webhooks` and manual registration |
| Spec regeneration depends on `converter.swagger.io` | Output committed; builds unaffected |

## 20. Open items

- **Owner for `cli/spec/commands.yaml`** — needs a name in CODEOWNERS.
- **Keychain library** — `zalando/go-keyring` versus `99designs/keyring`, decided at spike time.
- **Annotate `GET /v1/environments`** with swaggo so it enters the spec (§6). Small backend change,
  not a v1.0 blocker.
- **Expose the active key's environment** — `environment_id` on `/secrets/api/keys`, or a `/v1/me`
  endpoint. Until this exists the CLI cannot label profiles by environment or offer a
  production-aware guard (§6, §10). This is the single highest-value backend change for CLI UX.
- **`flexprice/cli` is private and unlicensed** — make it public and set `cli/LICENSE` before release.
- Courtesy note to the author of the Rust `flexprice-cli` on the direction change, and archive that
  repository. Lower stakes than superseding it in place, but worth doing before `flexprice/cli` ships.
