# Flexprice Integration Journeys

Config-driven integration tests that exercise **real Flexprice API targets
through the published Go SDK**, organized as *journeys*: self-contained YAML
files that mirror how customers actually integrate Flexprice (create a plan,
subscribe a customer, ingest usage, invoice, tear down).

Each journey doubles as a **worked integration example** — if you are a
customer wondering "what calls do I make to run usage-based billing end to
end?", read [journeys/billing-lifecycle.yaml](journeys/billing-lifecycle.yaml).

```
integration-testing-suite/
├── journeys/        # the tests: one YAML file per customer workflow
├── runner/          # Go engine that executes journeys via the Go SDK
├── schema/          # JSON Schema for journey YAML (editor validation)
├── CLAUDE.md        # authoring guide (for humans and AI agents)
└── go/              # LEGACY hardcoded sanity test (superseded by journeys/)
```

## Quick start

```bash
# Point at a target (single pair…)
export FLEXPRICE_API_KEY=sk_...
export FLEXPRICE_API_HOST=https://api-dev.cloud.flexprice.io/v1   # optional

# …or multiple regions at once
export FLEXPRICE_TARGETS='[{"name":"staging","api_host":"https://api-dev.cloud.flexprice.io/v1","api_key":"sk_..."}]'
# or FLEXPRICE_TARGETS_FILE=targets.json (same shape, keeps secrets out of the shell)

# Run everything
make journeys-run

# Run a subset
make journeys-run TAGS=sanity
make journeys-run JOURNEY=customer-crud

# No secrets needed for these:
make journeys-validate    # static validation of all YAML (CI gate)
make journeys-coverage    # which SDK operations have no journey yet
make journeys-ops         # every SDK operation callable from YAML
```

Or drive the runner directly:

```bash
cd integration-testing-suite/runner
go run . -dir ../journeys -tags sanity -parallel 4 \
  -report-json report.json -junit junit.xml
```

Exit code is non-zero only on **core failures** — teardown failures are
reported but don't fail the run (matching the old sanity suite's semantics).

## How it works

The runner reflects over the official Go SDK (`github.com/flexprice/go-sdk/v2`)
at startup and exposes **every** SDK operation to YAML as `call:
Service.Method`. There is no per-endpoint glue code:

- YAML step inputs are decoded into the SDK's typed request structs (so
  requests travel the same serialization/auth path customer code uses — the
  suite is also a continuous SDK verification).
- Upgrading the SDK version in `runner/go.mod` instantly makes new operations
  callable; `make journeys-coverage` then lists them as uncovered, which is
  the signal to add journey steps for them.
- Endpoints missing from the SDK can still be tested with an `http:` step,
  and are reported as SDK gaps.

## Journey anatomy

```yaml
journey: wallet-lifecycle          # unique name
description: ...
tags: [sanity, wallets]            # for -tags filtering
vars: { topup_credits: "500" }     # constants, referenced as {{ .vars.* }}

steps:
  - id: customer                   # captures live under .steps.customer.*
    name: Create Customer          # display name (optional)
    call: Customers.CreateCustomer # any SDK operation (see -list-ops)
    with:                          # the request (single argument)
      external_id: "it-cust-{{ .run.id }}"   # .run.id = unique per run
    capture:
      customer_id: id              # dotted path into the response
    expect:
      - { path: id, not_empty: true }

  - id: update
    call: Customers.UpdateCustomer
    args:                          # multi-argument operations: positional,
      - { name: "Renamed" }        #   null for optional pointers
      - "{{ .steps.customer.customer_id }}"
      - null

  - id: wait                       # eventual consistency: poll until true
    call: Events.ListRawEvents
    with: { external_customer_id: "{{ .steps.customer.external_id }}" }
    until: [{ path: events, len_gte: 10 }]
    timeout: 60s
    interval: 4s

  - id: dup                        # negative test: the call MUST fail
    call: Customers.CreateCustomer
    with: { external_id: "{{ .steps.customer.external_id }}" }
    expect_error: { status: 409, contains: already exists }

teardown:                          # ALWAYS runs, even after failures;
  - call: Customers.DeleteCustomer #   steps whose captures never materialized
    with: "{{ .steps.customer.customer_id }}"   # are skipped automatically
```

Full schema reference: [CLAUDE.md](CLAUDE.md) ·
[schema/journey.schema.json](schema/journey.schema.json)

### Semantics that matter

- **Isolation** — every journey gets a unique `{{ .run.id }}`; use it in all
  entity names/keys so parallel runs never collide. Journeys must not share
  state; the runner executes them concurrently (`-parallel`, default 4).
- **Failure cascade** — a failed step skips the remaining core steps;
  teardown still runs. `optional: true` downgrades a step failure to a
  warning (used for known-eventually-consistent checks like usage pipelines).
- **Assertions** — `equals` compares numbers loosely (`"500.00"` == `500`),
  `approx: {value, epsilon}` for billing amounts, wildcard paths +
  `any_eq`/`any_gt` for "some array element matches" checks.

## CI

`.github/workflows/integration-journeys.yml`:

- **Pull requests touching this suite** → build + unit tests + static
  validation + coverage summary (no secrets needed).
- **Pull requests into `main`** (develop → main promotions) → runs
  `sanity`-tagged journeys against staging, which `deploy.yml` keeps deployed
  from develop — so release PRs are verified against the exact code being
  promoted. Requires the `INTEGRATION_TARGETS_JSON` secret
  (`FLEXPRICE_TARGETS` format).
- **Nightly + manual dispatch** → drift detection; dispatch inputs let you
  choose tags.

## Legacy suite

`go/` contains the previous hardcoded orchestrated sanity test. Its full flow
has been ported to `journeys/billing-lifecycle.yaml` (plus the focused
`customer-crud`, `wallet-lifecycle`, and `usage-metering` journeys). Keep it
until the journeys have soaked in CI, then delete it.
