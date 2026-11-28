# Collection playbooks: backend design and API reference

Status: implemented on `feat/playbook`, ready for engineering review.
Product spec: *PRD: Collection Playbooks*. Starting data model: *ERD: Collection Playbooks* (deviations in §6).

A **playbook** is a tenant's rule for chasing unpaid invoices: which invoices it picks up (trigger and audience), the
timed steps it runs for each one (reminder email, internal alert, task) and the events that end outreach. Live
playbooks are checked in priority order and the first one an invoice matches runs it. A daily scheduler enrols
invoices and runs due steps. Reminder emails go through the existing review queue (Collections > Dunning >
Reminders) unless a step is set to send without review.

---

## 1. What changed

| Layer | Files |
| -- | -- |
| Types and enums | `internal/types/collections_playbook.go`, `uuid.go` (prefixes `cpb`, `cpr`, `cpsr`, `ctask`), `email_notification.go` (template `collections_team_notice`), `temporal.go`, `schedule.go` |
| Domain | `internal/domain/collections/playbook.go`; `invoice_reminder.go` (+ `PlaybookID`, `PlaybookStepID`) |
| Schema | `ent/schema/collection_playbook.go`, `collection_playbook_run.go`, `collection_playbook_step_run.go`, `collection_task.go`; `invoice_reminder.go` (+ 2 columns); generated `ent/` |
| Migrations | `migrations/versioned/postgres/20261128120000_collection_playbooks.sql`, `20261128120100_invoice_reminders_playbook_step.sql`, `migrations/.hashes` |
| Repository | `internal/repository/ent/collection_playbook.go`; `invoice_reminder.go` (reads/writes the 2 new columns); `internal/repository/factory.go` |
| Service | `internal/ee/service/collections_playbooks.go` (CRUD, lifecycle, runs, tasks), `collections_playbook_engine.go` (scheduler), `collections_playbook_templates.go`, `collections_playbook_email.go` (team notice email); `collections_reminders.go` (playbook hook); `email_notification.go` (composer); `factory.go` (2 repos) |
| Email | `internal/email/render/team_notice.go`, `templates/collections_team_notice.{html,txt}` |
| API | `internal/api/v1/collections_playbooks.go`, `internal/api/dto/collections_playbooks.go`; `dto/collections.go` (reminder response + 2 fields); `internal/api/router.go`; `cmd/server/main.go` |
| Temporal | `internal/temporal/workflows/cron/collection_playbooks_workflow.go`, `activities/cron/collection_playbook_activities.go`, `registration.go`, `service/schedules.go` |
| Tests | `internal/ee/service/collections_playbooks_test.go`, `internal/api/v1/collections_playbooks_test.go`, `internal/testutil/inmemory_playbook_store.go` |
| Docs | this file; `docs/swagger/swagger.{json,yaml}`, `docs.go` |

---

## 2. Data model

```
collection_playbooks 1 ──< collection_playbook_runs 1 ──< collection_playbook_step_runs
   (rule, steps JSONB)        │ one per invoice enrolment       │ one per (run, step); result_ref ──► invoice_reminders.id
                              │                                 │                                  ├► email_logs.id
                              │ invoice_id, customer_id         │                                  └► collection_tasks.id
invoice_reminders.playbook_id / playbook_step_id ──► the step that last wrote the reminder (nullable)
collection_tasks (run_id, step_id) ──► the step that created the task
```

All four tables carry `tenant_id`, `environment_id`, `status` (soft delete), `created_*`, `updated_*`, and every query
filters on tenant and environment.

