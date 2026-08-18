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
