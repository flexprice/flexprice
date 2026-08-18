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
- `Load()` memoizes the parsed document via a package-level `sync.Once` — the
  first call pays the real ~48-73ms parse cost, every call after is
  effectively free. Call it as often as convenient; do not thread a
  `*openapi3.T` through extra parameters just to avoid a second `Load()`
  call, and do not add a second, uncached parse path.

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
- **`PageInfo` had the identical key-presence-not-key-type bug as
  `internal/output`'s `hasListMarker` — a real, shipped bug, not a
  hypothetical.** `InvoiceResponse`'s string `total` field made `PageInfo`
  believe a single invoice was a paginated list; for a whole-dollar invoice
  (common — `shopspring/decimal` strips trailing zeros), `flexprice invoices
  retrieve <id> --all` would re-issue the identical `GET` request until
  `shown` reached the invoice's dollar amount. Fixed: envelope detection now
  requires a `"pagination"` sub-object, an unambiguous `"items"` key, or (the
  legacy shape) genuinely number-typed `total`/`limit`/`offset` alongside a
  real array-of-objects field — never a bare same-named string. Array-key
  selection (`findObjectArrayKey`) now prefers `items` and falls back to a
  sorted scan instead of Go's randomized map iteration, so `Count` is
  deterministic across runs. If you see a key-presence-only check here
  again, you are reintroducing this bug — see the identical fix in
  `internal/output/table.go`'s `hasListMarker`.
- **`newRegistry`'s default-allow validation can still hard-fail the whole
  CLI on a name collision — a real gap in the design, currently latent.**
  The default-allow loop derives a name for every unmapped operation and
  calls `add`, which returns a hard error on ANY resource+action collision —
  including a collision between two derived names, or a derived name that
  happens to match an already-curated `commands.yaml` entry. That error
  propagates straight out of `NewRegistry`/`Load`, breaking every command at
  startup — directly contradicting the "never a hard failure" invariant
  above. Not currently exercised against the real spec (confirmed via
  `TestRegistry_EveryOperationIsAccountedFor` passing), so this has not
  broken anything yet, but it will the day two never-mapped operations under
  the same tag happen to derive the same kebab-case action name. Not fixed
  as of this writing — if you touch collision handling in `add`, make a
  collision between two *derived* names degrade to a warning (picking one
  deterministically, e.g. by operationId) rather than a hard error, matching
  how a collision with a *curated* name should still fail loudly (that one
  really is a human mistake worth blocking on).

## Related layers

- `internal/cmd` — the only consumer of `Registry`/`BuildRequest`/`Skeleton`
- `cli/spec/commands.yaml` — the curated map this package validates
- `cli/spec/openapi.json` — the embedded spec, synced via `make sync-cli-spec`
