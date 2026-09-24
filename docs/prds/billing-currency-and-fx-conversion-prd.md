# Billing Currency & FX Conversion — PRD

- **Ticket:** TBD
- **Date:** 2026-09-23
- **Status:** Proposed
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
| **Scope** | Which entity an FX rate is attached to: tenant+environment, customer or subscription. Every rate is environment-scoped regardless of level (§3.2.1) |
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

One customer, one currency, on **every** invoice — regardless of how many subscriptions they
hold, what those subscriptions are priced in, or what created the invoice. Subscriptions are
not the only thing that issues one (§3.8).

### 2.4 The rate is resolved, recorded, and reused

A rate is resolved from the first of these that answers:

```
subscription  →  customer  →  tenant+env  →  hard fail          [Phase 1]
subscription  →  customer  →  tenant+env  →  feed  →  hard fail  [Phase 2]
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
| R3 | Scopes: `tenant`, `customer`, `subscription` — each within a tenant **and environment**. Resolution is most-specific-first (§3.2.1) |
| R4 | Direction is explicit — an INR→USD conversion needs an INR→USD row, never `1/rate` (§3.5) |
| R5 | `effective_from` and optional `effective_to`. Overlapping windows for the same pair and scope are rejected at write |
| R6 | Editing a rate never alters a conversion already recorded |
| R7 | A rate row is **fixed** (carries a value) or **dynamic** (names a source to call) |

### 3.2.1 Environment scoping applies at every level

Every rate carries `tenant_id` **and** `environment_id`, like every other entity in the
product. That is orthogonal to `entity_type`, not an alternative to it:

```
tenant_id + environment_id     ← always, on every row, at every level
        └── entity_type        ← tenant | customer | subscription
