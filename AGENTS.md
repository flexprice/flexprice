# Flexprice backend

Flexprice is monetization infrastructure for AI-native and SaaS companies: usage metering, credits, pricing, invoicing and payments. It is one Go service (Gin, Uber FX, Ent, Temporal, Kafka) over PostgreSQL and ClickHouse that `FLEXPRICE_DEPLOYMENT_MODE` splits into `api`, `consumer`, `temporal_worker` and `admin` processes. The core is AGPLv3; `internal/ee/` is commercial.

`CLAUDE.md` includes this file. A directory's own `AGENTS.md` adds what is specific to that directory; read it when you work there. Commands, local stack and environment setup: [docs/operations/development.md](docs/operations/development.md).

## What we never compromise on

1. **Money is right.** An invoice, wallet balance or payment is either exactly right or clearly failed. Every write path that touches them is idempotent and safe to retry, because Kafka redelivers and Temporal replays.
2. **Tenants never see each other.** Every row carries `tenant_id` and `environment_id` and every query filters on both. The only cross-tenant reads are the admin email lookup in `internal/ee/service/admin` and self-billing's aggregates in `internal/ee/service/selfbilling`.
3. **The API is a contract.** SDKs and the MCP server are generated from `docs/swagger`. A DTO change ships with the regenerated spec, and existing fields keep their meaning.
4. **Open at the core.** Users run forks of this repo. Keep the core buildable and useful without `internal/ee/`.

## A note from the maintainers

We like small systems that make the correct behaviour unsurprising. Find the real constraint, then fight for the smallest change that satisfies it. Do not preserve complexity because it exists, and do not add machinery because it looks thorough.

Reuse before you add. Most of what you need already exists: a service method, a builder, a `testutil` store, a flow in `docs/FLOWS`. Search for it, read it, extend it. A wrapper around an existing method, a near-copy of an existing function, or a branch for a state the code cannot reach are the three things reviewers reject most.

Write for the reader, not for the token count. A 20-line function with a guard clause and a named intermediate value beats a 6-line one that nests three calls. Never compress code to save output. Never widen a function because it is the one you happen to be in; `subscription.go` and `invoice.go` are already too long, and new behaviour goes in a file named for its concern.

These are good defaults, not laws. The developer's preferences override them. If a rule here fights the task in front of you, say so plainly and get a human decision before breaking it.

## Glossary

