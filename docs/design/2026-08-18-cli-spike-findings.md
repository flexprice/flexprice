# CLI spike findings — kin-openapi skeleton generation

Date: 2026-08-18
Gate: design doc §14

## Verdict

**PASS — with one mandatory design change to the skeleton fill policy.**

The library question is settled and the answer is a clean yes. `kin-openapi` loads the
884 KB spec, fully resolves every `$ref`, exposes everything a runtime command resolver
needs, and the skeleton walk terminates in **microseconds**. The template code in the
implementation plan **compiled unmodified** — no API drift to work around.

The *usability* question surfaced a real problem that is not a library problem:

> A required-only skeleton for `CreateSubscriptionRequest` is **five lines of empty
> strings**. Every nested structure the `--edit` feature exists to serve — `phases`,
> `line_items`, `credit_grants`, `override_entitlements`, `line_item_commitments`,
> `coupons` — is **optional** in the spec, so a required-only walk omits 100% of it.

Spec-wide, only **5 of 473** schemas produce a required-only skeleton that nests two or
more levels deep. So this is not a quirk of one schema; "required fields only" is the
wrong fill policy for this spec in general. See [Recommended fill policy](#recommended-fill-policy).

Keep `--edit`. Change what it pre-fills.

## kin-openapi

- **Version pinned:** `github.com/getkin/kin-openapi v0.146.0` (resolved from `@latest`),
  built with Go 1.26.5, darwin/arm64. Transitive deps pulled in:
  `santhosh-tekuri/jsonschema/v6 v6.0.2`, `oasdiff/yaml v0.1.1`, `oasdiff/yaml3 v0.0.14`,
  `go-openapi/jsonpointer v0.22.5`, `go-openapi/swag/jsonname v0.25.5`, `golang.org/x/text v0.14.0`.
- **Loader entrypoint:** `openapi3.NewLoader()` → `*openapi3.Loader`, then
  `loader.LoadFromFile(path) (*openapi3.T, error)`.
  **`loader.LoadFromData([]byte) (*openapi3.T, error)` also verified working with
  `go:embed`** — this is the one the real CLI needs. 884 KB embedded spec parses in
  **~48 ms**. `doc.Validate(loader.Context)` returns no error on our spec.
- **Schema map accessor:** `doc.Components.Schemas` — type `openapi3.Schemas`, i.e.
  `map[string]*openapi3.SchemaRef`. Plain map indexing works.
  Resolved schema is `ref.Value` (`*openapi3.Schema`), with `.Properties`
  (`map[string]*SchemaRef`), `.Required` (`[]string`), `.Items` (`*SchemaRef`),
  `.Enum` (`[]any`), `.Default`/`.Example` (`any`), `.Format`/`.Description` (`string`).
- **Type predicate form:** `s.Type != nil && s.Type.Is("object")`. `s.Type` is
  `*openapi3.Types` (a `[]string` under the hood), and `Is(string) bool` exists.
  **This is exactly what the plan assumed — no change needed.** The `s.Type != nil` guard
  is not optional: schemas with no declared type are common.
- **Path item map accessor:** `doc.Paths` is **`*openapi3.Paths`, not a map** — this is
  the one accessor that differs from the naive guess and would have broken a first attempt
  at command enumeration. Use:
  - `doc.Paths.Map() map[string]*openapi3.PathItem` — to range over
  - `doc.Paths.Len() int`
  - `doc.Paths.Find(path) *openapi3.PathItem`
  - `pathItem.Operations() map[string]*openapi3.Operation` — keys are `"GET"`, `"POST"`, …
  - `op.OperationID`, `op.Tags`, and the request body via
    `op.RequestBody.Value.Content.Get("application/json").Schema` (which has both
    `.Ref` — e.g. `#/components/schemas/CreateAddonRequest` — and the resolved `.Value`).
  Verified enumeration: **209 paths, 255 operations, 473 schemas**. Every operation carries
  a populated `OperationID` and `Tags`, so the tag→noun / operationId→verb command mapping
  in the design doc is viable.
- **Any API differences from the code in the plan:** **None in the skeleton code.** It
  compiled and ran as written on the first attempt. The only correction needed anywhere was
  `doc.Paths` being a struct rather than a map (above), which the skeleton function does not touch.

## CreateSubscriptionRequest

- **Required fields:** `billing_period`, `currency`, `plan_id` — **all three are scalar
  strings**, so the required-only walk never recurses even once.
- **Top-level properties:** **37**, of which **13 are nested** (object/array) and 24 are
  scalar. (The task brief estimated 21 nested; the measured number is 13. The nested ones
  are `addons`, `coupons`, `credit_grants`, `inheritance`, `line_item_commitments`,
  `line_item_coupons`, `line_items`, `metadata`, `override_entitlements`,
  `override_line_items`, `phases`, `subscription_coupons`, `tax_rate_overrides`.)
- **Cycles encountered:** **Zero.** The reachable subtree is 41 schemas and is fully
  acyclic. Cycles *do* exist in the spec — 5 of them — but every one is between **response**
  schemas, never request schemas:
  - `SubscriptionResponse -> InvoiceResponse -> SubscriptionResponse`
  - `PlanResponse -> PriceResponse -> PlanResponse`
  - `EntitlementResponse -> PlanResponse -> EntitlementResponse`
  - `AddonResponse -> EntitlementResponse -> AddonResponse`
  - `AddonResponse -> EntitlementResponse -> PlanResponse -> PriceResponse -> AddonResponse`

  **Do not delete the cycle guard.** It was verified load-bearing on those response
  schemas: walking `SubscriptionResponse` with pointer-identity detection visits 1,693
  nodes and records 17 cycle hits; with detection removed the same walk visits **17,789
  nodes (10.5x)** and is rescued only by the depth cap. Pointer identity on
  `*openapi3.Schema` is a valid cycle key because the loader resolves every `$ref` to the
  same shared `*Schema` instance. The stack-scoped `seen` set with `defer delete(seen, s)`
  is correct — a global visited-set would wrongly prune legitimate repeated siblings.
- **Max depth reached:** **14** property-nesting levels (natural, uncapped). The longest
  simple `$ref` chain is 8 hops:
  `CreateSubscriptionRequest -> SubscriptionInheritanceConfig -> GroupedInvoicingChildRequest -> AddAddonToSubscriptionRequest -> LineItemCommitmentConfig -> CommitmentBucketRequest -> CreatePriceRequest -> PriceUnitConfig -> CreatePriceTier`.
- **Runtime:** required-only walk **1.6–2.9 µs**. Full all-property walk, uncapped: **102 µs**.
  Spec load dominates at 48–73 ms. Whole binary, load + every probe below: **0.63 s wall**.
  It terminates — measured under a hard 60 s alarm, exit code 0, not assumed.
- **Skeleton valid JSON:** Yes — `python3 -m json.tool` accepts it; `json.MarshalIndent`
  returned no error for any variant tried.
- **Depth cap:** the plan's `depth > 12` was **slightly too low** for a full walk (it
  truncated 4 nodes). Natural depth is 14, so **16 is the right cap** for all-property mode
  — high enough never to bite on today's spec, low enough to bound a future cyclic request
  schema. For required-only mode the cap is irrelevant (max depth 1). Note the honest
  ordering: **the depth cap, not the cycle guard, is what guarantees termination**; the
  cycle guard is what keeps the output small and sane. Keep both.

