# Billing Currency & FX Conversion — PRD

- **Ticket:** TBD
- **Date:** 2026-09-23
- **Status:** Proposed — v1, supersedes v0 which scoped conversion to integration egress
- **Companion design doc:** [`docs/design/2026-09-22-billing-currency-and-fx-conversion-erd.md`](../design/2026-09-22-billing-currency-and-fx-conversion-erd.md)
- **Builds on:** PR #2907 — Zoho / QuickBooks currency sync fix

---

## 0. Terminology

| Term | Meaning |
| --- | --- |
| **Billing currency** | The currency a customer is invoiced in. New concept — introduced by this document |
| **Charge currency** | The currency a price, plan or subscription is denominated in |
| **Custom currency** | A tenant-invented unit (`MAC`, credits). Not real money. Already exists |
| **FX rate** | A conversion rate between two currencies, `target = source × rate` |
| **Scope** | Which entity an FX rate is attached to: tenant, customer or subscription |
| **Frozen rate** | The rate recorded on an invoice, immune to later rate changes |
| **Egress** | Pushing an invoice into an external system — Zoho, QuickBooks |

---

## 1. Where this starts

The Zoho defect is fixed and shipped. A USD invoice now syncs to Zoho as USD, with an
exchange rate supplied for the ledger. QuickBooks has the same treatment.

That fixed *faithfulness* — what we send matches what we hold. It did not address the
question underneath it:

> **What currency should the invoice have been in?**

Today the answer is accidental. An invoice's currency is whatever the subscription got,
which is whatever the plan's prices happened to be in. Nobody decides it, and two
subscriptions for the same customer in different charge currencies produce two invoices in
two currencies.

That inconsistency is what this document removes.

---

## 2. What we are building

Four capabilities, in dependency order.

### 2.1 A customer has a billing currency

New field on the customer. **Null initially**, set on first invoice — mirroring how a Zoho
contact acquires its currency, and for the same reason: it is the first moment the answer is
knowable.

Also settable explicitly through the API, so a tenant who knows a customer bills in INR can
say so before the first invoice rather than after.

### 2.2 Plans and subscriptions stay in any currency

Nothing changes about pricing. A plan can hold USD prices, or EUR, or both. A subscription
is created in one of them, exactly as today.

### 2.3 A customer is always billed in their billing currency

This is the behavioural change. When a subscription's charge currency differs from the
customer's billing currency, the charges are **converted at invoice generation** and the
invoice is issued in the billing currency.

```
Subscription charges          Customer billing currency        Invoice
────────────────────          ─────────────────────────        ───────
USD  $100                     USD                              $100        no conversion
USD  $100                     INR                              ₹8,300      converted
EUR  €90                      INR                              ₹8,100      converted
```

One customer, one currency, on every invoice — regardless of how many subscriptions they
hold or what those subscriptions are priced in.

### 2.4 The rate is resolved, recorded, and reused

A rate is resolved from the first of these that answers:

```
subscription → customer → tenant → live market feed → hard fail
```

It is **recorded on the invoice**, and that recorded rate is what every downstream consumer
uses — including the accounting integrations, which today poll the provider for a rate of
their own.

---

## 3. Requirements

### 3.1 Billing currency

| # | Requirement |
| --- | --- |
| C1 | `customers.billing_currency`, nullable, defaults to null |
| C2 | Set automatically on first invoice, from that invoice's charge currency |
| C3 | Settable explicitly via the customer API before any invoice exists |
| C4 | Once set and once invoices exist, changing it is refused (§3.6) |
| C5 | Null billing currency → the invoice takes the charge currency and sets it, no conversion |

### 3.2 FX rates

| # | Requirement |
| --- | --- |
| R1 | A rate is `(from_currency, to_currency, rate, scope, effective window)` |
| R2 | `rate` is **target units per 1 source unit**: `target = source × rate`. USD→INR at `83` means $1 = ₹83 |
| R3 | Scopes: `tenant`, `customer`, `subscription`. Resolution is most-specific-first |
| R4 | Direction is explicit — an INR→USD conversion needs an INR→USD row, never `1/rate` (§3.5) |
| R5 | `effective_from` and optional `effective_to`. Overlapping windows for the same pair and scope are rejected at write |
| R6 | Editing a rate never alters a conversion already recorded |
| R7 | A rate row is **fixed** (carries a value) or **dynamic** (names a source to call) |

