# Integration Journeys — Authoring Guide

This guide is the maintenance contract for the journey suite. It applies to
humans and AI agents alike.

## The contract

**When you add or change an API endpoint, SDK operation, or billing behavior,
update the journeys in the same PR** — exactly like unit tests:

- New endpoint/feature → extend an existing journey or add a new one.
- Changed response shape → fix the affected `capture`/`expect` paths.
- New SDK release → bump `runner/go.mod`, run `make journeys-coverage`, and
  add steps for important new operations it lists as uncovered.

Your inner loop (no network, no secrets, < 10 seconds):

```bash
make journeys-validate     # YAML structure, call resolution, arg shapes,
                           # unknown-field typos, template refs, operators
make journeys-ops          # what can I call? full SDK catalog + request fields
make journeys-coverage     # what has no test yet?
```

`journeys-validate` must pass before you commit. It catches: misspelled
operations, unknown request fields, wrong argument counts, references to
undeclared steps, malformed expectations, and template syntax errors.

## Writing a journey

One journey = one customer workflow, self-contained, parallel-safe.

```yaml
journey: my-workflow            # unique across journeys/
description: One-line summary of the customer scenario
tags: [sanity, my-area]         # `sanity` = runs in release CI

vars:                           # optional constants
  currency: USD

steps:
  - id: thing                   # id => captures live at .steps.thing.*
    name: Create Thing          # display name (optional, defaults to id)
    call: Things.CreateThing    # any Service.Method from -list-ops
    with: { ... }               # single argument (most operations)
    capture: { thing_id: id }   # dotted-path extraction from the response
    expect:                     # assertions on the response
      - { path: id, not_empty: true }

teardown:                       # always runs; delete EVERYTHING you created,
  - call: Things.DeleteThing    # in reverse dependency order
    with: "{{ .steps.thing.thing_id }}"
```

### Hard rules

1. **Unique names**: every entity name / external_id / lookup_key must embed
   `{{ .run.id }}`. Two journeys (or two runs) must never collide.
2. **Full teardown**: every created entity gets a teardown step. Teardown
   steps referencing captures that never materialized are auto-skipped, so
   write teardown for the complete happy path.
3. **No cross-journey references**: a journey may only reference its own
   steps. Need shared setup? Duplicate it — isolation beats DRY here.
4. **Assert what matters**: at minimum capture the id and assert one
   meaningful response property per create. For billing math use
   `approx: { value: X, epsilon: 0.01 }`, not string equality.
5. **Eventual consistency**: anything flowing through Kafka/ClickHouse
   (events → usage) needs an `until:` poll, not a sleep. If lag is an
   acceptable outcome (it usually is for sanity runs), add `optional: true`.

### Step reference

| Field | Meaning |
|---|---|
| `call` | SDK operation `Service.Method` (see `make journeys-ops`) |
| `with` | the single argument (struct map or scalar string) |
| `args` | positional arguments for multi-arg operations; `null` for optional pointers; `{}` for empty structs |
| `http: {method, path, query, headers, body, status}` | raw request for endpoints the SDK lacks (reported as SDK gap) |
| `capture: {name: path}` | extract response values; `$status` captures the HTTP code |
| `expect` | assertions (see operators below) |
| `expect_error: {contains, status}` | negative test — the call must fail |
| `until` + `timeout` + `interval` | poll the call until assertions pass |
| `repeat: N` | run N times; `{{ .iter }}` is the 0-based index |
| `optional: true` | failure becomes a warning, journey continues |

**Multi-arg example** — signatures come from `make journeys-ops`:
`Customers.UpdateCustomer(types.UpdateCustomerRequest, *string, *string)` →

```yaml
args: [{ name: "New" }, "{{ .steps.customer.customer_id }}", null]
```

### Expectation operators (exactly one per item)

`equals`, `not_equals`, `exists: true|false`, `not_empty: true`, `contains`
(substring / array element / object key), `matches` (regex), `gt`/`gte`/`lt`/`lte`,
`len_eq`/`len_gte`, `approx: {value, epsilon}`, and for wildcard paths
(`items.*.feature_id`): `any_eq`, `any_gt`.

Numbers compare loosely across types: `"500.00"` equals `500`.

### Templates

`{{ .vars.x }}`, `{{ .steps.<id>.<capture> }}`, `{{ .env.VAR }}`,
`{{ .run.id }}` (unique 10-hex per run), `{{ .run.ts }}`, `{{ .target.name }}`,
`{{ .iter }}`. Functions: `{{ now }}`, `{{ nowAdd "-1h" }}`, `{{ uuid }}`,
`{{ randInt 1 50 }}`.

Templates render to strings. For a numeric/bool field that needs a dynamic
value, use the coercion marker: `usage_limit: "{{= .vars.limit }}"`.

If a template references a step that failed, the step is **skipped** (not
failed) — that is the dependency cascade.

## Discovering request/response shapes

- Request fields: `make journeys-ops` prints every operation with its request
  struct's JSON field names.
- Response paths: check the SDK's `models/types/*response*.go` JSON tags, the
  Swagger spec at `docs/swagger/`, or capture `$status` and iterate against a
  dev target. A wrong `capture` path fails with the body's top-level keys in
  the error message — use that to self-correct.

## Testing the engine itself

The runner has its own unit tests (`cd runner && go test ./...`), including
an httptest fake API exercised through the real SDK. If you change engine
behavior (new operator, new step field), add a test there AND update:
this file, `schema/journey.schema.json`, and the README.

## Tags

- `sanity` — runs on develop→main release PRs and nightly. Keep it fast
  (< ~3 min per journey) and reliable.
- `smoke` — minimal subset for quick manual checks.
- Area tags (`billing`, `wallets`, `events`, …) — for targeted runs.
