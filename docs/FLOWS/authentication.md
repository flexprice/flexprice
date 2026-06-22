# Flow: Authentication

## Trigger

- Any request to **`/v1/**` routes** gated by **`AuthenticateMiddleware`** (private group) vs public auth endpoints under `GuestAuthenticateMiddleware`.
- Middleware chain established in [`internal/api/router.go`](internal/api/router.go).

## Execution path

1. **Guest public auth** (`/v1/auth/signup`, `/v1/auth/login`) — bypass full auth stack (see router grouping).
2. **Protected routes:**
   - Read configured API key header (default **`x-api-key`** family per `cfg.Auth.APIKey.Header`).
   - **`validateAPIKey`** in [`internal/rest/middleware/auth.go`](internal/rest/middleware/auth.go):
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

## Related flows

- [api-request-lifecycle.md](api-request-lifecycle.md)
- Subscription/billing authorization via RBAC on specific routers.