### 3.3 Live market feed

The fallback when no configured rate resolves.

| # | Requirement |
| --- | --- |
| F1 | A tenant-level setting names the feed provider and its credentials |
| F2 | Rates are fetched and **cached with a TTL**, not called per invoice |
| F3 | A feed-sourced rate is recorded with `source = feed` and the fetch timestamp |
| F4 | Feed unavailable **and** no configured rate → invoice generation fails loudly. Never fall back to `1` |

> [!IMPORTANT]
> Rate `1` as a fallback is not a degraded mode — it is a wrong invoice. The Zoho defect was
> precisely an unconverted amount presented as a converted one. Nothing in this design may
> reintroduce it.

### 3.4 Conversion at invoice generation

| # | Requirement |
| --- | --- |
| I1 | The invoice is created in the customer's billing currency |
| I2 | Line items, discounts and adjustments are converted at one rate |
| I3 | Converted line items **must sum exactly** to the converted invoice total — the rounding residual is absorbed, not dropped (ERD §5.3) |
| I4 | The rate is live while the invoice is a draft, and **frozen at finalization** |
| I5 | The rate, the source currency and the source amounts are stored on the invoice |
| I6 | A finalized invoice's amounts never move, whatever happens to the rate afterwards |

I4 mirrors what custom currency already does, deliberately — see §4.

### 3.5 Why inverse rates are not derived

Given USD→INR at `83`, serving INR→USD as `1/83` is refused:

1. **Round-tripping leaks.** `1/83 = 0.01204819…` — truncate anywhere and `$5 → ₹415 → $4.99`.
2. **The directions aren't always reciprocal.** Contract rates carry spreads; a tenant buying at 83 and selling at 85 has two real rates and `1/83` is neither.
3. **It hides missing configuration.** A derived inverse looks configured. Nobody finds out it wasn't.

### 3.6 Changing a customer's billing currency

Refused once invoices exist, for the same reason Zoho and QuickBooks refuse it: historical
invoices, payments, credit notes and wallet balances are all denominated in the old currency,
and there is no correct thing to do with them.

The documented path is a new customer record — which is also what QuickBooks and Stripe tell
you to do.

---

## 4. This is the same mechanism custom currency already uses

Not a similarity worth noting in passing. **It is structurally identical, and the existing
implementation is the template.**

`invoices.custom_currency` already holds exactly what §3.4 requires:

```go
type CustomCurrency struct {
    Code  string          // the source currency
    Rate  decimal.Decimal // live while draft, frozen at finalization
    Subtotal, TotalDiscount, TotalTax,
    TotalPrepaidCreditsApplied, Total, AmountDue decimal.Decimal  // source amounts
}
```

And `invoice_line_items.custom_currency` carries the per-line source amounts.
`ProjectCustomCurrency()` converts into the invoice's currency; `RestoreFromDenomination()`
goes back to the source for every recomputation, which is how "converted exactly once" is
enforced today.

| | Custom currency | FX (this document) |
| --- | --- | --- |
| Source | a tenant-invented unit (`MAC`) | a real currency (`USD`) |
| Target | `default_fiat_currency` | the customer's billing currency |
| Rate from | `fiat_conversion_factors` | `exchange_rates` + feed |
| Scope | tenant + environment | tenant → customer → subscription |
| Converted at | invoice generation | invoice generation |
| Frozen at | finalization | finalization |
| Stored as | `custom_currency` + line item copies | the same shape |

**Only the first four rows differ, and two of those are the feature.**

### 4.1 This reverses the v0 recommendation

v0 of this document recommended *not* converging with custom currency (Option C: share the
conversion helper, leave the rate source alone). That was correct when FX only ran at egress
and never produced a denominated invoice.

It is wrong now. With conversion moving to invoice generation, keeping them separate means
**building a second denomination mechanism that does the same thing** — two ways to store a
source amount, two rate-freezing paths, two recompute rules, on the most correctness-critical
path in the product.

