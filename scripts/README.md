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
- **Flags → env:** Shared flags (`-tenant-id`, `-environment-id`, `-file-path`, `-dry-run`, `-worker-count`, etc.) are parsed in `main.go` and copied into `os.Setenv` so the implementation reads **`os.Getenv`** only. Command-specific flags can be added the same way (see `-effective-date` / `-failed-output` for calendar billing migration).
- **Implementations:** Live under [`scripts/internal/`](internal/) as `func MyScript() error` (or a thin wrapper that builds params and calls a private `run`). One file per concern (e.g. [`assign_plan.go`](internal/assign_plan.go), [`migrate_billing_cycle.go`](internal/migrate_billing_cycle.go)) is typical.
- **Dependencies:** Scripts load `config.NewConfig()` and construct `postgres.NewEntClients` → `postgres.NewClient`, optional ClickHouse, in-memory `cache`, then `entRepo.New*` repositories and a **`service.ServiceParams`** (often partial; heavier scripts fill more fields). Context must carry tenancy: `context.WithValue(ctx, types.CtxTenantID, …)` and `types.CtxEnvironmentID`.
- **Side effects:** Prefer [`mockWebhookPublisher`](internal/csv_feature_processor.go) for scripts that must not emit webhooks. Some scripts use real webhook/memory pubsub when needed (e.g. onboarding).
- **Local-only:** [`scripts/local/`](local/) holds scripts that are not registered in `main.go` or are environment-specific.

### migrate-calendar-billing-csv

Schedules cancellation (`scheduled_date`, proration none, webhooks suppressed) and creates a new monthly calendar-billing subscription in **one database transaction per CSV row**.

**CSV format (two modes):**

1. **Header row (default):** First line lists column names (Ent / DB names). **Required:** `id` (alias `subscription_id`), `customer_id`, `plan_id`, `currency`. Optional `tenant_id` / `environment_id` must match flags when set.

2. **Positional (`-positional-csv`):** No header; columns follow `public.subscriptions` export order (0-based): `id`(0), `lookup_key`(1), `customer_id`(2), `plan_id`(3), `subscription_status`(4), `status`(5), `currency`(6), then `billing_anchor` through `billing_period_count`(7–20), `tenant_id`(21), `created_at`…`metadata`(22–27), `environment_id`(28), `pause_status` through `payment_terms`(29–43). Use **`-positional-skip-header`** if line 1 is still a header. Use **`-csv-delimiter tab`** for TSV.

**Usage:**

```bash
go run scripts/main.go -cmd migrate-calendar-billing-csv \
  -tenant-id "<tenant_id>" \
  -environment-id "<environment_id>" \
  -file-path "/path/to/subs.csv" \
  -effective-date "2026-04-01" \
  -failed-output "/path/to/failed.csv" \
  -dry-run false \
  -worker-count 5
```

Positional TSV (no header row):

```bash
go run scripts/main.go -cmd migrate-calendar-billing-csv \
  -tenant-id "<tenant_id>" -environment-id "<environment_id>" \
  -file-path "/path/to/subs.tsv" -effective-date "2026-04-01" \
  -positional-csv -csv-delimiter tab
```

`EFFECTIVE_DATE` must be **in the future** (required by subscription cancel validation). Optional: `-user-id` sets audit context (`USER_ID`).

## Development

When adding new scripts:

1. Create the script function in `scripts/internal/` (and optional helpers in the same package).
2. Add the command to the `commands` slice in `scripts/main.go`; add any new flags there and map them to env vars if the script reads `os.Getenv`.
3. Update this README with usage instructions and, if non-obvious, a short note under **How scripts are structured**.
