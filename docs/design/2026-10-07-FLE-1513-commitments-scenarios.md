# FLE-1513 — Commitments & Billing Terms: Scenarios

The [implementation plan](./2026-10-27-FLE-1513-billing-terms-implementation-plan.md) defines the initial phase-2 release: the shipped terminate request/default policy stays in scope; `credit_note` and `bill_remaining` variants in A15/A16 are future design cases, not public inputs in that release.

Companion to the [ERD](./2026-10-07-FLE-1513-commitments-revamp-erd.md). Section numbers (§) and rule IDs (C…, T…) refer to the ERD.

Each scenario gives:

- the situation;
- the setup: commitment fields and, where relevant, `billing_terms` rows;
- the result: what each invoice shows.

Every billing-terms example below is also an expansion test case (ERD §13.3). Stored rows are always `fixed` or `remainder`; percentages are only an input convenience. `POST /v1/commitments/preview` shows the resulting schedule items before anything is created. Stored charges use `billing_schedule_items`; rule edits archive old definitions and regenerate the upcoming arrangement with fresh IDs (ERD §7.8).

## Conventions

- Monthly subscription anchored on the 1st, USD.
- Under `billing_terms`, instalments go on the subscription invoice (`invoice_delivery = subscription_invoice`) unless stated otherwise.
- Covered usage follows the binding contract; it bills $0 and never adds period true-up under `billing_terms`, even when the separate payment is later/unpaid. Consumed quantity/value and unused capacity are still reported.
- Paused/trialing subscriptions defer unclaimed commitment payments in both delivery modes until resume/trial end; item dates/amounts remain unchanged. Draft/incomplete subscriptions wait for activation.
- **Advance** means billed on the invoice issued at the start of the step. **Arrear** means billed on the invoice issued at its end.
- Rows are written in shorthand:


| Notation                                        | Row                                                                                                                     |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `once(fixed 300)`                               | `kind=once, value_type=fixed, value=300`, `bill_on` = commitment `start_date`                                           |
| `once(fixed 600, bill_on 1 Jul)` | `kind=once, value_type=fixed, value=600, bill_on=1 Jul` |
| `once(remainder, on_trigger)`                   | `kind=once, value_type=remainder, on_trigger=true`                                                                      |
| `recurring(fixed 300, advance, QUARTERLY×1)`    | `kind=recurring, value_type=fixed, value=300` (per instalment), `bill_at=advance, interval=QUARTERLY, interval_count=1` |
| `recurring(remainder, arrear, MONTHLY×1, even)` | `kind=recurring, value_type=remainder, bill_at=arrear, interval=MONTHLY, interval_count=1, split=even`                  |
| `…, start_date 1 Apr` / `…, end_date 1 Jul`       | `start_date` / `end_date` set (both must be billing boundaries)                                                           |


---



## Part A — Commitments



### A1. Monthly commitment, true-up each period (`per_period`)

**Situation:** $100/month for 12 months across all usage.
**Setup:** scope `subscription`, `type=amount`, `value=100`, `measurement=billing_period`, `binding=true`, `collection_method=per_period`.
**Result:**

- Every month-end invoice shows usage within the commitment (up to $100) at line prices.
- If usage is below $100, a **true-up** line covers the difference.
- Usage above $100 is billed as on-demand.
- Total commitment money is $1,200, with no instalments.


| Month usage | Within | True-up | On-demand |
| ----------- | ------ | ------- | --------- |
| $60         | $60    | $40     | —         |
| $120        | $100   | —       | $20       |




### A2. All upfront

**Situation:** $1,200 for the year, invoiced on day one.
**Setup:** as A1, with `collection_method=billing_terms` and preset `upfront` → `once(remainder)`.
**Result:**

