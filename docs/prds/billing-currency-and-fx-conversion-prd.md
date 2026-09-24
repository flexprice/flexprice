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
subscription  →  customer  →  tenant  →  hard fail          [M1]
subscription  →  customer  →  tenant  →  feed  →  hard fail  [M2]
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

### 3.3 Live market feed — **M2**

Deferred. **M1 resolves rates from configuration only.** With no configured rate, invoice
generation fails (§3.7) rather than reaching for a market rate.

Recorded here so M1's schema does not foreclose it:

| # | Requirement |
| --- | --- |
| F1 | A tenant-level setting names the feed provider and its credentials |
| F2 | Rates are fetched and **cached with a TTL**, never called per invoice |
| F3 | A feed-sourced rate is recorded with `source = feed` and the fetch timestamp |
| F4 | Feed unavailable **and** no configured rate → fail loudly. Never fall back to `1` |

The `rate_mode = dynamic` column and the `feed` source value ship in M1 unused, so enabling
this is a validation change rather than a migration on a populated table.

#### Why it is not in M1

The requirement driving this project is **contract rates** — "customer A signed at ₹83." A
market feed is the opposite of that: it is a rate that moves. Maxio documents the same
conclusion for the same reason:

> *"you may choose to define a specific exchange rate, which will ensure that as
> subscriptions renew each month, the price they're charged doesn't change each billing
> cycle based on the market rate."*

A feed also puts a third-party network dependency on the invoice-generation path, where a
slow or unavailable provider becomes a billing outage. And the industry runs slower than
instinct suggests: Microsoft uses **monthly** benchmark rates; Zuora's OANDA integration is
1–2 days behind by design. Nobody needs intraday rates to bill monthly.

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

### 3.7 Rate `1` is never a fallback

> [!IMPORTANT]
> Rate `1` as a fallback is not a degraded mode — it is a wrong invoice. The Zoho defect was
> precisely an unconverted amount presented as a converted one. Nothing in this design may
> reintroduce it, in M1 or M2.
>
> With no resolvable rate, invoice generation **fails**. A blocked invoice is recoverable; a
> wrong one, already sent and already in someone's ledger, is not.

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
| Rate from | `fiat_conversion_factors` | `exchange_rates` |
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

### 5.0 The starting position makes this safe

Worth stating plainly, because it is the reason this can ship incrementally:

> **Today, for every customer, invoice currency == subscription currency == wallet currency.**

Nothing in production is multi-currency at the customer level. A customer subscribes in USD,
is invoiced in USD, and holds a USD wallet. That alignment is not a coincidence — it is a
consequence of there being no way to express anything else.

Two things follow:

1. **Backfill is unambiguous.** `billing_currency` = the customer's existing invoice
   currency, which is already the only currency they have.
2. **M1 is inert until someone opts in.** With `billing_currency == charge_currency`, the
   conversion branch is never entered and every existing code path runs unchanged. The new
   behaviour engages only when a tenant deliberately sets a different billing currency.

A production audit found 123 customers with invoices in more than one currency — but only 7
are mapped to an accounting integration and **all 7 are test accounts**. There is no real
customer whose backfill is ambiguous.

### 5.1 M1 — custom FX rates

| # | |
| --- | --- |
| S1 | `customers.billing_currency`, null by default, set on first invoice, settable via API |
| S2 | `exchange_rates` — scoped, dated, **fixed rates only** |
| S3 | Resolution: subscription → customer → tenant → **fail** |
| S4 | Conversion at invoice generation into the billing currency |
| S5 | Rate frozen at finalization and recorded on the invoice |
| S6 | Integrations send the invoice's recorded rate instead of polling their own |
| S7 | An audit record of every conversion, queryable across invoices |

### 5.2 M2 — the rest

| # | | Why deferred |
| --- | --- | --- |
| M2-1 | Live market feed | §3.3 — contract rates are the requirement; a feed is the opposite of one |
| M2-2 | Fiat wallets follow billing currency | §6.1 — M1 leaves wallets on the denomination currency, where they already work |
| M2-3 | Custom currency generalizes onto the shared denomination | §4 — touches finalization for live tenants; highest risk, no user waiting |
| M2-4 | Marketplace usage conversion | Same rate table, different consumer |

### 5.3 Explicitly not in scope, either milestone

| # | | Why |
| --- | --- | --- |
| N1 | Cross-currency **payments** | `payment_processor.go:739` sums payments and hard-errors on mismatch. Needs a two-amount payment model first |
| N2 | Changing a customer's billing currency after invoicing | §3.6 |
| N3 | `price_units` convergence | Different discipline — immutable, snapshot-at-authoring, bound 1:1 to a price's currency |

---

## 6. Blast radius

Currency equality is load-bearing across the money path, enforced by exact string match —
`IsMatchingCurrency` is `strings.EqualFold`. Today it holds for free because everything
inherits one currency from the subscription. Billing currency breaks that, in five places.

| Path | Today | What breaks |
| --- | --- | --- |
| **Price → subscription** | Prices not matching the subscription currency are silently dropped (`subscription.go:3785, 3829, 4003`) | Unchanged mechanically, but the rule now needs restating: the filter is against *charge* currency, not billing currency |
| **Wallet → invoice** | Credits apply in the **denomination** currency, not the invoice currency (`credit_adjustment.go:221-223`) | **Nothing breaks.** §6.1 |
| **Payment → invoice** | Rejects on mismatch (`payment_processor.go:641`), sums raw amounts (`:739`) | Payments are in billing currency, consistently. Cross-currency payment stays out of scope (§5.3 N1) |
| **Credit notes** | Inherit invoice currency | Must inherit the *converted* currency and reuse the parent's **frozen** rate, never re-resolve |
| **Proration / plan change** | Built from `sub.Currency` | Must convert at the same point, at the same rate, as the parent invoice |

