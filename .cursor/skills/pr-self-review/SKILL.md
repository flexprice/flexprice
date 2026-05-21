---
name: pr-self-review
description: >-
  Structured self-review before opening or finalizing a FlexPrice PR — layering, secrets,
  generated code, migrations, API surface, and tests. Use before gh pr create, when the user
  asks for a pre-PR checklist, ship review, or PR readiness.
---

# PR self-review (FlexPrice)

Run mentally or out loud through this list; fix gaps before requesting review.

## Scope & layering

- [ ] Change lives in correct layer (handler vs service vs domain vs repo per **`docs/ARCHITECTURE.md`**).
- [ ] No new hidden globals unless unavoidable and documented (`HOTSPOTS.md` patterns).
- [ ] Tenant/environment scoping preserved for multi-tenant data paths.

## Security & config

- [ ] No secrets, tokens, or production URLs committed (`.env`, keys in tests).
- [ ] Auth/RBAC-sensitive routes still behind correct middleware (`internal/api/router.go` patterns).

## Data & migrations

- [ ] Ent schema edits accompanied by **`make generate-ent`** and migration plan (**`make migrate-ent`** / **`make generate-migration`** per deployment).
- [ ] ClickHouse changes have matching files under **`migrations/clickhouse/`** when applicable.

## API & clients

- [ ] Swagger annotations updated for new/changed endpoints; run **`make swagger`**.
- [ ] If public SDK contract changes: **`make sdk-all`** (or agreed subset) and **`api/custom`** merged.

## Quality

- [ ] **`gofmt`**, **`go vet`** on affected scope.
- [ ] **`go test -race`** on touched packages or **`make test`** for broad changes.
- [ ] Prefer table-driven tests for multiple cases (`internal/service/*_test.go` conventions).

## Docs & graph

- [ ] Structural change updates **`docs/REPO_MAP.md`** / **`DEPENDENCY_GRAPH.md`** / **`FLOWS/*`** when behavior or topology changes.
- [ ] If Graphify is used: **`graphify update .`** at repo root (see **`repo-architecture-intelligence`** personal skill).

## Tone for reviewers

Summarize intent, risk, and test evidence in the PR body (not “fixed stuff”).