- One $1,200 instalment on the 1 Jan invoice, with a service period of the whole year.
- Monthly invoices show within-commitment usage at $0, with the line "covered by commitment", plus on-demand usage.
- No true-up.
- **Analytics (ERD §9):** each month reports usage value (used within, unused commitment, on-demand) exactly as under `per_period`. Actual collection is $1,200 invoiced in January; the commitment's usage-billing contribution is $0 and overage is separate. Recognition/deferred revenue comes from the performance obligation independently.



### A3. Partial upfront, rest equally

**Situation:** $1,000 over 10 months. $300 upfront, the remaining $700 split equally.
**Setup:**

- `value=100`, term 1 Jan → 1 Nov, `billing_terms`.
- Preset `partial_upfront` (upfront fixed 300; rest arrear, even), which expands to `once(fixed 300)` + `recurring(remainder, arrear, MONTHLY×1, even)`.

**Result:**

- $300 on the 1 Jan invoice, then $70 on each month-end invoice (10 instalments), for a total of $1,000.
- February's usage is billed on the 1 Mar invoice. Its within-commitment line is $0 and refers to the commitment. The separate $70 collection line retains its exact schedule/rule source ID.



### A4. Milestones: 30% / 50% / rest

**Situation:** $1,200 for 2027. 30% at signing, 50% on 1 Jul, the rest at the end.
**Setup:** `once(fixed 360)` + `once(fixed 600, bill_on 1 Jul)` + `once(remainder, bill_on 1 Jan 2028)`. The request can send `percent: 30` and `percent: 50`; the API converts them to 360 and 600 before saving (ERD §7.1).
**Result:**

- $360 on the 1 Jan invoice.
- $600 on the 1 Jul invoice.
- $240 on the closing invoice of 1 Jan 2028.



### A5. Fully custom dates and amounts

**Situation:** $1,000. $400 on 15 Jan, $350 on 20 Jun, $250 at go-live.
**Setup:**

- `once(fixed 400, bill_on 15 Jan)` + `once(fixed 350, bill_on 20 Jun)` + `once(remainder, on_trigger)`.
- Optionally `invoice_delivery=separate_invoice`, to bill on the exact dates.

**Result with** `subscription_invoice`**:**

- The $400 lands on the 1 Feb invoice.
- The $350 lands on the 1 Jul invoice.
- The $250 waits for a trigger, or is billed at term end.

**Result with** `separate_invoice`**:** one-off invoices on 15 Jan and 20 Jun, and on the trigger date.

### A6. Upfront amount, then monthly collection from April

**Situation:** $100/month over a year; $300 upfront, then $100 per month from April.
**Setup:** `once(fixed 300)` plus `recurring(fixed 100, arrear, MONTHLY×1, start_date 1 Apr)`.
**Result:** $300 + 9 × $100 = $1,200. The once item snapshots the whole owner term; recurring items snapshot their own steps. No rule coverage override is required to express the three-month collection holiday. Usage is covered by the commitment; recognition follows the performance obligation separately.

### A7. Quarterly in advance

**Situation:** $1,200 a year, invoiced each quarter in advance.
**Setup:** `recurring(fixed 300, advance, QUARTERLY×1)`. Preset spread with even also gives $300 per quarter; proportional varies by the actual elapsed duration of each quarter.
**Result:** $300 on the 1 Jan, 1 Apr, 1 Jul and 1 Oct invoices. Each instalment's service period is its quarter.

### A8. Ramp across years

**Situation:** $1,000/month in year 1, $1,500/month in year 2.
**Setup:** two commitments on the same scope, Jan 2027 → Jan 2028 and Jan 2028 → Jan 2029, each with its own collection method. They don't overlap in time, so C12 and C14 don't apply.
**Result:** each year is billed independently. The second commitment can be created now, with a future `start_date`.

### A9. Stacked GPU reservations with time of day

**Situation:** 8 GPUs around the clock. From 25 Aug, an extra 32 GPUs from 9am to 9pm IST. Usage above both is billed on demand at $0.06 (the H100 price).
**Setup:**