| Table | Key columns | Constraints and indexes |
| -- | -- | -- |
| `collection_playbooks` | `name`, `description`, `state` (draft, live, paused), `priority` (1 first), `trigger` (overdue, before_due), `audience` jsonb, `steps` jsonb, `stops` jsonb, `min_gap_days`, `skip_weekends`, `template_id`, `version` (optimistic lock), `activated_at` | `(tenant, env, state, priority) WHERE status='published'` |
| `collection_playbook_runs` | `playbook_id` FK, `invoice_id`, `customer_id`, `state` (active, paused, stopped, completed), `stop_reason` (paid, voided, playbook_deleted), `paused_reason`, `invoice_due_date`, `next_step_at`, `started_at`, `ended_at` | **unique** `(tenant, env, invoice_id) WHERE published AND state IN (active, paused)`, so an invoice is in at most one live run; `idx_collection_playbook_run_playbook_state`, `idx_collection_playbook_run_customer_state`, `(…, next_step_at) WHERE state='active'` |
| `collection_playbook_step_runs` | `run_id` FK, `step_id`, `kind`, `due_at`, `state` (pending, done, skipped, failed), `skip_reason`, `result_ref`, `attempts`, `error`, `executed_at` | **unique** `(run_id, step_id)`, so a step runs at most once per invoice; `(tenant, env, state, due_at)` |
| `collection_tasks` | `invoice_id`, `customer_id`, `run_id`, `step_id`, `assignee_user_id` / `assignee_email`, `note`, `state` (open, done), `done_at`, `done_by` | **unique** `(run_id, step_id) WHERE run_id IS NOT NULL`; `(…, invoice_id)`, `(…, state, created_at)` |
| `invoice_reminders` (existing) | + `playbook_id`, `playbook_step_id` (nullable) | unchanged: still one live reminder per invoice |

Foreign keys are `ON DELETE NO ACTION`; nothing is hard-deleted (playbooks are soft-deleted, runs and step runs are
history).

**JSON shapes**

```jsonc
// audience: only the fields of its kind are kept
{ "kind": "all" }
{ "kind": "segment", "key": "segment", "value": "enterprise" }      // customers.metadata[key] equals value, any case
{ "kind": "amount_over", "amount": "10000", "currency": "usd" }      // amount_remaining > amount, same currency
{ "kind": "days_overdue_over", "days": 30 }

// steps: day is counted from the invoice's due date, -30 to 180; steps run in day order
{ "id": "friendly", "kind": "email", "day": 3, "tone": "friendly",
  "subject": "A quick reminder about {{invoice}}", "body": "Hi {{customer}}, ...", "send_without_review": false }
{ "id": "owner-alert", "kind": "internal", "day": 14, "channel": "email", "to": "ar@acme.com", "note": "..." }
{ "id": "owner-call", "kind": "task", "day": 35, "assignee": { "email": "owner@acme.com" }, "note": "Call {{customer}}" }

// stops: invoice_paid is always on; manually_paused allows pausing one invoice
["invoice_paid", "manually_paused", "customer_replied", "dispute_filed"]
```

Variables, filled in when a step runs: `{{customer}}`, `{{invoice}}`, `{{amount}}` (remaining balance),
`{{due_date}}`, `{{days_overdue}}`, `{{pay_link}}` (hosted invoice URL). Any other `{{…}}` is a 400.

---

## 3. Scheduler

`CollectionPlaybooksWorkflow` runs daily at 09:00 UTC on the cron task queue (schedule id `collection-playbooks`).
Its activity calls `PlaybookService.RunPlaybooks`, which lists the environments holding a live playbook (the one
cross-tenant read, like `BillDueInstalments`) and runs `RunEnvironment` for each in a scoped context. Environments
with Collections turned off are skipped. `POST /collections/playbooks/run` runs the same pass for one environment on
demand.

One pass, per environment:

1. **Stop.** Live runs whose invoice is paid, settled to zero, voided, deleted or no longer finalized are stopped
   (`paid` or `voided`), and their pending steps are skipped (`run_stopped`).
2. **Enrol.** For each live playbook in priority order, open finalized invoices with a due date that no run holds
   and that match the trigger and audience are enrolled. Only steps due today or later get a step run, so a new
   playbook never sends a burst of old steps. An invoice past every step is left alone. Invoices whose earlier run
   stopped because its playbook was deleted are offered to the next playbook. At most 10,000 invoices per playbook
   per pass.
3. **Run due steps.** For each active run of a live playbook with a pending step due now:
   * Steps due before the run's latest due day are skipped (`superseded`): a run that fell behind sends one step,
     not a burst.
   * **Email, invoice overdue:** the step's text is written into the invoice's reminder (§5). With
     `send_without_review` and a recipient, the reminder is approved and its email queued; otherwise it waits in the
     review queue. A reminder a person **held** is never overwritten (`reminder_held`).
   * **Email, invoice not yet overdue:** sent as written (the queue lists overdue invoices only). A step that needs
     review waits a day.
   * **Guardrails (customer emails):** with `skip_weekends`, a Saturday or Sunday email moves to Monday. With
     `min_gap_days`, an email within that many days of the customer's last playbook email moves forward.
   * **Internal alert:** email to `to` (template `collections_team_notice`). Slack is stored and skipped
     (`slack_not_connected`) until a Slack connection exists.
   * **Task:** a `collection_tasks` row, emailed to the assignee (user email or address) when emails are on.
