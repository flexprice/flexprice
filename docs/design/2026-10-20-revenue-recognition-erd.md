# Revenue Recognition — Phase 1 (ERD)

**Author:** Harshit Gupta

**Dated**: 20th Oct, 2026

## The Problem

Flexprice bills, but does not account for what it bills. An invoice says what a customer owes; it does not say when that money is earned. Finance needs:

- **Recognized revenue** — earned by delivering the service, independent of invoice and payment dates.
- **Deferred revenue** — billed but not yet earned (e.g. an annual prepaid invoice).
- **Unbilled revenue** — earned but not yet invoiced (e.g. usage billed in arrears).
- An auditable, balanced ledger behind those numbers, with rollforwards and a recognition waterfall — without requiring an ERP connection.



## Solution

Subscription-native recognition on top of the billing engine: each subscription becomes a contract, its line items become performance obligations with recognition schedules, and billing and earning events post balanced, immutable journal entries to an internal subledger. Reports are queries over that journal.

Out of Phase 1: contract extraction, SSP allocation, ERP posting, period close, multi-entity books, FX, free-credit contra revenue, breakage, deferred waterfall, ARR reporting.

Steps:

1. Product & Price
2. Contract
3. PerformanceObligation & RevenueSchedule
4. AccountingCalendar & AccountingPeriod
5. LedgerAccount
6. AccountingEvent & Journal
7. Rollforward & Waterfall

---



### Step 1 — Product & Price

A reusable commercial identity for what is sold. Phase 1 uses it only as catalog; Phase 2 hangs ERP item mappings on it.

#### DB schema

```sql
-- new
CREATE TABLE products (
    id             varchar(50)  PRIMARY KEY,   -- prefix "product"
    tenant_id      varchar(50)  NOT NULL,
    environment_id varchar(50)  NOT NULL,
    name           varchar(255) NOT NULL,
    description    text,
    lookup_key     varchar(255) NOT NULL,
    status         varchar(50)  NOT NULL DEFAULT 'published',
    created_at     timestamptz  NOT NULL,
    created_by     varchar(50),
    updated_at     timestamptz  NOT NULL,
    updated_by     varchar(50)
);

-- create-by-key, get-by-key
CREATE UNIQUE INDEX ON products (tenant_id, environment_id, lookup_key)
    WHERE status = 'published';
-- list / search
CREATE INDEX ON products (tenant_id, environment_id);

-- changed
ALTER TABLE prices ADD COLUMN product_id varchar(50);  -- nullable; set once, then immutable

-- list prices by product; delete-guard lookup
CREATE INDEX ON prices (tenant_id, environment_id, product_id);
```



#### APIs


| Endpoint                   | Role                              |
| -------------------------- | --------------------------------- |
| `POST /v1/products`        | Create                            |
| `GET /v1/products/{id}`    | Get                               |
| `GET /v1/products`         | List                              |
| `POST /v1/products/search` | Filtered search (`@x-scope read`) |
| `PUT /v1/products/{id}`    | Update name, description          |
| `DELETE /v1/products/{id}` | Archive                           |


New RBAC entity `EntityProduct`; `Products` tag added to `.speakeasy/mcp/allowed-tags.yaml`.

#### Invariants

- `lookup_key` is required on create and immutable, like addons and features; unique among published products.
- Prices without a product stay valid.
- A product with any published price referencing it cannot be deleted.
- Subscription overrides and price versions copy `product_id` from their source price at creation.
- A price's `product_id` can be set once (on create, or on update while empty); after that it never changes.



#### Services

`ProductService` (new) — owns product validation and the delete guard.

```go
type ProductService interface {
    CreateProduct(ctx context.Context, req dto.CreateProductRequest) (*dto.ProductResponse, error)
    GetProduct(ctx context.Context, id string) (*dto.ProductResponse, error)
    GetProductByLookupKey(ctx context.Context, lookupKey string) (*dto.ProductResponse, error)
    GetProducts(ctx context.Context, filter *types.ProductFilter) (*dto.ListProductsResponse, error)
    UpdateProduct(ctx context.Context, id string, req dto.UpdateProductRequest) (*dto.ProductResponse, error)
    DeleteProduct(ctx context.Context, id string) error
}
```



### Step 2 — Contract

The accounting agreement a subscription establishes.

#### DB schema

```sql
CREATE TABLE contracts (
    id                 varchar(50) PRIMARY KEY,   -- prefix "contract"
    tenant_id          varchar(50) NOT NULL,
    environment_id     varchar(50) NOT NULL,
    customer_id        varchar(50) NOT NULL,      
    currency           varchar(3)  NOT NULL,      
    term_start         timestamptz,               -- set on first activation
    term_end           timestamptz,               -- as per the contract
    ended_at           timestamptz,               -- contract actually ended
    contract_status    varchar(20) NOT NULL,      -- draft | active | ended | cancelled
    status             varchar(50) NOT NULL DEFAULT 'published',
    created_at         timestamptz NOT NULL,
    created_by         varchar(50),
    updated_at         timestamptz NOT NULL,
    updated_by         varchar(50)
);

-- contracts for a customer
CREATE INDEX ON contracts (tenant_id, environment_id, customer_id);

CREATE TABLE contract_associations (
    id             varchar(50) PRIMARY KEY,   -- prefix "contract_assoc"
    tenant_id      varchar(50) NOT NULL,
    environment_id varchar(50) NOT NULL,
    contract_id    varchar(50) NOT NULL,
    entity_type    varchar(50) NOT NULL,      -- subscription
    entity_id      varchar(50) NOT NULL,
    status         varchar(50) NOT NULL DEFAULT 'published',
    created_at     timestamptz NOT NULL,
    created_by     varchar(50)
);

-- contract of a subscription; an entity belongs to one contract
CREATE UNIQUE INDEX ON contract_associations (tenant_id, environment_id, entity_type, entity_id)
    WHERE status = 'published';
-- entities of a contract
CREATE INDEX ON contract_associations (tenant_id, environment_id, contract_id);
```