| Commitment | Scope      | Type     | Value | Committed rate | Measurement         | Windows                            | Start  | Priority | Collection   |
| ---------- | ---------- | -------- | ----- | -------------- | ------------------- | ---------------------------------- | ------ | -------- | ------------ |
| C1         | price H100 | quantity | 8     | 0.048          | window (per minute) | all day                            | 1 Aug  | 1        | `per_period` |
| C2         | price H100 | quantity | 32    | 0.0491667      | window              | `[{09:00, 21:00}]`, `Asia/Kolkata` | 25 Aug | 2        | `per_period` |


Both are `quantity` + `window` on the same price, which satisfies C14.

**Result (August invoice):** these are the Linear doc figures, which test E9 must reproduce to the cent.


| Line                              | Before 25 Aug                    | From 25 Aug                         |
| --------------------------------- | -------------------------------- | ----------------------------------- |
| C1 within (8-GPU reserved, used)  | 275,674 min × 0.048 = $13,232.35 | 80,378 min × 0.048 = $3,858.14      |
| C2 within (32-GPU reserved, used) | —                                | 148,726 min × 0.0491667 = $7,312.36 |
| On-demand                         | 699,664 min × 0.06 = $41,979.84  | 117,399 min × 0.06 = $7,043.94      |
| C1 true-up (unused)               | 806 min × 0.048 = $38.69         | 262 min × 0.048 = $12.58            |
| C2 true-up (unused)               | —                                | 12,554 min × 0.0491667 = $617.24    |




### A10. One commitment across a group of prices

**Situation:** $1,000/month across API calls and storage together.
**Setup:** put both prices in one price group; scope `group`, `type=amount`, `value=1000`, `per_period`.
**Result:** combined usage counts towards the $1,000. Any on-demand amount shows on the price that comes later in `display_order`.

### A11. Quantity commitment on a meter, across several prices

**Situation:** 10,000 tokens/month on the "tokens" meter, priced by two models.
**Setup:** scope `meter`, `type=quantity`, `value=10000`, `committed_rate=0.002`, `per_period`.
**Result:**

- Tokens from either price count towards the 10,000, at $0.002 each.
- Unused tokens are billed as a true-up at $0.002.
- Tokens beyond 10,000 are billed at each price's own rate.



### A12. Discounted rate with no obligation (non-binding)

**Situation:** up to 1M units/month at $0.08 instead of $0.10, with no minimum.
**Setup:** scope `price`, `type=quantity`, `value=1000000`, `committed_rate=0.08`, `binding=false`. No collection method (C13).
**Result:**

- Up to 1M units are billed at $0.08; units above that at $0.10.
- No true-up, no total, no instalments.



### A13. Custom overage rate

**Situation:** on-demand usage at $0.07 instead of the list price of $0.06.
**Setup:** a scope that resolves to a single meter, with `overage_rate=0.07` (C8).
**Result:** on-demand lines are billed at $0.07.

### A14. Mid-period start: even vs proportional

**Situation:** $100/month commitment from 15 Jan 2027 to 1 Jan 2028, value $1,154.84; $300 upfront and $854.84 remaining monthly.

- Even: eleven $71.24 charges, final $71.20.
- Proportional to elapsed duration (351 days): first 17 days $41.40; February 28 days $68.19; March 31 days $75.50; other months vary by duration; final December $75.51 absorbs residual.
- These are collection splits, not recognized revenue or commitment-priced-period weights.
- Subscription delivery puts the mid-period upfront amount on the next eligible boundary; separate delivery issues it when due.

### A15. Early termination: the rules by collection method

**Situation:** a $1,200/year commitment terminated at the end of month 4. Accrued contractual principal = $400.