- **tenant**, **environment**: the isolation boundary and the production/development split under it. Every entity is scoped to both.
- **billing entity**: the legal entity a connection belongs to when billing entities are on. See [Billing entity scope](#billing-entity-scope).
- **customer**, **plan**, **price**, **subscription**, **line item**: who is billed, the catalogue, one charge, the customer's active plan, one priced row on it.
- **period edge**: a billing period boundary. Edges are whole seconds; see [Time precision](#time-precision).
- **invoice**: `draft` is recomputable, `finalized` is a document, `voided` is dead. Finalize and void are one-way.
- **wallet**, **credit grant**, **commitment**: prepaid balance, an allocation into it, and a promise to spend that is billed by **billing terms** and **instalments**.
- **meter**, **event**, **meter_usage**: what we measure, one raw usage record, the windowed aggregate ClickHouse serves.
- **feature**, **entitlement**: a capability and the customer's allowance for it.
- **connection**, **provider**: a tenant's credentials for Stripe, Razorpay, Chargebee or another integration, and the adapter that uses them.
- **activity log**: the per-entity write history an ent hook captures and ClickHouse stores.
- **you**: the agent reading this. **we**: the maintainers. **user**: a Flexprice customer's engineer calling the API.

## Ways to hurt yourself

1. **Migrating the wrong database.** `make migrate-ent` reads `.env`, which may point at a shared or production database. Local work uses `make migrate-local`, which reads the committed `.env.local`.
2. **A query without both tenant filters.** It is a data leak, not a bug. Every repository method takes them from ctx through `types.GetTenantID` and `types.GetEnvironmentID`; new queries copy that pattern.
3. **A provider call outside its billing entity.** Before any `IntegrationFactory.Get*`, `connectionRepo.GetByProvider` or provider client, put the owning entity in ctx with `BillingEntityService.ScopeTo*`. Never fall back to the default entity or the first connection.
4. **A count-only bulk write on a tracked entity.** `Update()…Save()` and `Delete()…Exec()` hide their rows from the activity log. Use `SaveReturning` / `ExecReturning`. Lint CL004.
5. **Hand-editing generated code.** `ent/` (except `ent/schema`), `api/{go,python,typescript,mcp}` and `docs/swagger` are outputs; edit the schema or the handler annotations and run the `make` target.
6. **`time.Now()` in business code.** Use `clock.Now()`. Lint LL010.
7. **Running repo-wide checks in a loop.** `make test` runs thousands of tests. While iterating, run the suite beside your change (`go test ./internal/ee/service -run TestWalletServiceSuite`), then `make check-changed` before you push. CI runs the full suite.

## Where code lives

| Path | What is there | Rule |
|---|---|---|
| `internal/domain/<entity>/` | Models, repository interfaces, builders, `FromEnt` | Depends on nothing above it |
| `internal/repository/ent/`, `internal/repository/clickhouse/` | Repository implementations | The only packages that run queries |
| `internal/ee/service/` | All business logic; `ServiceParams` in `factory.go` wires it | One file per concern, named for the concern |
| `internal/ee/service/{admin,revenue,selfbilling}/` | Sub-domains with their own `AGENTS.md` | |
| `internal/api/v1/`, `internal/api/dto/` | Public handlers and wire types | Parse, validate, delegate, respond |
| `internal/api/admin/` | Operator handlers, admin mode only | Never mounted on the public router |
| `internal/integration/<provider>/` | Provider adapters behind `internal/integration/factory.go` | Gateway SDK types stay here |
| `internal/temporal/` | Workflows, activities, `registration.go` | Activities are idempotent |
| `internal/activity/` | Activity log registry and hook | |
| `internal/clock/` | The storage precision contract | |
| `internal/testutil/` | In-memory stores and `BaseServiceTestSuite` | Tests use these, not mocks of business logic |
| `cmd/server/main.go` | Every FX provider | A new dependency is registered here |
| `ent/schema/` | Data model | `make generate-ent`, then `make generate-migration` |
| `migrations/` | Postgres and ClickHouse SQL | New file sorts last; checksum in `migrations/.hashes` |
| `tools/lint/` | Our analyzers | See [Enforcement](#enforcement) |
| `docs/FLOWS/`, `docs/design/` | How a flow works end to end; dated design docs | Update the flow doc when the flow changes |

There is no internal/service package any more; business logic lives in `internal/ee/service/`.

## Hit every surface

The commonest defect is a change that works on the path you tested and is missing everywhere else. Before you call work done, walk this list and say which entries applied.

- **API shape.** Handler, DTO, validation, Swagger annotation with `@x-scope` where the HTTP method does not match the semantics, `make swagger`. The SDKs and MCP server follow from the spec.
- **Both entry points.** A behaviour reachable from the API is usually also reachable from a Temporal workflow, a Kafka consumer or the admin API. That only works when it is a service method, not handler code.
- **Preview and real.** Invoice preview, subscription change preview and usage analytics must agree with what finalize and billing actually produce.
- **The way back out.** Create needs cancel or void, pause needs resume, a schedule needs delete. A one-way door that is not meant to be one is a bug.
- **Billing entities on and off.** `ScopeTo*` is a no-op when billing entities are off, so a missing scope only shows up on. Test the provider path with billing entities enabled and an owned connection.
- **Activity log.** A new entity that users act on is registered in `internal/activity/entities.go`; a new state change declares its action. Use the `activity-log-entity` skill.
- **Webhooks and events.** A new provider webhook event is listed in `cmd/migrate/data.json`. A new system event has a consumer that tolerates duplicates and arrival out of order.
- **Backfill.** A new aggregation handles the events that already exist.
- **Docs.** If the change makes a `docs/FLOWS` page or an `AGENTS.md` inaccurate, fix it in the same PR.

## Hard invariants

### Layering
Handlers call services and nothing else. Domain interfaces live in `internal/domain`, implementations in `internal/repository`. Gateway SDK types never leave `internal/integration`. New dependencies are provided in `cmd/server/main.go`. Lint CL002.

### Multi-tenancy
Every entity carries `tenant_id` and `environment_id`; every query filters on both. See [Ways to hurt yourself](#ways-to-hurt-yourself).

### Event processing
Handlers are idempotent. Order is by event timestamp, not arrival. Every Kafka consumer and Temporal activity is safe to retry. ClickHouse queries carry the hardcoded `max_memory_usage` of 90 GB; analytics reads go to ClickHouse, transactional reads and writes to PostgreSQL.

### Time precision
Every stored instant is whole microseconds, because lib/pq rounds nanoseconds half-up. Four boundaries hold the contract, and business code never calls `.Truncate(time.Microsecond)`: `clock.Now()`; the Gin bindings in `internal/rest/middleware/binding.go`; the Temporal interceptor in `internal/temporal/interceptor/time_precision_interceptor.go`; the ent hook in `internal/postgres/ent_hooks.go`. Billing period edges are whole seconds (`CreateSubscriptionRequest.ApplyDefaults` drops the fraction). A commitment date within a second after an edge is that edge (`atPeriodEdge`). ClickHouse keeps its own precision (`DateTime64(3)`) and event timestamps are never exact-compared against stored rows. Lint LL010, CL005.

### Activity log
Writes to registered entities return their rows. New fields on a registered domain struct are stored in the snapshot unless tagged `activity:"redact"` (secrets, provider payloads) or `activity:"drop"` (bookkeeping, personal data). Actors are ids only. The full procedure is the `activity-log-entity` skill.

### Billing entity scope
Each connection belongs to one billing entity. Lookups return only the connection of the entity in ctx and return none when ctx has no entity but owned connections exist. Scope once at the service entry point with `ScopeToCustomer`, `ScopeToInvoice`, `ScopeToSubscription`, `ScopeToPayment` or `ScopeToConnection` and pass that ctx down.

### Observability
Log through the injected `internal/logger` with ctx first. `Error` always carries an `"error"` field. `Warn` is for bootstrap only; a recovered or skipped condition is `Info`. Log meaningful state changes, failures and operational decisions with their ids, at the boundary that owns the failure. Details: `internal/logger/README.md`. Lint LL001–LL010.

## Code shape

- **Names.** Clarity over brevity; length grows with distance from the declaration. Name a value for what it holds, not its type: `pricesByID`, not `priceMap` (CL007). Receivers are one or two letters and consistent across a type. Packages are named for what they provide; no `utils`, `helpers`, `common` (CL006).
- **Functions.** Guard clauses and early returns; the happy path reads top to bottom unindented. Over about 80 lines or three levels of nesting, extract a function named for what it does (golangci `funlen`, `nestif`, `gocognit`). Avoid several same-typed parameters in a row and nil-tolerant parameters that fork behaviour.
- **Structs.** New structs are private with constructors and nil-safe getters. Two exceptions by long convention: domain models (`internal/domain`) have exported fields and are mutated through their builders; wire types (DTOs, Temporal inputs, config) keep exported fields for JSON.
- **Builders.** Update a domain entity through its builder. If none exists, add one that takes the existing entity and sets only what changed.
- **Errors.** Build errors with `ierr`: `ierr.WithError(err).WithHint(...).Mark(ierr.ErrValidation)`. Never put `err.Error()` in a response body. Return errors from helpers; log at the boundary.
- **Comments.** Default to none. One to four lines for a constraint, an external quirk, or an invariant the code cannot show. No restating the name, no narrating steps, no history, no ticket numbers, no section dividers. A TODO links its ticket. Document exported symbols with what they are for; skip methods that implement an interface (CL001).
- **Lint suppressions.** `//nolint:<linter> // why` and `//lint:ignore CL00N why` always carry the reason, and a PR that adds one needs a human reviewer to agree with it.

## Tests

- Test behaviour that can break: billing arithmetic, state transitions, idempotency, tenant filtering, provider protocol translation, migrations, race fixes, and whatever a bug fix changed. Do not test what types guarantee, trivial getters, copy text, or that a mock was called.
- **Extend before you add.** When a suite or table already covers the function, add a row or an assertion there. A new test file is for a new source file, not a new scenario; reviewers will ask you to move it.
- **One layer.** Test at the lowest layer that fails without the fix. Do not repeat the case at every call site.
- **Shape.** Table-driven with one scenario per row and a name that says the behaviour. A test over 100 lines that is not a table is several tests (CL003). Use `testutil.BaseServiceTestSuite` and the in-memory stores; never mock business logic.
- **Determinism.** No `time.Sleep`, no polling. Inject the clock, drain the queue, assert on the persisted result. An unconditional `t.Skip` is a quarantined test and needs `// quarantine: FLE-1234 <why>` on the line above (CL003).
- **Bug fixes** start with the fewest tests that fail on `develop` for that reason, then the fix.

## Verifying

- While iterating: `go test ./internal/<pkg> -run <Suite or Test>`.
- Before you push: `make check-changed`. It runs gofmt, loglint, codelint and golangci-lint on the lines you changed, checks `AGENTS.md` paths, and tests the packages you touched. Set `LINT_BASE` if your base is not `origin/develop`.
- Behaviour that spans services, Kafka or Temporal gets one pass on the local stack (`make run-local`, see the operations doc) or on staging. Say what you ran and what you saw.
- CI runs the full suite and the same lint gates on pull requests.

## Pull requests

- Never open a PR unless the developer asks. Feature branches go to `flexprice/flexprice:develop`; `main` is releases only.
- One problem per PR. Title in conventional-commit form, plain language: `fix(wallet): compute balance changes from the locked row`. Body: Problem, Change, Verification, and the follow-ups you accepted but are not fixing here.
- Review replies name the commit and the proof: `Fixed in 3afe93e: reused afterCommit, dropped the ent hook.` or `Not changing: <reason, with the invariant or data that supports it>.` A valid finding you defer gets a ticket and a line in the PR body, not a promise in a thread.
- Verify each bot finding against the source before acting on it. Fix the real ones; dismiss the rest with a written reason. Stop when the bots are green on the latest commit. Use the `babysit-pr` skill.
- Review comments do not widen the PR. A fix the reviewer asked for that is not this problem is a follow-up.

## Documentation

- Most changes need no documentation; agents and people can read the code.
- `docs/FLOWS/` explains how a flow works across layers. Update it when the flow changes; do not append a second account.
- `docs/design/` holds dated design docs. Decisions and their reasons go there, not in code comments.
- An `AGENTS.md` carries constraints, invariants and traps for its directory. Never a feature list, a file catalog or a command dump. Every path it names must exist (`make check-agent-docs`).
- Do not commit plans, research notes or agent scratch files. The merged PR is the record.

## Enforcement

Prose above says what a rule is for; these make it stick. Run them all with `make check-changed`.

| Gate | Checks | Scope |
|---|---|---|
| `make lint-ci` (loglint LL001–LL010) | ctx-first logging, `"error"` field, `Warn` only in bootstrap, no `fmt.Print*`, `clock.Now()` | whole repo |
| `make lint-changed` (codelint CL000–CL007) | reasoned suppressions, comments, layering imports, test hygiene, activity-log writes, time precision, package names, type-suffixed names. See `tools/lint/README.md` | changed lines |
| `make lint-go` (golangci-lint, `.golangci.yml`) | `funlen` 80 lines, `gocognit` 30, `nestif` 6, `revive` naming and error-flow rules, `unparam`, `errorlint`, `testifylint`, `nolintlint` with reasons, gofmt and goimports | changed lines |
| `make check-agent-docs` | every path an `AGENTS.md` names exists | all `AGENTS.md` |
| CodeRabbit (`.coderabbit.yaml`) | the review themes above, on changed lines only | pull requests |
