# Exports: more sources, more destinations — backend design

Status: **Phases 1 and 2 built; later phases proposed**
Date: 2026-11-05
Related: FLE-2285 (P4), parent FLE-1579 Financial Reporting, FLE-1612 (P5 export scope)
Scope: backend only. Portal work is paused until the API is live on staging.

## Intent

An "export" in Flexprice is already a row in `scheduled_tasks` (the portal lists, creates, pauses, runs and deletes them through `/tasks/scheduled`). Its limits: fixed entity types, a mandatory S3 connection, intervals of 15 min to daily, one destination.

This change evolves that model, not a parallel one:

- new **sources** (later phase): predefined reports (`report`) and saved custom views (`analytics_view`), alongside the existing entity types;
- a **one-off export from the customer tab** (later phase), scoped to one customer and listed later in the Exports tab;
- new **destinations**: a console copy of every run and email (later phase), alongside the existing S3 connection; Phase 1 builds the console copy only for exports without a connection;
- weekly, monthly and on-demand-only exports at a chosen time and timezone.

Existing exports keep working unchanged.

Built in Phase 1: weekly, monthly and manual intervals with schedule and timezone, optional connection, and a Flexprice-bucket copy for exports without a connection. Reports, saved views, email, the customer-tab one-off and a console copy of every run are later phases.

## Goals

1. Export an existing entity type (built), a predefined report (`report`) or a saved view (`analytics_view`) (later phases) to CSV, on demand or on a schedule.
7. (Later phase.) One-off, customer-scoped export started from the customer tab; it shows up as a run in the Exports tab.
2. (Phase 1: exports without a connection only; every run of every export is a later phase.) Every run of a new-style export keeps a **managed copy** that the console downloads.
3. (Later phase.) Optional destinations: **S3 connection** (existing) and **email** (new). Email is a notification only: a deeplink to the run in the console, never a file, data or download link.
4. Weekly, monthly and manual (on-demand only) exports with time and timezone.
5. (Later phase.) Per-destination delivery results on every run; a failed destination never hides the file.
6. Zero behaviour change for existing exports and for clients that do not opt in.

## Non-goals (v1)

- XLSX, PDF, dashboard exports (FLE-1612 items 3-4 stay deferred).
- Converting existing exports to the new style. Phase 1 PUT accepts name and schedule.
- Custom SQL and operator-authored templates: parked (see "Not covered").
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

**Phase 1 as built.** Migration (`make generate-ent`, `make generate-migration`; no backfill):

| Change | Detail |
|---|---|
| `connection_id` | optional, default `""` |
| `interval` | allowlist gains `weekly`, `monthly`, `manual` |
| `name` | nullable string, at most 120 characters |
| `schedule` | nullable jsonb `{hour, minute, day_of_week?, day_of_month? (1-28), timezone (IANA)}` |
| `managed_copy` | bool, default `false`; `true` means the export is written to the Flexprice bucket |

Removed from Phase 1 (later phases may still add them, but not in this shape): `source_config`, `period`, `email_config`, `origin`, the `destinations` request wrapper, and `Task.metadata.storage`. The `entity_type` allowlist is unchanged.