| Collection                                                           | Billed so far                       | `forfeit`                                        | `credit_note`                                            | `bill_remaining`                |
| -------------------------------------------------------------------- | ----------------------------------- | ------------------------------------------------ | -------------------------------------------------------- | ------------------------------- |
| `per_period`                                                         | $400 (4 months of within + true-up) | nothing more                                     | not allowed (C18)                                        | `settlement` line of $800       |
| `billing_terms`, `upfront`                                           | $1,200                              | nothing more; $800 retained over accrued principal; recognition is separate     | credit note for $800 against the upfront line            | nothing more (already billed)   |
| `billing_terms`, `partial_upfront` 300 + rest even (12 × $75 arrear) | $300 + 4 × $75 = $600               | nothing more; $200 retained over accrued principal                    | credit note for $200 against the latest instalment lines | `settlement` instalment of $600 |
| `billing_terms`, back-loaded `once(remainder, bill_on end)`          | $0                                  | `settlement` of $400 (accrued contractual amount not yet billed) | `settlement` of $400                                     | `settlement` of $1,200          |


Under `billing_terms`, scheduled instalments are always canceled.

### A16. Termination mid-period

**Situation:** the A15 setup, terminated on 15 Apr.
**Result:**

- April's commitment is prorated (14 of 30 days), so Accrued contractual principal = $346.67.
- Under `per_period`, April's final invoice computes the true-up against $46.67.
- Everything else follows A15 with $346.67 as accrued contractual principal.



### A17. Plan change

**Situation:** the customer changes plan mid-term.
**Result:** commitments terminate under their own rule on the change date (A15). New commitments are created on the new subscription if the contract continues.

### A18. Commitments that can't be combined

**Situation:** a $10k/month amount commitment on the whole subscription, plus an 8-GPU quantity reservation on one price inside it.
**Result:** rejected by C14, because a different type and measurement overlap on the same usage. This is deferred (ERD §15).

---



## Part B — Billing terms and instalments

B1–B8 and B13–B18 use commitments as the owner. B9–B12 check that the same tables work for owners planned in phase 5; they are not built in v1.

### B1. Collect on the exact date

**Situation:** the term starts 25 Aug, mid-month. The upfront payment must be invoiced on 25 Aug.
**Setup:** `invoice_delivery=separate_invoice`; `once(fixed 300)` + `recurring(remainder, arrear, MONTHLY×1, even)`.
**Result:**

- Every instalment gets its own `ONE_OFF` invoice on its `bill_on` date: $300 on 25 Aug, then the monthly instalments at each month end.
- With `subscription_invoice`, the $300 would have landed on the 1 Sep invoice.



### B2. Milestone with an unknown date

**Situation:** 50% at signing, 50% at go-live.
**Setup:** `once(fixed 50% of the value)` (sent as `percent: 50`, stored as a fixed amount) + `once(remainder, on_trigger)`.
**Result:**

- The second instalment is `manual`, with `bill_on` NULL until `POST …/billing-schedule-items/{id}/trigger` is called.
- If it is never triggered, it is billed at term end.



### B3. Payment holiday

**Situation:** nothing billed for 3 months, then the whole total evenly over months 4–12.
**Setup:** `recurring(remainder, arrear, MONTHLY×1, start_date 1 Apr, even)`.
**Result:**

- 9 equal instalments, starting at the end of April.
- Collection is postponed; performance obligations determine recognition independently.



### B4. Ramp inside one commitment

**Situation:** $50/month for January–June, then the rest evenly over July–December.
**Setup:** `recurring(fixed 50, arrear, MONTHLY×1, end_date 1 Jul)` + `recurring(remainder, arrear, MONTHLY×1, start_date 1 Jul, even)`.
**Result:** 6 × $50, then 6 equal instalments of the remainder.

### B5. Bring payments forward

**Situation:** a $1,200 commitment has 12 monthly arrear charges of $100. After five charges have been assigned to live invoices, the customer wants to bring the next $300 forward.
**Setup:** the UI adds a once rule for $300 now and retains rules for the four $100 charges after the next three. Preview and save the complete remaining rule set through `POST …/billing-terms/preview` and `PUT …/billing-terms`, with the preview's `expected_state` and explicit rule dates.
**Result:**

