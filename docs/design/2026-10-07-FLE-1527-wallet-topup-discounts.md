# Discounts on Wallet Top-ups — PRD + ERD

- **Author:** Ojas Aggarwal
- **Date:** 2026-10-07
- **Linear:** [FLE-1527](https://linear.app/flexprice/issue/FLE-1527) (supersedes the FLE-1452 coupon PRD, PR #3004)
- **Status:** Ready for review

---

# Part 1 — PRD

## Problem

Tenants want to sell prepaid credits at a discount, e.g. "buy $500 of credits for $450". The end customer gets the full credits, but the tenant's books should record revenue at what was actually paid — $0.90 per $1 of credit consumed, not $1.

## Example

Wallet purchase and usage rates both 0.5 ($1 = 2 credits).

| Step | What happens |
|---|---|
| Top-up | 1,000 credits ($500 face) with a 10% coupon |
| Invoice | $500, coupon −$50, tax on $450 |
| Credits granted | 1,000 — after the invoice is paid |
| Usage worth $500 | 1,000 credits debited; the end customer sees $500 used |
| Revenue on the books | **$450** |

## How it works

**Credits are equal for usage, not for value.** Every top-up is a separate batch of credits. The wallet records where each batch came from (its invoice) and which batches each debit drew from. Valuing those credits — revenue — is the job of a separate revenue service, not the wallet.

**Revenue = cash paid for the credits used**, never the dollar value of the usage:

> cost per credit = amount paid before tax ÷ credits granted
> revenue of a debit = credits deducted × cost per credit

The usage rate only decides *how many* credits a debit takes; it never affects revenue. When purchase and usage rates are equal (the default) this is simply usage $ × (1 − discount). When they differ, the rates themselves act as a discount or markup, and revenue still totals the cash received.

| Wallet | Pay | Credits | Usage they cover | Revenue when fully used |
|---|---|---|---|---|
| purchase 0.5 / usage 0.5, 10% coupon | $450 | 1,000 | $500 | $450 |
| purchase 0.5 / usage 1, 10% coupon | $450 | 1,000 | $1,000 | $450 |
| purchase 0.5 / usage 1, no coupon | $500 | 1,000 | $1,000 | $500 |

| Grant | Cost per credit comes from | Revenue |
|---|---|---|
| **Purchased** (invoiced) | Its top-up invoice: amount after coupons, before tax ÷ credits | Credits used × cost per credit |
| **Purchased** (direct, no invoice) | Purchase rate on the transaction | Credits used × cost per credit |
| **Free** (free credits, subscription grants, bonus) | 0 | None |
| **Refund to wallet** (invoice void, or credit note refunded to the wallet) | The refunded invoice | Derived by the revenue service from that invoice |

## What this release delivers

- Discounted top-ups via coupons, end to end (API + dashboard).
- The ledger facts a revenue service needs: each batch's source invoice, and which batches each debit drew from.
- **Not** a revenue figure. "$450 on the books" needs the revenue service (Open Point 2). Set customer expectations accordingly.

## Rules

1. **Discounts come in as coupons**, entered by `coupon_code`, using the existing coupon model. Percentage and fixed coupons both work.
2. **Only invoiced purchases** (`PURCHASED_CREDIT_INVOICED`) take coupons. Direct purchases carry no discount.
3. **Credits are granted when the invoice is paid.** Tenants with `auto_complete_purchased_credit_transaction` on (off by default) get credits before payment, so they cannot use coupons on top-ups.
4. **Coupons are one-time, per top-up.** Each coupon applies once to that top-up's invoice.
5. **Multiple coupons** apply in order on the running subtotal, before tax. The API accepts a list; the dashboard sends one, as it does for subscriptions.
6. **Credits are never reduced** by a coupon; only the invoice is.
7. **Bonus credit slabs** are based on credits bought, not the discounted amount.
8. **One redemption per coupon per top-up**, counted when the invoice is created. It is not given back if the invoice is later voided.
9. **Coupon cadence is ignored** (it only applies to subscriptions). `max_redemptions` applies. There is no per-customer limit — tenants wanting one-use-per-customer issue customer-specific coupons with `max_redemptions = 1`.
10. **A 100% coupon** makes a $0 invoice; the credits are granted immediately.
11. **Any invalid coupon rejects the whole top-up.** Nothing is created.
12. **The end customer always sees face value**: wallet balance, usage, invoices paid by credits.
13. **Consumption order is unchanged** (priority → expiry → size).
14. **Conversion rates are never changed** by this feature.

## API

`POST /v1/wallets/{id}/top-up`

```json
{
  "credits_to_add": "1000",
  "transaction_reason": "PURCHASED_CREDIT_INVOICED",
  "coupons": [{ "coupon_code": "TOPUP10" }, { "coupon_code": "LOYAL5" }]
}
```

Response unchanged (`wallet_transaction`, `invoice_id`, `wallet`). The invoice shows `total_discount` and its `coupon_applications`.

| Case | Result |
|---|---|
| `coupons` with any reason other than `PURCHASED_CREDIT_INVOICED` | 400 |
| `coupons` together with `checkout` | 400 |
| `coupons` while the tenant has auto-complete on | 400 |
| Same code twice in one request | 400 |
| Unknown or unpublished code | 404 |
| Outside `redeem_after` / `redeem_before` | 400 |
| `max_redemptions` reached | 400 |
| Fixed-amount coupon in a currency different from the wallet's (percentage coupons work in any currency) | 400 |

## UI — Add Credits modal

Reuses the subscription-create coupon components as-is. [Mockup](https://claude.ai/artifact/6bGcPduhdEUpKWrVkxumg2)

- **Purchased** shows a **Discounts** section (`SubscriptionDiscountTable`): an Add button opens the **Link Coupon** dialog (`CouponModal`), and the chosen coupon shows as one row (name, discount, type, currency, remove). One coupon.
- Coupons are filtered with `filterValidCoupons` (redeem window, max redemptions, fixed-coupon currency). The label drops the cadence suffix ("10% off", not "10% off forever"), since cadence does not apply to top-ups.
- No invoice preview, as on subscription create.
- **Skip invoice is removed** for purchased credits. The API still accepts `PURCHASED_CREDIT_DIRECT`.
- **Checkout link** (Razorpay) is disabled while a coupon is selected, with a hint, because the API rejects coupons with checkout.
- Switching to **Free** clears and hides the coupon.

## Out of scope (v1)

- **Coupons on auto top-up** (v2). Needs a standing coupon attachment to a wallet: extend `coupon_association` with `entity_type` / `entity_id` and make `subscription_id` optional.
- **Coupons with pay-first checkout** — rejected with 400 so they are never silently dropped.
- End customers entering coupon codes in the customer portal.
- Discounts on direct (non-invoiced) purchases.
- The revenue service itself — designed separately.
- Accounting integrations (none sync wallet transactions today).

---

# Part 2 — ERD

## What already exists

- **Coupons:** three tables. `coupon` (the discount definition), `coupon_association` (a coupon attached to a subscription — not used here), `coupon_application` (the discount actually given on an invoice, with a frozen `coupon_snapshot`). One-off invoices already apply coupons through `ApplyCouponsToInvoice` during compute: in order, before tax, idempotent per (invoice, coupon), counting one redemption each. Top-ups reuse this path unchanged.
- **Top-up invoice:** `PURCHASED_CREDIT_INVOICED` creates a ONE_OFF invoice via `CreateOneOffInvoice` (or `CreateComputedDraftInvoice` for pay-first checkout). It passes no coupons today.
- **Debits:** every debit goes through `processWalletOperation`. `ConsumeCredits` works out how many credits to take from each batch, then discards the numbers; the debit row keeps only batch IDs (`metadata.consumed_credit_tx_ids`).
- **Conversion rates** are immutable in the schema.

## Principle

`wallet_transaction` records facts — where credits came from and where they went. It stores no cost or revenue. A revenue service derives value from these facts.

## Schema — `wallet_transaction`

| Column | On | Meaning |
|---|---|---|
| `source_type` | credit and debit rows | The document the transaction is linked to. v1 value: `INVOICE`. Null when there is none. |
| `source_id` | credit and debit rows | That invoice's ID: the top-up invoice for a purchase, the refunded invoice for a refund, the invoice the credits were applied to for a debit. |
| `consumption_breakdown` (jsonb) | debit rows | Which batches the debit drew from, and how many credits from each. |

- All nullable. `consumption_breakdown` is `Immutable()`. `source_*` on a purchase is written when it is completed, so set-once is enforced in code (written only while null).
- Index `(tenant_id, environment_id, source_type, source_id)`.
- `source_*` is separate from `reference_*` (what triggered the transaction), which is unchanged. The pair is generic, so it can later point at an invoice line item without a schema change.
- No backfill. Old invoiced purchases can be backfilled from `invoice.metadata.wallet_transaction_id` if ever needed.

### How a revenue service finds each batch's cost

| Batch | Lookup |
|---|---|
| Invoiced purchase | `source_type = INVOICE` → invoice and its `coupon_applications` |
| Bonus | `source_type = INVOICE` → the purchase's invoice; `parent_transaction_id` → the purchase |
| Direct purchase | `topup_conversion_rate` on the row |
| Free / subscription grant | `transaction_reason` → 0 |
| Refund to wallet | `source_type = INVOICE` → the refunded invoice: what was paid on it and which batches its debits drew from |

The revenue service should record each batch's cost once, when the credits are granted, rather than re-reading the invoice later — invoices can change after payment.

## Top-up flow

1. `TopUpWalletRequest` gets `coupons: [{ coupon_code }]`. Request validation rejects coupons with a non-invoiced reason, with `checkout`, or with duplicate codes.
2. `handlePurchasedCreditInvoicedTransaction` rejects coupons when auto-complete applies, then resolves each code (`CouponRepo.GetByCode`) and validates it with the existing `ValidateCoupon(coupon, nil)` (published, dates, redemptions), plus the wallet's currency for fixed-amount coupons. Any failure rejects the top-up.
3. `handlePurchasedCreditInvoicedTransaction` passes the coupons on a new server-only field `CreateInvoiceRequest.PreparedInvoiceCoupons` (`json:"-"`). `CreateOneOffInvoice` appends it to `InvoiceCoupons` after its own validation, so pre-validated coupons are never silently dropped and public callers cannot bypass validation.
4. The existing compute applies the coupons, writes `coupon_applications`, counts redemptions and taxes the net amount. The purchase row is written as today (pending, before the invoice), in the same DB transaction.
5. **On completion** set `source_type = INVOICE`, `source_id = invoice ID`, in the same write that flips the status to completed. The pending row is created before its invoice and the link today is one-way (`invoice.metadata.wallet_transaction_id`), so `CompletePurchasedCreditTransactionWithRetry` gains an `invoiceID` parameter. Every caller already holds the invoice: `payment_processor.go` (gateway payment), the two payment-status updates in `invoice.go`, and the $0 path below. The bonus row completed with it gets the same source. Auto-complete top-ups take no coupons and are left as they are: no source.
6. **$0 invoice (100% coupon):** `FinalizeInvoice` marks a $0 invoice paid without calling the payment hook, which would leave the credits pending forever. `handlePurchasedCreditInvoicedTransaction` sees the invoice already paid and calls `CompletePurchasedCreditTransactionWithRetry` right after its DB transaction commits. Not from inside `FinalizeInvoice`: that runs inside the top-up's open transaction, so the completion webhook would fire before commit.

## Refund flow

`refund.go` settles a refund to the wallet with `TopUpWallet` (`INVOICE_VOID_REFUND`, or `CREDIT_NOTE` for a credit-note refund). The refund row already holds the invoice ID; pass it so the new credit gets `source_type = INVOICE`, `source_id` = the refunded invoice. These credits are completed at creation.

## Debit flow

`ConsumeCredits` returns how many credits it took from each batch. `processWalletOperation` stores it on the debit row:

```json
{
  "amount": "350",
  "credit_amount": "700",
  "consumption_breakdown": [
    { "credit_transaction_id": "wtx_A", "credits": "200" },
    { "credit_transaction_id": "wtx_B", "credits": "400" },
    { "credit_transaction_id": "wtx_C", "credits": "100" }
  ]
}
```

- Covers every debit type: usage payment, credit adjustment, expiry settlement, expiry, termination, manual.
- **Debits applied to an invoice** (`CREDIT_ADJUSTMENT`, including expiry settlement) also get `source_type = INVOICE`, `source_id` = that invoice. This duplicates `reference_id`, on purpose: the revenue service reads only `source_*`, on every row. Expiry, termination and manual debits have no invoice, so `source_*` stays null.
- `amount` and `credit_amount` are unchanged, so balances, invoices and payments are unaffected.
- Breakdown credits sum to `credit_amount`, except for a manual debit that overdraws: only the credits real batches covered are recorded.
- `metadata.consumed_credit_tx_ids` keeps being written for backward compatibility.
- Debits from before this change have no breakdown; the revenue service treats them as unattributed.

## Build plan

**Backend (`flexprice`)**

| Area | Change |
|---|---|
| `ent/schema/wallettransaction.go` | 3 fields; `make generate-ent` |
| `migrations/versioned/postgres/` | One file adding the columns; a separate `transaction:false` file for `CREATE INDEX CONCURRENTLY` |
| `internal/types/wallet.go` | `WalletTxSourceType` (`INVOICE`), breakdown entry type |
| `internal/domain/wallet/` | Transaction fields + `ToEnt`/`FromEnt`; `ConsumeCredits` signature |
| `internal/repository/ent/wallet.go` | `ConsumeCredits` returns per-batch credits; setters in `CreateTransaction`; `UpdateTransaction` writes `source_*` while null |
| `internal/testutil/inmemory_wallet_store.go` | Same; align its batch ordering and status filter with the ent repo |
| `internal/api/dto/wallet.go` | `coupons` on `TopUpWalletRequest` + validation |
| `internal/api/dto/invoice.go` | `PreparedInvoiceCoupons` (`json:"-"`) |
| `internal/ee/service/wallet.go` | Coupon resolution in `TopUpWallet`; pass coupons in `handlePurchasedCreditInvoicedTransaction`; `invoiceID` on `CompletePurchasedCreditTransactionWithRetry` and `source_*` on completion (purchase and bonus); reject coupons when auto-complete is on; complete $0 top-up invoices after commit; `source_*` accepted on top-up and debit requests (server-only); breakdown in `processWalletOperation` |
| `internal/ee/service/invoice.go` | Append `PreparedInvoiceCoupons` in `CreateOneOffInvoice`; pass the invoice ID to completion |
| `internal/ee/service/payment_processor.go` | Pass the invoice ID to completion |
| `internal/ee/service/refund.go` | Pass the refunded invoice as `source_*` |
| `internal/ee/service/credit_adjustment.go` | Pass the invoice as `source_*` on credit-adjustment debits |
| `internal/webhook/dto/wallet.go` | Add the new fields (copied by hand) |
| Swagger / SDKs | `make swagger`, regenerate |

**Frontend (`flexprice-front`)**

| Area | Change |
|---|---|
| `src/components/molecules/WalletTopupCard/` | Discounts section reusing `SubscriptionDiscountTable` + `CouponModal` (one coupon); remove Skip invoice; disable Checkout link while a coupon is selected; clear on Free |
| `src/utils/common/format_coupon_name.ts` | Option to omit the cadence suffix |
| `src/models/WalletTransaction.ts` | New fields |

## Acceptance criteria

**Coupons**
1. A 10% coupon on a 1,000-credit top-up (rate 0.5): invoice subtotal $500, discount $50, tax on $450; 1,000 credits granted on payment.
2. Two coupons both apply, in order, each with its own `coupon_applications` row and +1 redemption.
3. Each error case in the API table rejects the top-up and creates no transaction, invoice or redemption.
4. A 100% coupon produces a $0 invoice and the credits are granted immediately.
5. With auto-complete on, a top-up with coupons is rejected; one without coupons behaves as today.
6. Top-ups without coupons behave exactly as before.
7. Dashboard: one coupon via Link Coupon; no Skip invoice for purchased credits; Checkout link disabled while a coupon is selected; switching to Free clears the coupon.

**Ledger**

8. `source_type = INVOICE` with the right `source_id` on: completed invoiced purchases and their bonus (pay-later, $0), refunds to the wallet, and credit-adjustment debits. Null on auto-complete purchases and on expiry, termination and manual debits.
9. Every debit type writes a `consumption_breakdown`; its credits sum to `credit_amount`; batch IDs match the batches whose `credits_available` went down.
10. Balances, invoice totals and payments are unchanged versus today for the same operations.

---

# Open Points

| # | Question | Status |
|---|---|---|
| 1 | **Bonus credits:** $0 vs sharing the purchase price (ASC 606). | Revenue service decides; wallet already links bonus → purchase. |
| 2 | **Revenue service:** where revenue is computed and exposed (`revenue_facts` is a natural home); records batch cost at grant time; handles invoices that change after payment and tax-inclusive line items. | Separate design. Blocks "revenue on the books" for the customer. |
| 3 | **Coupon redemption on void / abandoned invoice** is not given back. | Follow-up after v1. |