#### APIs


| Endpoint                    | Role                                              |
| --------------------------- | ------------------------------------------------- |
| `GET /v1/contracts/{id}`    | Get, with associations                            |
| `POST /v1/contracts/search` | Filter by customer, subscription, contract status |


Read-only: contracts are derived from subscriptions.

#### Invariants

- A contract links to its entities through associations. Phase 1 has exactly one per contract, `entity_type = subscription`; later sources (more subscriptions, standalone invoices, order forms) add entity types, not columns.
- An entity belongs to at most one published contract and never moves to another.
- Created with the subscription, in any status: `draft`, or `active` if the subscription is created active.
- `draft → active` on the subscription's first activation (draft activation, incomplete → active, trial → active); `term_start` is set then.
- A later `active → incomplete` or pause keeps the contract `active`.
- `active → ended` (`ended_at` set) only when the subscription actually stops: immediate cancel, or the period cron applying a deferred cancel / reaching `end_date`. A scheduled-but-pending cancel does not end it.
- `draft → cancelled` when the subscription is cancelled before ever activating.
- Archiving a draft subscription (checkout abandonment) archives its draft contract.
- A `draft` contract can carry billing journals (invoice, credit note, payment, refund; Step 6); recognition journals post only on `active` or `ended` contracts. Earnings for service delivered before activation catch up on the first recognition run after it.
- Plan change (v2) is in place and keeps the contract.
- Contract status changes cascade to its POBs (Step 3).
- Ending a contract keeps its POBs, schedules and journals.
- Nothing is written unless the environment has revenue recognition enabled.



#### Services

`ContractService` (new) — `SyncForSubscription` creates the contract if missing and derives its status from the subscription's current state; idempotent. Runs async after the subscription create, activation and termination transactions commit; errors are logged, never fail the subscription write. Later steps call it before using a contract, so a missed write heals.

```go
type ContractService interface {
    SyncForSubscription(ctx context.Context, sub *subscription.Subscription) (*contract.Contract, error)
    GetContract(ctx context.Context, id string) (*dto.ContractResponse, error)
    GetContracts(ctx context.Context, filter *types.ContractFilter) (*dto.ListContractsResponse, error)
}
```



#### Related changes

- New setting `revenue_recognition_config` gates every revrec write per environment.



### Step 3 — PerformanceObligation & RevenueSchedule

A POB is a continuing promise inside a contract, made of one or more subscription line items. A revenue schedule is a persisted, versioned allocation of consideration over a finite interval; per-period recognition amounts are derived from it, never stored.

Four reporting categories, one engine:

- **Actual** — posted recognition journals (Step 6).
- **Pending** — earned but not yet posted: the open month to date, and ended months the daily run has not posted.
- **Scheduled** — persisted schedule segments not yet earned.
- **Extrapolated** — recurring fixed POBs with no end date only, after the last segment, valued on read by the billing engine. Never persisted.



#### DB schema

```sql
CREATE TABLE performance_obligations (
    id                  varchar(50)  PRIMARY KEY,  -- prefix "pob"
    tenant_id           varchar(50)  NOT NULL,
    environment_id      varchar(50)  NOT NULL,
    contract_id         varchar(50)  NOT NULL,
    name                varchar(255) NOT NULL,     
    recognition_method  varchar(20)  NOT NULL,     -- straight_line | as_consumed | point_in_time
    amortization_method varchar(30)  NOT NULL,     -- daily | period_even | period_even_prorated
    product_id          varchar(50),               -- first line item's price.product_id; null = uncategorized
    service_start       timestamptz  NOT NULL,
    service_end         timestamptz,               -- set once every associated line item has ended
    pob_status          varchar(20)  NOT NULL,     -- draft | active | satisfied | cancelled
    status              varchar(50)  NOT NULL DEFAULT 'published',
    created_at          timestamptz  NOT NULL,
    created_by          varchar(50),
    updated_at          timestamptz  NOT NULL,
    updated_by          varchar(50)
);

-- POBs of a contract (sync, status cascade, search)
CREATE INDEX ON performance_obligations (tenant_id, environment_id, contract_id);

CREATE TABLE performance_obligation_associations (
    id             varchar(50) PRIMARY KEY,        -- prefix "pob_assoc"
    tenant_id      varchar(50) NOT NULL,
    environment_id varchar(50) NOT NULL,
    pob_id         varchar(50) NOT NULL,
    scope          varchar(20) NOT NULL,           -- billing
    entity_type    varchar(50) NOT NULL,           -- subscription_line_item
    entity_id      varchar(50) NOT NULL,
    status         varchar(50) NOT NULL DEFAULT 'published',
    created_at     timestamptz NOT NULL,
    created_by     varchar(50)
);

-- resolve the POB of a line item; one POB per entity per scope
CREATE UNIQUE INDEX ON performance_obligation_associations (tenant_id, environment_id, scope, entity_type, entity_id)
    WHERE status = 'published';
-- entities of a POB (forecast, service term)
CREATE INDEX ON performance_obligation_associations (tenant_id, environment_id, pob_id);

CREATE TABLE revenue_schedules (                -- append-only
    id                 varchar(50)   PRIMARY KEY,  -- prefix "rev_sched"
    tenant_id          varchar(50)   NOT NULL,
    environment_id     varchar(50)   NOT NULL,
    pob_id             varchar(50)   NOT NULL,
    sequence           bigint        NOT NULL,     -- monotonic per POB; highest covering sequence wins
    period_start       timestamptz   NOT NULL,     -- inclusive
    period_end         timestamptz   NOT NULL,     -- exclusive; equals period_start for point_in_time
    allocated_amount   numeric(20,8) NOT NULL,     
    source_entity_type varchar(50)   NOT NULL,     -- subscription_line_item | invoice_line_item
    source_entity_id   varchar(50)   NOT NULL,
    status             varchar(50)   NOT NULL DEFAULT 'published',
    created_at         timestamptz   NOT NULL,
    created_by         varchar(50)
);

-- effective schedule of a POB (recognition run, waterfall); sequence allocated under a POB row lock
CREATE UNIQUE INDEX ON revenue_schedules (tenant_id, environment_id, pob_id, sequence);
-- idempotency: one segment per source per interval
CREATE UNIQUE INDEX ON revenue_schedules (tenant_id, environment_id, pob_id, source_entity_type, source_entity_id, period_start)
    WHERE status = 'published';
```