### Skeleton, required-only (what the plan's algorithm produces today)

```json
{
  "billing_period": "",
  "currency": "",
  "plan_id": ""
}
```

This is valid and instantly generated — and useless as an editing surface for a
subscription with phases and line items.

### Skeleton, all properties, depth-capped at 1 (top-level shape)

Included so the shape can be eyeballed and used for the manual round-trip. `null` marks a
node truncated by the depth cap, not an intentional null.

```json
{
  "addons": [ null ],
  "auto_invoice_threshold": "",
  "billing_anchor": "",
  "billing_cycle": "",
  "billing_period": "",
  "billing_period_count": 0,
  "collection_method": "",
  "commitment_amount": "",
  "commitment_duration": "",
  "coupons": [ null ],
  "credit_grants": [ null ],
  "currency": "",
  "customer_id": "",
  "enable_true_up": false,
  "end_date": "",
  "external_customer_id": "",
  "gateway_payment_method_id": "",
  "inheritance": {
    "external_customer_ids_to_inherit_subscription": null,
    "grouped_invoicing_children_to_create": null,
    "invoicing_customer_external_id": null,
    "parent_subscription_id": null,
    "subscriptions_ids_for_grouped_invoicing": null
  },
  "line_item_commitments": {},
  "line_item_coupons": {},
  "line_items": [ null ],
  "lookup_key": "",
  "metadata": {},
  "overage_factor": "",
  "override_entitlements": [ null ],
  "override_line_items": [ null ],
  "payment_behavior": "",
  "payment_terms": "",
  "phases": [ null ],
  "plan_id": "",
  "proration_behavior": "",
  "start_date": "",
  "subscription_coupons": [ null ],
  "subscription_status": "",
  "tax_rate_overrides": [ null ],
  "timezone": "",
  "trial_period_days": 0
}
```

