# Mixed-Interval Fixed Billing – Algorithm (Plain Language)

How we decide **whether** to put a fixed line item on an invoice and **what period and amount** to charge. One invoice period at a time; each line item is decided on its own.

---

## 1. Only consider active fixed line items

- Look only at line items that are **fixed** (not usage).
- **Start:** line item must have started on or before the **end** of this invoice period.
- **End:** if the line item has an end date, it must be on or after the **start** of this invoice period.

If any of these fail, skip the line item for this invoice (no line).

---

## 2. Compare the line item’s interval length to the invoice period length

- **Invoice period length:** number of calendar days in this invoice period (start → end, with your chosen end rule).
- **Line item interval length:** length of **one** of its billing intervals (e.g. one week, one month, one quarter), in days, based on the line item’s start and its billing cadence (e.g. monthly, quarterly).

We only need a rough comparison (e.g. “shorter”, “about the same”, “longer” than the invoice period). In code we use a small tolerance (e.g. ±3 days) for “about the same”.

---

## 3. Decide inclusion and charge type

- **Shorter:** Line item interval is **shorter** than the invoice period (e.g. weekly line, monthly invoice).  
  → **Include with proration.** Charge only for the part of the line item’s “value” that falls inside this invoice period (by days or your proration rule).

- **About the same:** Line item interval is **roughly equal** to the invoice period (e.g. both monthly).  
  → **Include full.** Charge a full period’s amount for this invoice period (with your usual proration rules if the line or period is partial).

- **Longer:** Line item interval is **longer** than the invoice period (e.g. quarterly line, monthly invoice).  
  → Go to step 4.

---

## 4. For “longer” intervals: only include when an interval boundary lands in this invoice period

- The line item has its **own** interval boundaries (e.g. 15 Jan, 15 Apr, 15 Jul for a quarterly line starting 15 Jan).
- Ask: **Does any end of one of those intervals fall inside this invoice period?**  
  (Inside = on or after period start and before period end, with your chosen end rule.)
- **Yes** → Include this line on this invoice. The **service period** we put on the invoice is **that** line-item interval (from its previous boundary to that interval end), not the invoice period. Charge the **full** amount for that one line-item interval.
- **No** → Do **not** include this line on this invoice (no line for this item this time).

So for a Feb 1–Mar 1 invoice and a quarterly line with intervals like 15 Jan–15 Apr: we only add a line if some quarter **end** (e.g. 15 Feb or 15 Apr) falls inside Feb 1–Mar 1. When we do, we charge for that full quarter (e.g. 15 Nov–15 Feb or 15 Feb–15 May), and we show **that** interval as the service period on the invoice, not “Feb 1–Mar 1”.

---

## 5. Set the invoice line’s service period and amount

- **Shorter (prorated):** Service period = the overlap of the line item’s active period with the invoice period. Amount = prorated (e.g. by effective days / interval days).
- **About the same (full):** Service period = invoice period. Amount = full (with existing proration logic if applicable).
- **Longer (included):** Service period = **the line item’s interval** whose end fell in this invoice period (from previous boundary to that end). Amount = full amount for that one interval.

---

## Summary table

| Line vs invoice   | Include? | Service period on invoice      | Amount              |
|------------------|----------|--------------------------------|---------------------|
| Shorter          | Yes      | Overlap with invoice period    | Prorated            |
| About the same   | Yes      | Invoice period                 | Full (plus proration rules) |
| Longer, boundary in period | Yes | That line-item interval (prev → end) | Full for that interval |
| Longer, no boundary in period | No  | —                              | —                   |

This is the abstract algorithm; the code (e.g. `CalculateFixedCharges`, date helpers) implements these rules with concrete date math and rounding.