#### Domain model

```go
// internal/types
type RecognitionMethod string // existing; values renamed
const (
    RecognitionMethodStraightLine RecognitionMethod = "straight_line"
    RecognitionMethodAsConsumed   RecognitionMethod = "as_consumed"
    RecognitionMethodPointInTime  RecognitionMethod = "point_in_time"
)

type AmortizationMethod string
const (
    AmortizationDaily              AmortizationMethod = "daily"
    AmortizationPeriodEven         AmortizationMethod = "period_even"
    AmortizationPeriodEvenProrated AmortizationMethod = "period_even_prorated" // default, set in code
)

type POBStatus string // draft | active | satisfied | cancelled

type CatchUpMode string
const (
    CatchUpProspective   CatchUpMode = "prospective"   // replacement starts at the effective date
    CatchUpRetrospective CatchUpMode = "retrospective" // replacement covers the original interval
)
```



#### Schedule rules


| Event                                                                                                                               | Schedule effect                                                                                                                                                                                                                                                                                                    |
| ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Billing period starts (subscription created / activated, period cron renewal)                                                       | Open a segment `[period_start, period_end)` per fixed POB, valued with the billing engine's line calculation (price × quantity, coupons applied). Advance and arrears alike.                                                                                                                                       |
| One-time charge                                                                                                                     | Same, with `period_start = period_end`.                                                                                                                                                                                                                                                                            |
| Usage line item                                                                                                                     | No segments. Earned comes from `revenue_facts` (below).                                                                                                                                                                                                                                                            |
| Modification (price override, line item change, plan change v2, line item removal, cancellation, coupon added / removed mid-period) | New line items join the POB of their predecessor or get a new POB. Append a superseding segment over the affected span: prospective by default, from the effective date to the period end.                                                                                                                         |
| Invoice finalized (`SUBSCRIPTION`)                                                                                                  | If a fixed line's net (`amount − line_item_discount − invoice_level_discount`) differs from the effective schedule over the line's period, append a superseding segment with the invoiced net, retrospective over that period, sourced `invoice_line_item`. Prepaid credits applied are settlement, not deviation. |
| Invoice voided                                                                                                                      | If the voided line set the effective segment, append a superseding segment with the billing engine's valuation, sourced `subscription_line_item`.                                                                                                                                                                  |
| Invoice finalized / voided (`ONE_OFF`)                                                                                              | None. Subscription-linked proration lines are already scheduled by the modification that produced them. Standalone invoices have no contract or POB, so they post billing journals only (`contract_id` null, Step 6).                                                                                              |


Recognition method and `product_id` are derived once from the first line item's price:


| Price                           | Method          |
| ------------------------------- | --------------- |
| FIXED, recurring                | `straight_line` |
| FIXED, `BILLING_PERIOD_ONETIME` | `point_in_time` |
| USAGE                           | `as_consumed`   |


**Usage evidence (**`as_consumed`**).** Read from `revenue_facts` by `sub_line_item_id` for the POB's line items:

- Open billing period: earned = Σ PROVISIONAL `net_amount` (the period-to-date bill), which accrues unbilled.
- Invoiced period: each day earns its FINAL row (marginal), period_only rows spread evenly by day, and the residual (invoice line net − Σ rows) lands on the period's last day. Already-reported days never move.
- Without revenue analytics enabled, there is no accrual and the invoiced amount spreads evenly.

**Earned to date.** `earned(pob, asOf)` evaluates the effective curve cumulatively: for each date the highest-sequence covering segment applies, using its original full-interval curve. Daily amounts are `earned(day_end) − earned(day_start)`, never rounded per day.

#### APIs


| Endpoint                                  | Role                                                     |
| ----------------------------------------- | -------------------------------------------------------- |
| `GET /v1/performance_obligations/{id}`    | Get, with associations and schedule segments             |
| `POST /v1/performance_obligations/search` | Filter by contract, customer, status, recognition method |


Read-only: POBs and schedules are derived from subscriptions and billing.

#### Invariants

- Created with the contract as `draft`; status cascades from the contract: `draft → active` on activation, `draft → cancelled` if the contract is cancelled. `active → satisfied` once every associated line item has ended and `service_end` has passed.
- Recognition runs on `active` and `satisfied` POBs (a satisfied POB still posts its last month and later corrections); never on `draft` or `cancelled`.
- An entity belongs to at most one POB per scope; associations stay within the POB's contract.
- A new line item with a `predecessor_line_item_id` joins its predecessor's POB; otherwise it gets a new POB. Plan change v2 carried lines keep their row, so keep their POB.
- Recognition and amortization methods are locked once recognition starts.
- Segments are append-only and finite: `period_start ≤ period_end`, within the POB's service term. A higher sequence supersedes overlapping dates; earlier segments are never re-amortized over their surviving dates.
- Only the current billing period is persisted; future renewals are extrapolated on read (Step 7).
- A schedule changes async after the subscription write or invoice that caused it commits (post-commit hook, like `revenue_facts`). A failed schedule write is logged and never fails the source write; a repair job reconciles line items and invoices against segments.
- Nothing is written unless the environment has revenue recognition enabled.



#### Services

`PerformanceObligationService` (new) — owns POB identity, associations and schedule segments, and evaluates earned-to-date for the recognition run (Step 6).

