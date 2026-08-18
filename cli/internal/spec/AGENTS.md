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
