# Mid-period credit expiry

Status: v1 built (PR #2949), periods that end early handled (PR #2995, FLE-1498). On for all tenants.
Linear: FLE-897.

Terms used below:
- **Draft**: the period's invoice before it's final. The billing run creates it at period end,
  computes it, and finalizes it a few hours later.
- **Finalization**: the moment credits are taken from the wallet and the invoice is locked.
- **Ongoing balance**: wallet balance minus usage not yet paid, shown to customers in real time.

## The problem

Credits are taken from the wallet only when the period's invoice is finalized. If a credit expires
before that, all of it expires, including the part that should have paid for usage before the
expiry. That usage is then paid from the customer's other credits.

Example: 100 purchased + 30 free credits, 20 of usage, the free credits expire mid-period.
The customer should end with 100. Today they end with 80.

## The idea

When a credit expires, first let it pay the usage from before its expiry, then expire the rest.

The payment goes onto the period's draft invoice (the same draft the billing run finalizes at
period end). Finalization then counts it as already paid and takes only what's still owed from
the wallet.

## Flow

```mermaid
flowchart TD
    A[Expiry job, every 15 min] --> Y[Credits expired 2h+ ago,<br/>earliest expiry first]
    Y --> C[For each subscription the credit can pay]
    C --> D[Find its unfinalized drafts that have usage before the expiry:<br/>earlier periods, then the current period<br/>current draft created only if it has such usage]
    D --> E[Amount per draft =<br/>usage before expiry, priced like an invoice for that window,<br/>minus credits already applied]
    E --> F[Apply to the drafts, oldest first:<br/>debit the credit, reduce the draft's total]
    F --> G[Expire what's left of the credit]

    P[Period end: billing run] --> Q[Reuses the same draft and recomputes it<br/>credits already applied are kept]
    Q --> R[Finalization<br/>waits while an expiry in this period is unprocessed]
    R --> S[Places the applied credits on the usage lines first,<br/>takes only the rest from the wallet]
```

## Example (the case above)

| Step | Wallet balance | Ongoing balance |
|---|---|---|
| 100 purchased + 30 free | 130 | 130 |
| 20 of usage | 130 | 110 |
| Free credits expire: 20 applied to the draft, 10 expired | 100 | 100 |
| 15 more usage | 100 | 85 |
| Period end: draft recomputed, still shows 20 applied | 100 | 85 |
| Finalization: 20 already applied, 15 taken from purchased | 85 | 85 |

Ongoing balance = wallet balance − usage not yet paid. It stays correct at every step.

## Rules for the amount applied

- Only usage **before the expiry** counts. The job waits 2h so late events timestamped before the
  expiry are counted; by then the draft also holds usage after the expiry, which is left out.
- Only **usage, after discounts**. That usage is priced exactly as an invoice for the window from
  period start to expiry would be (same pricing code, coupons and included quotas applied, nothing
  saved). Finalization never puts credits on fixed charges.
- Minus credits **already applied** to that draft by an earlier expiring credit.
- **Rounded down to cents**, and never more than what's left on the credit.
- Several subscriptions: the one whose period ends first is paid first.
- A draft with a payment recorded on it is skipped.
- Custom-currency invoices are handled in their own currency.
- Safe to re-run: a credit is applied to a draft at most once.
- Only active prepaid wallets; only active standalone or parent subscriptions in the same currency.

## When the expiry is processed

The job picks a credit up at least 2h after it expires, so its period may already have rolled over.
Relative to a period P1 followed by P2:

| # | Timing | What happens |
|---|---|---|
| T1 | Expiry mid-period, processed before the period ends | Pays the current draft, rest expires |
| T2 | Expiry inside P1, processed after P1 rolled over | Pays P1's draft; P1's finalization waits for it (at most expiry + 3h) |
| T3 | Expiry after P1 ends, P1 finalizes first | P1 uses the credit as today (still valid at P1's end); the job then pays P2's usage before the expiry |
| T4 | Expiry after P1 ends, job runs before P1 finalizes | One run pays P1's draft, then P2's draft |
| T5 | No usage before the expiry in the current period | No draft created; the credit expires |

## What else changes

| Where | Change |
|---|---|
| Recompute (`ComputeInvoice`) | Keeps credits applied at expiry; a draft with them is never marked SKIPPED |
| Finalization (`ApplyCreditsToInvoice`) | Credits applied at expiry go first and aren't debited again. Wallets count only credits valid at the period end (this part applies to all tenants: it stops "insufficient balance" failures) |
| Finalization hold (`IsFinalizationDue`) | Waits while a credit that expired inside the period is unprocessed |
| Ongoing balance (`GetUnpaidInvoicesToBePaid`, `pendingCharges`) | Credits applied to a draft are subtracted from the usage still owed, so the same usage isn't counted twice |
| Void | Unchanged: refunds applied credits as a top-up with no expiry |

Main new code: `internal/ee/service/credit_expiry.go`. Ledger: the applied part is a
`CREDIT_ADJUSTMENT` debit referencing the draft (`adjustment_type: expiry_settlement`), the rest a
`CREDIT_EXPIRED` debit.

## Rollout

On for every tenant. It started behind a per-environment setting (`credit_expiry_settlement_config`),
enabled for one tenant first; the setting and the old path (expire the whole credit 6h after expiry,
holding it while its period's invoice was open) were removed in PR #2995.

Cost: each expiring credit with usage computes a draft and runs a usage query on ClickHouse. Fine at
today's volume; worth watching if a tenant expires many credits at once.

## How to see it working

- Ledger: a `CREDIT_ADJUSTMENT` debit on the credit with `adjustment_type: expiry_settlement`,
  referencing the draft, followed by a `CREDIT_EXPIRED` debit for the rest.
- Draft: `total_prepaid_credits_applied` set before finalization; lines show credits only after it.
- Logs: `holding finalization until the expiry job applies an expiring credit` (hold working);
  `expiring credit still unprocessed past the finalization hold` (error: the expiry job is stuck);
  `skipping expiry settlement on a draft with a recorded payment`.

## What we tested

Unit tests in `credit_expiry_settlement_test.go` run the real draft compute against in-memory usage.
End to end on a local stack (wallet, ledger and invoices checked at each step):

| Run | Result |
|---|---|
| T1, then more usage, period end, finalization | Wallet 85 (today 65) |
| T2, finalization tried before the job | Held, then finalized with no extra debit; wallet 100 (today 80) |
| T3 | P1 paid from the credit, P2's draft got its share; purchased untouched |
| T4 | Both drafts paid in one run |
| Two credits in one period, processed in one run | 24 applied (20 if processed latest-expiry first) |
| Void of a draft with applied credits | Refunded, no expiry |

## When the period ends early

The period-end run finds the draft by its key: subscription, billing reason, period. Flows that end
or move the period early would bill a different invoice and leave the draft (with its applied
credits) behind, billing the usage before the expiry twice. So **the open draft follows the period**:
the flow moves the draft's period end and key, and bills that draft instead of a new invoice. Where
the flow raises no invoice, the draft is voided.

| Flow | What happens to the draft |
|---|---|
| Immediate cancel, `generate_invoice` | Moved to `[start, cancel]`, reason `PRORATION`; the cancel finalizes it |
| Immediate cancel, `skip` (also auto-cancel, Stripe cancel) | Voided; applied credits refunded with no expiry |
| Scheduled cancel inside the period | End moves to the cancel date; back again if the schedule is cancelled |
| Resume after a pause | End moves out by the pause length |
| Threshold invoice | Becomes the threshold invoice; the run waits while an expiring credit is pending |
| Plan change v2, `anchor_at_effect` | The cut-short period is billed like an immediate cancel: moved to `[start, change]`, reason `PRORATION`, finalized in the change, paid after commit |
| Cancel at period end, plan change v2 `unchanged`, deferred plan change, trials | Nothing to do: the period ends on time, or no draft exists |

Plan change with `anchor_at_effect`, for every tenant: the old period's invoice is a subscription
invoice with reason `PRORATION` (was a one-off, `SUBSCRIPTION_UPDATE`) and includes the old plan's
arrear fixed charges for the cut-short period, which were billed nowhere before. Pricing those for a
partial period is fixed separately (immediate cancels bill the full period amount today).

Code: `MoveCycleDraft` / `VoidCycleDraft` on the invoice service (no-ops without an open draft for the
current period), `InvoiceRepo.UpdateDraftInvoicePeriod`, and one key helper `subscriptionInvoiceKey`.
Invoice `period_end` is no longer immutable in the ent schema (Go-level only, no migration).

## Not handled yet

**Accepted limits**
- Events arriving after the job ran aren't counted.
- Immediate cancel or plan change within ~2h after a credit expires: the job hasn't applied the
  credit yet and these can't wait, so that usage is paid from other credits.
- Backdated subscriptions (a period that ended before the subscription was created).
- `RecalculateInvoiceV2` on a draft can drop the applied amount.

**Product calls pending**
- Drafts now exist mid-period: customers and admins can see them before period end, and integrations
  that sync drafts (e.g. Tabs) will receive them. Confirm that's fine.
- A backdated cancel that ends the period before an expiry that already paid usage.

## How others do it

| Platform | Usage before expiry, invoiced after |
|---|---|
| Metronome | Paid by the credit: credits applied to a draft open from period start |
| Orb | Paid by the credit: debited as events arrive |
| Stripe, Lago | Lost: credits apply only at finalization |

Our approach is closest to Metronome, which is also where we want to end up.