```go
type PerformanceObligationService interface {
    SyncForSubscription(ctx context.Context, sub *subscription.Subscription) ([]*pob.PerformanceObligation, error)
    OpenPeriod(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) error
    ApplyModification(ctx context.Context, sub *subscription.Subscription, effectiveAt time.Time, mode types.CatchUpMode) error
    ApplyInvoice(ctx context.Context, inv *invoice.Invoice) error // finalize and void
    Reconcile(ctx context.Context, contractID string) error       // repair job
    EarnedToDate(ctx context.Context, pobID string, asOf time.Time) (decimal.Decimal, error)
    GetPerformanceObligation(ctx context.Context, id string) (*dto.PerformanceObligationResponse, error)
    GetPerformanceObligations(ctx context.Context, filter *types.PerformanceObligationFilter) (*dto.ListPerformanceObligationsResponse, error)
}
```

Deferred: commitments and true-ups, net-negative proration (wallet credit), SSP allocation (Phase 3).

### Step 4 — AccountingCalendar & AccountingPeriod

One monthly calendar per environment. The calendar is configuration, held in the `revenue_recognition_config` setting. Periods are materialized rows that journal entries reference.

#### DB schema

```sql
CREATE TABLE accounting_periods (
    id             varchar(50) PRIMARY KEY,   -- prefix "acct_period"
    tenant_id      varchar(50) NOT NULL,
    environment_id varchar(50) NOT NULL,
    period_start   date        NOT NULL,      -- 1st of the month; the period runs to the next month start
    period_status  varchar(20) NOT NULL,      -- open | closed (closed reserved for close, deferred)
    status         varchar(50) NOT NULL DEFAULT 'published',
    created_at     timestamptz NOT NULL,
    created_by     varchar(50),
    updated_at     timestamptz NOT NULL,
    updated_by     varchar(50)
);

-- period of a business date (posting); idempotent materialization
CREATE UNIQUE INDEX ON accounting_periods (tenant_id, environment_id, period_start)
    WHERE status = 'published';
```



#### Domain model

```go
// internal/types
type AccountingPeriodStatus string
const (
    AccountingPeriodStatusOpen   AccountingPeriodStatus = "open"
    AccountingPeriodStatusClosed AccountingPeriodStatus = "closed" // reserved; close is deferred
)

// value of setting key revenue_recognition_config
type RevenueRecognitionConfig struct {
    Enabled    bool       `json:"enabled"`
    FiscalMonth string    `json:"fiscal_month"` // "YYYY-MM";
}
```



#### Amortization arithmetic

A segment covers local dates `[ds, de)` with amount `A`. Each covered day `d` has a weight:


| Method                 | `w(d)`                                 | Effect                                                       |
| ---------------------- | -------------------------------------- | ------------------------------------------------------------ |
| `daily`                | 1                                      | Equal per day                                                |
| `period_even`          | 1 / days of the segment in `d`'s month | Equal per month touched                                      |
| `period_even_prorated` | 1 / days in `d`'s month                | Equal per full month; partial months by the share they cover |


- `earned_seg(asOf) = A × Σ_{d < asOf} w(d) / Σ_{d ∈ seg} w(d)`. For each day, the highest-sequence covering segment applies (Step 3).
- `point_in_time`: all of `A` on `ds`.

Example: $300 over `[Jan 15, Apr 15)`, `period_even_prorated`.


| Month     | Days covered | `w(d)` | Month weight | Earned      |
| --------- | ------------ | ------ | ------------ | ----------- |
| Jan       | 17           | 1/31   | 0.548        | $54.56      |
| Feb       | 28           | 1/28   | 1            | $99.50      |
| Mar       | 31           | 1/31   | 1            | $99.50      |
| Apr       | 14           | 1/30   | 0.467        | $46.43      |
| **Total** |              |        | **3.015**    | **$300.00** |


On Mar 10: `300 × (0.548 + 1 + 9/31) / 3.015 = $182.95`. The same segment under `daily` is $3.33 a day for 90 days; under `period_even` it is $75 for each of the four months.

- Never rounded per day. A period's recognition amount is `round(earned(period_end)) − round(earned(period_start))` at currency precision, so periods sum exactly to `A`.



#### APIs


| Endpoint                             | Role                         |
| ------------------------------------ | ---------------------------- |
| `POST /v1/accounting_periods/search` | Filter by status, date range |


The calendar is configured through the existing settings API.

#### Invariants

- One calendar per environment; monthly periods only. Periods are contiguous, non-overlapping `[start, end)` months.
- Periods are rows because journal entries reference them and they will carry the close lifecycle (`open → closed`) once close ships; Phase 1 never closes one. Their end is always the next month start.
- Periods are materialized on demand inside the posting transaction (insert on conflict do nothing); a missing period never fails a post.
- `fiscal_month` is the first period finance reports on: at most next month (the default). Enabling rebuilds every contract from its own start into its real periods, so the `fiscal_month` opening balance is the sum of everything posted before it; there are no opening entries. It is immutable once any journal entry exists.
- An entry posts to the period containing its business date. If that period is closed, it posts to the first open period after it.
- Nothing is written unless the environment has revenue recognition enabled.



#### Services

`AccountingPeriodService` (new) — resolves and materializes posting periods.

```go
type AccountingPeriodService interface {
    PeriodFor(ctx context.Context, businessDate time.Time) (*accountingperiod.AccountingPeriod, error)
    GetAccountingPeriods(ctx context.Context, filter *types.AccountingPeriodFilter) (*dto.ListAccountingPeriodsResponse, error)
}
```



### Step 5 — LedgerAccount

A fixed set of account roles defined in code. Journal lines reference an account by key. Tenants cannot add or change accounts; ERP GL codes map onto these roles in Phase 2.

#### Domain model

Normal side and statement follow the class: asset and contra_revenue are debit-normal, the rest credit-normal; revenue and contra_revenue are income statement, the rest balance sheet.