```

So the broadest scope is **tenant + environment**, not tenant. A rate configured in
`production` does not apply in `staging`, and a customer-scoped rate applies only to that
customer within the environment its row lives in.

Two consequences worth stating, because both are easy to assume wrong:

- **There is no cross-environment rate.** A tenant running production and staging configures
  each separately. This is the same rule the rest of the product follows, and it is what
  makes a staging misconfiguration unable to affect production billing.
- **A "tenant-level" rate means "everything in this environment".** It is the default that
  applies when no customer or subscription rate is set — not a rate that spans the tenant's
  environments.

### 3.3 Live market feed — **Phase 2**

Deferred. **Phase 1 resolves rates from configuration only.** With no configured rate, invoice
generation fails (§3.7) rather than reaching for a market rate.

Recorded here so Phase 1's schema does not foreclose it:

| # | Requirement |
| --- | --- |
| F1 | A tenant-level setting names the feed provider and its credentials |
| F2 | Rates are fetched and **cached with a TTL**, never called per invoice |
| F3 | A feed-sourced rate is recorded with `source = feed` and the fetch timestamp |
| F4 | Feed unavailable **and** no configured rate → fail loudly. Never fall back to `1` |

The `rate_mode = dynamic` column and the `feed` source value ship in Phase 1 unused, so enabling
this is a validation change rather than a migration on a populated table.

#### Why skipping it costs nothing today

**No fiat-to-fiat conversion exists in the product.** Custom currency converts a
tenant-invented unit into fiat; nothing converts USD into INR. Every fiat invoice is issued in
the currency its subscription was priced in, because there is no way to express anything else.

So deferring the feed defers nothing that exists. It is not a capability being removed from
anyone — it is a capability nobody has, and Phase 1 still doesn't give them a *worse* one:

| | Today | Phase 1 | Phase 2 |
| --- | --- | --- | --- |
| Customer invoiced in subscription currency | ✅ | ✅ unchanged | ✅ |
| Customer invoiced in a different currency | ❌ impossible | ✅ with a configured rate | ✅ |
| …with no rate configured | ❌ | Invoice generation fails | ✅ feed rate |

The third row is the only gap, and it describes a tenant who wants to bill in a currency they
have no rate for. **That tenant does not exist yet.** The ones asking for this have a rate —
it is in the contract.

#### And a feed is the wrong tool for the requirement

The requirement driving this project is **contract rates** — "customer A signed at ₹83." A
market feed is the opposite of that: a rate that moves. Maxio reaches the same conclusion for
the same reason:

> *"you may choose to define a specific exchange rate, which will ensure that as
> subscriptions renew each month, the price they're charged doesn't change each billing
> cycle based on the market rate."*

§6.3 works one plan through both models side by side: a fixed rate holds the customer's bill
constant and lets the tenant's realisation move, a market rate does the reverse. Which is
correct depends on the tenant's unit of account, and for the tenants asking for this it is fixed.

A feed also puts a third-party network dependency on the invoice-generation path, where a
slow or unavailable provider becomes a billing outage. And the industry runs slower than
instinct suggests: Microsoft uses **monthly** benchmark rates; Zuora's OANDA integration is
1–2 days behind by design. Nobody needs intraday rates to bill monthly.

**What would pull it into Phase 1:** a tenant who wants a customer billed in a currency they
cannot name a rate for — typically self-serve signups across many currencies, where nobody is
negotiating anything. That is a different product motion from the enterprise contracts this was
built for, and no one has asked for it. The tenants who need this today *can* name a rate: Vaani
bills Indian clients in USD against INR-priced subscriptions (§5.0), which is a commercial
arrangement with a rate attached, not a market lookup.

### 3.4 Conversion at invoice generation

| # | Requirement |
| --- | --- |
| I1 | The invoice is created in the customer's billing currency |
| I2 | Line items, discounts and adjustments are converted at one rate |
| I3 | Converted line items **must sum exactly** to the converted invoice total — the rounding residual is absorbed, not dropped (ERD §5.3) |
| I4 | The rate is live while the invoice is a draft, and **frozen at finalization** |
| I5 | The rate, the source currency and the source amounts are stored on the invoice |
| I6 | A finalized invoice's amounts never move, whatever happens to the rate afterwards |
| I7 | A converted invoice **shows the source amount and the rate** to the customer — *"$100.00 converted at 83.00"* |

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
> reintroduce it, in Phase 1 or Phase 2.
>
> With no resolvable rate, invoice generation **fails**. A blocked invoice is recoverable; a
> wrong one, already sent and already in someone's ledger, is not.

### 3.8 Every invoice, whatever created it

Three code paths create invoices, and only one is subscription-driven. All three are in scope:

| # | Path | Currency today | Under this design |
| --- | --- | --- | --- |
| G1 | Subscription billing | Subscription's charge currency | Source currency; converted to billing currency |
| G2 | Wallet top-up | The wallet's own currency (`wallet.go:1164`, `InvoiceType: ONE_OFF`) | Source currency; converted to billing currency |
| G3 | One-off invoice API | Supplied by the caller, `validate:"required"` | Source currency; converted to billing currency |

G2 is the one that would otherwise slip through. A customer billed in INR who tops up a USD
wallet receives a USD invoice today, because the top-up path reads the wallet and never
consults the subscription — and would not consult `billing_currency` either unless made to.

**The wallet keeps its own currency.** Only the invoice converts:

```
wallet USD, top up $100        →  invoice ₹8,300, denomination USD $100
                                  wallet still holds $100 of credits
