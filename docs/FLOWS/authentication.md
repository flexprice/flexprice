# Flow: Authentication

## Trigger

- Any request to **`/v1/**` routes** gated by **`AuthenticateMiddleware`** (private group) vs public auth endpoints under `GuestAuthenticateMiddleware`.
- Middleware chain established in [`internal/api/router.go`](../../internal/api/router.go).

## Execution path

1. **Guest public auth** (`/v1/auth/signup`, `/v1/auth/login`) — bypass full auth stack (see router grouping).
2. **Protected routes:**
   - Read configured API key header (default **`x-api-key`** family per `cfg.Auth.APIKey.Header`).
   - **`validateAPIKey`** in [`internal/rest/middleware/auth.go`](../../internal/rest/middleware/auth.go):
     - First: static/config keys (`internal/auth.ValidateAPIKey`).
     - Else: **`SecretService.VerifyAPIKey`** (DB-backed secrets with RBAC roles + environment affinity).
   - Else: parse **`Authorization: Bearer <jwt>`**.
   - **`auth.Provider` validates JWT** and extracts claims mapped into context.
   - Subsequent **`EnvAccessMiddleware`** ensures environment accessibility for SaaS tenancy model.

## Modules touched

- `internal/rest/middleware` — primary logic
- `internal/auth` — provider abstraction / config key validation helpers
- `internal/ee/service` (secret/auth services) — API key persistence & verification paths
- `internal/rbac` + permission middleware (`RequirePermission` on granular routes such as `/v1/events`)

## Database operations

- Config API keys — none (pure config).
- Stored API keys — lookup via **`SecretRepository`**/`SecretService` (Ent-backed).
- User profile reads may occur indirectly on RBAC-heavy routes (`/rbac/**` grouping in router).

## External systems

- Optional **Supabase client** instantiated in `cmd/server/main.go` for certain auth-aligned features (presence gated by configuration).

## Async operations

- None on the synchronous auth gate itself beyond possible cache reads if introduced.

## Failure points

- Invalid/expired JWT → `401 Unauthorized`.
- Invalid DB API key → `401`.
- Misconfigured **`HeaderEnvironment`** pairing with constrained secrets → downstream `403`/handler errors depending on env access middleware decision.

## Retry behavior

- None at middleware level (caller must retry explicitly).

## State transitions

Authentication itself is **stateless** per request aside from **`context.Context`** enrichment (`types.CtxTenantID`, user id, roles, optional environment).

## Multi-tenant membership (FLE-1662)

Human users may belong to multiple tenants via **`tenant_memberships`** (roles are per-tenant). The JWT still carries a single active `tenant_id`; middleware loads roles from the membership for that tenant (falling back to `users.roles` for service accounts / legacy rows).

| API | Purpose |
|---|---|
| `POST /users` (invite) | If the email already has an identity, adds a membership instead of creating a second login |
| `GET /users/me/tenants` | List memberships for the current user |
| `POST /users/me/switch-tenant` | Mint a token for another membership |
| `POST /users/me/leave` | Archive the current membership; delete the auth identity only when none remain |
| `POST /users/{id}/remove` | Admin eject of a membership (same teardown rules) |

Login accepts optional `tenant_id` to activate a non-home membership.

## Related flows

- [api-request-lifecycle.md](api-request-lifecycle.md)
- Subscription/billing authorization via RBAC on specific routers.