- Five invoice-assigned items are archived as schedule history but keep their amounts, execution status and references; their invoices remain live, including drafts/unpaid invoices.
- Seven old scheduled items are canceled/archived with their source IDs retained; replacements are one $300 item and four $100 items, all linked to the new rule rows.
- The total remains $1,200. Bringing forward is an amendment shortcut, not a second mutation path that leaves rules stale.

### B6. Edit the remaining arrangement

**Situation:** $1,200 over 1 Jan 2027–1 Jan 2028, $300 upfront plus 12 × $75 monthly in arrears. On 10 May, upfront and four monthly items are assigned: $600. The remaining $600 should be paid in four charges, every two months.
**Setup:** amendment preview and save with the full new remaining rule set: recurring remainder, `MONTHLY × 2`, arrear, even, `start_date = 1 May`, `end_date = 1 Jan`, plus the preview's `expected_state`.
**Result:**

- All thirteen old items are archived: five retain live invoice assignments and eight pending items are canceled. None are deleted.
- The replacement plan produces four $150 items on 1 Jul, 1 Sep, 1 Nov and 1 Jan. Every item references its fresh source rule; no January–April charge is regenerated.
- Service coverage starts on 1 May even though the edit is on 10 May. The explicit start defines the recurring step; its bill date is derived by arrear timing, with no independent cutoff.
- If another $75 is invoiced before save, the computed-state check rejects the stale command. The user refreshes and previews against $525 remaining.
- Every rule in the old current set is archived, including unchanged rules and the upfront rule. New rules have fresh IDs. Archived rules and canceled items remain visible in history; preserved invoice lines still reference their original schedule items and source rules. A single-rule edit uses this same full remaining-plan amendment.

### B7. Void after terms were edited

**Situation:** after B6, the user voids an old invoice carrying $75 of commitment principal.
**Result:**

- Its original schedule item is canceled/archived with reason `invoice_voided`; invoice references and the archived source rule remain resolvable. It is never returned to scheduled.
- Assigned principal drops to $525; the current upcoming plan is still $600; $75 is unallocated. The UI shows **$75 needs scheduling** and both delivery paths defer this commitment's pickup.
- Review/save a new upcoming arrangement allocating $675. The complete replacement clears the gap and resumes collection without reviving the old item.
- A repeated old-invoice void cannot touch replacement items. Reissue requires an explicit plan, not an automatic side effect of void.

### B8. Rounding

**Situation:** $1,000 over 3 monthly instalments.
**Setup:** `recurring(remainder, arrear, MONTHLY×1, even)` over a 3-month term.
**Result:** $333.33, $333.33 and $333.34.

### B9. Fixed fee in instalments (future owner: subscription line item)

**Situation:** a $12k annual platform fee on a monthly subscription, billed 30% now and the rest on 1 Jul.
**Setup:** rows owned by the fixed line item: `once(fixed 3600)` (sent as `percent: 30`) + `once(remainder, bill_on 1 Jul)`.
**Result:** the line item's regular fixed charge is suppressed, and the fee is collected through instalments.

### B10. Setup fee by milestone (future owner: a "scheduled charge" entity)

**Situation:** a $20k onboarding fee, 50% at signing and 50% at go-live, with no subscription involved.
**Setup:** a scheduled charge owns `once(fixed 10000)` (sent as `percent: 50`) + `once(remainder, on_trigger)`. Its delivery is `separate_invoice`, because there is no subscription (T14).
**Result:** two `ONE_OFF` invoices.

### B11. Credit purchase in instalments (future owner: credit purchase)