4. **Advance.** `next_step_at` points at the next pending step; with none left the run is `completed`.

Idempotency: a step run is updated only from `pending`. Every side effect has its own key: the reminder records its
step id, before-due emails use the step run id and due date, team notices use the step run id, and tasks are unique
per (run, step). A retried or concurrent pass therefore never sends twice. A failing step stays pending and is
retried on the next pass, and is left `failed` with its error after 3 attempts.

---

## 4. API reference

Base path `/v1/collections`. Every route needs an API key or session. Reads need `collections:read` and writes need
`collections:write` (RBAC entity `collections`). Every route returns **403 `permission_denied`** while Collections is off
for the environment. Errors use the standard body:

```json
{ "code": "version_conflict", "message": "This playbook was changed by someone else since you opened it. Reload it and try again.", "http_status_code": 409 }
```

| Method | Path | Purpose |
| -- | -- | -- |
| GET | `/playbook-templates` | Starter playbooks |
| GET | `/playbooks` | List in priority order, with problems and stats |
| POST | `/playbooks` | Create a draft (blank or from a template) |
| GET | `/playbooks/{id}` | One playbook |
| PUT | `/playbooks/{id}` | Save the whole playbook (optimistic lock) |
| DELETE | `/playbooks/{id}` | Delete; invoices move to the next playbook |
| POST | `/playbooks/{id}/duplicate` | Copy as a draft |
| POST | `/playbooks/{id}/activate` | Draft or paused → live (readiness checked) |
| POST | `/playbooks/{id}/pause` | Live → paused |
| POST | `/playbooks/{id}/resume` | Paused → live |
| PUT | `/playbooks/reorder` | Set priority order |
| GET | `/playbooks/{id}/preview` | Invoices it would enrol today |
| GET | `/playbooks/{id}/runs` | Invoices in the playbook |
| POST | `/playbook-runs/{id}/pause` · `/resume` | Pause or resume one invoice |
| POST | `/customers/{id}/pause` · `/resume` | Pause or resume a customer's runs |
| GET | `/tasks` | Tasks created by steps |
| POST | `/tasks/{id}/complete` · `/reopen` | Mark a task done or open |
| POST | `/playbooks/run` | Run the scheduler for this environment now |

### The playbook object

```json
{
  "id": "cpb_01JDX4M2Q8V3K9",
  "name": "High-value invoices",
  "description": "For large balances: every email is reviewed, and finance is alerted early.",
  "state": "live",
  "priority": 1,
  "trigger": "overdue",
  "audience": { "kind": "amount_over", "amount": "10000", "currency": "usd" },
  "steps": [
    { "id": "friendly", "kind": "email", "day": 1, "tone": "friendly",
      "subject": "A quick reminder about {{invoice}}",
      "body": "Hi {{customer}},\n\nJust a friendly note that invoice {{invoice}} for {{amount}} was due on {{due_date}}...\n\nYou can pay here: {{pay_link}}" },
    { "id": "finance-alert", "kind": "internal", "day": 3, "channel": "email", "to": "finance@acme.com",
      "note": "{{invoice}} ({{amount}}) is overdue. A reminder is waiting for review." },
    { "id": "firm", "kind": "email", "day": 7, "tone": "firm", "subject": "{{invoice}} is {{days_overdue}} days overdue", "body": "..." }
  ],
  "stops": ["invoice_paid", "manually_paused"],
  "min_gap_days": 3,
  "skip_weekends": true,
  "template_id": "high-value",
  "version": 7,
  "activated_at": "2026-11-18T10:12:00Z",
  "problems": [],
  "stats": {
    "in_progress": 4,
    "reminders_sent_30d": 9,
    "recovered_30d": [{ "currency": "usd", "amount": "61200" }],
    "paid_within_7d_rate": 0.71
  },
  "created_at": "2026-11-02T08:00:00Z",
  "updated_at": "2026-11-18T10:12:00Z",
  "created_by": "user_01J...",
  "updated_by": "user_01J..."
}
```

* `steps` is always returned in day order. `problems` lists what blocks activation (`[{step_id?, field, message}]`)
  and is empty when the playbook is ready.