- `job_config` keeps meaning "output settings" for every export, with or without a connection.
- With `connection_id`: today's flow and validation; `job_config` is required.
- Without `connection_id`: `managed_copy=true`. The server fills provider, bucket (and region) from the platform export config and sets `key_prefix = exports/{tenant}/{env}/{scheduled_task_id}`. The client may send only `export_metadata_fields` and `compression`; any storage field is rejected. `15MIN` and `30MIN` are rejected without a connection.
- Intervals: `hourly`, `daily`, `weekly`, `monthly`, `manual`. `schedule` is required for weekly and monthly, optional for hourly and daily (absent means today's 00:15 UTC behaviour), and rejected for `manual`.
- `PUT` accepts `enabled`, `name` and `schedule` (at least one). Writes to deleted or archived rows are rejected.
- Runs stay `Task` rows (`task_type=EXPORT`, `scheduled_task_id` set). `tasks.file_url` stays `s3://` or `gs://`; no new task fields.
- Ops note: renaming or migrating the platform export bucket breaks existing Flexprice-bucket rows (non-retryable) until they are migrated.

**Phase 2 as built.**
- P2-R1: `source_config` is a nullable jsonb `{report_key?, report_params?{shape, dimensions, grain, currency, filters[{field, op, value[]}]}, view_id?}`. There is no `period` column; report windows come from the schedule and `/run`.
- `entity_type` gains `report` and `analytics_view`. A `report` export needs `report_key`; an `analytics_view` export needs only `view_id`.
- P2-R3: a report source may write to the Flexprice bucket or to an S3 connection.
- P2-R4: `export_metadata_fields` is rejected for report sources. `source_config` is rejected for every other entity type.
- P2-R5: the source cannot be edited. `PUT` still accepts only `enabled`, `name` and `schedule`.

**Later phases (original design, not built).** The remaining notes in this section describe `source_config`, `period`, `email_config`, `origin` and `destinations`; read them as a design sketch that Phase 2 onward must reconcile with the as-built shape above.

Column changes planned for later phases:

| Change | Detail |
|---|---|
| `entity_type` | allowlist gains `report` and `analytics_view` |
| `origin` | nullable string: `console` (default) or `customer_tab` |
| `source_config` | nullable jsonb `{view_id?, report_key?, report_params?, metadata_fields?, customer_external_ids?}` |
| `period` | nullable jsonb `{range?}`; allowed for `manual` `analytics_view` exports only |
| `email_config` | nullable jsonb `{recipients: []}` |

Destinations are derived: console copy = `managed_copy`; S3 = `connection_id != ""` plus `job_config`; email = `email_config`. The API presents them as a `destinations` object.

**New-style** means the create request included `destinations`.

- Legacy flat request (`connection_id` + `job_config`, no `destinations`): exactly today's behaviour, `managed_copy=false`. `weekly`, `monthly`, `manual`, `analytics_view` and email are rejected on this path. Superseded in Phase 1: weekly, monthly and manual are accepted with or without a connection; `analytics_view` and email remain later phases.
- New-style `destinations`: at most one `s3` (`connection_id` + `job_config`) and one `email` (`recipients`); console copy implicit; S3 optional.
  - `s3` goes through the same connection checks as today's create, extracted into one shared helper. A **Flexprice-managed connection is rejected** as an `s3` destination (the managed copy already covers it and would duplicate the object in the same bucket).
- `analytics_view`: new-style only; requires `source_config.view_id` (exists, published, in tenant and environment); the view must resolve with no supplied variables (`ResolveVariables(def, nil)` succeeds, else rejected); `period.range` is allowed only for `manual` exports, as an absolute `YYYY-MM-DD..YYYY-MM-DD` range or empty (the view's own range); scheduled exports always use the previous UTC period of their frequency (section 2); `interval` in `daily|weekly|monthly|manual`; guarded by `requireReports()` and an explicit service-layer check of `EntityAnalytics` read on create, update and run.
- `report` (predefined, built-in key from `GET /analytics/reports`): new-style only; requires `source_config.report_key` that exists, plus optional `report_params` (the report's own filters, never a time range); same intervals, `period` rule, windows, row cap and `requireReports()` + `EntityAnalytics` checks as `analytics_view`. It runs through `RunReport(key, ReportQueryRequest)` with the resolved UTC range.
- **Customer-tab one-off**: a new-style create with `interval=manual`, `origin=customer_tab`, `run_now=true` and `source_config.customer_external_ids` holding exactly the one customer. The server creates the definition (auto-named, visible in the Exports tab) and starts one run. Allowed sources are those that can scope to a customer: `wallet_ledger` today, `analytics_view` and `report` where the family has an `external_customer_id` filter, and any other entity exporter only after it gains a customer filter; other combinations are rejected with a clear error.
- Existing sources in new-style: no `period`; hourly, daily, weekly, monthly, manual allowed.
- `schedule` required unless `interval=manual`; IANA timezone. Email destinations are rejected on `hourly` exports. There is no minimum fire time: the window is the previous full period before the fire instant (section 2).
- `metadata_fields` live in `source_config` for new-style exports; legacy rows keep them in `job_config`.
- Every new-style write path (update, run, resume, start schedule) rejects rows with `Status` `deleted` or `archived`, because the repository does not filter on status.
- Repository: add explicit methods for the new columns that can also clear `job_config`, `temporal_schedule_id` and `email_config` (the existing `Update` cannot), and `DetachConnection(connectionID)` used by connection deletion (below).
- Logging never prints recipient addresses; the `Email` service wrapper, which currently logs `to`, is changed to log a masked address.

Runs stay `Task` rows (`task_type=EXPORT`, `scheduled_task_id` set). New-style runs add to `Task.metadata`: `window{from,to}`, `view_id`, `view_updated_at`, `storage{provider,bucket,key}`, `deliveries[{type, target (masked), status, detail, at}]`. No new table, no new task type, no definition snapshot (recipients stay out of the task list). Recipients in `deliveries` are masked (`a***@domain`).

Task status: no new values. A new-style run is `COMPLETED` once the managed copy exists, whatever the destinations did; `FAILED` means no file was produced.

### 2. Windows

**Phase 1 as built.** Window = previous period (day, Monday-to-Monday week, calendar month; hourly = previous hour) ending at the latest period boundary at or before the scheduled start, in the schedule's timezone, converted to UTC instants. `manual` exports use the previous UTC day unless `/run` gives a window. Exports without a schedule keep today's windows. The report and view rules below are later phases.

**Phase 2 as built.** P2-R2: report sources allow only `daily`, `weekly`, `monthly` and `manual`, and `schedule` is required unless `manual`. The window is the previous UTC period, counted back from the scheduled start. A `/run` with a custom start and end must use whole UTC days. A manual report run with no `/run` window uses the previous UTC day. A scheduled report window is the last full UTC period before the fire time, so a run early in a local day east of UTC covers the UTC day before yesterday's local date; this is intentional, because the UTC day must be closed. A report's `MaxRangeDays` is enforced at create (daily 1 day, weekly 7, monthly 31; manual is skipped) and on `/run` for a custom window longer than the cap.

The window is computed in the `details` step from the **scheduled start time** (no ingestion buffer; the fire time is the user's choice), never from wall clock, so catch-up runs after downtime get the correct historical window. It is stored on the `Task` on the first attempt; retries reuse it.

- **Existing sources**: the previous period at the export's frequency in the **schedule's timezone** (previous local day, previous local Monday-to-Monday week, previous local calendar month; hourly = previous hour), converted to exact UTC instants. Weekly and monthly windows do not depend on the weekday or day chosen. `manual` exports use the previous UTC day unless `/run` supplies a window.
- **`analytics_view` and `report`, scheduled**: the previous **UTC** period at the export's frequency (UTC day, Monday-to-Monday week, calendar month), counted back from the scheduled start, applied as an absolute date-only range `YYYY-MM-DD..YYYY-MM-DD` (inclusive start, exclusive end). Report data is bucketed in UTC days, so this guarantees the UTC period has closed for any timezone. Example: a daily 00:30 Asia/Kolkata export fires at 19:00 UTC and covers the UTC day before the one still in progress. The UI must say these windows are UTC.
- **`analytics_view` and `report`, `manual`**: `/run` uses `period.range` if set, else the previous UTC day. A custom `start_time`+`end_time` must be whole UTC days.
- **`/run` on a new-style export** never passes an automatic window as a custom one: `triggerForceRun` is changed so only a user-supplied window reaches the workflow. An empty body means the period the schedule would export now by the rules above. `customer_external_ids` on `/run` overrides `source_config` and is allowed for wallet_ledger only.

### 3. Run flow

**Phase 1 as built.** Runs use the same ExportData, upload, CompleteTask path for every export. A Flexprice-bucket run resolves the platform store instead of a connection. There is no `GenerateExport`, `DeliverS3`, `SendEmail` or `RecordDeliveries` yet; the steps below are the later-phase design.

**Phase 2 as built.** P2-R6: a `report` export runs through `RunReport`. The row cap is the reports repository `MaxRows`, 10,000 by default. A saved view runs through `ExecuteView` for usage and billing, and fails above 1,000,000 rows. A report-family view runs through `RunReport`. A run never truncates silently; it fails when over the cap. P2-R7: the view's `updated_at` travels from the export response metadata into the completed task's metadata as `view_updated_at`.

`ExecuteExportWorkflow` is extended, not replaced, and the new branch uses `workflow.GetVersion` so in-flight legacy runs replay identically. Keep the input field names `TenantID`, `EnvID`, `UserID`, `ScheduledTaskID` exactly (the tracking interceptor reads them). New activities are registered with the export activities in `internal/temporal/registration.go`.

1. **Details**: load the row (status published, not deleted or archived; disabled exports are refused with a clear error on `/run` instead of a silent no-op); compute the window; carry the export definition into activity inputs so in-flight runs finish with their original config even if the export is edited.
2. **Create task** (idempotent: tolerate an existing task with the same id) and mark processing.
3. **Branch on `managed_copy`**:
   - `false`: today's `ExportData`, unchanged.
   - `true`: `GenerateExport`: produce CSV; write plain CSV to `ForPlatform(PurposeExport)` at `exports/{tenant}/{env}/{scheduled_task_id}/{sanitized_task_id}.csv` (task id with characters outside `A-Za-z0-9_-` replaced by `-`); set `file_url`, size, count, `storage`, status `COMPLETED` in the same activity (retried until it succeeds; a failed completion never leaves the task in PROCESSING). Then `DeliverS3` and `SendEmail` run in parallel; each **returns** its result and does not write the task. A single `RecordDeliveries` activity then writes `deliveries` once (no concurrent metadata writers).
4. **Generate**:
   - existing sources: `PrepareData` with an `ExportRequest` built from the export (minimal `JobConfig` carrying only `metadata_fields`, plus `CustomerExternalIDs`). `StreamingExporter` sources use `PrepareData` in v1 and buffer in memory as today.
   - `report`: same adapter; builds the `ReportQueryRequest` from `report_key`, `report_params` and the resolved range.
   - `analytics_view`: new adapter. Load the published view; copy its definition and apply the absolute range; usage and billing run through `ExecuteView(def, nil)`, report family builds a `ReportQueryRequest` from the saved settings and calls `RunReport`. Adapter-side row cap: fail the run when the result exceeds `ReportsRepo.MaxRows()` (report family) or 1,000,000 rows (usage and billing), never truncating silently. A deleted or unpublished view fails the run with a clear reason (views are not version-pinned; the run records `view_updated_at`).
5. **DeliverS3**: read the managed copy and call `store.Upload(UploadRequest{Key, Data, Format: CSV, Compress: gzip})` through `ForConnectionExport`. New-style key: `{key_prefix}/{entity}/{entity}-{start}-{end}-{task_id_short}.csv[.gz]` (a new key function; the legacy function is unchanged), so two runs never overwrite each other and a retry overwrites only its own object.
6. **SendEmail**: one plain-text email per recipient with the console deeplink (section 4). Re-validate recipients first. The activity checks `resp.Success`; at-least-once delivery, with a Resend idempotency key `exports-{task_id}-{hash(recipient)}` where the client supports it (add support to the wrapper). A crash between send and record can duplicate one email; accepted. Abuse limits: a per-tenant cap on emails per hour (`exports.max_emails_per_hour`, run is still `COMPLETED` and the delivery is recorded as `rate_limited`), no email on hourly exports, and the export name is stripped of control characters, limited to 120 characters and kept out of the subject line.

Limits: query timeout 5 minutes, activity retries 3, validation errors non-retryable. Existing exporters keep their own caps.

### 4. Scheduling

**Phase 1 as built.** Exports with `schedule` get a Temporal schedule with `TimeZoneName`, overlap policy BUFFER_ALL and a 24h catch-up window; hourly, daily, weekly and monthly build their cron from the schedule, and `manual` creates no schedule. Exports without `schedule` use `getCronExpression` as today.

- New-style with `schedule`: Temporal `CronExpressions` plus `TimeZoneName`. Daily `M H * * *`, weekly `M H * * D`, monthly `M H DOM * *`, hourly `M * * * *`. `manual` creates no schedule; no code path may create one for it (`startScheduledTask` and `resumeScheduledTask` branch on interval before touching `getCronExpression`).
- Rows without `schedule` use `getCronExpression` exactly as today.
- Set `Overlap` and `CatchupWindow` explicitly on new-style schedules (buffer overlapping runs; a bounded catch-up window; the implementer confirms the Temporal semantics) so missed runs after downtime run instead of being dropped; each catch-up run derives its window from its own scheduled start.
- DST: a local time inside a spring-forward gap may fire shifted or be skipped by the schedule engine; windows come from the scheduled start, so a skipped fire loses a run but never mis-windows data. Documented limitation.
- Updating a new-style schedule: update the Temporal schedule first (spec, and action args if `UserID` should change), preserving paused state and overlap policy, then write the database row; on database failure revert the Temporal schedule. The details step reads the definition from the database at fire time, so there is no state in the workflow input to go stale.

### 5. Email and download

**Phase 1 as built.** No email. `GET /tasks/:id/download` presigns on request for Flexprice-bucket runs after checking the bucket equals the platform export bucket and the key starts with `exports/{tenant}/{env}/`; `file_url` is not rewritten. The `Task.metadata.storage` variant below is superseded.

**Phase 2 as built.** Downloading a report-source run also requires analytics read, so task read alone cannot fetch report data.

- Email: export name, window, row count, status, and one deeplink. Plain text in v1. No data, no file, no download link.
- Deeplink: `{exports.console_base_url}/exports/{scheduled_task_id}/runs/{url_escaped_task_id}`. The recipient signs in; the console applies its normal access checks and downloads through `GET /tasks/:id/download`. The path is a contract with the UI work. New config `exports.console_base_url`; creating or updating an email destination fails fast if it is empty or `email.enabled=false`.
- `GenerateDownloadURL` branch: if `Task.metadata.storage` is present, require `task_type=EXPORT`, require the recorded key to start with `exports/{ctx tenant}/{ctx env}/`, and the recorded bucket to equal the current platform export bucket, then resolve `ForPlatform(PurposeExport)` and presign for 30 minutes. As defense in depth, the stored location is validated against the caller's tenant and environment before use. For `analytics_view` runs the service also requires `EntityAnalytics` read, so task read alone cannot download report data. Tasks without `storage` take the existing path unchanged.
- Retention target 90 days on the `exports/` prefix. That lifecycle rule is infra, owned outside this repo; until it is confirmed the app does not promise 90 days. Download past expiry returns "file expired"; `Task` rows stay.
- Redeliver is deferred; in v1 a failed destination is retried by running the export again.

### 6. API

**Phase 1 as built.**
- `POST /tasks/scheduled`: `connection_id` optional, plus `name`, `schedule`, `entity_type`, `interval`, `job_config`, `enabled`. Responses add `name`, `schedule`, `managed_copy`.
- `GET /tasks/scheduled`: returns all exports, no `style` filter and no feature flag. Interval enum is declared open in the OpenAPI overlay so generated SDKs tolerate new values.
- `PUT /tasks/scheduled/:id`: `enabled`, `name`, `schedule`.
- `POST /tasks/scheduled/:id/run`: optional window; with no window on a scheduled or manual export the response carries zero start and end times.
- `DELETE`: unchanged.
- Release order: workers before API.

**Phase 2 as built.**
- `POST /tasks/scheduled` accepts `source_config` and the new entity types, and responses return `source_config`. The entity type enum is open in the OpenAPI overlay.
- P2-R8: report sources need analytics read on create, update, run and download. The check fails closed. Delete, get and list are not gated. Scheduled runs do not re-check the creator's permissions; download stays gated.

**Phase 3 as built.**
- Decisions: a Flexprice copy is made only when the export has no S3 connection (no `DeliverS3`, no managed copy for S3 exports). Deleting a connection still deletes its exports (no `DetachConnection`). Email is sent per run, plain text, deeplink only, to at most 10 tenant users, never for hourly exports, with a per-tenant hourly cap. Email is offered only when configured.
- `email_config {recipients}` column (migration `20261111120000`). Create and update validate recipients (syntax, dedupe, max 10, tenant users only). On `PUT`, an absent `email_config` leaves it unchanged and `[]` clears it.
- `GET /tasks/scheduled/capabilities` returns `{email:{available,max_recipients}}`. `available` is `email.enabled` and a Resend key and `exports.console_base_url` all set.
- Config: `exports.console_base_url` and `exports.max_emails_per_hour` (default 200); env `FLEXPRICE_EXPORTS_CONSOLE_BASE_URL` and `FLEXPRICE_EXPORTS_MAX_EMAILS_PER_HOUR`.
- Workflow: after the run completes or fails, `GetVersion("export-deliveries")` v1 runs `SendExportEmails` (non-hourly, with recipients), then `RecordDeliveries`, which writes `metadata.deliveries[{type: flexprice|s3|gcs|email, target (masked), status: delivered|failed|skipped|rate_limited, detail, at}]`. Failed runs also send email.
- Email cap: Redis counter `exports:email:{tenant}:{yyyymmddhh}`. With Redis disabled there is no cap; a counter error marks the delivery failed. Resend idempotency key is `exports-{task_id}-{sha256(recipient)[:16]}`.
- Deeplink: `{console_base_url}/exports/{scheduled_task_id}/runs/{task_id}?env={environment_id}`.
- Download: a missing object returns 404 "expired or removed".

The shapes below (`destinations`, `source_config`, `style`, `run_now`) are removed from Phase 1 and kept as the later-phase sketch.

Reuse `/tasks/scheduled`; extend its DTOs.

- `POST /tasks/scheduled`: legacy flat shape or new shape (`name`, `entity_type`, `source_config`, `period`, `interval`, `schedule`, `destinations{s3?, email?}`, `enabled`, `origin?`, `run_now?`). With `run_now=true` (manual only) the response carries the started run's `workflow_id`, which is the task id.
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

**Phase 1 checks (built):**
- Existing scheduled-task create, validate, schedule and export tests stay green; a request with `connection_id` and `job_config` behaves as before.
- Table-driven unit tests: request validation (optional connection, storage fields rejected without one, schedule required for weekly and monthly, forbidden for manual), schedule to cron and timezone including a DST zone and an IST monthly case, window math from a scheduled start (daily, weekly, monthly, catch-up, 15-minute offset), key prefix and download bucket and tenant/env prefix checks.
- Repository tests against a real DB for the new columns and optional `connection_id`; no mocked Ent client.
- Update validates `name` and `schedule`; archived and deleted rows are rejected; `/run` without a window returns zero times.
- Staging check (after workers run the new code): create a daily export without a connection, trigger `/run`, download the CSV from the console, and confirm an untouched connection export still runs.

**Later-phase checks (not built):**
- Legacy list `style` default hiding new rows, and generated-SDK decode of new-style rows: dropped; Phase 1 lists all exports.
- Workflow tests for managed copy with S3 and email, partial delivery failure, `RecordDeliveries`, `GetVersion` replay.
- Windows complete at fire time for `analytics_view` in IST and America/Los_Angeles; download denied without `EntityAnalytics` read.
- Captured logs never contain a recipient address; `GetByEmail` activities refuse an empty tenant id.
- Staging check with a saved view, email and an S3 test bucket: email carries only a well-formed deeplink; broken email leaves the copy downloadable.

## Rollout

Four phases. Each merges and ships on its own, and each has an exit check.

**Phase 1: foundation and existing sources (Flexprice-bucket copy).**
- Schema (`name`, `schedule`, `managed_copy`, optional `connection_id`, new intervals), domain, DTOs, validation.
- Schedule windows for existing sources, timezone Temporal schedules, `manual` interval, `/run` zero-window response.
- Flexprice-bucket runs through the existing export path; download presign with bucket and tenant/env prefix checks.
- Update and delete endpoints; swagger and SDK regeneration.
- Exit: an export of an existing entity type with no connection, daily and manual, produces a console-downloadable CSV with correct local-timezone windows; connection exports are untouched.

**Phase 2: report sources (built).**
- `report` and `analytics_view` adapters, UTC windows, row caps, `requireReports()` gating, `EntityAnalytics` checks on create, update, run and download, `view_updated_at` recording.
- Exit: a predefined report and a saved view each export on a schedule and manually; a user without analytics read cannot create or download them.

**Phase 3: destinations.**
- `DeliverS3`, `SendEmail` with the deeplink, `RecordDeliveries`, masked-address logging in the email wrapper, `exports.console_base_url`, per-tenant email cap, connection detach on delete.
- Exit: the staging check in "Testing" (email with deeplink only, S3 object, broken email leaves the managed copy downloadable).

**Phase 4: customer-tab one-off.**
- `origin`, `run_now`, customer scope validation per source, and the customer filter for any entity exporter that lacks one (otherwise it stays unsupported).
- Exit: from the customer tab, a one-off export for one customer appears in the Exports tab and downloads.

**Phase 4 as built (replaces the customer-tab one-off).**
- Decision: the export starts from the console `/customers` list, not from inside a customer. The list's current filter is saved with the export and re-applied on every run, so customers who match later are included.
- Two kinds: the filtered customer list itself (new `entity_type=customers`: id, external id, name, email, status, billing currency and entity, address, timezone, created/updated, one `metadata_<key>` column per key), or a chosen data type limited to those customers.
- Data types that can be limited: `wallet_ledger`, `invoice`, `credit_topups`, `credit_usage`, `usage_analytics`, and `report` sources whose report has a `customer_id` dimension (`invoice_total`, `invoice_total_issued`, `receivables_aging`, `days_to_pay`, `credit_consumption`, `credit_topups`). Others, and saved views, return 400 "This data can't be limited to customers yet."
- Storage: `source_config.customer_filter` holds the customers search body without paging, sort or expand (`filters`, `metadata`, `email`, `billing_entity_id`, `status`). Status follows the list: filters without a status match published and archived customers.
- `origin` column (`console` default, `customers_page`; migration `20261111140000`). `GET /tasks/scheduled?origin=` and `?exclude_origin=`. `customer_filter` is accepted only with `origin=customers_page`.
- `run_now=true` (manual only) creates the export and starts one run; the response carries `run.workflow_id`, which is the run's task id. `run_window` is validated before the export is created. The customer list ignores the window (snapshot) and never runs hourly.
- Limits: a run resolves the filter once (keyset by id). Above 10,000 customers (1,000 for reports) the run fails with "Filter matches more than N customers; narrow it" and nothing is uploaded. No matches gives a header-only file; a report's empty header comes from the same column builder as a real run.
- The customer list export streams in two passes (metadata keys, then rows). Customers created between the passes may carry metadata keys not in the header; those values are left out (snapshot).

Phase 1 has no feature flag. Release workers before the API, so a row created by the new API is never picked up by old workers (an old worker ignores `customer_filter` and exports all customers). Later phases may add a flag if they introduce rows old workers cannot run. Connection exports default `managed_copy=false`, so nothing changes for them.

## Not covered

Custom SQL is parked. If revived: first an operator-authored `template` source (reviewed query, typed parameters, tenant, environment and window bound server-side, read-only); tenant-written SQL only later, behind a read-only ClickHouse user with row policies, a table allowlist and query limits. Neither is designed here.

## Calls made by me (please check)

1. Evolve `scheduled_tasks`. Phase 1: `managed_copy=true` means Flexprice bucket only, not a "new-style" marker; connection exports are unchanged and `PUT` accepts `enabled`, `name`, `schedule`.
2. (Later phase; no `destinations` object in Phase 1.) Destinations are derived from columns; at most one S3 and one email; a Flexprice-managed connection cannot be an S3 destination.
3. (Later phase.) Managed copy is plain CSV; S3 compression applies at upload; new-style S3 keys include the task id.
4. (Later phase.) A new-style run is `COMPLETED` once the managed copy exists; delivery results in masked metadata; no new status or task type; no definition snapshot.
5. (Later phase.) Extend `ExecuteExportWorkflow` behind `workflow.GetVersion` and a creation flag; no separate redeliver workflow in v1.
6. (Later phase.) Email carries only a console deeplink, plain text, at-least-once, per-tenant hourly cap, no email on hourly exports; recipients must be tenant users and are stored as emails.
7. Existing-source windows use the schedule's timezone; scheduled report windows are the previous UTC period (the UI says UTC); both computed from the fire instant.
8. Report exports invent no presets: scheduled ones use the previous UTC period of their frequency, `manual` ones an absolute range or the view's own range.
9. `/run` with an empty body means the period the schedule would export now; only a user-supplied window is passed as a custom window; disabled and deleted exports are refused.
10. ~~`style` filter~~ Removed: `GET /tasks/scheduled` returns all exports, no flag.
11. Views are not version-pinned; a deleted view fails the run; views needing variables are rejected; runs record the view's `updated_at`.
12. Reuse `EntityTask` plus an explicit analytics check on create, update, run and download; scheduled runs cannot re-check permissions.
13. (Later phase.) Connection deletion detaches the S3 destination of new-style exports instead of deleting them.
14. Monthly day 1-28 (built); at most one S3 and one email destination (later phase).
15. (Later phase for the report cap.) Existing sources run through the existing export path; usage and billing report row cap 500,000 (matches events).
16. Redeliver is deferred.
17. Predefined reports are a separate `report` source (built-in reports have no saved-view row); they share the analytics windows and checks.
18. A customer-tab one-off creates a visible, auto-named manual export (`origin=customer_tab`) plus one run; no separate ad-hoc run record.
19. Custom SQL is parked; phases ship in order console copy, report sources, destinations, customer one-off.

## Needs an owner outside this repo

- A 90-day lifecycle rule on the `exports/` prefix of the export bucket (S3 and GCS).

## Open for the implementer

- Which entity exporters can take a customer filter (only `wallet_ledger` is verified), and whether report-family reports expose `external_customer_id`.
- Exact CSV flattening against `dto.AnalyticsQueryResult` (columns, buckets, currency split).
- Shape of the `ReportQueryRequest` built from a saved report-family view.
- Temporal schedule `Overlap` and `CatchupWindow` values that satisfy the catch-up rule.
- How to extend `ExportRequest` (or wrap it) to carry `source_config` and the resolved range to the analytics adapter.
- Whether `GetExportMetadataFields()` is nil-safe on a nil `JobConfig` (the minimal config avoids the question).
- Whether the Resend client supports an idempotency key and how to pass it through the wrapper.
- Final deeplink path, agreed with the UI work.
- Temporal schedule update API details (`ScheduleHandle.Update`, preserving state and overlap policy).