```go
// internal/types
type LedgerAccountKey string
const (
    LedgerAccountCash                 LedgerAccountKey = "cash"
    LedgerAccountReceivable           LedgerAccountKey = "accounts_receivable"
    LedgerAccountUnbilledReceivable   LedgerAccountKey = "unbilled_receivable"
    LedgerAccountDeferredRevenue      LedgerAccountKey = "deferred_revenue"
    LedgerAccountCustomerCredit       LedgerAccountKey = "customer_credit"
    LedgerAccountTaxPayable           LedgerAccountKey = "tax_payable"
    LedgerAccountRevenue              LedgerAccountKey = "revenue"
    LedgerAccountContraRevenue        LedgerAccountKey = "contra_revenue"
)

type AccountClass string  // asset | liability | revenue | contra_revenue
type Direction string     // debit | credit
type StatementType string // balance_sheet | income_statement
```


| Key                   | Class          | Posted by                                                                                                    |
| --------------------- | -------------- | ------------------------------------------------------------------------------------------------------------ |
| `cash`                | asset          | Payment succeeded (debit), refund succeeded (credit)                                                         |
| `accounts_receivable` | asset          | Invoice finalized / voided, credit note, credits applied, payment, refund                                    |
| `unbilled_receivable` | asset          | Earned before invoiced (usage, arrears)                                                                      |
| `deferred_revenue`    | liability      | Invoiced before earned                                                                                       |
| `customer_credit`     | liability      | Wallet top-up `ONE_OFF` invoice finalized (credit); credits applied to an invoice, `CREDITS` payment (debit) |
| `tax_payable`         | liability      | Invoice tax; tax reversed by a credit note                                                                   |
| `revenue`             | revenue        | Recognition; standalone one-off invoices at finalize                                                         |
| `contra_revenue`      | contra_revenue | Credit notes                                                                                                 |




#### Invariants

- Account roles are fixed in code; no table, no tenant configuration.
- Coupons and discounts are netted into revenue; there is no discount account.
- Every credit note posts Dr `contra_revenue`, Dr `tax_payable` (when its tax is reversed) / Cr AR in the month it is issued. Credit notes never change schedules, so recognized revenue is never reversed.
- Credits applied to an invoice settle AR from `customer_credit`, free and purchased alike. Splitting free credits (`FREE_CREDIT_GRANT`, `PURCHASED_CREDIT_BONUS`) into `contra_revenue` via `consumption_breakdown` is deferred; until then `customer_credit` runs a debit balance equal to the free credits used.
- A succeeded payment settles AR into `cash`, except method `CREDITS` (post-paid wallet), which settles from `customer_credit`. A refund credit note credits AR; the refund posts on success: `GATEWAY` / `OUT_OF_BAND` from `cash`, `WALLET` into `customer_credit`.



### Step 6 — AccountingEvent & Journal

Billing and earning events post balanced, immutable journal entries. Billing entries post when the source commits; recognition entries post from a daily job, once per POB per ended month plus corrections. Balances and reports are sums over journal lines.

#### DB schema

```sql
CREATE TABLE journal_entries (                     -- immutable
    id                 varchar(50)  PRIMARY KEY,   -- prefix "je"
    tenant_id          varchar(50)  NOT NULL,
    environment_id     varchar(50)  NOT NULL,
    event_type         varchar(50)  NOT NULL,      -- invoice_finalized | invoice_voided | credit_note_finalized | credit_note_voided | payment_succeeded | payment_reversed | refund_succeeded | revenue_earned
    source_entity_type varchar(50)  NOT NULL,      -- invoice | credit_note | payment | refund | pob
    source_entity_id   varchar(50)  NOT NULL,
    idempotency_key    varchar(255),               -- billing entries only; recognition relies on the POB lock
    period_id          varchar(50)  NOT NULL,
    business_date      date         NOT NULL,      -- local date of the activity
    customer_id        varchar(50)  NOT NULL,
    contract_id        varchar(50),                -- null for standalone one-off invoices and their payments
    currency           varchar(3)   NOT NULL,
    reversal_of        varchar(50),
    status             varchar(50)  NOT NULL DEFAULT 'published',
    created_at         timestamptz  NOT NULL,
    created_by         varchar(50)
);

-- exactly-once posting per billing event
CREATE UNIQUE INDEX ON journal_entries (tenant_id, environment_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
-- entries of a source (drill-down, reconcile)
CREATE INDEX ON journal_entries (tenant_id, environment_id, source_entity_type, source_entity_id);
-- rollforward by period
CREATE INDEX ON journal_entries (tenant_id, environment_id, period_id);

CREATE TABLE journal_lines (                       -- immutable
    id               varchar(50)   PRIMARY KEY,    -- prefix "jl"
    tenant_id        varchar(50)   NOT NULL,
    environment_id   varchar(50)   NOT NULL,
    journal_entry_id varchar(50)   NOT NULL,
    account          varchar(50)   NOT NULL,       -- LedgerAccountKey
    direction        varchar(10)   NOT NULL,       -- debit | credit
    amount           numeric(20,8) NOT NULL,       -- > 0
    pob_id           varchar(50)
);

-- lines of an entry
CREATE INDEX ON journal_lines (tenant_id, environment_id, journal_entry_id);
-- POB position (deferred / unbilled) and posted revenue
CREATE INDEX ON journal_lines (tenant_id, environment_id, pob_id, account);
```

No `entry_date`: Phase 1 never closes a period, so it equals `business_date`; once close ships it is `max(business_date, period_start)`.

#### Posting rules

**POB position.** Per POB, `billed − earned` from its lines: deferred = Σ(credit − debit) on `deferred_revenue`, unbilled = Σ(debit − credit) on `unbilled_receivable`. A POB never carries both. A billing amount `+b` credits unbilled up to its balance, then deferred; an earning `+e` debits deferred up to its balance, then unbilled. Negative amounts mirror this. Invoice lines resolve their POB through `subscription_line_item_id`.