### Size of the all-property skeleton by depth cap

This is the budget Task 11 has to design against — it is the difference between a usable
editor buffer and a wall of text.

| depth cap | bytes | lines | max depth reached | nodes truncated |
|---|---|---|---|---|
| 1 | 1,256 | 63 | 1 | 14 |
| 2 | 3,369 | 150 | 2 | 76 |
| 3 | 4,661 | 210 | 3 | 49 |
| 4 | 6,706 | 297 | 4 | 68 |
| 6 | 13,331 | 542 | 6 | 66 |
| 8 | 19,803 | 748 | 8 | 75 |
| 12 | 23,819 | 872 | 12 | 4 |
| 20 | 23,970 | **876** | 14 | 0 |

Dumping every property is **876 lines / 24 KB** into `$EDITOR`. That is not an editing
surface either. The answer is between the two extremes, not at either end.

## Other schemas checked

- **`CreatePriceRequest`** — required-only skeleton has **8** keys, all scalar strings
  (`billing_model`, `billing_period`, `currency`, `entity_id`, `entity_type`,
  `invoice_cadence`, `price_unit_type`, `type`); 25 properties total; natural depth 4; walk
  in 2–7 µs; no cycles. This is the one schema of the three where a required-only skeleton
  is genuinely a decent starting point.
- **`CreatePlanRequest`** — required-only skeleton is a **single key**, `{"name": ""}`.
  The schema has only 5 properties and is depth-1: prices and entitlements are attached via
  separate calls, so there is nothing deep here. `--edit` adds little value for this one;
  plain flags cover it.

## Spec-wide properties worth recording

These simplify Task 11 considerably and should be relied on:

- **No `allOf` / `oneOf` / `anyOf` / `not` / `discriminator` anywhere in the spec.** Zero
  occurrences across all 473 schemas — swaggo emits fully flat schemas. The skeleton walker
  does **not** need composition handling. Re-check this if the spec generator ever changes.
- **`additionalProperties` appears 96 times** — the only composition-adjacent keyword in
  use, and the walker currently ignores it (renders `{}`, e.g. `metadata`,
  `line_item_commitments`, `line_item_coupons`). Acceptable, but it means map-typed fields
  give the user no hint about the expected value shape.
- **Zero unresolved (`nil` `.Value`) property refs.** No skeleton can be silently truncated
  by a dangling `$ref`.