**Recommendation: generalize the existing mechanism rather than duplicate it.** The
denomination object stops being custom-currency-specific and becomes "the currency this
invoice was converted from, and at what rate". Custom currency becomes one producer of it;
FX becomes another.

### 4.2 What that unlocks, and what it costs

**Unlocks.** The `mac → inr` case works for free. Today `fiat_conversion_factors` can hold
an `inr` factor that is unreachable, because invoice currency is pinned to
`default_fiat_currency` tenant-wide. With a per-customer billing currency, a MAC-priced
subscription for an INR customer converts directly, one hop, using the factor that already
exists. That config was written for this feature and has been dead since.

**Costs.** It touches invoice finalization for tenants already live on custom currency. That
is the single riskiest file in this project and the reason §6 sequences it behind everything
else.

---

## 5. Scope

### 5.1 In scope

| # | |
| --- | --- |
| S1 | `customers.billing_currency`, null by default, set on first invoice, settable via API |
| S2 | `exchange_rates` — scoped, dated, fixed-or-dynamic |
| S3 | Resolution: subscription → customer → tenant → feed → fail |
| S4 | Live market feed with cached rates and a TTL |
| S5 | Conversion at invoice generation into the billing currency |
| S6 | Rate frozen at finalization and recorded on the invoice |
| S7 | Integrations send the invoice's recorded rate instead of polling their own |
| S8 | An audit record of every conversion, queryable across invoices |

### 5.2 Explicitly not in scope

| # | | Why |
| --- | --- | --- |
| N1 | Cross-currency wallets | A wallet pays invoices, so it follows billing currency. Converting at application time means FX gain/loss accounting — a separate project (§6.3) |
| N2 | Cross-currency payments | `payment_processor.go:739` sums payments and hard-errors on mismatch. Needs a two-amount payment model first |
| N3 | Changing a customer's billing currency after invoicing | §3.6 |
| N4 | Marketplace usage conversion | Uses the same rate table, different consumer. Sequenced after |
| N5 | `price_units` convergence | Different discipline — immutable, snapshot-at-authoring, bound 1:1 to a price's currency |

---

## 6. Blast radius

Currency equality is load-bearing across the money path, enforced by exact string match —
`IsMatchingCurrency` is `strings.EqualFold`. Today it holds for free because everything
inherits one currency from the subscription. Billing currency breaks that, in five places.

| Path | Today | What breaks |
| --- | --- | --- |
| **Price → subscription** | Prices not matching the subscription currency are silently dropped (`subscription.go:3785, 3829, 4003`) | Unchanged mechanically, but the rule now needs restating: the filter is against *charge* currency, not billing currency |
| **Wallet → invoice** | *"wallets are per-currency; an EUR invoice can't be paid by a USD wallet"* (`wallet.go:2634`) | A wallet in charge currency cannot pay an invoice in billing currency. **Wallets must follow billing currency** |
| **Payment → invoice** | Rejects on mismatch (`payment_processor.go:641`), and sums raw amounts (`:739`) | Payments are in billing currency — consistent, provided wallets move too |
| **Credit notes** | Inherit invoice currency | Must inherit the *converted* currency and reuse the parent's **frozen** rate, never re-resolve |
| **Proration / plan change** | Built from `sub.Currency` | Must convert at the same point, at the same rate, as the parent invoice |

### 6.1 The wallet decision

**A wallet should be denominated in the customer's billing currency, not the subscription's.**

A wallet exists to pay invoices; invoices are in billing currency; therefore wallets are too.
Any other answer means converting at credit-application time, which is realised FX gain or
loss and needs somewhere in the books to land.

This has migration consequences for existing wallets and is the first thing to settle.

### 6.2 What "converted exactly once" must mean

Three conversions could plausibly apply to one invoice:

```
MAC charges ──▶ fiat            (custom currency)
USD charges ──▶ INR             (FX, billing currency)
INR invoice ──▶ INR ledger      (integration exchange_rate — metadata, not a conversion)
```

The rule: **an invoice is converted once, into the billing currency, and the source
denomination is what every recomputation starts from.** The integration rate is ledger
metadata and never rescales anything.

