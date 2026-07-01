# Self-Hosted Svix Consumer App Portal — Design

**Date:** 2026-07-01
**Status:** Approved, pending implementation plan
**Envs:** Sandbox first (`billing.sandbox.sarvam.ai`); prod follow-up.

## Problem

The flexprice frontend Webhooks page shows the Svix OSS docs page instead of a
webhook-management UI.

Root cause (verified against the live sandbox API and Svix OSS source):

- `GET /v1/webhooks/dashboard` returns `{"svix_enabled":true,"url":"https://docs.svix.com/app-portal/oss"}`.
- Svix OSS hardcodes the App Portal URL to that docs stub in
  `server/svix-server/src/v1/endpoints/auth.rs`:
  ```rust
  Ok(Json(AppPortalAccessOut::from(DashboardAccessOut {
      url: "https://docs.svix.com/app-portal/oss".to_owned(),
      token,
  })))
  ```
  The response also carries a real **app-scoped JWT `token`**. The hosted App
  Portal iframe UI is closed-source and not in OSS.
- Backend `svix.Client.GetDashboardURL` returns only `dashboard.Url` (the stub),
  discarding `dashboard.Token`.
- Frontend `WebhookDashboard.tsx` iframes that `url` via `svix-react`'s
  `<AppPortal url=.../>` → renders the docs page = the screenshot.

Svix OSS's own guidance: build your own portal with `svix-react`
(`<SvixProvider token appId>` + hooks) or the JS SDK. `svix-react` talks to the
Svix API **from the browser**, so the Svix API must be browser-reachable — today
it is VPC-internal only (`svix.flexprice-sandbox.local:8071`).

## Approach

Expose the Svix API publicly on the existing billing host (path-scoped,
WAF-allowlisted), and render an endpoints-management portal in flexprice-front
using `svix-react` hooks.

Decisions made during brainstorming:
- **Reachability:** expose Svix API publicly (Svix's intended design; least app code).
- **Exposure scope:** path-allowlist at ALB/WAF (block admin + app create/delete).
- **Hostname:** path on billing host (reuse cert + DNS). `/api/v1/*` is free on
  that host — billing uses `/v1/*`, Svix natively serves `/api/v1`, so **no path
  rewrite** is needed.
- **Env:** sandbox first.
- **Portal scope (v1):** endpoints list + add + delete + reveal signing secret.
  No message-log / attempts / replay tabs (add later).
- **server_url:** frontend-derived (backend returns only `token` + `app_id`).

## Components & Changes

### 1. Infra — `sarvam-aws-flexprice-gitops` (sandbox)

- New target group `flexprice-sandbox-svix-tg` (port 8071). Register the Svix
  ECS Fargate service (currently Cloud-Map-only) to it.
- ALB listener rule on the billing host: `path_pattern /api/v1/*` → Svix TG, at a
  **priority higher (lower number) than** the billing `/v1/*` rule (priority 10).
  No rewrite; Svix serves `/api/v1` natively, billing serves `/v1`.
- WAF: allowlist only the portal-consumed paths and methods —
  `GET/POST/DELETE /api/v1/app/{app_id}/endpoint*`, endpoint secret/headers,
  `GET /api/v1/app/{app_id}/msg*`, `GET .../attempt*`, `GET /api/v1/event-type*`.
  Block `POST/DELETE /api/v1/app` (app create/delete) and any admin route.
  Add rate-limiting.
- Svix ECS security group: allow ingress from the ALB SG on 8071.

The `alb` module today defines a single API TG + one priority-10 rule and has no
Svix TG and no rewrite capability. This design adds the Svix TG + rule via the
module (or a sibling resource), following the existing ownership split (API TG +
443 listener + rules = terraform; UI default = CLI).

### 2. Backend — `flexprice/internal`

- `svix/client.go` `GetDashboardURL`: return the `token` and the `app_id` (the
  `tenantID_environmentID` app id) instead of the stub `url`. Rename/extend to
  e.g. `GetAppPortalAccess(ctx, appID) (token string, err error)` returning the
  token; `app_id` is already known by the caller.
- `api/v1/webhook.go` `GetDashboardURL` handler: respond
  `{"svix_enabled":true,"token":"<jwt>","app_id":"<tenant_env>"}` when enabled;
  keep `{"svix_enabled":false}` (and optional legacy `url`) when disabled.
- No new config field. `server_url` is derived on the frontend.

### 3. Frontend — `flexprice-front/src/pages/webhooks/WebhookDashboard.tsx`

- Response DTO (`types/dto/webhook.ts`): add `token`, `app_id`.
- `serverUrl` derived on the client (new `VITE_SVIX_URL`, defaulting to the
  billing origin — e.g. `https://billing.sandbox.sarvam.ai`). Do **not** use the
  internal `.local:8071` DNS.
- Replace `<AppPortal url=.../>` with a portal component wrapped in
  `<SvixProvider token={token} appId={app_id} options={{ serverUrl }}>`.
- v1 portal component uses `svix-react` hooks:
  - `useEndpoints` — list endpoints (with pagination controls the hook provides).
  - `useNewEndpoint` — add-endpoint form (URL + event-type filters optional).
  - endpoint delete (via `useSvix`/SDK `endpoint.delete`).
  - `useEndpointSecret` — reveal signing secret.
- Keep the existing `svix_enabled:false` fallback (docs/empty card).

## Data Flow

1. Webhooks page → `GET /v1/webhooks/dashboard` (flexprice, JWT/api-key auth) →
   `{svix_enabled, token, app_id}`.
2. Frontend sets `serverUrl` (derived) and renders
   `<SvixProvider token appId options={{serverUrl}}>`.
3. `svix-react` hooks call, from the browser,
   `https://billing.<env>.sarvam.ai/api/v1/app/{app_id}/endpoint...` → ALB
   (`/api/v1/*` rule) → Svix TG → Svix Fargate. The app-scoped token authorizes
   only that tenant+env application.

## Error Handling

- `svix_enabled:false` → existing docs/empty fallback (`WebhookDashboard.tsx`).
- Empty `token` or Svix unreachable → svix-react hooks surface `.error` → render
  an inline error card (the hooks' documented pattern).
- Token expiry (app-portal token is short-lived): on 401, invalidate the
  `/webhooks/dashboard` react-query cache and refetch once to mint a fresh token.

## Testing

- **Backend unit:** `GetDashboardURL` returns `token`+`app_id` when Svix enabled;
  `{svix_enabled:false}` when disabled. Extend existing webhook handler tests.
- **Frontend component:** WebhookDashboard renders `SvixProvider` + portal when
  `svix_enabled` and `token` present; renders fallback otherwise. Mock the query.
- **Infra:** `terragrunt plan` on ALB/TG change; post-deploy curl of a Svix
  health/allowed path via `https://billing.sandbox.sarvam.ai/api/v1/...`; verify
  WAF blocks `POST /api/v1/app`.
- **E2E manual:** local flexprice-front pointed at sandbox → add an endpoint →
  confirm it appears in Svix and a test event delivers.

## Out of Scope (v1)

- Message log, delivery attempts, replay, event-type management tabs.
- Prod rollout (follow-up once sandbox is validated end-to-end).
- Dedicated Svix subdomain (using billing-host path instead).
