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

## Development

When adding new scripts:

1. Create the script function in `scripts/internal/`
2. Add the command to the `commands` slice in `scripts/main.go`
3. Update this README with usage instructions
