# FlexPrice Scripts

This directory contains various scripts for managing FlexPrice data and operations.

## Available Scripts

### 1. Assign Plan to Customers

Assigns a specific plan to all customers who don't already have a subscription for it.

**Usage:**

```bash
go run scripts/main.go -cmd assign-plan -tenant-id <tenant_id> -environment-id <environment_id> -plan-id <plan_id>
```

**Example:**

```bash
go run scripts/main.go -cmd assign-plan -tenant-id "tenant_123" -environment-id "env_456" -plan-id "plan_01JV2ZF6B57XZ7MRW72Q2QWQ98"
```

**What it does:**

1. Lists all customers in the specified tenant/environment
2. Checks which customers already have an active subscription for the specified plan
3. Creates new subscriptions for customers who don't have the plan
4. Uses the following default subscription settings:
   - Currency: USD
   - Billing Cadence: RECURRING
   - Billing Period: MONTHLY
   - Billing Period Count: 1
   - Billing Cycle: CALENDAR
   - Start Date: Current time

**Output:**
The script provides detailed logging including:

- Number of customers processed
- Number of subscriptions created
- Number of customers skipped (already have plan, inactive, etc.)
- Any errors encountered

### 2. Sync Plan Prices

Synchronizes all prices from a plan to existing subscriptions.

**Usage:**

```bash
go run scripts/main.go -cmd sync-plan-prices -tenant-id <tenant_id> -environment-id <environment_id> -plan-id <plan_id>
```

### 3. Next SDK version (CI / optional)

Prints the next SDK version (patch by default) without writing. Used by CI and by `make sdk-all` when `VERSION` is not set.

**Usage:**

```bash
./scripts/next-sdk-version.sh [major|minor|patch] [baseVersion]
```

Default is `patch`. Omit `baseVersion` to use `.speakeasy/sdk-version.json`; CI passes base from `npm view flexprice-ts version`.

### 4. Sync SDK version to gen.yaml

Writes the given version into `.speakeasy/gen/*.yaml` and `.speakeasy/sdk-version.json` (central config). Run before generate (Makefile does this in `sdk-all`).

**Usage:**

```bash
./scripts/sync-sdk-version-to-gen.sh <VERSION>
```

### 5. Sync gen to output (pre-generate)

Copies `.speakeasy/gen/<lang>.yaml` to `api/<lang>/.speakeasy/gen.yaml` so the Speakeasy CLI finds config. Run automatically before `speakeasy run` in `make speakeasy-generate`.

**Usage:**

```bash
./scripts/sync-gen-to-output.sh
```

### 6. Other Scripts

- `seed-events`: Seed events data into Clickhouse
- `generate-apikey`: Generate a new API key
- `assign-tenant`: Assign tenant to user
- `onboard-tenant`: Onboard a new tenant
- `migrate-subscription-line-items`: Migrate subscription line items
- `import-pricing`: Import pricing data
- `reprocess-events`: Reprocess events

## General Usage

1. List all available commands:

```bash
go run scripts/main.go -list
```

2. Run a specific command:

```bash
go run scripts/main.go -cmd <command-name> [flags...]
```

## Environment Variables

Scripts typically require these environment variables (set via command flags):

- `TENANT_ID`: The tenant identifier
- `ENVIRONMENT_ID`: The environment identifier
- `PLAN_ID`: The plan identifier (for plan-related scripts)

## How scripts are structured

