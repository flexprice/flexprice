---
layer: service/admin
owns:
  - "internal/ee/service/admin/**"
---

# Admin services

> Business logic for the internal admin portal. Handlers delegate here.

## Purpose

Operator actions on tenant accounts and settings: creating and removing users, environments, and similar tenant administration. This package is not the customer-facing service layer.

Services take `service.ServiceParams` from `internal/ee/service` so they share repositories, the logger, and the rest of the process dependencies. Request and response types live in `internal/api/dto/admin` and use `internal/types` for shared fields such as environment type.

## Planned, not built

Per-operator RBAC and an audit log of actions. The admin router already requires `admin.secret`; cross-tenant tenant resolution in this package stays intentional for operators. New write methods should stay easy to wrap with that audit log later: one service method per operator action, with the tenant and actor ids available on the context.

## Invariants

- No Gin types in this package.
- No customer self-serve quota checks unless the method is explicitly enforcing one.
- Tenant resolution for an operator call happens here, then the context carries `tenant_id` before any repository write.
