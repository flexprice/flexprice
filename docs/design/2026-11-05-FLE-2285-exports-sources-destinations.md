# Exports: more sources, more destinations — backend design

Status: **Proposed** (design only; no code in this PR)
Date: 2026-11-05
Related: FLE-2285 (P4), parent FLE-1579 Financial Reporting, FLE-1612 (P5 export scope)
Scope: backend only. Portal work is paused until the API is live on staging.

## Intent

An "export" in Flexprice is already a row in `scheduled_tasks` (the portal lists, creates, pauses, runs and deletes them through `/tasks/scheduled`). Its limits: fixed entity types, a mandatory S3 connection, intervals of 15 min to daily, one destination.

This change evolves that model, not a parallel one:

- new **source**: saved reports (`analytics_view`);
- new **destinations**: a console copy of every run, plus email, alongside the existing S3 connection;
- weekly, monthly and on-demand-only exports at a chosen time and timezone.

Existing exports keep working unchanged.

## Goals

1. Export a saved report (`analytics_view`) to CSV, on demand or on a schedule.
2. Every run of a new-style export keeps a **managed copy** that the console downloads.
3. Optional destinations: **S3 connection** (existing) and **email** (new). Email is a notification only: a deeplink to the run in the console, never a file, data or download link.
4. Weekly, monthly and manual (on-demand only) exports with time and timezone.
5. Per-destination delivery results on every run; a failed destination never hides the file.
6. Zero behaviour change for existing exports and for clients that do not opt in.

## Non-goals (v1)

- XLSX, PDF, dashboard exports (FLE-1612 items 3-4 stay deferred).
- Converting existing exports to the new style, or editing them beyond `enabled`.
- Raw SQL or operator-authored report templates (see "Not covered").
- Per-recipient authorization, recipient accounts, unsubscribe.
- 15- and 30-minute schedules for new-style exports.
- Exporting unsaved ad-hoc queries (save the report first).
- Timezone-aware report aggregation (report data is bucketed in UTC).

## Anti-goals

- Nothing that lets one tenant or environment read another's data.
- No file, data or download link in an email; only a console deeplink.
- No silent failures: every destination outcome is visible on the run.
- No change to what existing exports write, where, or when.

## What exists today (verified in code)

