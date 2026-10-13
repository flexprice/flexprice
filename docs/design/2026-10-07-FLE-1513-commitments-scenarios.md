# FLE-1513 — Commitments & Billing Terms: Scenarios

Companion to the [ERD](./2026-10-07-FLE-1513-commitments-revamp-erd.md). Section numbers (§) and rule IDs (C…, T…) refer to the ERD.

Each scenario gives:

- the situation;
- the setup: commitment fields and, where relevant, `billing_terms` rows;
- the result: what each invoice shows.

Every billing-terms example below is also an expansion test case (ERD §13.3). Stored rows are always `fixed` or `remainder`; percentages are only an input convenience. `POST /v1/commitments/preview` shows the resulting instalments before anything is created.

## Conventions

- Monthly subscription anchored on the 1st, USD.
- Under `billing_terms`, instalments go on the subscription invoice (`instalment_invoicing = subscription_invoice`) unless stated otherwise.
- **Advance** means billed on the invoice issued at the start of the step. **Arrear** means billed on the invoice issued at its end.
- Rows are written in shorthand:


| Notation                                        | Row                                                                                                                     |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `once(fixed 300)`                               | `kind=once, value_type=fixed, value=300`, `bill_on` = commitment `start_date`                                           |
| `once(fixed 300, covers 1 Jan–1 Apr)` | `… covers_from=1 Jan, covers_to=1 Apr`: the service this instalment pays for (default: the whole term) |
| `once(fixed 600, bill_on 1 Jul)` | `kind=once, value_type=fixed, value=600, bill_on=1 Jul` |
| `once(remainder, on_trigger)`                   | `kind=once, value_type=remainder, on_trigger=true`                                                                      |
| `recurring(fixed 300, advance, QUARTERLY×1)`    | `kind=recurring, value_type=fixed, value=300` (per instalment), `bill_at=advance, interval=QUARTERLY, interval_count=1` |
| `recurring(remainder, arrear, MONTHLY×1, even)` | `kind=recurring, value_type=remainder, bill_at=arrear, interval=MONTHLY, interval_count=1, split=even`                  |
| `…, starts_on 1 Apr` / `…, ends_on 1 Jul`       | `starts_on` / `ends_on` set (both must be billing boundaries)                                                           |


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
- Monthly invoices show within-commitment usage at $0, with the line "covered by commitment — instalment of 1 Jan (Jan–Dec)", plus on-demand usage.
- No true-up.
- **Analytics (ERD §9):** each month reports usage value (used within, unused (prepaid), on-demand) exactly as under `per_period`. The billed amount is $1,200 in January and only on-demand usage afterwards. Deferred revenue starts at $1,100 after January and falls by $100 each month.



### A3. Partial upfront, rest equally

**Situation:** $1,000 over 10 months. $300 upfront, the remaining $700 split equally.
**Setup:**

- `value=100`, term 1 Jan → 1 Nov, `billing_terms`.
- Preset `partial_upfront` (upfront fixed 300; rest arrear, even), which expands to `once(fixed 300)` + `recurring(remainder, arrear, MONTHLY×1, even)`.

**Result:**

- $300 on the 1 Jan invoice, then $70 on each month-end invoice (10 instalments), for a total of $1,000.
- February's usage is billed on the 1 Mar invoice. Its within-commitment line is $0 and lists two covering instalments: the upfront instalment (service period Jan–Oct) and February's $70 instalment, which is on that same invoice.



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
- Optionally `instalment_invoicing=separate_invoice`, to bill on the exact dates.

**Result with** `subscription_invoice`**:**

- The $400 lands on the 1 Feb invoice.
- The $350 lands on the 1 Jul invoice.
- The $250 waits for a trigger, or is billed at term end.

**Result with** `separate_invoice`**:** one-off invoices on 15 Jan and 20 Jun, and on the trigger date.

### A6. Deposit covering the first months, then monthly

**Situation:** $100/month for 12 months. A $300 deposit covers January–March; monthly billing resumes in April.
**Setup:** `once(fixed 300, covers_from 1 Jan, covers_to 1 Apr)` + `recurring(fixed 100, arrear, MONTHLY×1, starts_on 1 Apr)`.
**Result:**

- $300 on 1 Jan, then $100 at the end of each month from April to December. 300 + 9 × 100 = 1,200.
- January to March invoices show within-commitment usage at $0, "covered by the deposit of 1 Jan (Jan–Mar)", and bill only on-demand usage. From April, each month is covered by its own instalment.
- **Analytics (ERD §9.3):** for January with $80 of usage, the usage value is $80 used plus $20 unused, while the billed amount on the January usage lines is $0. The $300 deposit is billed at commitment level. Spend alerts and the wallet's pending charges count only on-demand usage.



### A7. Quarterly in advance

**Situation:** $1,200 a year, invoiced each quarter in advance.
**Setup:** `recurring(fixed 300, advance, QUARTERLY×1)`. Alternatively, preset `spread` (advance, QUARTERLY×1, proportional) gives the same result on equal quarters.
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

**Situation:** $100/month from 15 Jan for 12 periods, $300 upfront, and the rest monthly.
**Setup:** term 15 Jan → 1 Jan 2028, total $1,154.84; `once(fixed 300)` + `recurring(remainder, arrear, MONTHLY×1, <split>)`.
**Result:**