| Event (business date)                                         | Entry                                                                                                                                                                                                                                             |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SUBSCRIPTION` / proration invoice finalized (`finalized_at`) | Dr AR (total), Dr `customer_credit` (credits applied) / per line, its net (after discounts and inclusive tax) to its POB: Cr `unbilled_receivable` up to the POB's unbilled balance, the rest Cr `deferred_revenue`; Cr `tax_payable` (total tax) |
| Standalone `ONE_OFF` invoice finalized                        | Dr AR / Cr `revenue`, Cr `tax_payable`                                                                                                                                                                                                            |
| Wallet top-up `ONE_OFF` invoice finalized                     | Dr AR / Cr `customer_credit` (invoice metadata carries `wallet_transaction_id`)                                                                                                                                                                   |
| Invoice voided (`voided_at`)                                  | Inverse POB billing, Dr `tax_payable` / Cr AR by the gross including credits applied; the void's wallet refund returns the credits                                                                                                                |
| Credit note finalized                                         | Dr `contra_revenue`, Dr `tax_payable` (when reversed) / Cr AR                                                                                                                                                                                     |
| Credit note voided                                            | Inverse of its finalize entry, `reversal_of` set                                                                                                                                                                                                  |
| Payment succeeded (`succeeded_at`)                            | Dr `cash` / Cr AR; method `CREDITS`: Dr `customer_credit` / Cr AR                                                                                                                                                                                 |
| Payment reversed                                              | Inverse of the payment entry, `reversal_of` set                                                                                                                                                                                                   |
| Refund succeeded (`succeeded_at`)                             | Dr AR / Cr `cash` (`GATEWAY`, `OUT_OF_BAND`) or Cr `customer_credit` (`WALLET`)                                                                                                                                                                   |
| Recognition (last day of the period)                          | Per POB and ended month `P`: `δ = round(earned(P_end)) − round(earned(P_start)) − posted(P)`. Positive `δ`: Dr `deferred_revenue` up to the POB's deferred balance, the rest Dr `unbilled_receivable` / Cr `revenue`. Negative `δ` mirrors it     |


`posted(P)` is Σ(credit − debit) on the POB's `revenue` lines in recognition entries of `P`.

Example: $1,200 annual prepaid from Jan 1, `period_even`, invoiced Jan 1 (Cr `deferred_revenue` 1,200).


| Run      | Period | Earned in `P` | `posted(P)` | `δ`  | Entry                                        |
| -------- | ------ | ------------- | ----------- | ---- | -------------------------------------------- |
| Jan 2–31 | —      | —             | —           | —    | None; January has not ended                  |
| Feb 1    | Jan    | 100           | 0           | +100 | Dr `deferred_revenue` 100 / Cr `revenue` 100 |
| Feb 2    | Jan    | 100           | 100         | 0    | None                                         |
| Mar 1    | Feb    | 100           | 0           | +100 | Dr `deferred_revenue` 100 / Cr `revenue` 100 |


A backdated modification that cuts January's earned to 90 makes the next run post `δ = −10` to January: Dr `revenue` 10 / Cr `deferred_revenue` 10. Without it, deferred after March 1 is 1,200 − 100 − 100 = 1,000; with it, 1,010.

#### APIs


| Endpoint                          | Role                                                                   |
| --------------------------------- | ---------------------------------------------------------------------- |
| `GET /v1/journal_entries/{id}`    | Get, with lines                                                        |
| `POST /v1/journal_entries/search` | Filter by period, event type, account, customer, contract, POB, source |


Read-only: the journal is derived from billing and schedules.

#### Invariants

- Entries are immutable. Each has at least one debit and one credit line, positive amounts, Σ debits = Σ credits, one currency (the invoice's), one period. Corrections are deltas or inverses linked by `reversal_of`.
- Billing entries carry the idempotency key `{event_type}:{source_entity_id}`. Recognition entries carry none: a run computes `δ` under the POB lock, so a rerun computes `δ = 0` and posts nothing.
- Billing entries post async after the source commits (post-commit hook, like `revenue_facts`), each in its own transaction. A failure is logged and never fails the source write; `Reconcile` re-posts anything missing.
- Recognition posts only from the daily job (and the backfill on enable), under a per-POB advisory lock, for ended periods only. Each run re-evaluates the last ended month and any earlier month a new segment or usage fact touched; an unchanged period computes `δ = 0` and posts nothing. The current month's earned-to-date is computed on read, never posted.
- Business dates are local dates in the subscription's timezone (the customer's without a subscription); the entry posts to that date's period (Step 4).
- Billing entries post for `draft` contracts too; recognition only for `active` and `satisfied` POBs, catching up on the first run.
- Enabling revenue recognition replays every contract from its start: contracts, POBs and segments, then billing entries in business-date order, then recognition. No opening-balance entries.
- Nothing is written unless the environment has revenue recognition enabled.



#### Services

`JournalService` (new) — builds, balances and posts entries; owns the recognition run and the backfill. A new daily Temporal schedule `revenue_recognition` fans out over enabled environments like `RevenueRollupWorkflow`; each run calls `Reconcile`, then per contract `PerformanceObligationService.Reconcile` (Step 3 repair) and `Recognize`.

```go
type JournalService interface {
    PostInvoice(ctx context.Context, inv *invoice.Invoice) error         // finalized and voided
    PostCreditNote(ctx context.Context, cn *creditnote.CreditNote) error // finalized and voided
    PostPayment(ctx context.Context, p *payment.Payment) error           // succeeded and reversed
    PostRefund(ctx context.Context, r *refund.Refund) error
    Recognize(ctx context.Context, contractID string, asOf time.Time) error
    Backfill(ctx context.Context) error                                  // replay on enable
    Reconcile(ctx context.Context, since time.Time) error                // re-posts billing entries missing since `since`
    PreviewRecognition(ctx context.Context, contractID string, asOf time.Time) ([]*journal.JournalEntry, error) // unsaved; Step 7 pending
    GetJournalEntry(ctx context.Context, id string) (*dto.JournalEntryResponse, error)
    GetJournalEntries(ctx context.Context, filter *types.JournalEntryFilter) (*dto.ListJournalEntriesResponse, error)
}
```

`Reconcile` is environment-wide, not per contract, so it also covers standalone `ONE_OFF` invoices, wallet top-ups and their payments. It finds every invoice, credit note, payment or refund finalized, voided, succeeded or reversed since `since` that has no entry under its key, and posts it. Recognition needs no repair: every run recomputes `δ`.

### Step 7 — Rollforward & Waterfall

Reports are queries over the journal and the POB schedules; nothing is persisted. The rollforward explains how an account's balance moved in ended months; the waterfall shows revenue per customer per month, past, current and future.


| View                                                                | Endpoint                                   | Request                                                                          |
| ------------------------------------------------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------- |
| Deferred / unbilled rollforward (summary, per contract, drill-down) | rollforward, then `journal_entries/search` | `accounts=[deferred_revenue]`, one period, `group_by=[customer_id, contract_id]` |
| Revenue summary, trial balance                                      | rollforward                                | `accounts=[revenue, contra_revenue]` or all accounts                             |
| Revenue recognition (customers × months, with details)              | waterfall                                  | `group_by=customer_id`, `details=true`                                           |
| Customer, contract revenue schedule                                 | waterfall                                  | `customer_ids` / `contract_ids`, `group_by=pob_id`                               |




#### APIs


| Endpoint                               | Role                                                                      |
| -------------------------------------- | ------------------------------------------------------------------------- |
| `POST /v1/revenue_reports/rollforward` | Opening, movements by category and closing per ended period and account   |
| `POST /v1/revenue_reports/waterfall`   | Revenue per group and month: actual, pending, scheduled, extrapolated     |
| `POST /v1/journal_entries/search`      | Drill-down to lines (Step 6), by period, account, customer, contract, POB |


Both are `@x-scope read`. Periods are `YYYY-MM`; amounts are in `currency`, never combined across currencies.

**Rollforward**

```go
type RollforwardRequest struct {
    PeriodStart string                   `json:"period_start" validate:"required"` // YYYY-MM
    PeriodEnd   string                   `json:"period_end"`                       // YYYY-MM; default period_start; an ended month
    Currency    string                   `json:"currency" validate:"required"`
    Accounts    []types.LedgerAccountKey `json:"accounts"`                         // default [deferred_revenue]
    GroupBy     []string                 `json:"group_by"`                         // customer_id | contract_id | pob_id
    CustomerIDs []string                 `json:"customer_ids"`
    ContractIDs []string                 `json:"contract_ids"`
    POBIDs      []string                 `json:"pob_ids"`
    AsOf        *time.Time               `json:"as_of"`                            // only entries created at or before it
    Limit       int                      `json:"limit"`                            // rows per page; totals are never paged
    Offset      int                      `json:"offset"`
}