- **Export definition**: `ent/schema/scheduledtask.go`. `connection_id`, `entity_type`, `interval` are `NotEmpty`; `job_config` (S3 job config) optional; `enabled`; `temporal_schedule_id`. Entity types (8): events, invoice, credit_topups, wallet_debits, wallet_ledger, credit_usage, usage_analytics, revenue_facts. `Interval.Validate()` accepts `15MIN, 30MIN, custom, hourly, daily`; `CreateScheduledTaskRequest.Validate` rejects `custom`; the API docs say hourly, daily. `CalculateIntervalBoundaries` handles 15MIN, but `calculateTimeRange` has no 15MIN or 30MIN case, so those legacy exports cover a 24-hour window ending on an interval boundary.
- **API** (`/tasks/scheduled`, `EntityTask` RBAC): create requires `connection_id`, `entity_type`, `interval`, `job_config`. `UpdateScheduledTaskRequest.Enabled` is required and `UpdateScheduledTask` only toggles it (pause or resume the Temporal schedule). `DeleteScheduledTask` sets `Status=StatusDeleted` and removes the Temporal schedule; its guards check only `StatusArchived`, and `ScheduledTaskRepository.Get` and `List` do not filter on status, so new code must check status explicitly.
- **Repository `Update`**: always sets `connection_id`, entity type, interval, enabled, status; sets `job_config` and `temporal_schedule_id` only when non-empty (cannot clear them); has no support for new columns.
- **Run now**: `POST /:id/run` (`triggerForceRun`) always computes and passes a custom start and end to the workflow (the current interval when the body is empty, which ends in the future), rejects `customer_external_ids` unless the export is wallet_ledger, and uses the Temporal workflow id as the task id. A disabled export makes the workflow return success without creating a `Task`.
- **Resume**: `resumeScheduledTask` re-creates the Temporal schedule through `startScheduledTask` whenever none is found, and `startScheduledTask` falls back to `getCronExpression`'s daily 00:15 default.
- **Schedule**: one Temporal schedule per export; cron hard-coded UTC at :15 (`getCronExpression`, so late-arriving usage is included); window = the previous completed interval from the workflow execution time. Schedule action args (including `UserID` = creator) are fixed at create time.
- **Workflow** `ExecuteExportWorkflow` (60-minute workflow timeouts, which cap the 6-hour `ExportData` activity timeout; no schedule overlap policy is set, so the server default applies) is keyed by `ScheduledTaskID`: details activity, `CreateTask` (a plain insert with a pre-generated id, so not idempotent on retry), mark processing, `ExportData`, complete. A failed `CompleteTask` is ignored, which can leave a task in PROCESSING.
- **Run record**: `Task`, `task_type=EXPORT`, `scheduled_task_id`, `entity_type` (plain string, no validation), `file_url`, `task_status` (closed enum `PENDING, PROCESSING, COMPLETED, FAILED`), `metadata`. `taskRepo.Update` overwrites the whole `metadata` map. `POST /tasks` only creates IMPORT/EVENTS tasks and `PUT /:id/status` changes only the status, so API clients cannot write EXPORT task metadata; the download branch still validates the stored location as defense in depth. The portal lists runs with `GET /tasks?scheduled_task_id=…&task_type=EXPORT`, downloads with `GET /tasks/:id/download`. Scheduled-run workflow ids are `<scheduleID>-<timestamp>` and so contain `:`.
- **Workflow tracking table**: `workflow_executions`, written by `interceptor/workflow_tracking_interceptor.go` at the start and end of every workflow not in `WorkflowTypesExcludedFromTracking`. `ExecuteExportWorkflow` is tracked. The interceptor reads the first workflow argument by reflection: `TenantID`, `EnvironmentID` or `EnvID`, `UserID`, `ScheduledTaskID` (entity `scheduled_task`). No `TenantID` means tracking is skipped silently.
- **Connections table**: `connections` holds `provider_type`, `encrypted_secret_data` and `sync_config` JSON whose `s3` tag is a `StorageExportConfig` (bucket, region, key prefix, compression, encryption, `IsFlexpriceManaged`). `CreateScheduledTask` fetches the connection; managed means it fills `job_config` from platform config (`ValidateForFlexpriceManaged`), custom S3 means it validates the request's `job_config` with the provider forced from the connection; custom GCS is rejected. `DeleteConnection` deletes every export that references the connection. Managed connections write under `{tenant}/{env}` in the same Flexprice bucket the managed copy will use.
- **Exporters** (`internal/ee/service/sync/export`): `Exporter.PrepareData(ctx, ExportRequest) → (csv []byte, count, error)`; generate only. `StreamingExporter` adds `WriteData` (wallet_ledger). `ExportRequest.JobConfig` is read only for `GetExportMetadataFields()` (credit_usage, usage_analytics); `CustomerExternalIDs` is read by wallet_ledger. Events enforces its own cap (500,000 rows).
- **Upload**: `ExportService.executeExport` requires `ConnectionID` and `JobConfig`, accepts S3 or GCS, resolves the store, and `uploadToStorage` calls `store.Upload(UploadRequest{Key, Data, Format: CSV, Compress: gzip})`. `exportObjectKey` is `{prefix}/{entity}/{entity}-{start}-{end}[-customers-{digest}].csv[.gz]`: deterministic per window, not per run, so two runs of the same window overwrite each other.
- **Download**: `GenerateDownloadURL` requires a scheduled task and connection, mints a 30-minute presigned URL, and has a managed-connection branch that rejects a bucket different from the configured one. Legacy downloads break if the connection or export row is gone.
- **Storage resolver**: `ForPlatform(PurposeExport)` (Flexprice export bucket), `ForConnectionExport(connID, jobConfig)` (customer store); both backends implement `PresignGet`.
- **Reports**: `analyticsService.ExecuteView` runs usage and billing views only; report-family views run through `RunReport(key, ReportQueryRequest)` with the saved settings. `ExecuteView` ignores view `Variables`; a required variable with no default fails resolution. Report presets are `last_7_days, last_30_days, last_90_days, this_month, last_month, this_quarter, this_year`, plus absolute `YYYY-MM-DD..YYYY-MM-DD` (inclusive start, exclusive end; the report family also requires the end after the start). Relative tokens resolve against `clock.Now()` as rolling windows (`last_N_days` = now-N×24h, `today` and `this_*` run to now). Usage-family results are cut silently by the view's own `Limit`; report-family has a `MaxRows` check. `requireReports()` gates billing and report families per deployment; there is no per-tenant flag. Saved views are readable only while published, and there is no version pinning. The dataset family is not in this checkout.
- **Email**: the `Email` service wrapper logs the recipient address at Info and Error and sends text only (`html=""`) and returns `Success:false` with no error when email is disabled; the Resend call has no idempotency key.
- **Users**: `GetByEmail` scopes by the tenant id carried in `ctx`, so callers must set it first.
- **Activity log**: neither `ScheduledTask` nor `Task` is registered in `internal/activity/entities.go`, so no activity-log work is needed for them.
- **Console URL config**: no general tenant-facing console base URL exists (the `dashboard_url` entries are the SAML callback and Flexprice's own self-billing).

## Design

### 1. Data model (additive to `scheduled_tasks`)

Column changes (`make generate-ent`, `make generate-migration`; no backfill):

| Change | Detail |
|---|---|
| `connection_id` | drop `NotEmpty`; optional, default `""` |
| `interval` | allowlist gains `weekly`, `monthly`, `manual` (no Temporal schedule; run only through `/run`) |
| `entity_type` | allowlist gains `analytics_view` |
| `name` | nullable string |
| `managed_copy` | bool, default `false`. `true` marks a **new-style** export |
| `source_config` | nullable jsonb `{view_id?, metadata_fields?, customer_external_ids?}` |
| `period` | nullable jsonb `{range?}`; allowed for `manual` `analytics_view` exports only |
| `schedule` | nullable jsonb `{hour, minute, day_of_week?, day_of_month? (1-28), timezone (IANA)}`; frequency is the `interval` column |
| `email_config` | nullable jsonb `{recipients: []}` |

Destinations are derived: console copy = `managed_copy`; S3 = `connection_id != ""` plus `job_config`; email = `email_config`. The API presents them as a `destinations` object.

**New-style** means the create request included `destinations`.

- Legacy flat request (`connection_id` + `job_config`, no `destinations`): exactly today's behaviour, `managed_copy=false`. `weekly`, `monthly`, `manual`, `analytics_view` and email are rejected on this path.
- New-style `destinations`: at most one `s3` (`connection_id` + `job_config`) and one `email` (`recipients`); console copy implicit; S3 optional.
  - `s3` goes through the same connection checks as today's create, extracted into one shared helper. A **Flexprice-managed connection is rejected** as an `s3` destination (the managed copy already covers it and would duplicate the object in the same bucket).
- `analytics_view`: new-style only; requires `source_config.view_id` (exists, published, in tenant and environment); the view must resolve with no supplied variables (`ResolveVariables(def, nil)` succeeds, else rejected); `period.range` is allowed only for `manual` exports, as an absolute `YYYY-MM-DD..YYYY-MM-DD` range or empty (the view's own range); scheduled exports always use the previous UTC period of their frequency (section 2); `interval` in `daily|weekly|monthly|manual`; guarded by `requireReports()` and an explicit service-layer check of `EntityAnalytics` read on create, update and run.
- Existing sources in new-style: no `period`; hourly, daily, weekly, monthly, manual allowed.
- `schedule` required unless `interval=manual`; IANA timezone. Email destinations are rejected on `hourly` exports. There is no minimum fire time: windows are computed 15 minutes before the fire instant (section 2).
- `metadata_fields` live in `source_config` for new-style exports; legacy rows keep them in `job_config`.
- Every new-style write path (update, run, resume, start schedule) rejects rows with `Status` `deleted` or `archived`, because the repository does not filter on status.
- Repository: add explicit methods for the new columns that can also clear `job_config`, `temporal_schedule_id` and `email_config` (the existing `Update` cannot), and `DetachConnection(connectionID)` used by connection deletion (below).
- Logging never prints recipient addresses; the `Email` service wrapper, which currently logs `to`, is changed to log a masked address.

Runs stay `Task` rows (`task_type=EXPORT`, `scheduled_task_id` set). New-style runs add to `Task.metadata`: `window{from,to}`, `view_id`, `view_updated_at`, `storage{provider,bucket,key}`, `deliveries[{type, target (masked), status, detail, at}]`. No new table, no new task type, no definition snapshot (recipients stay out of the task list). Recipients in `deliveries` are masked (`a***@domain`).

Task status: no new values. A new-style run is `COMPLETED` once the managed copy exists, whatever the destinations did; `FAILED` means no file was produced.

### 2. Windows

The window is computed in the `details` step from the **scheduled start time minus 15 minutes** (an ingestion buffer, replacing the legacy cron's :15; the fire time itself is the user's choice), never from wall clock, so catch-up runs after downtime get the correct historical window. It is stored on the `Task` on the first attempt; retries reuse it.

- **Existing sources**: the previous period at the export's frequency in the **schedule's timezone** (previous local day, previous local Monday-to-Monday week, previous local calendar month; hourly = previous hour), converted to exact UTC instants. Weekly and monthly windows do not depend on the weekday or day chosen. `manual` exports use the previous UTC day unless `/run` supplies a window.
- **`analytics_view`, scheduled**: the previous **UTC** period at the export's frequency (UTC day, Monday-to-Monday week, calendar month), counted back from the buffered instant, applied as an absolute date-only range `YYYY-MM-DD..YYYY-MM-DD` (inclusive start, exclusive end). Report data is bucketed in UTC days, so this guarantees the UTC period has closed for any timezone. Example: a daily 00:30 Asia/Kolkata export fires at 19:00 UTC and covers the UTC day before the one still in progress. The UI must say these windows are UTC.
- **`analytics_view`, `manual`**: `/run` uses `period.range` if set, else the previous UTC day. A custom `start_time`+`end_time` must be whole UTC days.
- **`/run` on a new-style export** never passes an automatic window as a custom one: `triggerForceRun` is changed so only a user-supplied window reaches the workflow. An empty body means the period the schedule would export now by the rules above. `customer_external_ids` on `/run` overrides `source_config` and is allowed for wallet_ledger only.

### 3. Run flow

`ExecuteExportWorkflow` is extended, not replaced, and the new branch uses `workflow.GetVersion` so in-flight legacy runs replay identically. Keep the input field names `TenantID`, `EnvID`, `UserID`, `ScheduledTaskID` exactly (the tracking interceptor reads them). New activities are registered with the export activities in `internal/temporal/registration.go`.

1. **Details**: load the row (status published, not deleted or archived; disabled exports are refused with a clear error on `/run` instead of a silent no-op); compute the window; carry the export definition into activity inputs so in-flight runs finish with their original config even if the export is edited.
2. **Create task** (idempotent: tolerate an existing task with the same id) and mark processing.
3. **Branch on `managed_copy`**:
   - `false`: today's `ExportData`, unchanged.
   - `true`: `GenerateExport`: produce CSV; write plain CSV to `ForPlatform(PurposeExport)` at `exports/{tenant}/{env}/{scheduled_task_id}/{sanitized_task_id}.csv` (task id with characters outside `A-Za-z0-9_-` replaced by `-`); set `file_url`, size, count, `storage`, status `COMPLETED` in the same activity (retried until it succeeds; a failed completion never leaves the task in PROCESSING). Then `DeliverS3` and `SendEmail` run in parallel; each **returns** its result and does not write the task. A single `RecordDeliveries` activity then writes `deliveries` once (no concurrent metadata writers).
4. **Generate**:
   - existing sources: `PrepareData` with an `ExportRequest` built from the export (minimal `JobConfig` carrying only `metadata_fields`, plus `CustomerExternalIDs`). `StreamingExporter` sources use `PrepareData` in v1 and buffer in memory as today.
   - `analytics_view`: new adapter. Load the published view; copy its definition and apply the absolute range; usage and billing run through `ExecuteView(def, nil)`, report family builds a `ReportQueryRequest` from the saved settings and calls `RunReport`. Adapter-side row cap: fail the run when the result exceeds `ReportsRepo.MaxRows()` (report family) or 1,000,000 rows (usage and billing), never truncating silently. A deleted or unpublished view fails the run with a clear reason (views are not version-pinned; the run records `view_updated_at`).
5. **DeliverS3**: read the managed copy and call `store.Upload(UploadRequest{Key, Data, Format: CSV, Compress: gzip})` through `ForConnectionExport`. New-style key: `{key_prefix}/{entity}/{entity}-{start}-{end}-{task_id_short}.csv[.gz]` (a new key function; the legacy function is unchanged), so two runs never overwrite each other and a retry overwrites only its own object.
6. **SendEmail**: one plain-text email per recipient with the console deeplink (section 4). Re-validate recipients first. The activity checks `resp.Success`; at-least-once delivery, with a Resend idempotency key `exports-{task_id}-{hash(recipient)}` where the client supports it (add support to the wrapper). A crash between send and record can duplicate one email; accepted. Abuse limits: a per-tenant cap on emails per hour (`exports.max_emails_per_hour`, run is still `COMPLETED` and the delivery is recorded as `rate_limited`), no email on hourly exports, and the export name is stripped of control characters, limited to 120 characters and kept out of the subject line.

Limits: query timeout 5 minutes, activity retries 3, validation errors non-retryable. Existing exporters keep their own caps.

### 4. Scheduling

- New-style with `schedule`: Temporal `CronExpressions` plus `TimeZoneName`. Daily `M H * * *`, weekly `M H * * D`, monthly `M H DOM * *`, hourly `M * * * *`. `manual` creates no schedule; no code path may create one for it (`startScheduledTask` and `resumeScheduledTask` branch on interval before touching `getCronExpression`).
- Rows without `schedule` use `getCronExpression` exactly as today.
- Set `Overlap` and `CatchupWindow` explicitly on new-style schedules (buffer overlapping runs; a bounded catch-up window; the implementer confirms the Temporal semantics) so missed runs after downtime run instead of being dropped; each catch-up run derives its window from its own scheduled start.
- DST: a local time inside a spring-forward gap may fire shifted or be skipped by the schedule engine; windows come from the scheduled start, so a skipped fire loses a run but never mis-windows data. Documented limitation.
- Updating a new-style schedule: update the Temporal schedule first (spec, and action args if `UserID` should change), preserving paused state and overlap policy, then write the database row; on database failure revert the Temporal schedule. The details step reads the definition from the database at fire time, so there is no state in the workflow input to go stale.

### 5. Email and download

- Email: export name, window, row count, status, and one deeplink. Plain text in v1. No data, no file, no download link.
- Deeplink: `{exports.console_base_url}/exports/{scheduled_task_id}/runs/{url_escaped_task_id}`. The recipient signs in; the console applies its normal access checks and downloads through `GET /tasks/:id/download`. The path is a contract with the UI work. New config `exports.console_base_url`; creating or updating an email destination fails fast if it is empty or `email.enabled=false`.
- `GenerateDownloadURL` branch: if `Task.metadata.storage` is present, require `task_type=EXPORT`, require the recorded key to start with `exports/{ctx tenant}/{ctx env}/`, and the recorded bucket to equal the current platform export bucket, then resolve `ForPlatform(PurposeExport)` and presign for 30 minutes. As defense in depth, the stored location is validated against the caller's tenant and environment before use. For `analytics_view` runs the service also requires `EntityAnalytics` read, so task read alone cannot download report data. Tasks without `storage` take the existing path unchanged.
- Retention target 90 days on the `exports/` prefix. That lifecycle rule is infra, owned outside this repo; until it is confirmed the app does not promise 90 days. Download past expiry returns "file expired"; `Task` rows stay.
- Redeliver is deferred; in v1 a failed destination is retried by running the export again.

### 6. API

Reuse `/tasks/scheduled`; extend its DTOs.

- `POST /tasks/scheduled`: legacy flat shape or new shape (`name`, `entity_type`, `source_config`, `period`, `interval`, `schedule`, `destinations{s3?, email?}`, `enabled`).
- `GET /tasks/scheduled`: new query param `style=legacy|managed|all`, **default `legacy`**, so existing portal, SDK and MCP clients never receive new-style rows (new enum values, empty `connection_id`) until they opt in with `all`. Responses for legacy rows are unchanged; new fields are omitted when null. `GET /tasks/scheduled/:id` and the run list return new-style rows by id as before. Enum fields in the OpenAPI spec are declared open so strict generated SDKs tolerate the new values; a generated-SDK decode test covers it.
- `PUT /tasks/scheduled/:id`: legacy rows accept `enabled` only and still require it (400 otherwise). New-style rows accept `name`, `source_config`, `period`, `schedule`, `destinations`, `enabled` (optional), through the new repository methods, with status checks. In-flight runs are unaffected.
- `DELETE`: unchanged for legacy. For new-style it sets `StatusDeleted` and removes the Temporal schedule; runs and managed copies stay downloadable.
- `POST /tasks/scheduled/:id/run`: same request; new-style behaviour per section 2; the response's `workflow_id` is the task id.
- Redeliver is deferred (see section 5).
- `GET /tasks?scheduled_task_id=…&task_type=EXPORT`: unchanged; items carry `metadata.deliveries`.
- Connection deletion: for legacy exports unchanged. For new-style exports `DeleteConnection` calls `DetachConnection` (clears `connection_id` and `job_config`, keeps the export with console and email) instead of deleting them.
- RBAC: `EntityTask` as today (route middleware cannot vary per source), plus the explicit service check for `EntityAnalytics` read on `analytics_view`. Scheduled runs execute as the creator's recorded user id and cannot re-check permissions; a user who loses analytics access keeps their schedule until it is edited or deleted. Known trade-off: anyone with task write can email a notification about any source to any tenant user, Downloading `analytics_view` runs also requires `EntityAnalytics` read; other sources rely on task read, and per-source checks are a follow-up.
- Run `make swagger` and regenerate SDKs; the enum values and fields change the published API.

### 7. Recipients

- Validate syntax, dedupe, max 10 per export, at write time and again at send.
- Allowed: an address matching a tenant user via `GetByEmail`. Every activity and service path that calls it sets `tenant_id` on `ctx` first; add a test for it. Because the email only points into the console, no domain allow-list. Whether a user may open a given environment is enforced by the console.
- If `email.enabled=false`, creating or updating an email destination fails fast.

### 8. Cross-cutting rules (AGENTS.md)

- Every read and write filters `tenant_id` and `environment_id`; activities set both on `ctx` from workflow input.
- Logging via `internal/logger`, ctx first, literal `"error"` key on every `Error` call (LL006); never log recipient addresses. `clock.Now()` outside workflows, `workflow.Now()` inside.
- Handlers parse and delegate; services in `internal/ee/service/`; new deps in `cmd/server/main.go`.
- Comments under seven words.

## Testing

- **Regression first**: existing scheduled-task create, validate, schedule and export tests stay green and unchanged; a legacy-shape create yields `managed_copy=false` and the old workflow path; legacy list default hides new-style rows.
- Table-driven unit tests: new-shape validation matrix, schedule to cron and timezone including a DST zone and an IST monthly case, window math (daily, weekly, monthly, catch-up from a scheduled start in the past), the 00:15 rule, preset to absolute range, recipient validation, sanitized key and deeplink escaping, download key-prefix check (a `metadata.storage` outside the caller's tenant and environment is rejected).
- Workflow tests (Temporal test env): managed-copy happy path with S3 and email; email fails while S3 succeeds and vice versa; retry does not double-write or overwrite another run's object; `manual` export via `/run`; disabled export refused; deleted export refused; `RecordDeliveries` writes once; in-flight legacy replay with `GetVersion`.
- Repository tests against a real DB for the new columns, optional `connection_id`, clearing fields, `DetachConnection`. No mocked Ent client. In-memory test stores get the new fields.
- Windows complete at fire time: IST and America/Los_Angeles schedules for `analytics_view` assert the UTC period has closed; existing-source windows match local boundaries including a DST change.
- Temporal test env: catch-up after an outage with the chosen overlap and catch-up settings.
- Mixed-version safety: creation is refused while `exports.managed_enabled` is off.
- Download denied without `EntityAnalytics` read for `analytics_view` runs.
- Generated Go SDK decodes `GET /tasks/scheduled/:id` for a new-style row.
- Captured logs never contain a recipient address; activities that call `GetByEmail` refuse to run with an empty tenant id.
- Staging check: create an export from a saved view with email and an S3 test bucket; a schedule a few minutes ahead; the email arrives with only a deeplink and it is well formed for the configured base URL; console download works; break email and confirm the managed copy still downloads; confirm an untouched legacy export still runs as before.

## Rollout

PR stack: (1) schema, domain, DTOs, validation, repository methods, list `style` filter; (2) windows, managed copy, `analytics_view` adapter, workflow branch, download branch; (3) `DeliverS3`, `SendEmail` with the deeplink and the email wrapper's masked logging, `exports.console_base_url` config, per-tenant email cap, `RecordDeliveries`; (4) update endpoint, connection detach, swagger, SDK regeneration. Creating new-style exports is behind a deployment flag (`exports.managed_enabled`, default off) that is turned on only after the export workers run the new code; otherwise a new-style row could be picked up by old workers (empty connection, daily default window, unregistered activities). Rollout order: workers first, then the flag. Analytics sources are also gated by `requireReports()`. Legacy rows default `managed_copy=false`, so nothing changes for them.

## Not covered

Raw SQL or operator-authored report templates (for example a wallet-balance and usage report that needs balances over time and several source tables). It would be a new source type, `template`, under this same model: a reviewed query with typed parameters, tenant and environment bound server-side, read-only. Decide separately.

## Calls made by me (please check)

1. Evolve `scheduled_tasks`; new-style marked by `managed_copy=true`; legacy rows frozen except `enabled`.
2. Destinations are derived from columns; at most one S3 and one email; a Flexprice-managed connection cannot be an S3 destination.
3. Managed copy is plain CSV; S3 compression applies at upload; new-style S3 keys include the task id.
4. A new-style run is `COMPLETED` once the managed copy exists; delivery results in masked metadata; no new status or task type; no definition snapshot.
5. Extend `ExecuteExportWorkflow` behind `workflow.GetVersion` and a creation flag; no separate redeliver workflow in v1.
6. Email carries only a console deeplink, plain text, at-least-once, per-tenant hourly cap, no email on hourly exports; recipients must be tenant users and are stored as emails.
7. Existing-source windows use the schedule's timezone; scheduled report windows are the previous UTC period (the UI says UTC); both computed 15 minutes before the fire instant.
8. Report exports invent no presets: scheduled ones use the previous UTC period of their frequency, `manual` ones an absolute range or the view's own range.
9. `/run` with an empty body means the period the schedule would export now; only a user-supplied window is passed as a custom window; disabled and deleted exports are refused.
10. `GET /tasks/scheduled` hides new-style rows unless `style=managed|all`; default `legacy`.
11. Views are not version-pinned; a deleted view fails the run; views needing variables are rejected; runs record the view's `updated_at`.
12. Reuse `EntityTask` plus an explicit analytics check on create, update, run and download; scheduled runs cannot re-check permissions.
13. Connection deletion detaches the S3 destination of new-style exports instead of deleting them.
14. Monthly day 1-28; at most one S3 and one email destination.
15. All eight existing sources work in new-style via `PrepareData` (no streaming yet); usage and billing report row cap 500,000 (matches events).
16. Redeliver is deferred.

## Needs an owner outside this repo

- A 90-day lifecycle rule on the `exports/` prefix of the export bucket (S3 and GCS).

## Open for the implementer

- Exact CSV flattening against `dto.AnalyticsQueryResult` (columns, buckets, currency split).
- Shape of the `ReportQueryRequest` built from a saved report-family view.
- Temporal schedule `Overlap` and `CatchupWindow` values that satisfy the catch-up rule.
- How to extend `ExportRequest` (or wrap it) to carry `source_config` and the resolved range to the analytics adapter.
- Whether `GetExportMetadataFields()` is nil-safe on a nil `JobConfig` (the minimal config avoids the question).
- Whether the Resend client supports an idempotency key and how to pass it through the wrapper.
- Final deeplink path, agreed with the UI work.
- Temporal schedule update API details (`ScheduleHandle.Update`, preserving state and overlap policy).
