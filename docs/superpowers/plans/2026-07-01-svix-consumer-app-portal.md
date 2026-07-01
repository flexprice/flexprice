# Svix Consumer App Portal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Svix OSS docs stub on the Webhooks page with a working self-hosted endpoints-management portal (list/add/delete endpoints + reveal signing secret), reachable via a publicly-exposed, WAF-scoped Svix API on the billing host.

**Architecture:** Svix OSS `app-portal-access` returns a real app-scoped JWT `token` (and a useless docs `url`). Backend returns the `token` + `app_id`; frontend renders `svix-react` `<SvixProvider>` + hooks that call the Svix API directly from the browser. The Svix API is exposed on `billing.<env>.sarvam.ai/api/v1/*` (a namespace billing doesn't use; Svix serves it natively — no path rewrite), scoped by a WAF path allowlist.

**Tech Stack:** Go (gin, svix-go SDK), React + TypeScript (svix-react, react-query, vitest + RTL), Terraform/terragrunt (AWS ALB + WAF + ECS on `sarvam-aws-flexprice-gitops`).

## Global Constraints

- Sandbox only in this plan (`billing.sandbox.sarvam.ai`). Prod is a follow-up.
- Commits: GPG-signed (auto), **no `Co-Authored-By` trailer**.
- gitops repo uses **unsigned** commits (no GPG key there) — overrides the sign rule for infra commits only.
- v1 portal scope: endpoints list + add + delete + reveal secret. No message-log/attempts/replay.
- `serverUrl` is frontend-derived; backend returns only `token` + `app_id`.
- Svix API paths are `/api/v1/*`; billing uses `/v1/*`. Do not rewrite paths.
- Browser must use the **public** billing origin, never `svix.flexprice-sandbox.local:8071`.
- Repo `flexprice/flexprice` gitignores `docs/superpowers/` — commit plan/spec with `git add -f`.

## File Structure

**Backend (`flexprice/flexprice`)**
- `internal/svix/client.go` — modify `GetDashboardURL` → return `(token string, err error)`.
- `internal/api/v1/webhook.go` — modify `GetDashboardURL` handler to return `{svix_enabled, token, app_id}`.
- `internal/api/v1/webhook_dashboard_test.go` — create; handler unit tests.

**Frontend (`flexprice/flexprice-front`)**
- `src/types/dto/webhook.ts` — add `token`, `app_id` to `WebhookDashboardResponse`.
- `src/pages/webhooks/WebhookDashboard.tsx` — modify: render SvixProvider + portal.
- `src/pages/webhooks/AppPortal.tsx` — create; the portal (endpoints list/add/delete/secret).
- `src/pages/webhooks/WebhookDashboard.test.tsx` — create; component test.
- `.env` / `.env.example` — add `VITE_SVIX_URL`.

**Infra (`flexprice/sarvam-aws-flexprice-gitops`)**
- `clients/_modules/alb/*.tf` — add optional Svix TG + listener rule inputs.
- `clients/_modules/alb/waf` (or existing WAF module) — path allowlist.
- `clients/sarvamai/aws/326334469038/ap-south-1/sandbox/*` — wire Svix TG, register ECS service, SG ingress, env `VITE_SVIX_URL` for the UI build.

---

## Task 1: Backend — return Svix app-portal token

**Files:**
- Modify: `internal/svix/client.go:95-116` (`GetDashboardURL`)
- Modify: `internal/api/v1/webhook.go:84-135` (`GetDashboardURL` handler)
- Test: `internal/api/v1/webhook_dashboard_test.go` (create)

**Interfaces:**
- Consumes: `svix.Client.GetOrCreateApplication(ctx, tenantID, environmentID) (string, error)` (unchanged).
- Produces: `svix.Client.GetDashboardURL(ctx, applicationID string) (token string, err error)` — now returns the JWT token, not the stub url. Handler responds JSON `{"svix_enabled": bool, "token": string, "app_id": string}`.

- [ ] **Step 1: Change the client method to return the token**

In `internal/svix/client.go`, replace the body of `GetDashboardURL` (keep the name; only its return value changes) so it returns `dashboard.Token`:

```go
// GetDashboardURL returns the Svix app-portal access token for the given
// application. (In Svix OSS the returned `url` is a docs stub; the usable
// value is the app-scoped JWT token, consumed by svix-react in the browser.)
func (c *Client) GetDashboardURL(ctx context.Context, applicationID string) (string, error) {
	if !c.enabled || c.client == nil {
		return "", nil
	}

	span, ctx := c.startSpan(ctx, "get_dashboard_url", map[string]interface{}{
		"application_id": applicationID,
	})
	if span != nil {
		defer span.Finish()
	}

	dashboard, err := c.client.Authentication.AppPortalAccess(ctx, applicationID, models.AppPortalAccessIn{}, &svix.AuthenticationAppPortalAccessOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get dashboard access: %w", err)
	}

	return dashboard.Token, nil
}
```

- [ ] **Step 2: Update the handler to return token + app_id**

In `internal/api/v1/webhook.go`, in `GetDashboardURL`, rename the local `url` to `token` and change the success response. Replace lines from the `GetDashboardURL` call through the final `c.JSON`:

```go
	// Get app-portal access token
	token, err := h.svixClient.GetDashboardURL(c.Request.Context(), appID)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to get Svix app-portal token",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"app_id", appID,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"svix_enabled": true,
		"token":        token,
		"app_id":       appID,
	})
```

Leave the early `!h.config.Webhook.Svix.Enabled` branch returning `{"url":"","svix_enabled":false}` unchanged (back-compat).

- [ ] **Step 3: Write the failing handler test**

Create `internal/api/v1/webhook_dashboard_test.go`. Follow the existing v1 handler test style (gin test context + a stubbed svix client). If the current `WebhookHandler` construction can't accept a fake svix client, add the smallest seam needed (a package-level interface the handler already depends on) — do not restructure unrelated code.

```go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetDashboardURL_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &WebhookHandler{config: &config.Configuration{}} // Svix.Enabled defaults false
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/webhooks/dashboard", nil)

	h.GetDashboardURL(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, false, body["svix_enabled"])
	assert.NotContains(t, body, "token")
}
```

(If a fake enabled-svix client seam is added, also assert the enabled path returns `svix_enabled:true`, a non-empty `token`, and `app_id == "<tenant>_<env>"`. Only add that assertion once the seam exists — otherwise this disabled-path test is the deliverable for Task 1.)

- [ ] **Step 4: Run the test to verify it fails (before the code edits from Steps 1-2 are compiled in) or passes (after)**

Run: `cd /Users/agrim/projects/flexprice/flexprice && go test ./internal/api/v1/ -run TestGetDashboardURL -v`
Expected: PASS (disabled path). If it fails to compile, fix the handler construction in the test to match the real `WebhookHandler` zero-value usage.

- [ ] **Step 5: Verify the whole package builds**

Run: `cd /Users/agrim/projects/flexprice/flexprice && go build ./internal/... && go test ./internal/svix/... ./internal/api/v1/... 2>&1 | tail -20`
Expected: build OK, tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/agrim/projects/flexprice/flexprice
git add internal/svix/client.go internal/api/v1/webhook.go internal/api/v1/webhook_dashboard_test.go
git commit -m "feat(webhooks): return svix app-portal token + app_id from /webhooks/dashboard"
```

---

## Task 2: Frontend — DTO + API contract

**Files:**
- Modify: `src/types/dto/webhook.ts`
- Modify: `.env.example`, `.env` (local test already has `.env.local`)

**Interfaces:**
- Produces: `WebhookDashboardResponse { url?: string; svix_enabled: boolean; token?: string; app_id?: string }`. `WebhookApi.getWebhookDashboardUrl()` return type updated accordingly (no code change needed — it's generic on the DTO).

- [ ] **Step 1: Update the DTO**

Replace `src/types/dto/webhook.ts`:

```typescript
export interface WebhookDashboardResponse {
	url?: string;
	svix_enabled: boolean;
	token?: string;
	app_id?: string;
}
```

- [ ] **Step 2: Add the Svix server URL env var**

In `.env.example` add:

```
# Public origin of the self-hosted Svix API (browser-reachable). No trailing slash.
VITE_SVIX_URL=https://billing.sandbox.sarvam.ai
```

In `.env` add the same line (prod value `https://billing.sarvam.ai` when prod ships; sandbox uses the sandbox host). Local dev already overrides via `.env.local`; add `VITE_SVIX_URL=https://billing.sandbox.sarvam.ai` there too.

- [ ] **Step 3: Type-check**

Run: `cd /Users/agrim/projects/flexprice/flexprice-front && npx tsc -b --noEmit 2>&1 | tail -20`
Expected: no new errors from `webhook.ts`.

- [ ] **Step 4: Commit**

```bash
cd /Users/agrim/projects/flexprice/flexprice-front
git add src/types/dto/webhook.ts .env.example
git commit -m "feat(webhooks): add token/app_id to dashboard DTO + VITE_SVIX_URL"
```

(Do not commit `.env`/`.env.local` if gitignored — check `git status` first; only stage `.env.example`.)

---

## Task 3: Frontend — AppPortal component (endpoints CRUD + secret)

**Files:**
- Create: `src/pages/webhooks/AppPortal.tsx`

**Interfaces:**
- Consumes (from `svix-react`):
  - `useEndpoints(): { data?: EndpointOut[]; loading: boolean; error?: Error; reload(): void; hasPrevPage: boolean; hasNextPage: boolean; prevPage(): void; nextPage(): void }`
  - `useNewEndpoint(): { url: {value,setValue}; description: {value,setValue}; eventTypes: {value,setValue}; rateLimitPerSecond: {value,setValue}; createEndpoint(): Promise<{endpoint?: EndpointOut; error?: Error}> }`
  - `useEndpointFunctions(endpointId: string): { deleteEndpoint(): Promise<void>; updateEndpoint(...); recoverEndpointMessages(...) }`
  - `useEndpointSecret(endpointId: string): { data?: { key: string }; loading; error; reload() }`
  - `EndpointOut` has `{ id: string; url: string; description: string; disabled?: boolean }`
- Produces: `default export function AppPortal(): JSX.Element` — assumes it is rendered inside a `<SvixProvider>` (Task 4 wraps it).

- [ ] **Step 1: Write the component**

Create `src/pages/webhooks/AppPortal.tsx`. Use existing atoms/molecules where the codebase has them (Button, Input, Loader from `@/components/atoms`); keep styling minimal and consistent with the page. Reveal-secret is per-endpoint on demand.

```tsx
import { useState } from 'react';
import { useEndpoints, useNewEndpoint, useEndpointFunctions, useEndpointSecret } from 'svix-react';
import type { EndpointOut } from 'svix';
import { Button, Input, Loader } from '@/components/atoms';
import toast from 'react-hot-toast';

function EndpointRow({ ep, onDeleted }: { ep: EndpointOut; onDeleted: () => void }) {
	const { deleteEndpoint } = useEndpointFunctions(ep.id);
	const secret = useEndpointSecret(ep.id);
	const [showSecret, setShowSecret] = useState(false);

	return (
		<li className='flex items-center justify-between gap-4 border-b py-3'>
			<div className='min-w-0'>
				<div className='truncate font-medium'>{ep.url}</div>
				{ep.description && <div className='truncate text-sm text-gray-500'>{ep.description}</div>}
				{showSecret && (
					<div className='mt-1 break-all font-mono text-xs'>
						{secret.loading ? 'Loading…' : secret.error ? 'Failed to load secret' : secret.data?.key}
					</div>
				)}
			</div>
			<div className='flex shrink-0 gap-2'>
				<Button
					variant='outline'
					onClick={() => {
						setShowSecret((s) => !s);
						if (!secret.data) secret.reload();
					}}>
					{showSecret ? 'Hide secret' : 'Reveal secret'}
				</Button>
				<Button
					variant='outline'
					onClick={async () => {
						if (!window.confirm(`Delete endpoint ${ep.url}?`)) return;
						try {
							await deleteEndpoint();
							toast.success('Endpoint deleted');
							onDeleted();
						} catch {
							toast.error('Failed to delete endpoint');
						}
					}}>
					Delete
				</Button>
			</div>
		</li>
	);
}

export default function AppPortal() {
	const endpoints = useEndpoints();
	const form = useNewEndpoint();
	const [submitting, setSubmitting] = useState(false);

	if (endpoints.loading && !endpoints.data) {
		return (
			<div className='flex h-96 items-center justify-center'>
				<Loader />
			</div>
		);
	}
	if (endpoints.error) {
		return <div className='p-4 text-red-600'>Failed to load webhook endpoints.</div>;
	}

	return (
		<div className='space-y-6'>
			<form
				className='flex items-end gap-3'
				onSubmit={async (e) => {
					e.preventDefault();
					if (!form.url.value) return;
					setSubmitting(true);
					const res = await form.createEndpoint();
					setSubmitting(false);
					if (res.error) {
						toast.error('Failed to add endpoint');
						return;
					}
					toast.success('Endpoint added');
					form.url.setValue('');
					form.description.setValue('');
					endpoints.reload();
				}}>
				<Input
					label='Endpoint URL'
					placeholder='https://example.com/webhooks'
					value={form.url.value}
					onChange={(v: string) => form.url.setValue(v)}
				/>
				<Input
					label='Description'
					placeholder='Optional'
					value={form.description.value}
					onChange={(v: string) => form.description.setValue(v)}
				/>
				<Button type='submit' disabled={submitting || !form.url.value}>
					{submitting ? 'Adding…' : 'Add endpoint'}
				</Button>
			</form>

			<ul>
				{endpoints.data?.length ? (
					endpoints.data.map((ep) => <EndpointRow key={ep.id} ep={ep} onDeleted={endpoints.reload} />)
				) : (
					<li className='py-6 text-center text-gray-500'>No endpoints yet. Add one above.</li>
				)}
			</ul>

			{(endpoints.hasPrevPage || endpoints.hasNextPage) && (
				<div className='flex gap-2'>
					<Button variant='outline' disabled={!endpoints.hasPrevPage} onClick={endpoints.prevPage}>
						Previous
					</Button>
					<Button variant='outline' disabled={!endpoints.hasNextPage} onClick={endpoints.nextPage}>
						Next
					</Button>
				</div>
			)}
		</div>
	);
}
```

**Note on atom prop shapes:** `Button`/`Input` prop names (`variant`, `onChange` signature, `label`) must match this repo's atoms. Before finalizing, open `src/components/atoms/index.ts` (or the Button/Input files) and adjust the props above to the real signatures — e.g. if `Input`'s `onChange` is `(e) => ...` instead of `(v) => ...`. This is a mechanical alignment, not a design change.

- [ ] **Step 2: Type-check**

Run: `cd /Users/agrim/projects/flexprice/flexprice-front && npx tsc -b --noEmit 2>&1 | grep -i appportal; echo done`
Expected: no errors referencing `AppPortal.tsx`.

- [ ] **Step 3: Commit**

```bash
cd /Users/agrim/projects/flexprice/flexprice-front
git add src/pages/webhooks/AppPortal.tsx
git commit -m "feat(webhooks): add svix-react endpoints portal component"
```

---

## Task 4: Frontend — wire SvixProvider into WebhookDashboard

**Files:**
- Modify: `src/pages/webhooks/WebhookDashboard.tsx`
- Test: `src/pages/webhooks/WebhookDashboard.test.tsx` (create)

**Interfaces:**
- Consumes: `WebhookDashboardResponse` (Task 2), `AppPortal` default export (Task 3), `SvixProvider` from `svix-react`, `VITE_SVIX_URL`.
- Produces: the Webhooks page renders `<SvixProvider token={data.token} appId={data.app_id} options={{ serverUrl }}><AppPortal/></SvixProvider>` when `svix_enabled && token && app_id`; otherwise the existing fallback.

- [ ] **Step 1: Write the failing component test**

Create `src/pages/webhooks/WebhookDashboard.test.tsx`. Mock `svix-react` (both `SvixProvider` and the hooks used by `AppPortal`) and the prefetch query so the test is deterministic.

```tsx
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';

vi.mock('svix-react', () => ({
	SvixProvider: ({ children }: { children: React.ReactNode }) => <div data-testid='svix-provider'>{children}</div>,
	useEndpoints: () => ({ data: [], loading: false, error: undefined, reload: vi.fn(), hasPrevPage: false, hasNextPage: false, prevPage: vi.fn(), nextPage: vi.fn() }),
	useNewEndpoint: () => ({ url: { value: '', setValue: vi.fn() }, description: { value: '', setValue: vi.fn() }, eventTypes: { value: [], setValue: vi.fn() }, rateLimitPerSecond: { value: undefined, setValue: vi.fn() }, createEndpoint: vi.fn() }),
	useEndpointFunctions: () => ({ deleteEndpoint: vi.fn(), updateEndpoint: vi.fn(), recoverEndpointMessages: vi.fn() }),
	useEndpointSecret: () => ({ data: undefined, loading: false, error: undefined, reload: vi.fn() }),
}));

const mockData = vi.fn();
vi.mock('@tanstack/react-query', () => ({
	useQuery: () => mockData(),
}));
vi.mock('@/hooks/useEnvironment', () => ({ default: () => ({ activeEnvironment: { id: 'env_1' } }) }));

import WebhookDashboard from './WebhookDashboard';

describe('WebhookDashboard', () => {
	it('renders SvixProvider portal when svix enabled with token', () => {
		mockData.mockReturnValue({ data: { svix_enabled: true, token: 'tok_123', app_id: 'tenant_env' }, isLoading: false, isError: false });
		render(<WebhookDashboard />);
		expect(screen.getByTestId('svix-provider')).toBeInTheDocument();
	});

	it('renders fallback when svix disabled', () => {
		mockData.mockReturnValue({ data: { svix_enabled: false }, isLoading: false, isError: false });
		render(<WebhookDashboard />);
		expect(screen.queryByTestId('svix-provider')).not.toBeInTheDocument();
	});
});
```

(Adjust the `useEnvironment`/`react-query`/i18n mocks to match how `WebhookDashboard.tsx` actually imports them — mirror any existing `*.test.tsx` setup in the repo, including a required i18n mock for `useTranslation`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/agrim/projects/flexprice/flexprice-front && npx vitest run src/pages/webhooks/WebhookDashboard.test.tsx 2>&1 | tail -25`
Expected: FAIL — the current page renders `<AppPortal url=.../>`, no `svix-provider` testid.

- [ ] **Step 3: Rewrite the render branch**

In `src/pages/webhooks/WebhookDashboard.tsx`:
- Remove the `import { AppPortal } from 'svix-react'` and `import 'svix-react/style.css'` and the `appPortalProps` useMemo.
- Add `import { SvixProvider } from 'svix-react';` and `import AppPortal from './AppPortal';`.
- Replace the final `return` (the `svix_enabled` success branch) with:

```tsx
	const serverUrl = import.meta.env.VITE_SVIX_URL ?? '';

	if (!data?.token || !data?.app_id) {
		// Enabled but missing token/app_id — treat as the disabled/empty state.
		return (
			<Page className='h-full w-full' heading={webhooksHeading}>
				<ApiDocsContent tags={API_DOCS_TAGS.Webhooks} />
				<EmptyPage
					heading={webhooksHeading}
					emptyStateCard={{
						heading: t('developers:webhooks.disabled.heading'),
						description: t('developers:webhooks.disabled.description'),
					}}
				/>
			</Page>
		);
	}

	return (
		<Page className='h-full w-full' heading={webhooksHeading}>
			<ApiDocsContent tags={API_DOCS_TAGS.Webhooks} />
			<SvixProvider token={data.token} appId={data.app_id} options={{ serverUrl }}>
				<AppPortal />
			</SvixProvider>
		</Page>
	);
```

Keep the existing `isLoading`, `isError`, and `!data?.svix_enabled` branches unchanged.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/agrim/projects/flexprice/flexprice-front && npx vitest run src/pages/webhooks/WebhookDashboard.test.tsx 2>&1 | tail -15`
Expected: PASS (both cases).

- [ ] **Step 5: Type-check + build**

Run: `cd /Users/agrim/projects/flexprice/flexprice-front && npx tsc -b --noEmit 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/agrim/projects/flexprice/flexprice-front
git add src/pages/webhooks/WebhookDashboard.tsx src/pages/webhooks/WebhookDashboard.test.tsx
git commit -m "feat(webhooks): render self-hosted svix app portal via SvixProvider"
```

---

## Task 5: Infra — expose Svix API on billing host (sandbox), WAF-scoped

**Files (`flexprice/sarvam-aws-flexprice-gitops`):**
- Modify: `clients/_modules/alb/variables.tf`, `clients/_modules/alb/main.tf`
- Modify/Create: WAF allowlist (existing WAF module or new rule file)
- Modify: `clients/sarvamai/aws/326334469038/ap-south-1/sandbox/` — Svix TG target, ECS service registration, SG ingress

**Interfaces:**
- Consumes: existing sandbox ALB, ACM cert, billing host, Svix ECS service (Cloud-Map, port 8071).
- Produces: `https://billing.sandbox.sarvam.ai/api/v1/*` routed to a new `flexprice-sandbox-svix-tg`, with a WAF path allowlist. UI build env gets `VITE_SVIX_URL=https://billing.sandbox.sarvam.ai`.

> Infra is applied via terragrunt through the `ext-flexprice-sarvam` push key; **run `plan` only** in this plan — do not `apply` without explicit user go-ahead (the spec's sandbox-first, ask-before-AWS discipline). Infra commits in this repo are unsigned.

- [ ] **Step 1: Add a Svix target group + listener rule to the alb module**

In `clients/_modules/alb/variables.tf` add:

```hcl
variable "svix_target_port" {
  type        = number
  default     = 0
  description = "Port for the Svix service target group. 0 disables the Svix rule."
}

variable "svix_path_patterns" {
  type        = list(string)
  default     = ["/api/v1/*"]
  description = "Path patterns routed to the Svix target group (higher priority than the API rule)."
}
```

In `clients/_modules/alb/main.tf`, add a Svix TG + a listener rule at **priority 5** (before the priority-10 API rule), gated on `var.svix_target_port > 0`:

```hcl
resource "aws_lb_target_group" "svix" {
  count       = var.svix_target_port > 0 ? 1 : 0
  name        = "${var.prefix}-svix-tg"
  port        = var.svix_target_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"
  health_check {
    path    = "/api/v1/health"
    matcher = "200-399"
  }
}

resource "aws_lb_listener_rule" "svix" {
  count        = var.svix_target_port > 0 && var.certificate_arn != "" ? 1 : 0
  listener_arn = aws_lb_listener.https[0].arn
  priority     = 5
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.svix[0].arn
  }
  condition {
    path_pattern { values = var.svix_path_patterns }
  }
}
```

(Match the existing module's names for `aws_lb_listener.https`, `var.prefix`, `var.vpc_id`, `var.certificate_arn` — read `main.tf` first and align. Output the TG arn if the sandbox layer needs it to register the ECS service.)

- [ ] **Step 2: Register the Svix ECS service to the TG + open SG**

In the sandbox layer, add `load_balancer` registration on the Svix ECS service (container `svix`, port 8071 → `aws_lb_target_group.svix`) and an ingress rule on the Svix ECS SG allowing the ALB SG on 8071. Read the existing Svix service definition (`values.yaml` svix block + its terragrunt) and wire minimally.

- [ ] **Step 3: Add the WAF path allowlist**

Add a WAF rule (or rule group) associated with the ALB that, for requests matching host `billing.sandbox.sarvam.ai` and path prefix `/api/v1/`, **allows only**:
- `GET|POST|DELETE /api/v1/app/*/endpoint*`
- `GET|PATCH|PUT /api/v1/app/*/endpoint/*/secret*` and `/headers*`
- `GET /api/v1/app/*/msg*`, `GET /api/v1/app/*/attempt*`
- `GET /api/v1/event-type*`
- `GET /api/v1/health`

and **blocks** everything else under `/api/v1/` (notably `POST /api/v1/app` and `DELETE /api/v1/app/*`). Add an AWS rate-based rule. Follow the WAF pattern already in the repo; if none exists, add the smallest managed + custom rule set that expresses this allowlist.

- [ ] **Step 4: Set VITE_SVIX_URL for the sandbox UI build**

In the sandbox UI build config (the nginx/S3 UI image build env or the front `.env` baked at build — per the ui-nginx-ecs setup), set `VITE_SVIX_URL=https://billing.sandbox.sarvam.ai`.

- [ ] **Step 5: Plan (no apply)**

Run: `cd clients/sarvamai/aws/326334469038/ap-south-1/sandbox && terragrunt plan 2>&1 | tail -40` (per-affected-layer: alb, ecs/svix, waf).
Expected: creates svix TG + listener rule (priority 5) + WAF rule + SG ingress; no destructive changes to the existing API TG / UI default.

- [ ] **Step 6: Commit (unsigned, infra repo)**

```bash
cd /Users/agrim/projects/flexprice/sarvam-aws-flexprice-gitops
git add clients/_modules/alb clients/sarvamai/aws/326334469038/ap-south-1/sandbox
git commit -m "feat(sandbox): expose svix api on billing host /api/v1/* with WAF allowlist"
```

---

## Task 6: End-to-end validation (sandbox)

**Files:** none (validation only).

**Interfaces:** Consumes the deployed sandbox from Task 5 + the frontend from Tasks 1-4.

- [ ] **Step 1: Apply infra (with user go-ahead)**

After user approves, `terragrunt apply` the sandbox layers. Confirm the Svix TG shows healthy targets and the listener rule is priority 5.

- [ ] **Step 2: Verify routing + WAF**

Run:
```bash
curl -s -o /dev/null -w '%{http_code}\n' https://billing.sandbox.sarvam.ai/api/v1/health   # expect 200
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://billing.sandbox.sarvam.ai/api/v1/app  # expect 403 (WAF block)
```

- [ ] **Step 3: Portal smoke test**

With the local frontend already pointed at sandbox (`.env.local` + dev server on :3000, `VITE_SVIX_URL=https://billing.sandbox.sarvam.ai`), log into a sandbox tenant → Webhooks page → confirm the endpoints portal renders (not the docs page). Add an endpoint → confirm it appears, reveal its secret, then delete it.

- [ ] **Step 4: Confirm delivery**

Trigger a webhook-producing action (or use an existing invoice/subscription event) and confirm delivery to the added endpoint in the portal / at the endpoint URL.

- [ ] **Step 5: Record result + clean up local test rig**

Note the outcome. Kill the local dev server and remove `.env.local` if no longer needed.

---

## Self-Review Notes

- **Spec coverage:** infra exposure (Task 5), WAF allowlist (Task 5.3), backend token (Task 1), frontend SvixProvider + endpoints CRUD + secret (Tasks 2-4), error/disabled fallback preserved (Task 4.3), testing (Tasks 1.3, 4.1), E2E (Task 6). All spec sections mapped.
- **Placeholders:** none — code shown for every code step; the two "align to real atom/module names" notes are explicit mechanical-alignment steps, not deferred logic.
- **Type consistency:** `GetDashboardURL(ctx, appID) (string, error)` returns token in Task 1 and is consumed by the handler in the same task; `WebhookDashboardResponse.token/app_id` (Task 2) consumed in Task 4; `AppPortal` default export (Task 3) consumed in Task 4; svix-react hook shapes (Task 3) match the mocks (Task 4.1).