```

This is the same shape as a converted subscription invoice, which is why it needs no separate
mechanism: the source denomination is USD, the document is INR, and §6.1's credit matching
keeps working because credits still apply in the denomination currency.

G3 matters because an explicit currency on the API would otherwise be a hole through which any
caller could issue an invoice that violates the invariant. A supplied currency is a **source**,
not an override.

| # | Requirement |
| --- | --- |
| G4 | A top-up invoice is issued in the customer's billing currency; the wallet's currency is unchanged |
| G5 | A one-off invoice's supplied currency is treated as the source currency, never as an override |
| G6 | Every path records the rate and source amounts on the invoice, identically (§3.4 I5) |

---

## 4. Relationship to custom currency

Custom currency already converts a source unit into an invoice currency, freezes the rate at
finalization, and keeps the source amounts. FX needs the same three things. Whether that makes
it the *template* for FX or merely its closest neighbour is a design question, and this section
exists to frame it — not to answer it.

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
| Scope | tenant + environment | tenant+env → customer → subscription |
| Converted at | invoice generation | invoice generation |
| Frozen at | finalization | finalization |
| Stored as | `custom_currency` + line item copies | the same shape |

**Only the first four rows differ, and two of those are the feature itself.** That closeness is
what makes D2 a real question rather than an obvious yes or no.

### 4.1 Converge, or build alongside — decided in the ERD

The two mechanisms are close enough that building FX its own denomination is not obviously
right, and generalizing the existing one is not obviously right either. **This PRD does not
decide it.** It is a structural question about storage and the finalization path, which is the
ERD's subject, and it is recorded as D2.

What the ERD has to weigh:

**For converging.** One denomination object — *"the currency this invoice was converted from,
and at what rate"* — with custom currency and FX as two producers of it. The alternative is two
mechanisms that must agree forever on rounding, freezing, and what a recompute starts from, on
invoice finalization. The tax engine is the cautionary example in this codebase:
`tax_associations.priority` is stored, validated and copied but nothing sorts by it, and
`EntityHierarchy` is declared but called nowhere, because the real cascade was written a second
time elsewhere. Parallel mechanisms drift, and a divergence here is a mis-billed invoice rather
than a cosmetic inconsistency.

**For building alongside.** Cheaper, and genuinely lower risk in the short term. Converging
means a migration on a live JSON column, on the finalization path, for tenants already running
custom currency — and no user is waiting for it. The source units also differ in kind: a
tenant-invented unit is not money, and a mechanism that treats `MAC` and `USD` identically may
be eliding a distinction that matters later.

**What is not in question:** Phase 1 does not touch custom currency either way. FX can write the
same shape alongside it while the two remain separate producers, so this decision can be taken
with the conversion path already running in production rather than ahead of it.

### 4.2 What billing currency unlocks for custom currency, either way

The `MAC → INR` case becomes reachable, and this does not depend on how D2 goes.

Today `fiat_conversion_factors` can hold an `inr` factor that is **unreachable**, because
invoice currency is pinned to `default_fiat_currency` tenant-wide. With a per-customer billing
currency, a MAC-priced subscription for an INR customer converts directly — one hop, using the
factor that already exists.

That configuration was written for this and has been dead since. A per-customer billing currency
is what makes it live; convergence only decides whether the two paths share code getting there.

---

## 5. Scope

### 5.0 What production actually looks like

An audit of all 182,674 customers, every tenant and environment, September 2026.

The load-bearing fact is narrower than "everything is single-currency", and it is exactly true:

> **No fiat-to-fiat conversion exists in the product.** Custom currency converts a
> tenant-invented unit (`MAC`) into fiat. Nothing converts USD into INR. Every fiat invoice is
> issued in the currency its subscription was priced in.

Currency does vary per customer, in five ways — 248 customers, 0.14%. None of them is a defect:

| | Customers | What it turns out to be |
| --- | --- | --- |
| Invoices span >1 currency | 123 | Largely drafts in a second currency that never finalize |
| Subscriptions span >1 currency | 20 | All dormant, or already matching the invoiced currency |
| Wallets span >1 currency | 55 | **Designed.** `WalletConfig.AllowedPriceTypes` supports several wallets per customer; there is no unique constraint on `(customer_id, currency)` |
| Invoice ccy ≠ subscription ccy | 17 | 15 are custom currency (`MAC → USD`) working correctly; 2 are test rows with no payments |
| Invoice ccy ≠ wallet ccy | 42 | A wallet whose currency does not match is simply never selected (§6.1) |

Two of those are features, not anomalies, which is why the invariant has to be stated as
fiat-to-fiat rather than as universal alignment.

#### The backfill rule

Restricted to **finalized** invoices — a draft's currency is not a commitment — every customer
with real payment history has one defensible answer:

```
billing_currency :=
  1. the currency of the customer's finalized, paid invoices   if exactly one
  2. the currency of the customer's finalized invoices         if exactly one
  3. NULL                                                      otherwise
