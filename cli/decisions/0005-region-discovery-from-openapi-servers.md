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