type RollforwardResponse struct {
    Totals     []*RollforwardRow         `json:"totals"` // per period and account, all groups
    Items      []*RollforwardRow         `json:"items"`  // per period, account and group
    Pagination *types.PaginationResponse `json:"pagination"`
}

type RollforwardRow struct {
    Period     string                                     `json:"period"`
    Account    types.LedgerAccountKey                     `json:"account"`
    CustomerID *string                                    `json:"customer_id,omitempty"`
    ContractID *string                                    `json:"contract_id,omitempty"`
    POBID      *string                                    `json:"pob_id,omitempty"`
    Opening    decimal.Decimal                            `json:"opening"`
    Movements  map[types.MovementCategory]decimal.Decimal `json:"movements"` // missing category = 0
    Closing    decimal.Decimal                            `json:"closing"`
}
```

Example, deferred revenue for Feb, `group_by=[customer_id, contract_id]`:

```json
{
  "totals": [{"period": "2026-02", "account": "deferred_revenue",
              "opening": "1100", "movements": {"invoiced": "100", "earned": "-200"}, "closing": "1000"}],
  "items":  [{"period": "2026-02", "account": "deferred_revenue", "customer_id": "cust_acme", "contract_id": "contract_1",
              "opening": "1100", "movements": {"earned": "-100"}, "closing": "1000"},
             {"period": "2026-02", "account": "deferred_revenue", "customer_id": "cust_beta", "contract_id": "contract_2",
              "opening": "0", "movements": {"invoiced": "100", "earned": "-100"}, "closing": "0"}]
}
```

**Waterfall**

```go
type WaterfallRequest struct {
    PeriodStart string                 `json:"period_start" validate:"required"` // YYYY-MM
    PeriodEnd   string                 `json:"period_end" validate:"required"`   // at most 36 months after period_start
    Currency    string                 `json:"currency" validate:"required"`
    GroupBy     string                 `json:"group_by"`                         // customer_id (default) | contract_id | pob_id
    CustomerIDs []string               `json:"customer_ids"`
    ContractIDs []string               `json:"contract_ids"`
    POBIDs      []string               `json:"pob_ids"`
    Extrapolate bool                   `json:"extrapolate"`                      // default false
    Details     bool                   `json:"details"`                          // balances and product split
    Sort        []*types.SortCondition `json:"sort"`                             // field: customer name or a period
    Limit       int                    `json:"limit"`
    Offset      int                    `json:"offset"`
}

type WaterfallResponse struct {
    Totals     []*WaterfallCell          `json:"totals"` // per period, all rows, never paged
    Items      []*WaterfallRow           `json:"items"`
    Pagination *types.PaginationResponse `json:"pagination"`
}

type WaterfallRow struct {
    CustomerID *string          `json:"customer_id,omitempty"`
    ContractID *string          `json:"contract_id,omitempty"`
    POBID      *string          `json:"pob_id,omitempty"`
    Cells      []*WaterfallCell `json:"cells"` // one per period, in order
}