A MAC-priced subscription for an INR customer is `MAC → INR`, one hop. Not `MAC → USD → INR`.

### 6.3 FX exposure is created, not removed

Worth stating plainly because it is a business consequence, not a technical one.

A fixed contract rate does not eliminate currency risk. It moves it from the customer to you:

```
Invoice booked at contract rate 83    $100  →  ₹8,300
Customer pays                                  ₹8,300
Bank settles at market 85             ₹8,300 →  $97.65
                                               ────────
Revenue recognised $100, received $97.65   →   $2.35 FX loss
```

Nothing in the product accounts for this today. It may be acceptable at current volumes, but
it should be a decision rather than a discovery.

---

## 7. Sequencing

Ordered by risk, each step independently shippable.

| Phase | | Risk |
| --- | --- | --- |
| **1** | `exchange_rates` table, resolution, admin API. Nothing consumes it yet | Low — additive |
| **2** | `customers.billing_currency`, null by default, set on first invoice. No conversion yet: billing currency always equals charge currency | Low — no behaviour change |
| **3** | Live market feed with cached rates | Low — additive |
| **4** | **Conversion at invoice generation.** Wallets move to billing currency | **High** — the real change |
| **5** | Integrations read the invoice's frozen rate instead of polling | Medium — touches shipped sync paths |
| **6** | Custom currency generalizes onto the same denomination (§4) | **High** — touches finalization for live tenants |

Phase 2 is deliberately inert: it establishes and backfills the field while
`billing_currency == charge_currency` for everyone, so phase 4 turns on conversion against
data that is already correct.

---

## 8. Success criteria

| # | |
| --- | --- |
| S1 | A customer with USD and EUR subscriptions and billing currency INR receives **every** invoice in INR |
| S2 | The rate used is on the invoice and retrievable |
| S3 | Line items sum exactly to the invoice total — no residual drift |
| S4 | Changing a tenant rate does not alter any finalized invoice |
| S5 | No configured rate and no feed → invoice generation fails, naming the pair. No invoice is created |
| S6 | A customer whose billing currency equals the charge currency follows an unchanged code path |
| S7 | Zoho and QuickBooks send the invoice's recorded rate, not one polled from the provider |
| S8 | Enabling customer or subscription scoped rates requires no migration |

---

## 9. Open decisions

| # | Decision | Recommendation |
| --- | --- | --- |
| **D1** | Wallets follow billing currency | §6.1 — yes. Needs a migration plan for existing wallets. **Settle first** |
| **D2** | Generalize the custom-currency denomination, or build a parallel one | §4 — generalize. Highest risk, last phase |
| **D3** | Which market feed | Not chosen. Needs a provider, credentials and a TTL. ECB is free and daily; commercial feeds are intraday |
| **D4** | Rate frozen at finalization, or at draft creation | Finalization, matching custom currency. A draft that moves with the rate is arguably correct, but two freeze points would be confusing |
| **D5** | Does the customer-facing invoice show the source amount? | Recommend yes — *"$100.00 converted at 83.00"*. It is the only place the contract denomination survives |
| **D6** | What happens to in-flight drafts when a rate changes | Recommend: drafts re-resolve, finalized never move. Follows D4 |

---

## 10. References

| | |
| --- | --- |
| Invoice currency choke point | `internal/ee/service/invoice.go` — `CreateEmptyDraftInvoice` |
| Custom-currency projection | `internal/types/custom_currency.go`, `internal/domain/invoice/model.go` |
| Currency equality | `internal/types/currency.go:85` — `IsMatchingCurrency` |
| Rounding | `internal/types/currency.go:137` — `RoundToCurrencyPrecision` |
| Wallet currency rule | `internal/ee/service/wallet.go:2634` |
| Payment reconciliation | `internal/ee/service/payment_processor.go:641, :739` |
| Price filtering | `internal/ee/service/subscription.go:3785, 3829, 4003` |
| Shipped Zoho/QBO fix | PR #2907 |
| Custom currency design | `docs/design/2026-08-27-FLE-1201-tenant-custom-currency.md` |
