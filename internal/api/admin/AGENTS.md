---
layer: api/admin
owns:
  - "internal/api/admin/**"
---

# Admin API

> Operator HTTP only. Parse → validate → delegate → respond.
> This process is the backend for the internal admin portal.

## Purpose

Flexprice staff manage tenant accounts and settings from the admin portal: users, environments, and other tenant-scoped administration. `deployment.mode=admin` serves this router and does not mount the public API, Kafka consumers, or Temporal workers.

These routes are not part of the public OpenAPI contract or the generated SDKs.

## Layout

| Path | Role |
|---|---|
| `internal/api/admin/router.go` | `Handlers` and `NewRouter`. Register every admin route here. |
| `internal/api/admin/v1/` | One handler file per resource |
| `internal/api/dto/admin/` | Admin request and response types |
| `internal/ee/service/admin/` | Admin business logic |
| `cmd/server/main.go` | `provideAdminHandlers`, `provideAdminRouter` |

## Adding an endpoint

1. Add the request and response in `internal/api/dto/admin`. Reuse `internal/types` for shared enums.
2. Add the service in `internal/ee/service/admin`, taking `service.ServiceParams`.
3. Add a handler in `internal/api/admin/v1`.
4. Add the handler to `Handlers`, construct it in `provideAdminHandlers`, and register the route in `NewRouter`.

## Auth

`/v1` requires header `X-Admin-Secret` matching `admin.secret` (`FLEXPRICE_ADMIN_SECRET`). Admin mode refuses to boot when that secret is empty. `/health` is open for probes.

Cross-tenant access remains: an authorized operator names the tenant by `tenant_id` or email. That lookup lives in `internal/ee/service/admin`.

## Planned, not built

- RBAC for each operator.
- An audit log of actions taken through this API.

Keep this process off the public internet. Do not mount these handlers on the public router.

## Invariants

- No business logic in handlers.
- No repository calls from handlers.
- Do not apply customer self-serve quotas to operator actions unless the endpoint is explicitly meant to.