- **Entrypoint:** [`scripts/main.go`](main.go) defines a `commands` slice (`Name`, `Description`, `Run func() error`). Use `-list` to print commands and `-cmd <name>` to run one.
- **Flags → env:** Shared flags (`-tenant-id`, `-environment-id`, `-file-path`, `-dry-run`, `-worker-count`, etc.) are parsed in `main.go` and copied into `os.Setenv` so the implementation reads **`os.Getenv`** only. Command-specific flags can be added the same way (see `-effective-date`, `-failed-output`, and `-success-output` for calendar billing migration).
- **Implementations:** Live under [`scripts/internal/`](internal/) as `func MyScript() error` (or a thin wrapper that builds params and calls a private `run`). One file per concern (e.g. [`assign_plan.go`](internal/assign_plan.go), [`migrate_billing_cycle.go`](internal/migrate_billing_cycle.go)) is typical.
- **Dependencies:** Scripts load `config.NewConfig()` and construct `postgres.NewEntClients` → `postgres.NewClient`, optional ClickHouse, in-memory `cache`, then `entRepo.New*` repositories and a **`service.ServiceParams`** (often partial; heavier scripts fill more fields). Context must carry tenancy: `context.WithValue(ctx, types.CtxTenantID, …)` and `types.CtxEnvironmentID`.
- **Side effects:** Prefer [`mockWebhookPublisher`](internal/csv_feature_processor.go) for scripts that must not emit webhooks. Some scripts use real webhook/memory pubsub when needed (e.g. onboarding).
- **Local-only:** [`scripts/local/`](local/) holds scripts that are not registered in `main.go` or are environment-specific.

### migrate-calendar-billing-csv

Calls the **Flexprice HTTP API** (not local DB): for each subscription ID in a CSV, schedules cancellation (`scheduled_date`, proration none) then creates a new monthly calendar-billing subscription. **Cancel and create are separate requests** — if create fails after cancel succeeds, you can be left in a partial state. The public cancel API does **not** expose webhook suppression; expect normal webhook behavior on cancel.

**Auth and base URL**

- **API key** (required): `-api-key` → `SCRIPT_FLEXPRICE_API_KEY`, or set `FLEXPRICE_API_KEY`. Sent as header `x-api-key`.
- **Base URL** (optional): `-api-base-url` → `API_BASE_URL`. Default is `https://api.cloud.flexprice.io/v1` (must include `/v1`).
- **Environment** (optional): `-environment-id` → `ENVIRONMENT_ID`, sent as `X-Environment-ID` when your API key does not pin an environment.

Tenant and environment come from the **API key** (and optional `X-Environment-ID`); the CSV is **subscription IDs only** for that scope.

**CSV format**

- **Header row:** first line contains `id` or `subscription_id`; that column is read for each data row.
- **No header:** one column per row (first column is the subscription ID).

**Usage:**

```bash
go run scripts/main.go -cmd migrate-calendar-billing-csv \
  -api-key "<api_key>" \
  -file-path "/path/to/subs.csv" \
  -effective-date "2026-04-01" \
  -success-output "/path/to/successful.csv" \
  -failed-output "/path/to/failed.csv" \
  -dry-run false \
  -worker-count 3
```

Successful rows (validated or migrated) are appended to **`successful_calendar_billing_migration.csv`** by default, or to `-success-output` / `SUCCESS_OUTPUT_PATH`. Columns: `original_subscription_id`, `customer_id`, `plan_id`, `currency`, `effective_date`, `new_subscription_id`, `mode` (`dry_run` or `migrated`).

Optional: `-api-base-url "http://localhost:8080/v1"` for local API. If `-worker-count` is omitted, this command defaults to **3** workers (other scripts are unchanged and use their own defaults).

`EFFECTIVE_DATE` must be **in the future** (required by subscription cancel validation).

**Environment variables (alternative to flags):** `SCRIPT_FLEXPRICE_API_KEY` or `FLEXPRICE_API_KEY`, `API_BASE_URL`, `FILE_PATH`, `EFFECTIVE_DATE`, `ENVIRONMENT_ID`, `SUCCESS_OUTPUT_PATH`, `FAILED_OUTPUT_PATH`, `DRY_RUN`, `WORKER_COUNT`.

## Development

When adding new scripts:

1. Create the script function in `scripts/internal/` (and optional helpers in the same package).
2. Add the command to the `commands` slice in `scripts/main.go`; add any new flags there and map them to env vars if the script reads `os.Getenv`.
3. Update this README with usage instructions and, if non-obvious, a short note under **How scripts are structured**.