* `stats` covers the last 30 days. `reminders_sent_30d` counts email steps that ran (sent, or written into the review
  queue). `recovered_30d` is the amount paid, per currency, on invoices paid within 7 days after one of the playbook's
  reminders. `paid_within_7d_rate` is those invoices divided by reminded invoices, or `null` when none were reminded.

### GET `/playbook-templates`

200 → `{ "items": [{ "id": "standard", "name": "Standard reminders", "description": "...", "trigger": "overdue",
"audience": {"kind":"all"}, "steps": [ … ] }, …] }`. The template ids are `standard`, `before-due`, `enterprise`,
`high-value` and `final-push`.

### GET `/playbooks`

Query: `state` (comma-separated `draft,live,paused`). 200 → `{ "items": [Playbook…] }`, sorted by priority.
400 for an unknown state.

### POST `/playbooks`

```json
{ "name": "Enterprise follow-up", "template_id": "enterprise" }
```

Both fields are optional. Without a template you get one friendly email on day 3, stops `invoice_paid` and
`manually_paused`, min gap 3, skip weekends. Without a name, the template's name or "Untitled playbook" is used. The
new playbook is a draft placed last. 201 → Playbook (a template may leave `problems` to fill in, e.g. the alert
recipient). 400 for an unknown template or a name over 255 characters.

### PUT `/playbooks/{id}`

Send the whole playbook and the `version` you loaded:

```json
{
  "version": 7,
  "name": "High-value invoices",
  "description": "Every email reviewed; finance alerted on day 3.",
  "trigger": "overdue",
  "audience": { "kind": "amount_over", "amount": "10000", "currency": "USD" },
  "steps": [ { "id": "friendly", "kind": "email", "day": 1, "tone": "friendly", "subject": "...", "body": "..." } ],
  "stops": ["manually_paused"],
  "min_gap_days": 3,
  "skip_weekends": true
}
```

200 → Playbook with `version` incremented.

Validation (400 `validation_error`): `version` ≥ 1; `trigger` overdue or before_due; name ≤ 255 characters,
description ≤ 2000; at most 20 steps; step ids unique, 1–50 characters from `[A-Za-z0-9_-]`; `day` between −30 and
180; known `kind`; email `tone` friendly, firm or final; subject ≤ 255 characters; texts ≤ 10,000 characters; only
known variables; a valid audience shape (segment needs a value; amount needs a positive amount and a 3-letter
currency; days between 1 and 180); known stops; `min_gap_days` between 0 and 30.

Normalisation: fields of other kinds are dropped, currency is lower-cased, and `invoice_paid` is always added to stops.

Other errors: 409 `version_conflict` if someone saved since you loaded it. 400 `invalid_operation` with
`details.problems` if the playbook is live and the edit would leave it incomplete.

Effect on invoices already in a live playbook: pending steps that were removed are skipped (`edited_out`); a step that
moved or changed kind is rescheduled in place (never into the past); a new step is scheduled only if its day is still
ahead. Audience and trigger changes affect new enrolments only.

### DELETE `/playbooks/{id}`

204. Live runs stop (`playbook_deleted`) and their pending steps are skipped. The next pass offers those invoices to
the next live playbook they match, starting from that playbook's next future step. Reminders already written stay as
they are. The remaining playbooks are renumbered 1..n.

### POST `/playbooks/{id}/duplicate`

201 → a draft copy named "<name> (copy)", placed last, with empty stats.

### POST `/playbooks/{id}/activate` · `/pause` · `/resume`

Body `{ "version": 7 }`. 200 → Playbook.

* activate: allowed from draft or paused; resume: from paused; pause: from live. From any other state, or with a stale
  version: 409.
* Going live checks readiness. Failure is 400 `invalid_operation`:

```json
{
  "code": "invalid_operation",
  "message": "Finish the playbook before it goes live. step finance-alert: Add an address; Add at least one reminder email.",
  "http_status_code": 400,
  "details": { "problems": [
    { "step_id": "finance-alert", "field": "to", "message": "Add an address" },
    { "field": "steps", "message": "Add at least one reminder email" } ] }
}
```

Readiness rules: a name; at least one email step; email steps need a subject and body; an email step before the due
date (`day < 0`) must send without review; an internal email alert needs a valid address and a note; a Slack alert
needs a channel and a note; a task needs an assignee (user id or valid email) and a note; a before-due playbook needs a
step on or before the due date.

