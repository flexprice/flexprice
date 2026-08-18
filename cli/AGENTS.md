---
layer: constitution
owns:
  - "cli/**"
---

# Flexprice CLI — Constitution

> Invariants that must hold everywhere in this repo.
> Package-specific rules → the AGENTS.md in that package.
> Why → decisions/, narrative → ARCHITECTURE.md.

## Stack
Go 1.25 · cobra · kin-openapi · go-keyring · BurntSushi/toml · go-retryablehttp

## Directory map
| Package | Path | One-line rule |
|---|---|---|
| Client | `internal/client/` | The only package that makes a network call — [ADR 0001](decisions/0001-no-sdk-single-http-path.md) |
| Spec | `internal/spec/` | Loads the embedded OpenAPI doc, resolves commands, builds requests — never calls the network |
| Config | `internal/config/` | Profiles and credential precedence, no secrets |
| Keyring | `internal/keyring/` | API key storage — OS keychain, encrypted-file fallback |
| Output | `internal/output/` | Renders responses — the only package that writes to stdout |
| Cmd | `internal/cmd/` | The only package that imports cobra; wires the above together |

## Hard invariants

- No second way to make an HTTP request — everything goes through
  `client.Client.Do` ([ADR 0001](decisions/0001-no-sdk-single-http-path.md)).
- Never retry a non-idempotent method (POST/PATCH) on 5xx or a transport
  error ([ADR 0002](decisions/0002-retry-only-idempotent-methods.md)).
- Never send `x-environment-id` — an API key already determines its
  environment.
- No live/test flag on a profile, no `--environment` flag anywhere — nothing
  reachable by a key reveals its environment
  ([ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md)).
- `commands.yaml` is hand-curated. An unmapped operation gets a derived name
  and a CI warning, never a build failure
  ([ADR 0004](decisions/0004-curated-commands-yaml-over-mechanical-derivation.md)).
- Region list comes from the embedded spec's `servers[]`, never hardcoded
  ([ADR 0005](decisions/0005-region-discovery-from-openapi-servers.md)).
- Comments only where the logic or a constraint is genuinely non-obvious —
  no narration of straightforward code.
- No `Co-Authored-By`, no mention of Claude/Anthropic/AI assistance, anywhere
  in a commit or a comment.

## Where to look next

- [ARCHITECTURE.md](ARCHITECTURE.md) — narrative overview, request lifecycle
- [decisions/](decisions/) — why, for the five decisions above
- [guides/](guides/) — how to do the two most common maintenance tasks
- The package you're working in has its own `AGENTS.md` — read it first
