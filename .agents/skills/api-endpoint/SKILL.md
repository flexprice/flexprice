---
name: api-endpoint
description: Use when adding or changing a public API endpoint, a request or response DTO, or a route. Walks the layers from domain to Swagger so the SDKs, MCP server and both entry points stay in step.
---

# Adding or changing an API endpoint

Read `internal/api/v1/AGENTS.md` first. Then, in this order, touching only the layers the change needs:

1. **Domain** (`internal/domain/<entity>/`): model, repository interface, builder. Nothing here imports a layer above.
2. **Schema** (`ent/schema/<entity>.go`), then `make generate-ent` and `make generate-migration`. The new migration file sorts after the newest on `develop` and its checksum lands in `migrations/.hashes`.
3. **Repository** (`internal/repository/ent/<entity>.go`): every query filters `tenant_id` and `environment_id` from ctx. Bulk writes on an activity-tracked entity use `SaveReturning` / `ExecReturning`.
4. **Service** (`internal/ee/service/`): the behaviour, as a method on the service that owns the entity, in a file named for the concern. Scope provider work with `BillingEntityService.ScopeTo*` at the entry point. Register new dependencies in `cmd/server/main.go`.
5. **DTO** (`internal/api/dto/<entity>.go`): request validation in `Validate()`, conversion in `To<Entity>()` / `From<Entity>()`. Decimals are strings on the wire.
6. **Handler** (`internal/api/v1/<entity>.go`): bind, validate, call one service method, `c.Error(err)` on failure. Swagger annotations on every handler, with `@x-scope "read"` on a read-only POST and `@x-scope "delete"` on an irreversible one.
7. **Route** in `internal/api/router.go`.
8. `make swagger`, and commit `docs/swagger/`. SDKs and the MCP server regenerate from it on merge.

## Before you call it done

- Is the same behaviour reachable from a Temporal workflow, a consumer or the admin API? Then it must be the service method they all call.
- Does preview agree with the real operation?
- Is there a way back out of any new state?
- Did you run it with billing entities enabled and an owned connection?
- Is the test a new row in the entity's existing suite in `internal/ee/service/<entity>_test.go`?
- `make check-changed`.