On resume, steps that fell due while paused are skipped except those on each invoice's latest due day.

### PUT `/playbooks/reorder`

```json
{ "ids": ["cpb_high", "cpb_enterprise", "cpb_default"] }
```

Send every playbook of the environment once, highest priority first. 200 → `{ "items": [Playbook…] }`. A missing,
unknown or repeated id is 400 (`details.missing` / `details.unknown`). Invoices already in a playbook stay in it.

### GET `/playbooks/{id}/preview`

Read-only.

```json
{
  "as_of": "2026-11-28T09:30:00Z",
  "matching_count": 12,
  "taken_by_higher_priority": 3,
  "truncated": false,
  "sample": [
    { "invoice_id": "inv_01…", "invoice_number": "INV-2026-0142", "customer_id": "cust_01…", "customer_name": "Acme Analytics",
      "currency": "usd", "amount_remaining": "4800", "due_date": "2026-11-21T00:00:00Z", "days_overdue": 7,
      "first_step": { "step_id": "firm", "kind": "email", "due_at": "2026-11-28T00:00:00Z" } }
  ]
}
```

`matching_count` counts invoices that match and that no playbook holds now. Those a higher-priority live playbook
would take first are counted in `taken_by_higher_priority` instead. The count stops at 1,000 (`truncated`). The sample
has at most 10 invoices.

### GET `/playbooks/{id}/runs`

Query: `state` (comma-separated `active,paused,stopped,completed`; default `active,paused`), `limit` (1–100,
default 25), `offset`.

```json
{
  "items": [ {
    "id": "cpr_01…", "playbook_id": "cpb_01…", "invoice_id": "inv_01…", "invoice_number": "INV-2026-0142",
    "customer_id": "cust_01…", "customer_name": "Acme Analytics", "currency": "usd", "amount_remaining": "4800",
    "invoice_due_date": "2026-11-21T00:00:00Z", "days_overdue": 7, "state": "active", "started_at": "2026-11-22T09:00:02Z",
    "next_step": { "id": "cpsr_01…", "step_id": "firm", "kind": "email", "due_at": "2026-11-28T00:00:00Z", "state": "pending", "attempts": 0 }
  } ],
  "pagination": { "total": 1, "limit": 25, "offset": 0 }
}
```

Ordered by oldest due date first.

### POST `/playbook-runs/{id}/pause` · `/resume`

The pause body is optional: `{ "reason": "Talking to their CFO" }` (≤ 1000 characters). 200 → the run.

* Pausing needs the playbook's `manually_paused` stop (otherwise 400 `invalid_operation`) and an active run
  (otherwise 409).
* Resuming needs a paused run (otherwise 409) and skips missed steps except those on the latest due day.

### POST `/customers/{id}/pause` · `/resume`

Pauses every active run of the customer, or resumes every paused one. 200 →
`{ "runs_affected": 2, "runs": [PlaybookRun…] }`. 404 if the customer doesn't exist. Invoices that enrol later are
not paused (see §7).

### GET `/tasks` · POST `/tasks/{id}/complete` · `/reopen`

* Query: `invoice_id`, `customer_id`, `assignee_user_id`, `state` (open or done), `limit`, `offset`.
* List → `{ "items": [{ "id": "ctask_01…", "invoice_id": "…", "customer_id": "…", "run_id": "cpr_…", "step_id": "owner-call",
  "assignee_email": "owner@acme.com", "note": "Call Acme Analytics about INV-2026-0142.", "state": "open",
  "created_at": "…" }], "pagination": { … } }`, newest first.
* complete and reopen return the task; 409 if it is already in that state.

### POST `/playbooks/run`

Runs one scheduler pass for the environment. 200 →
`{ "runs_stopped": 1, "runs_enrolled": 4, "steps_executed": 3, "steps_skipped": 1, "steps_failed": 0, "steps_deferred": 0 }`.

### Changes to existing endpoints

`GET /collections/reminders` and `GET /collections/reminders/{id}` (and the responses of the update endpoints) now
include `playbook_id` and `playbook_step_id` when a playbook step wrote the reminder. `tone_reason` then reads
"Tone set to firm by the playbook step (invoice is 9 days overdue)." A playbook reminder shows and sends the step's
text: it is never re-rendered from the invoice's age, including on approval.

---

## 5. How playbooks share the reminder queue