```

Rule 3 is the safety valve. C5 already makes NULL correct: the invoice takes the charge
currency and sets it, converting nothing. Anything genuinely ambiguous keeps today's behaviour
until a human decides.

#### The six that do not satisfy the invariant

A customer breaks at conversion only if **all** of: the backfill assigned a currency, they have
a **live** subscription, its currency differs, and it is real fiat rather than a custom unit.
Six production customers, across three tenants, meet all four:

| Tenant | Customers | Rate needed |
| --- | --- | --- |
| Vaani | 4 | `INR → USD` |
| DRIZZ Automation | 1 | `USD → INR` |
| Flexprice (own test data) | 1 | `USD → EUR` |

Out of 7,200 production customers with finalized invoices. The other 7,194 satisfy it by
construction: 6,903 have a live subscription already in the assigned currency, 280 have no live
subscription at all, and 11 backfill to NULL.

> [!IMPORTANT]
> These six are **work to do before step 3**, not a footnote. Configure the two real pairs, or
> set those customers to NULL — otherwise their next invoice resolves no rate and §3.7 fails it.
> Worth re-running the audit immediately before enabling conversion rather than trusting this
> list: a tenant creates a seventh by giving a USD-billed customer a EUR subscription.

#### Vaani is the demand case, not an edge case

Four of the six are one tenant, and they are identical: **billed and paid in USD, live
subscriptions in INR.** Their INR subscriptions generate invoices — 18 and 23 of them on two
customers — and every one dies as a draft, while the money arrives through separate USD
invoices.

That is a tenant hand-working around the absence of this feature. It is the strongest evidence
in this document that the feature is needed, and it raises the stakes on shipping it correctly:
today those INR invoices sit harmlessly as drafts, whereas after conversion without a
configured rate they fail outright.

### 5.1 Phase 1 — custom FX rates

| # | |
| --- | --- |
| P1-1 | `customers.billing_currency`, null by default, set on first invoice, settable via API |
| P1-2 | `exchange_rates` — scoped, dated, **fixed rates only** |
| P1-3 | Resolution: subscription → customer → tenant+env → **fail** |
| P1-4 | Conversion at invoice generation into the billing currency |
| P1-5 | Rate frozen at finalization and recorded on the invoice |
| P1-6 | Integrations send the invoice's recorded rate instead of polling their own |
| P1-7 | An audit record of every conversion, queryable across invoices |
| P1-8 | Backfill follows the rule in §5.0, assigning NULL wherever the answer is ambiguous |
| P1-9 | Rates configured for the customers in §5.0 before conversion is enabled |
| P1-10 | All three invoice-generation paths convert — subscription, wallet top-up, one-off API (§3.8) |
| P1-11 | Converted invoices display the source amount and rate (I7) |
| P1-12 | Rate management API — CRUD, plus **resolve**: which rate applies to a customer, and from which scope. ERD §3.7 |

### 5.2 Phase 2 — the rest

| # | | Why deferred |
| --- | --- | --- |
| P2-1 | Live market feed | §3.3 — contract rates are the requirement; a feed is the opposite of one |
| P2-2 | Fiat wallets follow billing currency | §6.1 — Phase 1 leaves wallets on the denomination currency, where they already work |
| P2-3 | Custom currency and FX converge on one denomination — **if the ERD decides to** | §4.1 — touches finalization for live tenants; highest risk, no user waiting |
| P2-4 | Marketplace usage conversion | Same rate table, different consumer |

### 5.3 Explicitly not in scope, either phase

| # | | Why |
| --- | --- | --- |
| N1 | Cross-currency **payments** | `payment_processor.go:739` sums payments and hard-errors on mismatch. Needs a two-amount payment model first |
| N2 | Changing a customer's billing currency after invoicing | §3.6 |
| N3 | `price_units` convergence | Different discipline — immutable, snapshot-at-authoring, bound 1:1 to a price's currency |

---

## 6. Blast radius

Currency equality is load-bearing across the money path, enforced by exact string match —
`IsMatchingCurrency` is `strings.EqualFold`. Today it holds for free because every fiat invoice
inherits its currency from the subscription. Billing currency breaks that, in five places.

| Path | Today | What breaks |
| --- | --- | --- |
| **Price → subscription** | Prices not matching the subscription currency are silently dropped (`subscription.go:3785, 3829, 4003`) | Unchanged mechanically, but the rule now needs restating: the filter is against *charge* currency, not billing currency |
| **Wallet → invoice** | Credits apply in the **denomination** currency, not the invoice currency (`credit_adjustment.go:221-223`) | **Nothing breaks.** §6.1 |
| **Payment → invoice** | Rejects on mismatch (`payment_processor.go:641`), sums raw amounts (`:739`) | Payments are in billing currency, consistently. Cross-currency payment stays out of scope (§5.3 N1) |
| **Credit notes** | Inherit invoice currency | Must inherit the *converted* currency and reuse the parent's **frozen** rate, never re-resolve |
| **Proration / plan change** | Built from `sub.Currency` | Must convert at the same point, at the same rate, as the parent invoice |
| **Wallet top-up → invoice** | Invoice takes the wallet's currency (`wallet.go:1164`) | Must convert to billing currency while the wallet keeps its own (§3.8 G2) |

### 6.1 Wallets — nothing to do in Phase 1

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

So Phase 1 requires no wallet change and no migration. Existing wallets keep working, because
denomination is the pre-conversion source currency — the same currency the wallet is already in.

**The Phase 2 question**, recorded as D1: should a *fiat* wallet instead follow billing currency,
so an INR-billed customer holds an INR balance rather than a USD one? It is the one place Phase 1
does not deliver "one currency everywhere" — such a customer sees a USD balance against INR
invoices, and their INR wallet, if they have one, is never selected for those invoices.

This is no longer only a product judgement. **55 customers already fund prepaid wallets in more
than one currency deliberately** (§5.0), which is what `WalletConfig.AllowedPriceTypes` exists
for. Customers want per-currency balances before we have asked them. D1 should be decided with
that in evidence rather than from first principles.

### 6.2 What "converted exactly once" must mean

Three conversions could plausibly apply to one invoice:

```
credits     ──▶ currency        (wallet topup_conversion_rate — unit arithmetic)
MAC charges ──▶ fiat            (custom currency)
USD charges ──▶ INR             (FX, billing currency)
INR invoice ──▶ INR ledger      (integration exchange_rate — metadata, not a conversion)
```

Only the third is a conversion in the sense this document means. The rule: **an invoice is
converted once, into the billing currency, and the source denomination is what every
recomputation starts from.**

The other three are not conversions and must not be counted as one:

- `topup_conversion_rate` turns wallet credits into a currency amount. That is unit arithmetic
  *within* the wallet, the same kind custom currency's `fiat_conversion_factors` does — it
  produces the source amount, which the single FX hop then converts.
- The integration `exchange_rate` is ledger metadata and never rescales anything.

A MAC-priced subscription for an INR customer is `MAC → INR`, one hop. Not `MAC → USD → INR`.
A top-up of a USD wallet for an INR customer is `credits → USD` then `USD → INR`: one unit
step, one conversion.

### 6.3 Fixed rates vs market rates — who carries the movement

A worked "FX loss" is easy to get wrong, so take one plan through both models.

**A plan priced at $100, a customer with billing currency INR, a tenant rate of 95**, and three
months in which the market moves.

#### Fixed rate — what Phase 1 does

| Month | Market | Invoice issued | Customer pays | Tenant receives | Worth in USD |
| --- | --- | --- | --- | --- | --- |
| 1 | 95 | ₹9,500 | ₹9,500 | ₹9,500 | $100.00 |
| 2 | 100 | ₹9,500 | ₹9,500 | ₹9,500 | $95.00 |
| 3 | 90 | ₹9,500 | ₹9,500 | ₹9,500 | $105.56 |

The customer's bill never moves, and every invoice settles in full. **There is no loss anywhere
in this table.** What varies is the USD *equivalent* of a constant rupee amount, which matters
only to someone keeping score in USD.

#### Market rate — what Phase 2 would do

| Month | Market | Invoice issued | Customer pays | Tenant receives | Worth in USD |
| --- | --- | --- | --- | --- | --- |
| 1 | 95 | ₹9,500 | ₹9,500 | ₹9,500 | $100.00 |
| 2 | 100 | ₹10,000 | ₹10,000 | ₹10,000 | $100.00 |
| 3 | 90 | ₹9,000 | ₹9,000 | ₹9,000 | $100.00 |

The tenant realises exactly $100 every month, and the customer's bill swings ₹1,000 on a plan
they never changed.

#### The trade

```
          Customer's ₹ bill        Tenant's $ realisation
