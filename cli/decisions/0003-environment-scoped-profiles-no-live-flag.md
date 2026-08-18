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