| Split          | Step 1 (15 Jan–1 Feb) | Steps 2–12                             |
| -------------- | --------------------- | -------------------------------------- |
| `even`         | $71.24                | $71.24 each, except the last at $71.20 |
| `proportional` | $40.59                | $74.02 each, except the last at $74.05 |


- The $300 lands on the 1 Feb invoice, the first boundary after 15 Jan. Use `instalment_invoicing=separate_invoice` to bill it on 15 Jan.
- Under `per_period` instead, period 1's true-up is computed against $54.84.



### A15. Early termination: the rules by collection method

**Situation:** a $1,200/year commitment terminated at the end of month 4. Earned = $400.


| Collection                                                           | Billed so far                       | `forfeit`                                        | `credit_note`                                            | `bill_remaining`                |
| -------------------------------------------------------------------- | ----------------------------------- | ------------------------------------------------ | -------------------------------------------------------- | ------------------------------- |
| `per_period`                                                         | $400 (4 months of within + true-up) | nothing more                                     | not allowed (C18)                                        | `settlement` line of $800       |
| `billing_terms`, `upfront`                                           | $1,200                              | nothing more; $800 recognised at termination     | credit note for $800 against the upfront line            | nothing more (already billed)   |
| `billing_terms`, `partial_upfront` 300 + rest even (12 × $75 arrear) | $300 + 4 × $75 = $600               | nothing more; $200 recognised                    | credit note for $200 against the latest instalment lines | `settlement` instalment of $600 |
| `billing_terms`, back-loaded `once(remainder, bill_on end)`          | $0                                  | `settlement` of $400 (earned but not yet billed) | `settlement` of $400                                     | `settlement` of $1,200          |


Under `billing_terms`, scheduled instalments are always canceled.

### A16. Termination mid-period

**Situation:** the A15 setup, terminated on 15 Apr.
**Result:**

- April's commitment is prorated (14 of 30 days), so Earned = $346.67.
- Under `per_period`, April's final invoice computes the true-up against $46.67.
- Everything else follows A15 with $346.67 as Earned.



### A17. Plan change

**Situation:** the customer changes plan mid-term.
**Result:** commitments terminate under their own rule on the change date (A15). New commitments are created on the new subscription if the contract continues.

### A18. Commitments that can't be combined

**Situation:** a $10k/month amount commitment on the whole subscription, plus an 8-GPU quantity reservation on one price inside it.
**Result:** rejected by C14, because a different type and measurement overlap on the same usage. This is deferred (ERD §15).

---



## Part B — Billing terms and instalments

B1–B8 use commitments as the owner. B9–B12 check that the same tables work for owners planned in phase 5; they are not built in v1.

### B1. Collect on the exact date

**Situation:** the term starts 25 Aug, mid-month. The upfront payment must be invoiced on 25 Aug.
**Setup:** `instalment_invoicing=separate_invoice`; `once(fixed 300)` + `recurring(remainder, arrear, MONTHLY×1, even)`.
**Result:**

- Every instalment gets its own `ONE_OFF` invoice on its `bill_on` date: $300 on 25 Aug, then the monthly instalments at each month end.
- With `subscription_invoice`, the $300 would have landed on the 1 Sep invoice.



### B2. Milestone with an unknown date

**Situation:** 50% at signing, 50% at go-live.
**Setup:** `once(fixed 50% of the value)` (sent as `percent: 50`, stored as a fixed amount) + `once(remainder, on_trigger)`.
**Result:**

- The second instalment is `manual`, with `bill_on` NULL until `POST …/instalments/{id}/trigger` is called.
- If it is never triggered, it is billed at term end.



### B3. Payment holiday

**Situation:** nothing billed for 3 months, then the whole total evenly over months 4–12.
**Setup:** `recurring(remainder, arrear, MONTHLY×1, starts_on 1 Apr, even)`.
**Result:**

- 9 equal instalments, starting at the end of April.
- Deferred revenue is negative (earned but not yet billed) until the instalments catch up.



### B4. Ramp inside one commitment

**Situation:** $50/month for January–June, then the rest evenly over July–December.
**Setup:** `recurring(fixed 50, arrear, MONTHLY×1, ends_on 1 Jul)` + `recurring(remainder, arrear, MONTHLY×1, starts_on 1 Jul, even)`.
**Result:** 6 × $50, then 6 equal instalments of the remainder.

### B5. Bring payments forward

**Situation:** a $1,200 commitment billed as 12 monthly arrear instalments of $100. In month 6, the customer wants to pay 3 more months now.
**Setup:** `POST /v1/commitments/{id}/instalments/bring-forward {amount: 300}`.
**Result:**

- A $300 instalment, billed now.
- The next three scheduled instalments (months 6–8) drop to $0 and are canceled.
- The total is unchanged.



### B6. Re-plan

**Situation:** the customer asks to move the last two instalments to new dates.
**Setup:** `PUT /v1/commitments/{id}/instalments` with the full new list of scheduled instalments. They must add up to the total minus what has already been invoiced.
**Result:**

- Scheduled instalments are replaced atomically. The new ones have `billing_term_id = NULL`.
- Invoiced instalments and the billing terms rows don't change.



### B7. Voided instalment invoice

**Situation:** the invoice carrying instalment 3 is voided.
**Result:** instalment 3 returns to `scheduled` and is billed again on the next run.

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
**Setup:** `recurring(fixed 3000, advance, MONTHLY×1, ends_on 1 Apr)`, on a scheduled charge whose term starts 1 Jan.
**Result:** $3,000 on each of the 1 Jan, 1 Feb and 1 Mar invoices.

---



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