Fixed     constant ₹9,500          varies  $95.00 – $105.56
Market    varies ₹9,000 – 10,000   constant $100.00
```

**Neither model removes variance. They choose who receives the certainty** — fixed gives it to
the customer, market gives it to the tenant. Conversion does not create or destroy currency
risk; it moves it across the invoice.

So "which is right" is really "whose books is the variance harmless in":

| Tenant's unit of account | Right model | Why |
| --- | --- | --- |
| **INR** — prices in USD by convention, costs and books in INR | **Fixed** | ₹9,500 every month is exactly what they want. Market rates would make their *revenue* bounce for no reason |
| **USD** — real USD costs sit behind the $100 price | **Market** | Fixed leaves them short in month 2, against costs that did not move |

This is the substance behind §3.3's decision to ship fixed rates first: the tenants asking for
this negotiated a number with their customer, and both sides wanted it to hold. Vaani is the
second shape though — plans priced in INR, invoices issued in USD — so they price ₹8,300,
collect $100, and carry the difference until it settles.

#### The hazard that is real for fixed rates

Not a per-invoice loss — **silent drift.** Set 95 when the market is 95 and it is aligned.
Nobody re-reads the configuration, and two years later the market is 110 while invoices still go
out at ₹9,500, now $86 of value against a $100 plan. A 14% gap that no invoice ever reported,
because every one of them was paid in full.

That argues for **reviewing** rates — an alert when a configured rate drifts from market beyond
a threshold — more directly than it argues for converting at market. It is a cheaper feature
than the Phase 2 feed and recovers most of the protection.

**Prepaid credits hold the position longest.** A top-up fixes the rate at purchase for service
consumed weeks or months later, so the configured rate has more time to drift than on a usage
invoice settled within terms.

None of this is a defect and none of it blocks Phase 1. It is recorded because **nothing in the
product measures it** — there is no view of open position by currency and no signal when a rate
has drifted. A tenant should carry this knowingly.

---

## 7. Sequencing

### Phase 1 — custom FX rates

Four steps, each independently shippable, ordered so risk arrives last.

| Step | | Risk |
| --- | --- | --- |
| **1** | `exchange_rates` table, resolution, admin API. Nothing consumes it | Low — additive, unreachable |
| **2** | `customers.billing_currency` — add, backfill, set on first invoice. **No conversion**: billing currency equals charge currency for everyone | Low — no behaviour change |
| **3** | **Conversion at invoice generation.** Preceded by P1-9 — the §5.0 rates configured | **High** — the real change |
| **4** | Integrations send the invoice's frozen rate instead of polling their own | Medium — touches the sync paths shipped in #2907 |

Steps 1 and 2 are deliberately inert and can go in parallel. After them the system is identical
to today, with the field populated and the table empty.

**Step 3 is where a tenant opts in**, by setting a billing currency that differs from a
subscription's charge currency. For 7,194 of 7,200 production customers it changes nothing
observable — but for six it changes everything, so the two rate pairs in §5.0 are configured
first.

### Phase 2

| Step | | Gated on |
| --- | --- | --- |
| **5** | Live market feed | A tenant who cannot answer "what rate should we use?" themselves |
| **6** | Fiat wallets follow billing currency | D1 |
| **7** | Custom currency and FX converge on one denomination, if the ERD decides to | D2, and step 3 having run in production — highest risk, no user waiting (§4.1) |
| **8** | Marketplace usage conversion | Already blocked in code; unblocked by Phase 1's rate table |

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
| S9 | An INR-billed customer topping up a USD wallet receives an **INR** invoice, and the wallet still holds USD credits |
| S10 | A one-off invoice posted in USD for an INR-billed customer is issued in INR |
| S11 | Every converted invoice renders the source amount and the rate used |

---

## 9. Decisions

### 9.1 Closed

| # | Decision | Resolution |
| --- | --- | --- |
| **D3** | Which market feed | **Deferred to Phase 2.** Phase 1 resolves from configuration only. §3.3 |
| **D4** | Rate frozen at finalization or draft creation | **Finalization**, matching custom currency. Two freeze points would be confusing, and a draft that tracks the rate is the more useful preview |
| **D5** | Does the customer-facing invoice show the source amount | **Yes, required.** On a top-up especially: an invoice reading "₹8,300" with no "$100 of credits" leaves the customer unable to tell what they bought. §3.4 I7 |
| **D6** | In-flight drafts when a rate changes | **Drafts re-resolve, finalized never move.** Follows D4 |
| **D7** | Do wallets need migrating for Phase 1 | **No.** Credits already apply in the denomination currency. §6.1 |
| **D9** | How does the backfill choose a billing currency | **Finalized-and-paid, then finalized, then NULL.** A draft's currency is not a commitment. §5.0 |
| **D10** | Do non-subscription invoices convert | **Yes — all three paths.** The wallet keeps its currency; only the invoice converts. §3.8 |

### 9.2 Open — needed before Phase 1 ships

| # | Decision | Recommendation |
| --- | --- | --- |
| **D8** | Which rate belongs on a statutory document | **Unresolved, and the one that could change the model.** §9.4 |

### 9.3 Open — needed before Phase 2

| # | Decision | Recommendation |
| --- | --- | --- |
| **D1** | Should a fiat wallet follow billing currency? | **Yes, probably** — and 55 customers already fund multi-currency wallets by choice (§6.1), so this is evidenced rather than speculative. It moves credit application to post-conversion, so it still needs to be deliberate |
| **D2** | Generalize the custom-currency denomination, or build FX alongside it | **Deferred to the ERD** — a storage and finalization-path question, not a product one. §4.1 lays out both sides. Phase 1 is unaffected either way |

### 9.4 D8 — the one that could change the model

The moment we convert, we own a number that lands in someone's statutory filing. For an
Indian entity, the INR value on a GST return may require a **prescribed reference rate**,
not the rate sales negotiated. Those are different numbers, and *"our billing system
decided"* is not an answer an auditor accepts.

If a tenant's finance team confirms a reference rate is required, the model needs **two
rates per invoice** — one commercial, one statutory — rather than one. That is a schema
consequence, not a configuration one, which is why it belongs before Phase 1 rather than after.

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