**Situation:** $50k of credits, paid quarterly.
**Setup:** `recurring(fixed 12500, advance, QUARTERLY×1)`, owned by the credit purchase.
**Result:** quarterly instalments. When credits are granted is the owner's decision. Wallets are unchanged.

### B12. Subscription-level fee (future owner: scheduled charge)

**Situation:** a $9k implementation fee billed with the first three invoices.
**Setup:** `recurring(fixed 3000, advance, MONTHLY×1, end_date 1 Apr)`, on a scheduled charge whose term starts 1 Jan.
**Result:** $3,000 on each of the 1 Jan, 1 Feb and 1 Mar invoices.

---



### B13. Editing before the first invoice

**Situation:** no item has been assigned to an invoice yet.
**Result:** the amendment can redistribute the full total. All rules in the old current set are archived. Scheduled items are canceled and replaced under a new published rule set; nothing is hard-deleted. Explicit new bill dates/ranges are still required, so editing does not implicitly backdate collection to the original term start.

### B14. A draft or unpaid invoice during editing

**Situation:** an item is already on a draft invoice, or the invoice is unpaid.
**Result:** the item is preserved and excluded from the remaining budget. To change it, first use the invoice correction flow, then refresh the editor. A void cancels/archives the item and creates a visible allocation gap; it cannot race the editor by rescheduling that item. Payment receipt does not control whether a scheduled charge is editable.

### B15. Editing milestones

**Situation:** one pending charge is waiting for go-live, and another has been triggered but not yet claimed.
**Result:** both are part of the replaceable scheduled set. Retain the first with an `on_trigger` rule and the second with a dated once rule, or explicitly redistribute their amounts. Triggering while an edit is open changes the computed state token; canceled milestone IDs cannot be triggered.

### B16. Edit retries and concurrent changes

**Situation:** two editors save against the same preview state, or a successful save's response is lost.
**Result:** competing commands cannot both apply. The first distinct save installs the new plan; a competing command gets 409. An identical current-plan retry is a no-op. An old retry after a different later edit conflicts, without restoring history.

### B17. Wrong initial setup with separate invoices

**Situation:** an incorrect initial plan has already produced a separate invoice, while future items have not yet been invoiced.
**Setup:** open **Correct billing setup** and draft/review the full desired arrangement before changing the invoice. The proposal shows which principal must be released and stays non-executable. Explicitly keep or correct/void the invoice through the normal invoice flow, then refresh state and preview/save against actual retained principal.
**Result:** future invoices were never pre-created. Old rules/items are archived; obsolete due jobs cannot create an invoice because they re-check current/published eligibility. A voided charge stays archived, the gap defers pickup, and saving a complete corrected plan resumes only the new items. A live retained invoice still counts toward the total and may continue its normal payment retries.

### B18. Separate-invoice worker races with editing

**Situation:** a due worker has loaded an old item while the user saves new terms.
**Result:** the shared owner lock determines the winner. If save wins, the worker sees the archived item and exits; if invoicing wins, it changes the authoritative state and the edit must be previewed again with that invoice included. A stale workflow retry cannot bypass the current-rule check.

### B19. Correct the full contract and reissue a past charge

**Situation:** $1,200 over 1 Jan 2027–1 Jan 2028. On 10 Feb, an incorrect $300 upfront invoice is already live; future charges total $900. The intended arrangement was $600 upfront and six $100 charges every two months in arrears.
**Setup:** draft the full $1,200 replacement, with once $600 dated 1 Jan 2027 and recurring fixed $100 per charge from 1 Jan 2027 to 1 Jan 2028, `MONTHLY × 2`, arrear. The conditional preview shows the old invoice must be corrected. Saving these $1,200 of explicit charges while the $300 remains assigned is rejected. Explicitly void the old invoice, refresh preview and save: all $1,200 is now available. Select the fresh $600 item for historical reissue; six fresh $100 items remain for their reviewed dates.
**Result:** full-contract correction from owner start is supported without collecting $1,500 or reviving the archived $300 source. Past-dated items are due now and must be shown in review. Reissue/worker races share the same unique claim. An old invoice may map to several new charges in other arrangements, so reissue uses explicit fresh IDs, not a guessed correspondence. Separate invoices use one-off delivery; subscription historical recalculation needs the planned source-aware extension. Unrelated mixed-invoice charges follow the normal invoice correction flow.

