# FLE-1513 — Billing Terms API Contract (commitments phase 2)

- **Date:** 2026-10-24
- **Author:** Ankit Malik
- **Status:** Proposed — contract-first scoping; semantics are normative in the
  [commitments ERD](./2026-10-07-FLE-1513-commitments-revamp-erd.md) §7, D11–D26, T1–T15
- **Scope of this phase:** the API surface below, its schema (`billing_terms`,
  `billing_instalments` tables per ERD §5.2–5.3), validation, preset/percent expansion and preview.
  Invoice delivery (claiming instalments onto subscription invoices, separate one-off invoices,
  void release) and the engine's covered-at-zero rendering ship behind it in the same phase but are
  not part of the contract freeze this document is for.

## 1. How billing terms enter a commitment

`collection_method: "billing_terms"` on `POST /v1/commitments` (and inline on subscription create)
unlocks two request fields:

| Field | Type | Meaning |
|---|---|---|
| `billing_terms` | object, required | How the commitment value is split into instalments: a `preset` or explicit `rows` (exactly one of the two) |
| `instalment_invoicing` | string, optional | `subscription_invoice` (default) or `separate_invoice` |

`per_period` commitments must not send either (today's behaviour, unchanged). Everything is
immutable after create, like the rest of the commitment's economics (D10); instalments are the
mutable layer (§4).

### 1.1 Presets (shorthand, expanded server-side)

```jsonc
{"billing_terms": {"preset": "upfront"}}

{"billing_terms": {"preset": "partial_upfront",
  "upfront": {"amount": "300"},                 // or {"percent": "30"}
  "rest": {"bill_at": "arrear", "interval": "MONTHLY", "interval_count": 1, "split": "even"}}}

{"billing_terms": {"preset": "spread",
  "bill_at": "advance", "interval": "QUARTERLY", "interval_count": 1, "split": "proportional"}}
```

### 1.2 Rows (full control)

```jsonc
{"billing_terms": {"rows": [
  {"kind": "once", "value_type": "fixed", "percent": "30", "description": "Signing payment"},
  {"kind": "once", "value_type": "fixed", "value": "350", "bill_on": "2027-06-20T00:00:00Z"},
  {"kind": "once", "value_type": "remainder", "on_trigger": true, "description": "Go-live payment"}
]}}
```

Row fields (ERD §7.2; storage is always fixed amounts — percent input is converted at create and
kept in `metadata.input_percent`):

| Field | `once` | `recurring` | Notes |
|---|---|---|---|
| `kind` | ✓ | ✓ | |
| `value_type` | ✓ | ✓ | `fixed` or `remainder`; at most one remainder row |
| `value` / `percent` | fixed only | fixed only (per instalment) | exactly one of the two |
| `bill_on` | optional, default `start_date` | — | within the term; absent when `on_trigger` |
| `on_trigger` | optional | — | milestone; billed when triggered, else at term end |
| `covers_from` / `covers_to` | optional, default the term | — | display + the "covered by" linkage |
| `starts_on` / `ends_on` | — | optional, default the term | billing boundaries inside the term |
| `interval` / `interval_count` | — | optional, default subscription period × 1 | step must be a whole multiple of the subscription period |
| `bill_at` | — | required | `advance` or `arrear` |
| `split` | — | required with remainder | `even` or `proportional` |
| `description` | optional | optional | `{n}` / `{of}` templating |

Validation is T1–T15 (ERD §7.5): 1–20 rows, one remainder max, fixed rows sum below (with
remainder) or exactly to (without) the commitment value, ≤ 500 instalments, every instalment > 0
and inside the term.

## 2. Preview and create responses

`POST /v1/commitments/preview` runs the full validation and expansion without saving — it is the
quote endpoint. Create returns the same shape, persisted. Added to `CommitmentResponse`:

```jsonc
{
  "id": "cmt_…",
  "collection_method": "billing_terms",
  "commitment_value": "1154.84",       // always computed
  "total_amount": "1154.84",           // snapshot the instalments were generated against
  "instalment_invoicing": "subscription_invoice",
  "billing_terms": [
    {"id": "bterm_…", "kind": "once", "value_type": "fixed", "value": "300",
     "bill_on": "2027-01-15T00:00:00Z", "resolved_amount": "300.00"},
    {"id": "bterm_…", "kind": "recurring", "value_type": "remainder", "interval": "MONTHLY",
     "interval_count": 1, "bill_at": "arrear", "split": "proportional",
     "resolved_amount": "854.84", "instalment_count": 12}
  ],
  "instalments": [
    {"id": "binst_…", "bill_on": "2027-01-15T00:00:00Z", "amount": "300.00",
     "service_period_start": "2027-01-15T00:00:00Z", "service_period_end": "2028-01-01T00:00:00Z",
     "bill_trigger": "on_date", "instalment_status": "scheduled",
     "description": "Signing payment", "invoice_id": null}
  ],
  "billing_summary": {"billed": "0.00", "earned": "0.00", "deferred": "0.00"}
}
```

Preview returns `billing_terms` without ids and `instalments` without ids/status. `GET
/v1/commitments/{id}` always carries all three blocks for billing-terms commitments; `per_period`
commitments return them as null/absent.

## 3. Instalment read API

| Method | Path | Scope | Returns |
|---|---|---|---|
| GET | `/v1/commitments/{id}/instalments` | read | the instalment list above, ordered by `bill_on` then id |

Instalment statuses: `scheduled` → `invoiced` (locked, carries `invoice_id` /
`invoice_line_item_id`) or `canceled`. `kind`: `instalment` or `settlement` (termination). The
invariant clients may rely on: non-canceled instalments of an active commitment always sum to
`total_amount` (D19).

## 4. Instalment mutations (ERD §7.8)

| Method | Path | Body | Behaviour |
|---|---|---|---|
| PUT | `/v1/commitments/{id}/instalments` | `{"instalments": [{"amount", "bill_on"?, "description"?}, …]}` | Re-plan: replaces every *scheduled* instalment; invoiced + new must equal `total_amount` |
| POST | `/v1/commitments/{id}/instalments/bring-forward` | `{"amount", "bill_on"?}` | Adds one instalment now and reduces the earliest scheduled ones by the same total |
| POST | `/v1/commitments/{id}/instalments/{instalment_id}/trigger` | `{"bill_on"?}` | Sets a manual (milestone) instalment's bill date, default now |

All three lock the commitment, require it active, and return the refreshed `GET` response.

## 5. Interaction with the invoice (for contract readers)

- Under `billing_terms`, period invoices show within-commitment usage at **$0** with
  `covered_by_instalment_ids` on the line, **no true-up**, and on-demand priced normally (D11/D13).
  Skipping only the true-up would double-collect the used portion — instalments already carry the
  full value.
- Instalment lines on invoices carry `commitment_id`, `billing_instalment_id`,
  `entity_type: "commitment"` and no price.
- Each instalment is invoiced at most once (claimed in the invoice transaction); voiding the
  invoice re-schedules it (D25).

## 6. Not in this contract

Owner types other than `commitment` (the tables carry `owner_type`/`owner_id` for later, ERD
§7.9), renewal, ledgers, per-instalment due dates, and the terminate interaction beyond what
already ships (a flat `cancellation_fee` on the terminate API; under billing terms termination
settles against instalments per ERD §8 — that lands with delivery, not the contract).