A reminder is still one row per invoice. When an email step comes due for an overdue invoice:

* No reminder yet → one is created with the step's text, pending (or approved and queued with `send_without_review`).
* A pending, approved or skipped reminder → it is reopened with the step's text. An already queued email is not
  cancelled.
* A **held** reminder → left alone; the step is skipped. Holding means "stop outreach for this invoice".
* The same step again (a retry) → no change.

The step's history stays in `collection_playbook_step_runs` (`result_ref` = reminder id), and each sent email stays
in `email_logs`.

---

## 6. Decisions and deviations from the ERD

| # | ERD said | Built | Why |
| -- | -- | -- | -- |
| 1 | Relax `invoice_reminders` to one reminder per step | Kept **one per invoice**; a step reopens it and records `playbook_id`/`playbook_step_id` | The board, the reminder list and every reminder route are keyed by invoice id. Many rows per invoice would break all of them. Step history is in step runs. |
| 2 | "A held or skipped reminder stays; the run moves on" | **Held** skips email steps; skipped is reopened by the next step | A hold is a person saying stop. Skipped means "not this one". |
| 3 | Before-due emails go through review | They must **send without review** (checked at activation) | The queue only lists overdue invoices, so a before-due reminder could never be approved. |
| 4 | 422 for readiness | **400 `invalid_operation`** with `details.problems` | The error system has no 422 kind; adding one changes every caller. |
| 5 | `stops text[]` | `stops jsonb` | Same as the other enum lists in ent; no `pq` array handling. |
| 6 | Tenant-time 09:00 | **09:00 UTC** | There is no tenant time zone setting yet. |
| 7 | Dry-run flag per tenant | **No flag**; the Collections switch gates everything, and emails wait for review unless a step opts out | One fewer switch. Review-by-default is the safety net. |
| 8 | Customer pause sets a flag | Pauses the customer's current runs only | No customer-level hold exists; adding one is a product decision (§7). |
| 9 | Task assignee as role | **User id or email** | Roles do not map to people anywhere yet. |
| 10 | Team notices as auto emails | **Manual trigger** | `email_logs` allows one automatic email per invoice and template, and a playbook can send several notices per invoice. A per-step idempotency key prevents repeats. |
| 11 | Recovery as "within 7 days of the last reminder" | Within 7 days of **any** reminder from the playbook | Simple, monotone, and matches the PRD's paid-within-7-days rate. |
| 12 | `in_progress`, stats cached | Computed per request (3 indexed queries for all playbooks in a list) | No cache to invalidate; revisit if the list gets slow. |

## 7. Pending and needs product input

* **Reminder list before the first step.** Until a playbook's first email step runs, an enrolled invoice still shows
  the age-based draft in Reminders, which someone could approve. Hiding enrolled invoices until their first step
  needs a change to the reminder list query and summary.
* **Customer replied / dispute raised** stops are stored and shown but never fire: there is no inbound-reply
  tracking or disputes entity.
* **Slack alerts** are skipped until a Slack connection exists.
* **Customer-level hold.** Should pausing a customer also block invoices that enrol later?
* **Tenant time zone** for the 09:00 run and the weekend rule (currently UTC).
* **Segments** read `customers.metadata[key]`; the console needs a way to set it.
* **Activity log.** Playbooks are not registered with the activity log yet (changes are traceable through
  `created_by`, `updated_by`, `version` and step runs).
* **OpenAPI 3.0 and SDKs.** `swagger.json`/`yaml` are regenerated; `make swagger-3-0` (which posts the spec to
  converter.swagger.io) and `make sdk-all` still need running.

## 8. Testing

* `go test ./internal/ee/service/ -run 'TestPlaybook|TestReminder'`: service and engine tests on in-memory stores
  covering CRUD, readiness, validation, optimistic locking, priority and first match, enrolment of future steps only,
  reminder writing and approval, send without review, held reminders, paid and voided runs, delete handover, pause and
  resume, superseded steps, guardrails, live edits, internal and task steps, before-due sends, stats, tenant and
  environment isolation, and the Collections switch.
* `go test ./internal/api/v1/ -run TestPlaybookHandler`: every route's binding, status mapping and pass-through.
* **Not run here:** the repository's raw SQL against Postgres (no database on the build machine), and
  `make migrate-check-sync` (needs Postgres). Run both before merging (see the PR checklist).