type WaterfallCell struct {
    Period       string             `json:"period"`
    Revenue      decimal.Decimal    `json:"revenue"` // actual + pending + scheduled + extrapolated
    Actual       decimal.Decimal    `json:"actual"`
    Pending      decimal.Decimal    `json:"pending"`
    Scheduled    decimal.Decimal    `json:"scheduled"`
    Extrapolated decimal.Decimal    `json:"extrapolated"`
    Deferred     *decimal.Decimal   `json:"deferred,omitempty"` // details; closing deferred_revenue, ended months only
    Unbilled     *decimal.Decimal   `json:"unbilled,omitempty"` // details; closing unbilled_receivable, ended months only
    Products     []*ProductRevenue  `json:"products,omitempty"` // details; revenue split by POB product
}

type ProductRevenue struct {
    ProductID *string         `json:"product_id"` // null = uncategorized
    Revenue   decimal.Decimal `json:"revenue"`
}
```

Example, `group_by=customer_id`, Jan–Apr, on Mar 15 (abridged):

```json
{
  "totals": [{"period": "2026-01", "revenue": "200", "actual": "200"},
             {"period": "2026-03", "revenue": "200", "pending": "90.32", "scheduled": "109.68"}],
  "items":  [{"customer_id": "cust_acme", "cells": [
               {"period": "2026-01", "revenue": "100", "actual": "100", "deferred": "1100", "unbilled": "0"},
               {"period": "2026-03", "revenue": "100", "pending": "45.16", "scheduled": "54.84"},
               {"period": "2026-04", "revenue": "100", "scheduled": "100"}]}]
}
```



#### Report rules

**Signs.** Amounts are in the account's normal direction: a movement is positive when it increases the balance.

**Movement categories**, fixed in code from the entry's `event_type`:


| Category      | Event types                                   |
| ------------- | --------------------------------------------- |
| `invoiced`    | `invoice_finalized`, `invoice_voided`         |
| `earned`      | `revenue_earned`                              |
| `adjustments` | `credit_note_finalized`, `credit_note_voided` |
| `payments`    | `payment_succeeded`, `payment_reversed`       |
| `refunds`     | `refund_succeeded`                            |


**Rollforward** is journal only. `opening(P)` = signed lines of entries in periods before `P`; movements are the lines of entries in `P`; `closing = opening + Σ movements`. Only ended months can be requested: the open month is not reported until it ends. With `as_of`, only entries created at or before it count, which reproduces a report as it was then. Without close, a recognition correction posts to its original month (Step 6), so ended months can change; `as_of` shows what changed.

**Waterfall cells**, per POB and month `P`, summed to the requested group:

- `actual` — posted `revenue` lines of `P` (credit − debit). Standalone `ONE_OFF` invoices count here at finalize, uncategorized. Credit notes are in `contra_revenue`, not in the waterfall.
- `pending` — earned but not yet posted: `PreviewRecognition` at `now`, with the open month treated as ended at `now`. It covers the open month to date and any ended month the daily run has not posted.
- `scheduled` — not yet earned, from persisted segments: `round(earned(P_end)) − round(earned(max(P_start, now)))` for the open and future months. Only the current billing period is persisted (Step 3), so recurring POBs with no end date schedule only to its end.
- `extrapolated` — only with `extrapolate`, only for recurring fixed POBs with no `service_end`, after the last segment: each future billing period is valued by the billing engine as a full period, amortized with the POB's method, and stops at the subscription's end or cancellation date. Never posted.
- Usage (`as_consumed`) is never scheduled or extrapolated; it appears as actual and pending only.
- `deferred` / `unbilled` — the group's closing balances of those accounts for ended months, from the journal as in the rollforward.
- `products` — `revenue` split by the POB's `product_id` (Step 3).

Example: $1,200 annual prepaid and $100/month recurring (in advance, no end date), both `period_even` from Jan 1; reported on Mar 15.


| Report               | Jan                                     | Feb                                       | Mar (open)                                                             |
| -------------------- | --------------------------------------- | ----------------------------------------- | ---------------------------------------------------------------------- |
| Deferred rollforward | 0 + 1,300 invoiced − 200 earned = 1,100 | 1,100 + 100 invoiced − 200 earned = 1,000 | Not available until March ends                                         |
| Waterfall, annual    | 100 actual                              | 100 actual                                | 45.16 pending + 54.84 scheduled; Apr–Dec 100 scheduled                 |
| Waterfall, recurring | 100 actual                              | 100 actual                                | 45.16 pending + 54.84 scheduled; Apr–Dec 100 extrapolated if requested |


Pending on Mar 15 is 14 of 31 days for each: 100 × 14/31 = 45.16.

#### Invariants

- Reports are read-only and computed on read in Postgres; nothing is persisted.
- Rollforward: `opening(P) = closing(P − 1)`; the first requested period's opening is the sum of everything posted before it (Step 4). A period's rows sum exactly to its journal lines.
- Waterfall: Σ `actual` over all periods equals the `revenue` balance; Σ `products` equals `revenue` in every cell.
- `totals` cover every row matching the filters, not only the page.
- Currencies are never combined.
- `group_by` / filter by POB applies only to accounts whose lines carry `pob_id` (`deferred_revenue`, `unbilled_receivable`, `revenue`); AR is grouped by customer or contract only.
- Pending, scheduled and extrapolated are never posted, and never mix.



#### Services

`RevenueReportService` (new) — builds the rollforward and the waterfall from journal lines, `PreviewRecognition` and POB schedules.

```go
type RevenueReportService interface {
    GetRollforward(ctx context.Context, req *dto.RollforwardRequest) (*dto.RollforwardResponse, error)
    GetWaterfall(ctx context.Context, req *dto.WaterfallRequest) (*dto.WaterfallResponse, error)
}
```

