# Billing Terms API Contract

Earlier shared draft: [Google Doc](https://docs.google.com/document/d/13ps74ey2zaisMstkWa6GwtPPKBLBhweeL4GuuN4C9Wk/edit).

Integration contract updated 29 October 2026. The implementation is under review in the existing six-PR stack; availability depends on merging and deploying that stack. This tracked contract is the current integration reference.

Billing terms collect a finite contractual amount through persisted rules and concrete schedule items. Commitments are the first owner. Covered usage retains its economic value and utilization, bills $0, and has no period true-up. Overage remains chargeable. Editing preserves invoice history and replaces the unclaimed collection arrangement.

## 1 Connection and conventions

Use the tenant's existing regional API base ending in `/v1`. Authenticate with its existing `x-api-key`; JWT callers also supply `x-environment-id` where required by the current authentication flow. Examples below use paths relative to `/v1`. All IDs and amounts are illustrative. Money is a decimal string; timestamps are RFC3339 UTC instants. Term and query ranges are half-open unless a billing-date boundary is explicitly inclusive.

All operations are tenant and environment scoped. Public commitment routes select the owner type server-side. There is no client-supplied tenant, arbitrary owner type or persisted revision number. The proposed input `invoice_delivery` maps to the existing `instalment_invoicing` storage column; the column and existing Go mappings remain unchanged.

## 2 Endpoint index

| Method | Path | Result |
|---|---|---|
| POST | `/commitments/preview` | 200 read-only commitment and complete schedule preview |
| POST | `/commitments` | 201 commitment created with rules and all schedule items atomically |
| POST | `/subscriptions` | Existing create response; accepts the same terms within `commitments[]` |
| GET | `/commitments/{id}` | Existing commitment response with current terms, collection summary and state token when applicable |
| GET | `/commitments/{id}/billing-terms` | 200 current rules, summary and state token |
| POST | `/commitments/{id}/billing-terms/preview` | 200 read-only complete replacement preview |
| PUT | `/commitments/{id}/billing-terms` | 200 complete replacement saved, or current state for an identical current-plan retry |
| GET | `/commitments/{id}/billing-schedule-items` | 200 paginated concrete charges and their invoice sources |
| GET | `/commitments/{id}/billing-schedule-items/{item_id}` | 200 exact source item, including archived history |
| GET | `/commitments/{id}/billing-terms/history` | 200 paginated archived/current source history |
| POST | `/commitments/{id}/billing-schedule-items/{item_id}/trigger` | 200 current manual item dated, without creating an invoice synchronously |
| POST | `/events/analytics` | Existing cost-bearing usage endpoint, with separately paginated commitment summaries |
| POST | `/invoices/{id}/recalculate` | Existing async endpoint extended with explicit replacement item selection for a voided source invoice |

Preview and analytics operations have read scope despite using POST. Creation, replacement and trigger have write scope. Existing invoice void and correction operations retain their existing authorization; saving terms never voids an invoice.

## 3 Create the commitment and terms together

`POST /commitments` retains all existing commitment fields and adds `billing_terms` and `invoice_delivery` when `binding=true` and `collection_method=billing_terms`. `per_period` and non-binding commitments reject those additions. `invoice_delivery` defaults to `subscription_invoice`; the other value is `separate_invoice`.

```json
{
  "subscription_id": "subs_example",
  "idempotency_key": "annual-platform-2027",
  "name": "Annual platform commitment",
  "scope": {"entity_type": "subscription", "entity_id": "subs_example"},
  "type": "amount",
  "value": "100",
  "measurement": "billing_period",
  "binding": true,
  "collection_method": "billing_terms",
  "invoice_delivery": "subscription_invoice",
  "start_date": "2027-01-01T00:00:00Z",
  "end_date": "2028-01-01T00:00:00Z",
  "billing_terms": {
    "rows": [
      {"kind": "once", "value_type": "fixed", "value": "300", "description": "Upfront commitment payment"},
      {"kind": "recurring", "value_type": "remainder", "interval": "MONTHLY", "interval_count": 1, "bill_at": "arrear", "split": "even", "description": "Monthly commitment payment"}
    ]
  }
}
```

For a monthly USD subscription whose contractual value is $1,200, this creates two rules and thirteen items: $300 upfront and twelve $75 payments. Persist the owner, rules and every item in one transaction. Only due eligible items become invoice lines. Creation does not pre-create twelve future invoices. A manual milestone item exists immediately with a null bill date.

`POST /commitments/preview` accepts the same body and returns the calculated commitment value, normalized rules, proposed items and collection summary without writes, IDs reserved for persistence, invoices or workflow starts. Persisted IDs are absent from proposed records. Percentage inputs normalize to fixed money using the original total.

For inline creation, each `POST /subscriptions` `commitments[]` entry accepts the same terms/delivery fields. Existing inline defaults still set `subscription_id` and a subscription-scope entity ID server-side and resolve overridden price scopes. Subscription, commitments, terms and items commit atomically. A pending/trialing subscription may have a schedule, but cannot collect before the applicable activation/trial-end policy allows it. Paid draft activation includes eligible due terms in its first invoice; trial activation defers them until trial end. Activation must preserve valid commitment windows and does not shift their dates or regenerate schedules.

The existing owner idempotency key remains first-write-wins: a create retry returns the original commitment rather than modifying its terms. Use the replacement endpoint for a changed arrangement. Quantity commitments retain the existing required `committed_price` object and scope/measurement validation; billing terms do not replace usage rate Prices.

## 4 Rule fields and limits

| Field | Meaning |
|---|---|
| `kind` | `once` or `recurring` |
| `value_type` | `fixed` or `remainder`; at most one remainder rule |
| `value` or `percent` | Exactly one for fixed rules. Recurring fixed input is per occurrence. Percent is based on original contract total, including on edits |
| `bill_on` or `on_trigger` | Once only; date or manual trigger, mutually exclusive. On create, a dated once rule defaults to owner start; edit requires its date explicitly |
| `start_date`, `end_date` | Recurring generation range, inside the owner term; create defaults to owner dates, edit requires explicit start |
| `interval`, `interval_count` | Recurring frequency and positive count; commitment cadence/boundaries follow subscription alignment |
| `bill_at` | Recurring `advance` or `arrear` |
| `split` | Recurring remainder only: `even` by occurrence count or `proportional` by elapsed duration |
| `description` | Optional label, bounded to 255 characters after expansion |

Billing-term invoices use the obligation currency. Cross-currency recipients and custom-currency conversion are unsupported in this release and fail validation; ordinary price invoices retain their existing conversion behavior.

Rules have no coverage overrides, ordering priority, stored versions or separate global date cutoff. A once item's service period snapshots the owner term; recurring items snapshot their individual steps. Those dates do not define revenue recognition.

Accept 1–20 rules and at most 500 relevant items per owner, counting retained live-invoice items plus proposed items. Archived canceled history does not consume the limit. Stop expansion at item 501. Use currency precision, positive rounded amounts and `numeric(20,8)` storage bounds; reject zero-valued occurrences rather than silently dropping them. Validate timezone/calendar arithmetic, owner permissions and exact allocation on both preview and save. A $900 remainder across 15-day and 30-day steps is $450/$450 with even split or $300/$600 with proportional split. Fixed occurrence amounts are not implicitly prorated.

## 5 Current state and collection summary

Create/get/save responses retain the existing commitment fields and add `billing_terms:{rows:[...]}`, `billing_schedule_items:{items:[...],pagination:{...}}`, `collection` and `state_token`. GET billing terms returns those same additions without requiring duplicate owner fields. Persisted rule records carry stable `id` values; the item page is bounded and its list endpoint supplies the remainder. `state_token` is an opaque computed concurrency precondition, not a stored version counter. This collection excerpt uses the owner's obligation currency:

```json
{
  "state_token": "opaque_current_state",
  "collection": {
    "as_of": "2027-05-10T00:00:00Z",
    "currency": "usd",
    "target_amount": "1200.00",
    "assigned_amount": "600.00",
    "invoiced_amount": "600.00",
    "scheduled_amount": "600.00",
    "due_amount": "0.00",
    "unallocated_amount": "0.00",
    "requires_replan": false,
    "deferred_reason": null,
    "next_bill_on": "2027-06-01T00:00:00Z"
  }
}
```

`assigned_amount` includes draft and finalized non-void invoice bindings, including archived sources. `invoiced_amount` is the subset assigned to finalized non-void invoices, before invoice adjustments; it is principal, not cash received. `scheduled_amount` includes all current unclaimed charges; `due_amount` is its date-due subset, which can still be deferred. `target_amount = assigned_amount + scheduled_amount + unallocated_amount`. A successful save has zero unallocated amount. `next_bill_on` is the earliest resolved pending date, possibly overdue, or null when none exists.

`deferred_reason` is null or a derived reason such as `subscription_paused`, `subscription_trialing`, `subscription_pending`, `requires_replan` or `delivery_disabled`; it is not a new persisted pause flag. Multiple reasons may apply; return the first actionable reason in that order, except an integrity error is reported as an error. Invoice payment status does not free assigned principal. Credit notes/refunds do not automatically expand the schedule budget. Owner-authorized termination can replace the normal target with its protected settlement target without changing original `total_amount`.

## 6 Preview and save an edit

An edit submits the complete desired unclaimed arrangement. The UI displays the full contract as retained invoice entries plus editable charges. For example, after $600 is retained on live invoices, this replaces the remaining $600 with four $150 payments:

```json
{
  "expected_state": "opaque_current_state",
  "reason": "Four remaining payments",
  "billing_terms": {
    "rows": [
      {"kind": "recurring", "value_type": "remainder", "start_date": "2027-05-01T00:00:00Z", "end_date": "2028-01-01T00:00:00Z", "interval": "MONTHLY", "interval_count": 2, "bill_at": "arrear", "split": "even"}
    ]
  }
}
```

Send this body to `POST /commitments/{id}/billing-terms/preview`, then submit its normalized `command` to `PUT /commitments/{id}/billing-terms`. Preview returns `state_token`, `can_apply`, `command`, `archive_rule_ids`, `archive_item_ids`, `retained_invoice_ids`, proposed `items`, `collection` and `warnings`. Proposed items contain no persisted ID; a successful save returns current persisted rules, `collection`, `state_token` and a bounded first item page, with the item-list URL for the remainder. The example dates produce payments on 1 July, 1 September, 1 November and 1 January. A past bill date is shown as due now; it does not silently rewrite an old invoice.

Save checks actual state under locks, archives the complete old published rule/item set, preserves assigned invoice principal and source links, cancels obsolete unclaimed items, then inserts fresh rules/items atomically. On failure roll back all changes. It creates/voids no invoice synchronously. A normalized identical currently installed plan without a gap is a no-op, including after subsequent invoicing; it does not recreate rows or reset milestones. A stale different edit gets 409 and must be previewed again.

For correction of the original setup, preview additionally accepts `replace_invoice_ids`. This is preview-only and PUT rejects it. Validate every scoped invoice contributes this owner's live assigned principal. The hypothetical preview returns `conditional_released_principal`, `blocking_invoice_ids`, `can_apply=false` and `command=null`; it neither voids nor reserves anything. Draft the full replacement first, explicitly correct the affected invoices, refresh/preview without exclusions, then save against actual released principal. If all claims are voided, the full target can be regenerated from owner start. Partial correction retains every other invoice's principal.

Publishing $1,200 of fresh charges while a retained $300 invoice remains live would authorize $1,500 and is rejected. Archiving a source does not offset its invoice. A later void permanently cancels the original item and creates a visible allocation gap; it never revives an old item. Both delivery paths defer new pickup for that owner until a complete reviewed replan clears the gap.

Historical reissue selects reviewed fresh item IDs; it never sweeps up every overdue charge. The invoice recalculation contract is defined in §11. In-place draft recomputation preserves already-assigned source lines. Mixed-invoice and paid-invoice corrections follow existing invoice rules and preserve unrelated charges.

## 7 Schedule items and source history

The item list supports `limit` (default 50, maximum 100), `cursor`, `include_archived` (default false) and optional `schedule_status` (`scheduled`, `invoiced`, `canceled`). History includes archives and uses the same limit/cursor convention. Return `{items, pagination:{limit, has_more, next_cursor}}`; cursors are opaque and bound to the filters/scope. Use stable ordering by bill date then ID for pending items and creation time then ID for history, with null bill dates last. Totals come from the collection summary, never a single page's sum.

```json
{
  "id": "bsi_example",
  "billing_term_id": "bterm_example",
  "owner_type": "commitment",
  "owner_id": "comm_example",
  "customer_id": "cust_example",
  "subscription_id": "subs_example",
  "currency": "usd",
  "amount": "75.00",
  "bill_on": "2027-06-01T00:00:00Z",
  "service_period_start": "2027-05-01T00:00:00Z",
  "service_period_end": "2027-06-01T00:00:00Z",
  "delivery": "subscription_invoice",
  "schedule_status": "scheduled",
  "status": "published",
  "invoice_id": null,
  "invoice_line_item_id": null
}
```

Invoice line → schedule item → exact original rule remains resolvable after editing, archival or void. History has no numeric revisions. Archived sources are included when reading historical invoice attribution; new pickup accepts only current eligible sources.

## 8 Manual trigger

`POST /commitments/{id}/billing-schedule-items/{item_id}/trigger` accepts `{expected_state, bill_on?}`. Omitted `bill_on` resolves to the first accepted server time, subject to owner validation. Return the dated item, collection summary and new state token. A retry with an omitted/matching date returns the original resolved result; a changed explicit date or canceled/archived item conflicts. Trigger resolves a date, not immediate synchronous invoice creation. An untriggered milestone falls back to owner term end once, through the scheduled runner. Pause/trial may defer delivery after its date is resolved.

## 9 Delivery and true up

`subscription_invoice` joins the next eligible opening/cycle/trial-end/resume invoice. `separate_invoice` creates one ONE_OFF per due item, normally picked up by a scoped 2-minute runner; this is not an exact-second delivery SLA. Both modes use identical eligibility, source binding, lifecycle and budget rules. Paused/trialing subscriptions defer unclaimed payments until resume/trial end, preserving original amounts/dates; draft/incomplete subscriptions normally wait for activation. At trial end, due subscription-delivery payments are included in the first real paid-period invoice even if trial conversion temporarily marks the subscription incomplete awaiting payment. This is a server-controlled trial-end exception, not a client request flag; it does not apply to separate delivery, ordinary incomplete subscriptions, paused/canceled subscriptions or trial previews. Retrying that invoice reuses its source bindings, and payment failure does not release assigned principal. Positive principal is never assigned to a zeroed trial invoice or skipped paused invoice.

Covered usage follows the applicable binding contract even before payment is due/received. Show economic capacity/used/unused values alongside $0 covered usage charge and `true_up_enabled=false`, `true_up_amount=0`. Overage still bills. Never enable per-period true-up because a schedule invoice is unpaid, a worker is delayed, or the owner requires replan. A valid owner-authorized termination collection is a separate settlement, not a period true-up.

## 10 Cost Explorer

`POST /events/analytics` adds a top-level `commitments` array and `commitments_pagination` when commitments are enabled. Existing usage `items`, `subtotal`, `total_discount` and `total_cost` keep their charge semantics. Collection principal is not added to those totals or repeated per meter/time point.

```json
{
  "external_customer_id": "tenant-customer-123",
  "start_time": "2027-01-01T00:00:00Z",
  "end_time": "2027-02-01T00:00:00Z",
  "group_by": ["feature_id"],
  "commitments_limit": 50
}
```

The following excerpt shows the additive fields. `usage` uses the selected range; `collection_scope=lifetime` makes the independent collection basis explicit.

```json
{
  "commitments": [
    {
      "commitment_id": "comm_example",
      "subscription_id": "subs_example",
      "usage_window": {
        "start_time": "2027-01-01T00:00:00Z",
        "end_time": "2027-02-01T00:00:00Z"
      },
      "usage": {
        "commitment_id": "comm_example",
        "collection_method": "billing_terms",
        "true_up_enabled": false,
        "true_up_amount": "0",
        "currency": "usd",
        "applies_from": "2027-01-01T00:00:00Z",
        "applies_to": "2027-02-01T00:00:00Z",
        "committed": {"quantity": "0", "amount": "100"},
        "used": {"quantity": "0", "amount": "80"},
        "unused": {"quantity": "0", "amount": "20"},
        "billed_amount": "0"
      },
      "collection_scope": "lifetime",
      "collection": {
        "as_of": "2027-05-01T00:00:00Z",
        "currency": "usd",
        "target_amount": "1200",
        "assigned_amount": "375",
        "invoiced_amount": "300",
        "scheduled_amount": "825",
        "due_amount": "75",
        "unallocated_amount": "0",
        "requires_replan": false,
        "deferred_reason": null,
        "next_bill_on": "2027-05-01T00:00:00Z"
      }
    }
  ],
  "commitments_pagination": {"limit": 50, "has_more": false, "next_cursor": null}
}
```

In this example, $75 is assigned to drafts (`assigned_amount - invoiced_amount`), $300 is invoiced and $825 remains scheduled. These are lifetime principal amounts before invoice discounts, credits and taxes. They are not cash collected. A void releases principal into `unallocated_amount` until an explicit replan; a refund or credit note does not automatically release it. Per-period commitments have `collection=null` because they have no billing-term schedule.

The commitment page defaults to 50 entries, maximum 100. Pass `commitments_cursor` to continue with the same explicit time range and filters. Cursors bind tenant, environment, customer/hierarchy selection and filters. Owner discovery is bounded to 500 candidates per page; a page can be short with a continuation cursor. Applicable owners remain visible with no usage events or invoices. An empty owner page can omit `commitments`; pagination is still returned when the feature is enabled.

Commitment utilization reads at most 500 distinct subscription billing periods per request; narrow the time range, customer selection or `commitments_limit` when this limit is exceeded.

Feature/meter filtering selects intersecting owners; capacity allocation uses the whole shared pool. Collection stays whole-owner money. When source/property filters cannot support commitment usage attribution, return `usage=null` with `usage_unavailable_reason`; retain the lifetime collection summary. Do not interpret unavailable usage as zero. `include_children` follows the selected customer hierarchy and keeps the actual owner subscription ID.

Use `GET /commitments/{id}/billing-schedule-items` for paginated charges, `.../billing-terms/history` for archived sources, and the invoice ID to open invoice/payment details. Each bound item includes `invoice_status` to distinguish draft assignment, finalization and void. Details are not embedded into analytics. Totals come from the authoritative lifetime summary, not a detail page sum.

Usage analytics continues through ClickHouse with its existing memory limit. Bounded owner/source state comes from PostgreSQL. This release has no new invoice/credit analytics projection, selected-window collection totals or per-owner cash-allocation metric.

## 11 Historical invoice correction

Extend existing `POST /invoices/{voided_invoice_id}/recalculate` with an optional body:

```json
{
  "billing_schedule_item_ids": ["bsi_corrected_upfront"]
}
```

Existing bodyless subscription recalculation remains compatible where there are no linked scheduled charges to replace. For a voided invoice with schedule sources, require the reviewed replacement IDs rather than auto-discovering every overdue item. Accept 1–500 unique IDs from the original invoice's commitment owners, with the same authorized recipient and compatible billing destination/currency. Validate them as published eligible unclaimed sources under the lock protocol, after the complete replan clears any gap. A separate ONE_OFF source uses the same explicit selection extension; ordinary bodyless one-off recalculation remains unsupported.

Reuse the existing 202 workflow result (`workflow_id`, `run_id`) and invoice replacement link. Preserve/recalculate unrelated subscription charges through the existing correction rules, but claim only the selected replacement schedule items. The old invoice's period supplies context, not an inferred one-to-one source mapping; each new line keeps its own reviewed period. Selection of future/not-yet-due or another owner's items fails. If a due worker already claimed an item into another invoice, return 409 with its invoice reference for renewed review; a retry of this exact replacement workflow recovers its own existing result. Never create another charge. Preview the proposed replacement through billing-terms preview before destructive invoice operations; void/refund eligibility remains governed by the existing invoice API.

Ordinary charges may be repriced or recalculated from corrected usage, including a zero result. If a previously charged ordinary source or billing window is absent from the prepared replacement, the workflow fails with a `version_conflict` identifying the missing invoice line; it creates no replacement and claims no schedule items. Restore the historical source before retrying. This check runs asynchronously after the 202 response, and also protects later replacement recomputation.

## 12 Errors and retries

Use the existing flat error envelope `{code,message,http_status_code,details?}`. Validation and unsupported field combinations return 400; foreign/not-found scoped IDs return 404; stale state and conflicting trigger operations return 409; unavailable required delivery/summary dependencies return 503. Return field/rule index and actionable reason in details; indexes are not stored positions. A stale-plan conflict includes the refreshed `state_token`; reload the current summary before retrying. No partial archive or duplicate invoice is an acceptable retry outcome.

```json
{
  "code": "version_conflict",
  "message": "Billing state changed; preview the replacement again",
  "http_status_code": 409,
  "details": {"reason": "billing_state_changed", "state_token": "new_opaque_state"}
}
```

History reads always preserve tenant/environment checks. A request cannot bypass the ordinary subscription Price requirement merely by supplying a schedule ID; schedule-source invoice lines are trusted server-generated inputs validated against owner/source claims.

## 13 Tenant integration sequence

1. Build create/edit UI against this contract and examples; confirm the feature is deployed/enabled for the target environment before live use.
2. Preview and create the commitment with nested terms, or include it in subscription creation.
3. Read current rules/collection and paginated items; render actual invoice links separately from usage value.
4. Preview complete replacement, review retained invoices and due-now effects, then PUT its normalized command/state token.
5. Read commitment summaries alongside usage; label lifetime collection separately and open the paginated item/history endpoints for detail.

Release gates include create/inline atomicity, invoice/usage true-up parity, real subscription and separate delivery, pause/trial deferral, edit/void/retry correctness, selected-item recalculation for its corresponding UI action, lifetime summary/drill-down consistency, isolation and the implementation plan's validation matrix. Performance-obligation recognition and payment-level cash attribution are separate projects. With revenue recognition enabled, journal posting rejects schedule-source invoices/credits until a commitment-to-performance-obligation allocation exists; it does not infer immediate revenue from a missing Price. This asynchronous accounting guard does not block invoice finalization or payment.