- **Metadata coverage across 3,046 properties:** `description` on 792, **`enum` on 419**,
  `format` on 343, `example` on 37, `default` on 9.

### Recommended fill policy

Directly implied by the numbers above; this is the change that makes `--edit` worth
shipping:

1. **Emit all top-level properties, not just required ones** — otherwise the nested
   structures the feature exists for never appear at all.
2. **Recurse only into required nested fields, plus one level for optional ones**, with a
   depth cap of 16 as the backstop. The cap-2/cap-3 rows above (150–210 lines) are the
   sweet spot.
3. **Use `enum` values when filling.** 419 properties carry enums, including
   `billing_period` (`MONTHLY`, `ANNUAL`, `WEEKLY`, `DAILY`, `QUARTERLY`, `HALF_YEARLY`,
   `ONETIME`). Emitting `""` for an enum field is strictly worse than emitting the first
   legal value; a user editing a skeleton has no other way to discover the legal set.
   Prefer `default`, then `example`, then first `enum` member, then the zero value.
4. **Mark required vs optional visibly.** JSON has no comments; either emit a leading
   `"_required": ["billing_period","currency","plan_id"]` key that is stripped before
   submit, or write the buffer as JSONC/YAML and parse comments out. Worth a decision in
   Task 11.
5. **Strip empty/zero-valued optional keys before submitting**, so an untouched skeleton
   field is not sent as `""` and rejected or, worse, silently accepted as an empty value.

## Round-trip (plan Step 4)

**NOT RUN** — requires a development-environment API key that was not available to the
spike, and the spike was explicitly scoped not to go looking for credentials or start any
container. **This remains an open risk:** nothing here proves the API *accepts* a
skeleton-shaped body. In particular item 5 above (empty-string optionals) is exactly the
kind of thing that only fails against a live server.

Command for a human to run manually, using the required-only skeleton (US region; swap the
base URL to `https://api.cloud.flexprice.io/v1` for India, or `http://localhost:8080/v1`
against a local server). Auth is the `x-api-key` header (`ApiKeyAuth`), per the spec's
`securitySchemes`:

```bash
curl -sS -X POST 'https://us.api.flexprice.io/v1/subscriptions' \
  -H "x-api-key: $FLEXPRICE_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "billing_period": "MONTHLY",
    "currency": "usd",
    "plan_id": "<REAL_PLAN_ID>",
    "customer_id": "<REAL_CUSTOMER_ID>",
    "start_date": "2026-09-01T00:00:00Z"
  }' | jq .
```

Note the required-only skeleton alone is **not** a submittable body — it has no
`customer_id`, which the API needs in practice even though the spec does not mark it
required. That gap is itself evidence for recommendation 1 above: **the spec's `required`
list under-describes what a real request needs**, so a required-only skeleton would send
users into an error loop.

Two things the round-trip should specifically confirm:
- whether unset optional scalars sent as `""` are rejected (drives recommendation 5), and
- whether the API's true required set matches the spec's `required` list (it already
  appears not to, for `customer_id`).

## Consequences

`--edit` **survives** the gate. Task 11 proceeds, with its scope amended:

- Fill policy is **all top-level properties + required-nested + one optional level**, not
  required-only. This is a change to Task 11's algorithm, not to its existence.
- Depth cap **16**; keep the cycle guard even though today's request schemas are acyclic
  (response schemas are not, and the guard is 10x on those).
- Skip composition handling entirely — the spec has none.
- Pin `kin-openapi v0.146.0`. Re-run this spike on any upgrade; `doc.Paths` already changed
  shape from a plain map historically, which is exactly the kind of drift that breaks
  command enumeration.
- The round-trip check is still **owed** before `--edit` is called done.

Tasks 8, 10 and 14 are unaffected. `--data @file.json` should still ship regardless —
`--edit` is the ergonomic path, `--data` is the escape hatch, and the 876-line worst case
shows there will always be bodies a user would rather build in a file than in a buffer.