### B20. Show the whole contract while retaining an invoice

**Situation:** $1,200 target with $300 already assigned to a live invoice, which the user wants to keep.
**Result:** the editor shows the fixed $300 invoice and the $900 editable arrangement together. Save archives the old source set and creates new executable items totaling $900. It does not regenerate $1,200 of fresh charges and rely on archival to offset the existing invoice. If that invoice is later voided, its $300 becomes an allocation gap requiring another reviewed plan. A payment, refund or credit note alone does not free its schedule principal.

### B21. Utilization when collection is separate

**Situation:** a $100/month amount commitment over a year, $80 January consumption and $300 already invoiced upfront through billing terms.
**Result:** show $100 capacity, $80 consumed/$20 unused, 80% utilization, $0 covered usage charge and $0 true-up. Show "$300 invoiced through billing terms; $900 scheduled" once at commitment level, with invoice links/payment status. Do not repeat that $300 on every meter/point or describe a draft/unpaid invoice as paid cash. For a quantity commitment, use units for utilization and retain their committed-rate economic value. Payment receipt does not switch coverage on/off. With $130 consumption, $100 is covered and $30 on-demand; if a $75 payment is due, its invoice totals $105 before tax/discounts.

### B22. No usage, but a payment is due

**Situation:** no events in a period and a current $75 billing-schedule item is due.
**Result:** utilization is zero with unused contractual capacity, no period true-up, and a $75 invoice through the configured delivery. Empty usage or hidden $0 informational lines must not cause the invoice to be skipped. The subscription activity creates its normal draft; schedule-aware compute supplies the charge. Separate mode uses the one new scoped due workflow and its create/recover activity, followed by existing invoice processing.

### B23. Pause and resume payments

**Situation:** a $75 item comes due while the subscription is paused.
**Result:** the paused subscription invoice is $0/SKIPPED and separate delivery also defers. The item remains scheduled for its original date/amount, without a waiver or term regeneration. The actual resume invoice picks up overdue subscription-delivery items; the separate worker catches up once active. Existing assigned invoices keep their normal invoice/payment lifecycle. Pause is not a reason for one-off fallback or a true-up.

### B24. Trial ends after a scheduled payment date

**Situation:** an upfront payment falls due during trial.
**Result:** both delivery modes defer it until trial end. A zeroed trial preview cannot claim its positive principal. Once active, the real trial-end invoice or separate worker collects the original amount once. Draft/incomplete inline subscriptions likewise persist terms/items atomically but cannot independently collect before activation.

## Not covered in v1


| Situation                                                      | Why                                    | Where it goes                         |
| -------------------------------------------------------------- | -------------------------------------- | ------------------------------------- |
| Renewal of a commitment                                        | Out of v1 scope                        | Create a new commitment. Deferred.    |
| Spend commitment checked once over the whole term              | Measured per term, not per period      | Deferred: term-cumulative commitments |
| Prepaid money that pays for *any* usage, with rollover         | Needs a drawdown ledger                | Deferred                              |
| Mixing amount and quantity commitments on the same usage (A18) | No well-defined stacking order (D6)    | Deferred                              |
| A recurring series starting off a billing boundary             | Steps must line up with invoices (T7)  | Use `once` rows                       |
| Instalment amounts that depend on usage or seats               | Instalments collect a fixed total      | Out of scope                          |
| A different due date (Net terms) per instalment                | This is an invoice-level setting today | Deferred                              |
| Carrying commitments across a plan change                      | —                                      | Deferred                              |