### 6.1 Wallets — nothing to do in M1

An earlier draft said wallets must move to the billing currency. That was wrong: the
mechanism already handles it.

Credits are selected and applied in the **denomination** currency, not the invoice currency:

```go
// credit_adjustment.go — Credits apply in the denomination currency, before any conversion.
// Match on the denomination currency so a fiat wallet never applies at a 1:1 rate.
wallets, err := walletPaymentService.GetWalletsForCreditAdjustment(ctx, inv.CustomerID, denominationCurrency)
```

`DenominationCurrency()` returns the custom currency when set, the invoice's currency
otherwise — so a MAC invoice draws only from MAC wallets, with a test asserting exactly that.
Under FX the same rule gives: invoice in INR, denomination USD, credits drawn from the USD
wallet, pre-conversion. **Which is what wallets already are.**

So M1 requires no wallet change and no migration. Existing wallets keep working because for
every existing customer denomination == invoice currency anyway (§5.0).

**The M2 question**, recorded as D1: should a *fiat* wallet instead follow billing currency,
so an INR-billed customer holds an INR balance rather than a USD one? That is a product
judgement about what a customer expects to see, not a constraint — and it is the one place
M1 does not deliver "one currency everywhere", since such a customer sees a USD balance
against INR invoices until M2.

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

### M1 — custom FX rates

Four steps, each independently shippable, ordered so risk arrives last.

| Step | | Risk |
| --- | --- | --- |
| **1** | `exchange_rates` table, resolution, admin API. Nothing consumes it | Low — additive, unreachable |
| **2** | `customers.billing_currency` — add, backfill, set on first invoice. **No conversion**: billing currency equals charge currency for everyone | Low — no behaviour change |
| **3** | **Conversion at invoice generation** | **High** — the real change |
| **4** | Integrations send the invoice's frozen rate instead of polling their own | Medium — touches the sync paths shipped in #2907 |

Steps 1 and 2 are deliberately inert and can go in parallel. After them the system is
identical to today, with the field populated and the table empty — so step 3 turns conversion
on against data already known to be correct (§5.0).

**Step 3 is where a tenant opts in**, by setting a billing currency that differs from a
subscription's charge currency. Until someone does that, step 3 changes nothing observable.

### M2

| Step | | Gated on |
| --- | --- | --- |
| **5** | Live market feed | A tenant who cannot answer "what rate should we use?" themselves |
| **6** | Fiat wallets follow billing currency | D1 |
| **7** | Custom currency generalizes onto the shared denomination | D2 — highest risk, no user waiting |
| **8** | Marketplace usage conversion | Already blocked in code; unblocked by M1's rate table |

---

## 8. Success criteria

| # | |
| --- | --- |
| S1 | A customer with USD and EUR subscriptions and billing currency INR receives **every** invoice in INR |
| S2 | The rate used is on the invoice and retrievable |
| S3 | Line items sum exactly to the invoice total — no residual drift |
| S4 | Changing a tenant rate does not alter any finalized invoice |
| S5 | No configured rate → invoice generation fails, naming the pair. No invoice is created |
| S6 | A customer whose billing currency equals the charge currency follows an unchanged code path |
| S7 | Zoho and QuickBooks send the invoice's recorded rate, not one polled from the provider |
| S8 | Enabling customer or subscription scoped rates requires no migration |

---

## 9. Decisions

### 9.1 Closed

| # | Decision | Resolution |
| --- | --- | --- |
| **D3** | Which market feed | **Deferred to M2.** M1 resolves from configuration only. §3.3 |
| **D4** | Rate frozen at finalization or draft creation | **Finalization**, matching custom currency. Two freeze points would be confusing, and a draft that tracks the rate is the more useful preview |
| **D6** | In-flight drafts when a rate changes | **Drafts re-resolve, finalized never move.** Follows D4 |
| **D7** | Do wallets need migrating for M1 | **No.** Credits already apply in the denomination currency. §6.1 |

### 9.2 Open — needed before M1 ships

| # | Decision | Recommendation |
| --- | --- | --- |
| **D5** | Does the customer-facing invoice show the source amount? | Recommend yes — *"$100.00 converted at 83.00"*. It is the only place the contract denomination survives, and the first thing a customer will ask |
| **D8** | Which rate belongs on a statutory document | **Unresolved, and the one that could change the model.** §9.4 |

### 9.3 Open — needed before M2

| # | Decision | Recommendation |
| --- | --- | --- |
| **D1** | Should a fiat wallet follow billing currency? | Probably yes — an INR-billed customer expects an INR balance. But it moves credit application to post-conversion, so it needs to be deliberate. §6.1 |
| **D2** | Generalize the custom-currency denomination, or keep a parallel one | Generalize. §4. Highest risk, no user waiting, so last |

### 9.4 D8 — the one that could change the model

The moment we convert, we own a number that lands in someone's statutory filing. For an
Indian entity, the INR value on a GST return may require a **prescribed reference rate**,
not the rate sales negotiated. Those are different numbers, and *"our billing system
decided"* is not an answer an auditor accepts.

If a tenant's finance team confirms a reference rate is required, the model needs **two
rates per invoice** — one commercial, one statutory — rather than one. That is a schema
consequence, not a configuration one, which is why it belongs before M1 rather than after.

**How to close it:** one conversation with the finance contact at a tenant who would use
this. Not a question the codebase can answer.

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
