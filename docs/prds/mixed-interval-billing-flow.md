# Mixed-Interval Billing – Single Flow Diagram

One diagram showing how all pieces of the mixed-interval billing solution connect: data model, invoice period, line-item inclusion (Algorithm A), alignment check (Algorithm B), formulas (F1–F5), date helpers, and output.

---

```mermaid
flowchart TB
  subgraph dataModel [Data model]
    Sub["Subscription\nbilling_period, billing_period_count\nCurrentPeriodStart, CurrentPeriodEnd\nBillingAnchor"]
    LI["SubscriptionLineItem\nbilling_period, billing_period_count\nstart_date, end_date\ninvoice_cadence, price_id"]
    Price["Price\nbilling_period, billing_period_count\ninvoice_cadence, amount"]
    Sub -->|"has many"| LI
    Price -->|"copied at create"| LI
  end

  subgraph invoiceTrigger [Invoice trigger]
    PeriodEnd["invoice_period_end = sub.CurrentPeriodEnd"]
    PeriodStart["invoice_period_start = sub.CurrentPeriodStart"]
    Anchor["billing_anchor = sub.BillingAnchor"]
  end

  subgraph algoA [Algorithm A: Line item inclusion]
    ForEach["For each line item"]
    Active["Active? start_date ≤ period_end\nAND end_date ≥ period_start"]
    ActiveNo["EXCLUDE"]
    PeriodDays["invoice_period_days =\ndays(period_start, period_end)"]
    IntervalDays["line_item_interval_days from\nitem.billing_period + count\nF3: 1, 7, or calendar"]
    Compare["Compare:\nli_interval_days vs inv_period_days"]
    BranchShorter["Shorter"]
    BranchEqual["Equal"]
    BranchLonger["Longer"]
  end

  subgraph shorterPath [Shorter than invoice period]
    AlgC["Algorithm C: effective_days =\ndays(max(li_start, period_start), period_end)"]
    F1["F1: quantity =\neffective_days / line_item_interval_days"]
    F2["F2: amount =\nunit_price × quantity"]
    Round1["RoundToCurrencyPrecision"]
    LineShorter["Invoice line: prorated"]
  end

  subgraph equalPath [Equal to invoice period]
    ExistingClass["Existing ClassifyLineItems\nCurrentPeriodAdvance\nCurrentPeriodArrear\nNextPeriodAdvance"]
    LineEqual["Invoice line: full amount or usage"]
  end

  subgraph longerPath [Longer than invoice period]
    AlgoB["Algorithm B: Is period_end an interval end?"]
    NextBilling["NextBillingDate(current, anchor, count, period, nil)\nloop until match or intervalEnd > period_end"]
    Match["intervalEnd date == period_end date?"]
    AlignYes["INCLUDE: aligned"]
    AlignNo["EXCLUDE"]
    PrevBilling["F4: interval_start =\nPreviousBillingDate(period_end, count, period)"]
    ServicePeriod["Service period:\ninterval_start .. period_end"]
    LineLonger["Invoice line: full interval amount\nusage over interval_start..period_end"]
  end

  subgraph output [Output]
    Inv["Invoice: sum of line amounts\nperiod_start, period_end"]
  end

  Sub --> PeriodEnd
  Sub --> PeriodStart
  Sub --> Anchor
  ForEach --> Active
  Active -->|No| ActiveNo
  Active -->|Yes| PeriodDays
  PeriodDays --> Compare
  LI --> IntervalDays
  IntervalDays --> Compare
  Compare --> BranchShorter
  Compare --> BranchEqual
  Compare --> BranchLonger

  BranchShorter --> AlgC
  LI --> AlgC
  PeriodStart --> AlgC
  PeriodEnd --> AlgC
  AlgC --> F1
  IntervalDays --> F1
  F1 --> F2
  Price --> F2
  F2 --> Round1
  Round1 --> LineShorter
  LineShorter --> Inv

  BranchEqual --> ExistingClass
  ExistingClass --> LineEqual
  LineEqual --> Inv

  BranchLonger --> AlgoB
  AlgoB --> NextBilling
  LI --> NextBilling
  Anchor --> NextBilling
  PeriodEnd --> NextBilling
  NextBilling --> Match
  Match -->|Yes| AlignYes
  Match -->|No| AlignNo
  AlignYes --> PrevBilling
  PeriodEnd --> PrevBilling
  LI --> PrevBilling
  PrevBilling --> ServicePeriod
  ServicePeriod --> LineLonger
  LineLonger --> Inv
```

---

## How to read this diagram

| Section | What it shows |
|--------|----------------|
| **Data model** | Subscription (invoice period), SubscriptionLineItem (per-price interval from Price). Line item gets `billing_period` and `billing_period_count` from Price at create. |
| **Invoice trigger** | `invoice_period_start`, `invoice_period_end`, and `billing_anchor` come from Subscription and drive all inclusion and proration. |
| **Algorithm A** | For each line item: check active → compute invoice and line-item interval days (F3) → compare lengths → branch Shorter / Equal / Longer. |
| **Shorter path** | Algorithm C → effective_days; F1 (quantity); F2 (amount); round → invoice line. Depends on LI, period, Price. |
| **Equal path** | Existing ClassifyLineItems (advance/arrear buckets) → invoice line. No new formulas. |
| **Longer path** | Algorithm B uses NextBillingDate in a loop; if period_end matches an interval end, use PreviousBillingDate (F4) for service period → full amount and usage window → invoice line. |
| **Output** | All branches contribute invoice lines; invoice is the sum over the period. |

**Key dependencies:** NextBillingDate and PreviousBillingDate ([internal/types/date.go](internal/types/date.go)) are used only in the longer path (alignment + F4). F1/F2 use effective_days (Algorithm C) and line_item_interval_days (F3). Subscription and LineItem fields feed every branch.
